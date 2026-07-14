package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

// fetchAttempts / fetchDelay bound the post-release verification retry: a freshly
// pushed tag can take a short while to propagate to proxy.golang.org.
// Vars, not consts, so the hermetic tests can exercise the retry loop.
var (
	fetchAttempts = 6
	fetchDelay    = 15 * time.Second
)

// modulePath is keel's import path — the target of the post-release anonymous
// fetch verification.
const modulePath = "github.com/david-aggeler/keel"

// semverTag matches a strict vMAJOR.MINOR.PATCH tag, optionally with a
// pre-release suffix (e.g. v0.1.0, v1.2.3-rc.1). Build metadata is not accepted
// as a Go module version.
var semverTag = regexp.MustCompile(`^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?$`)

// validateVersion rejects anything that is not a well-formed vX.Y.Z(-pre) tag.
func validateVersion(version string) error {
	if !semverTag.MatchString(version) {
		return fmt.Errorf("invalid version %q: expected a semver tag like v0.1.0", version)
	}
	return nil
}

// runRelease cuts a release in one invocation:
//
//	preflight (clean tree + green `keel-dev ci` + green `keel-dev vsix ci`) ->
//	stamp vsix/package.json from the tag and COMMIT the stamp -> build the VSIX
//	asset -> annotated tag (whose tree now carries the stamped version) ->
//	`gh release create` -> anonymous go-get verification from a clean cache.
//
// It refuses before creating any tag if the tree is dirty or the gate is red,
// so a broken tag-on-red state cannot happen.
//
// DHF-REQ: keel/requirement-9, keel/requirement-25
func runRelease(ctx context.Context, logger *slog.Logger, dir string, version string) error {
	logger.Info("release "+version, "banner", "section", "name", "release "+version)

	if err := validateVersion(version); err != nil {
		return err
	}

	// --- Preflight: refuse before mutating anything. ---
	if err := ensureCleanTree(ctx, logger, dir); err != nil {
		return fmt.Errorf("release preflight: %w", err)
	}
	if err := ensureTagAbsent(ctx, logger, dir, version); err != nil {
		return fmt.Errorf("release preflight: %w", err)
	}
	if err := runCI(ctx, logger, dir); err != nil {
		return fmt.Errorf("release preflight: %w", err)
	}
	if err := runVSIXGate(ctx, logger, dir); err != nil {
		return fmt.Errorf("release preflight: %w", err)
	}
	logger.Info("preflight green", "version", version)

	// --- Stamp + commit the VSIX version, then build the asset. ---
	// The stamp is committed BEFORE the tag so the tag's tree carries the same
	// vsix/package.json version as the release asset (one-version invariant);
	// tagging a dirty stamp would publish an asset the tagged source disagrees
	// with.
	if err := stampVSIXPackageVersion(filepath.Join(dir, "vsix", "package.json"), strings.TrimPrefix(version, "v")); err != nil {
		return fmt.Errorf("stamp vsix version: %w", err)
	}
	if err := commitVSIXStamp(ctx, logger, dir, version); err != nil {
		return err
	}
	asset, err := buildVSIXReleaseAsset(ctx, logger, dir, version)
	if err != nil {
		return err
	}

	// --- Tag. ---
	if err := runCmd(ctx, logger, dir, "git", "tag", "-a", version, "-m", "keel "+version); err != nil {
		return fmt.Errorf("create annotated tag: %w", err)
	}
	if err := runCmd(ctx, logger, dir, "git", "push", "origin", version); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}
	// Push the branch too: the tag push uploads the stamp commit's objects, but
	// without this origin/main never advances to it and every later checkout
	// forks from a pre-stamp base.
	if err := runCmd(ctx, logger, dir, "git", "push", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push release commit: %w", err)
	}
	logger.Info("tag and release commit pushed", "version", version)

	// --- GitHub release. ---
	if err := runCmd(ctx, logger, dir, "gh", "release", "create", version, "--title", "keel "+version, "--generate-notes", asset); err != nil {
		return fmt.Errorf("gh release create: %w", err)
	}
	logger.Info("github release created", "version", version)

	// --- Post-release anonymous fetch verification. ---
	if err := runVerify(ctx, logger, version); err != nil {
		return err
	}
	logger.Info("release complete", "module", modulePath, "version", version)
	return nil
}

