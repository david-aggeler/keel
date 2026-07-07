package main

import (
	"context"
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
const (
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
//	preflight (clean tree + green `keel-dev ci`) -> annotated tag ->
//	`gh release create` -> anonymous go-get verification from a clean cache.
//
// It refuses before creating any tag if the tree is dirty or the gate is red,
// so a broken tag-on-red state cannot happen.
//
// DHF-REQ: keel/requirement-9
func runRelease(ctx context.Context, logger *slog.Logger, dir string, version string) error {
	logSection(logger, "keel-dev release "+version)

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
	logger.Info("preflight green", "version", version)

	// --- Tag. ---
	if err := runCmd(ctx, logger, dir, "git", "tag", "-a", version, "-m", "keel "+version); err != nil {
		return fmt.Errorf("create annotated tag: %w", err)
	}
	if err := runCmd(ctx, logger, dir, "git", "push", "origin", version); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}
	logger.Info("tag created and pushed", "version", version)

	// --- GitHub release. ---
	if err := runCmd(ctx, logger, dir, "gh", "release", "create", version, "--title", "keel "+version, "--generate-notes"); err != nil {
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
// builds a throwaway module in a temp dir, points GOMODCACHE at a fresh empty
// directory, and scrubs GOPRIVATE/GOINSECURE/GONOSUMCHECK/GONOSUMDB and any
// netrc so only anonymous public resolution can succeed.
//
// DHF-REQ: keel/requirement-9
func verifyAnonymousFetch(ctx context.Context, logger *slog.Logger, version string) error {
	tmp, err := os.MkdirTemp("", "keel-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	work := filepath.Join(tmp, "mod")
	cache := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(work, 0o755); err != nil {
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

// runCmd launches a subprocess through keel/exec, mirroring its output to the
// terminal and logging the START/END lifecycle through keel/log. Returns an
// error on a non-zero exit.
func runCmd(ctx context.Context, logger *slog.Logger, dir, program string, args ...string) error {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: program,
		Args:    args,
		Dir:     dir,
		Stdout:  os.Stdout,
		Logger:  logger,
	})
	if err != nil {
		return err
	}
	res, waitErr := proc.Wait()
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

// captureEnv is capture with an explicit environment (nil inherits the parent's).
func captureEnv(ctx context.Context, logger *slog.Logger, dir string, env []string, program string, args ...string) (string, string, error) {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: program,
		Args:    args,
		Dir:     dir,
		Env:     env,
		Logger:  logger,
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