// commitVSIXStamp commits the stamped vsix/package.json so the annotated tag
// records the same version the release asset carries. A no-op when the
// committed file already carries the target version.
//
// DHF-REQ: keel/requirement-40
func commitVSIXStamp(ctx context.Context, logger *slog.Logger, dir, version string) error {
	rel := filepath.Join("vsix", "package.json")
	out, _, err := capture(ctx, logger, dir, "git", "status", "--porcelain", "--", rel)
	if err != nil {
		return fmt.Errorf("git status %s: %w", rel, err)
	}
	if strings.TrimSpace(out) == "" {
		logger.Info("vsix version already committed", "version", version)
		return nil
	}
	if err := runCmd(ctx, logger, dir, "git", "add", "--", rel); err != nil {
		return fmt.Errorf("stage vsix version stamp: %w", err)
	}
	if err := runCmd(ctx, logger, dir, "git", "commit", "-m", "keel "+version+": stamp VSIX version"); err != nil {
		return fmt.Errorf("commit vsix version stamp: %w", err)
	}
	logger.Info("vsix version stamp committed", "version", version)
	return nil
}

// buildVSIXReleaseAsset packages the (already stamped and committed) extension
// and returns the asset path.
//
// DHF-REQ: keel/requirement-40
func buildVSIXReleaseAsset(ctx context.Context, logger *slog.Logger, dir, version string) (string, error) {
	vsixDir := filepath.Join(dir, "vsix")
	if err := runStep(ctx, logger, dir, step{
		name:    "vsix:package",
		program: "pnpm",
		args:    []string{"--dir", vsixDir, "run", "package:vsix"},
	}); err != nil {
		return "", err
	}
	asset := filepath.Join(dir, "bin", "keel-test-bridge-"+strings.TrimPrefix(version, "v")+".vsix")
	if _, err := os.Stat(asset); err != nil {
		return "", fmt.Errorf("vsix package asset %s: %w", asset, err)
	}
	return asset, nil
}

// vsixVersionLine matches the manifest's top-level version line. Dependency
// maps key by package name and engines by tool name, so a line-anchored
// "version" key exists only at the top level of package.json.
var vsixVersionLine = regexp.MustCompile(`(?m)^(\s*)"version"\s*:\s*"[^"]*"`)

// stampVSIXPackageVersion rewrites only the version line, preserving the
// hand-maintained manifest's key order and formatting — the stamp is committed
// to history, so it must read as a one-line version bump, not a rewrite.
func stampVSIXPackageVersion(path, version string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m := vsixVersionLine.FindSubmatchIndex(body)
	if m == nil {
		return fmt.Errorf("no version field in %s", path)
	}
	var buf bytes.Buffer
	buf.Write(body[:m[0]])
	buf.Write(body[m[2]:m[3]]) // original indentation
	buf.WriteString(`"version": "` + version + `"`)
	buf.Write(body[m[1]:])
	if !json.Valid(buf.Bytes()) {
		return fmt.Errorf("version stamp broke %s", path)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// runVerify validates the version and confirms the tag resolves anonymously,
// retrying to absorb proxy.golang.org propagation lag. It is the tag-triggered
// CI entrypoint (`keel-dev verify vX.Y.Z`) and the release verb's final step.
//
// DHF-REQ: keel/requirement-9, keel/requirement-10
func runVerify(ctx context.Context, logger *slog.Logger, version string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= fetchAttempts; attempt++ {
		lastErr = verifyAnonymousFetch(ctx, logger, version)
		if lastErr == nil {
			logger.Info("release verified: anonymous go get resolves", "module", modulePath, "version", version)
			return nil
		}
		logger.Warn("anonymous fetch not yet resolvable; retrying",
			"attempt", attempt, "of", fetchAttempts, "error", lastErr.Error())
		if attempt < fetchAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(fetchDelay):
			}
		}
	}
	return fmt.Errorf("anonymous-fetch verification: %w", lastErr)
}

// ensureCleanTree fails unless `git status --porcelain` is empty.
func ensureCleanTree(ctx context.Context, logger *slog.Logger, dir string) error {
	out, _, err := capture(ctx, logger, dir, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("working tree is dirty; commit or stash before releasing:\n%s", strings.TrimSpace(out))
	}
	return nil
}

// ensureTagAbsent fails if the version tag already exists locally, so a release
// never silently reuses or clobbers an existing tag.
func ensureTagAbsent(ctx context.Context, logger *slog.Logger, dir, version string) error {
	out, _, err := capture(ctx, logger, dir, "git", "tag", "--list", version)
	if err != nil {
		return fmt.Errorf("git tag --list: %w", err)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("tag %s already exists", version)
	}
	return nil
}

// verifyAnonymousFetch confirms `go get <module>@<version>` resolves through the
// default toolchain from a clean module cache with no private-access config: it
// builds a throwaway module in a temp dir, points GOMODCACHE and HOME at fresh
// empty directories, disables GOAUTH/netrc, and scrubs private module settings
// so only anonymous public resolution can succeed.
//
// DHF-REQ: keel/requirement-8, keel/requirement-9
func verifyAnonymousFetch(ctx context.Context, logger *slog.Logger, version string) error {
	tmp, err := os.MkdirTemp("", "keel-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	work := filepath.Join(tmp, "mod")
	cache := filepath.Join(tmp, "cache")
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	gomod := "module keelverify\n\ngo 1.25\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}

	// A clean, anonymous-only environment: keep PATH/HOME-independent Go behavior
	// but forbid every private-access escape hatch and isolate the module cache.
	env := append(os.Environ(),
		"GOMODCACHE="+cache,
		"HOME="+home,
		"NETRC=/dev/null",
		"GOAUTH=off",
		"GOFLAGS=",
		"GOPRIVATE=",
		"GONOSUMCHECK=",
		"GONOSUMDB=",
		"GOINSECURE=",
		"GIT_CONFIG_GLOBAL=/dev/null", // ignore any credential helper / insteadOf
	)

	logger.Info("verifying anonymous fetch from a clean module cache", "module", modulePath, "version", version)
	_, stderr, err := captureEnv(ctx, logger, work, env, "go", "get", modulePath+"@"+version)
	if err != nil {
		return fmt.Errorf("go get %s@%s: %w: %s", modulePath, version, err, strings.TrimSpace(stderr))
	}
	return nil
}

// runCmd launches a subprocess through keel/exec, surfacing its output live as
// keel/log records (keel/ac-35) and logging the START/END lifecycle. Returns an
// error on a non-zero exit.
func runCmd(ctx context.Context, logger *slog.Logger, dir, program string, args ...string) error {
	lines := newLineLogWriter(logger, program, "stdout")
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: program,
		Args:    args,
		Dir:     dir,
		Stdout:  lines,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	res, waitErr := proc.Wait()
	lines.Flush()
	if waitErr != nil {
		return waitErr
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s %s exited %d", program, strings.Join(args, " "), res.ExitCode)
	}
	return nil
}

// capture runs a subprocess through keel/exec and returns its stdout/stderr,
// still emitting lifecycle logs through keel/log.
func capture(ctx context.Context, logger *slog.Logger, dir, program string, args ...string) (string, string, error) {
	return captureEnv(ctx, logger, dir, nil, program, args...)
}

func captureWithMaxOutput(ctx context.Context, logger *slog.Logger, dir string, maxOutputBytes int, program string, args ...string) (string, string, error) {
	return captureEnvWithMaxOutput(ctx, logger, dir, nil, maxOutputBytes, program, args...)
}

// captureEnv is capture with an explicit environment (nil inherits the parent's).
func captureEnv(ctx context.Context, logger *slog.Logger, dir string, env []string, program string, args ...string) (string, string, error) {
	return captureEnvWithMaxOutput(ctx, logger, dir, env, 0, program, args...)
}

func captureEnvWithMaxOutput(ctx context.Context, logger *slog.Logger, dir string, env []string, maxOutputBytes int, program string, args ...string) (string, string, error) {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program:        program,
		Args:           args,
		Dir:            dir,
		Env:            env,
		Logger:         logger,
		MaxOutputBytes: maxOutputBytes,
	})
	if err != nil {
		return "", "", err
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return res.Stdout, res.Stderr, waitErr
	}
	if res.ExitCode != 0 {
		return res.Stdout, res.Stderr, fmt.Errorf("%s exited %d", program, res.ExitCode)
	}
	return res.Stdout, res.Stderr, nil
}
