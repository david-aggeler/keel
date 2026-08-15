package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	procexec "github.com/david-aggeler/keel/exec"
)

// toolCacheEnv overrides the gate-tool cache root. It exists for environments
// where the default user cache dir is unusable (CI images, read-only homes);
// there is deliberately no switch that turns the cache OFF, because resolving
// gate tools from PATH is the defect (keel/issue-142), not a fallback.
const toolCacheEnv = "KEEL_DEV_TOOL_CACHE"

// Install methods declared by a tool pin. `go` materializes the pinned binary
// into the version-keyed cache with `go install <package>@<version>`; `path`
// declares the tool as host-global (keel does not install it) and keeps PATH
// resolution for it.
const (
	toolInstallGo   = "go"
	toolInstallPath = "path"
)

// toolInstall says how one pinned gate tool is materialized. It is what gives
// the pin declaration and the binary it verifies the same scope: a `go` pin is
// installed per pinned version, so two worktrees declaring different versions
// resolve different binaries on one host (keel/ac-465).
type toolInstall struct {
	method  string
	pkg     string
	version string
}

// cacheKey is the version segment of the tool's cache path. `go` pins key on
// the installed module version, which is exact even for tools whose binary
// cannot report a version (deadcode, gitleaks).
func (i toolInstall) cacheKey() string {
	return i.version
}

// toolCacheRoot returns the version-keyed gate-tool cache root:
// $KEEL_DEV_TOOL_CACHE when set (must be absolute), else
// <user cache dir>/keel-dev/tools.
func toolCacheRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv(toolCacheEnv)); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("keel-dev: %s must be an absolute path, got %q", toolCacheEnv, override)
		}
		return filepath.Clean(override), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("keel-dev: locating the gate-tool cache root: %w (set %s to choose one)", err, toolCacheEnv)
	}
	return filepath.Join(base, "keel-dev", "tools"), nil
}

// toolResolver resolves the configured pins to concrete binaries, installing
// version-keyed cache entries on demand and verifying every resolved binary
// before it is handed to a gate step. One resolver serves a whole gate run, so
// each tool is installed and probed at most once.
type toolResolver struct {
	pins map[string]toolPin
	mu   sync.Mutex
	// resolved memoizes verified binary paths by tool name.
	resolved map[string]string
}

func newToolResolver(pins map[string]toolPin) *toolResolver {
	return &toolResolver{pins: pins, resolved: map[string]string{}}
}

// resolve returns the verified absolute path of the pinned tool. A tool that is
// absent, un-installable, or at the wrong version is a hard error naming it —
// never a silent skip and never a fallback to whatever PATH happens to hold
// (keel/ac-42, keel/ac-465).
func (r *toolResolver) resolve(ctx context.Context, logger *slog.Logger, name string) (string, error) {
	pin, ok := r.pins[name]
	if !ok {
		return "", fmt.Errorf("keel-dev: no version pin registered for gate tool %q", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if path, ok := r.resolved[name]; ok {
		return path, nil
	}

	path, err := locateToolPin(ctx, logger, pin)
	if err != nil {
		return "", err
	}
	if err := verifyResolvedToolPin(ctx, logger, pin, path); err != nil {
		return "", err
	}
	r.resolved[name] = path
	return path, nil
}

// verifyPins resolves and verifies the named pins, accumulating failures so ONE
// gate run enumerates every drifted pin. Stopping at the first is what made
// keel/issue-142 cost three full gate runs to diagnose three drifted tools.
// Only the tools this run will actually use are named: a repo with no shell
// scripts must not be asked for shellcheck.
func (r *toolResolver) verifyPins(ctx context.Context, logger *slog.Logger, names []string) error {
	names = append([]string(nil), names...)
	sort.Strings(names)

	var failures []string
	for _, name := range names {
		if _, err := r.resolve(ctx, logger, name); err != nil {
			failures = append(failures, "  - "+err.Error())
		}
	}
	if len(failures) == 0 {
		logger.Debug("gate tool pins verified", "tools", len(names))
		return nil
	}
	return fmt.Errorf("keel-dev: %d of %d gate tool pins are not satisfied:\n%s",
		len(failures), len(names), strings.Join(failures, "\n"))
}

// locateToolPin returns the binary the pin's declared install method resolves
// to, installing it into the version-keyed cache when it is not there yet.
func locateToolPin(ctx context.Context, logger *slog.Logger, pin toolPin) (string, error) {
	switch pin.install.method {
	case toolInstallPath:
		path, err := exec.LookPath(pin.name)
		if err != nil {
			return "", fmt.Errorf("keel-dev: required gate tool %q not found on PATH (want %s); install it via scripts/setup_user.sh", pin.name, pin.wantDesc())
		}
		return path, nil
	case toolInstallGo:
		return resolveGoToolFromCache(ctx, logger, pin)
	default:
		return "", fmt.Errorf("keel-dev: gate tool %q declares no install method; set tools.pins[].install.method in %s", pin.name, keelDevConfigFile)
	}
}

// resolveGoToolFromCache returns <cache>/<tool>/<version>/<tool>, installing it
// with `go install <package>@<version>` when that entry is missing or unusable.
func resolveGoToolFromCache(ctx context.Context, logger *slog.Logger, pin toolPin) (string, error) {
	root, err := toolCacheRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, pin.name, pin.install.cacheKey())
	binary := filepath.Join(dir, pin.name)
	if isExecutableFile(binary) {
		logger.Debug("gate tool resolved from cache", "tool", pin.name, "version", pin.install.version, "path", binary)
		return binary, nil
	}
	if err := installGoToolIntoCache(ctx, logger, pin, dir, binary); err != nil {
		return "", err
	}
	return binary, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// installGoToolIntoCache installs the pinned package into a scratch GOBIN and
// atomically renames the built binary into its keyed cache path. The rename is
// what makes concurrent worktrees safe: a reader either sees no entry or a
// complete one, never a half-written binary. A failed install is a hard error
// naming the tool, the version, and the command — an install that quietly does
// nothing is indistinguishable from a check that passed (keel/ac-42).
func installGoToolIntoCache(ctx context.Context, logger *slog.Logger, pin toolPin, dir, binary string) error {
	spec := pin.install.pkg + "@" + pin.install.version
	command := "go install " + spec

	staging, err := os.MkdirTemp(filepath.Dir(dir), ".install-"+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		// The parent may not exist yet on a cold cache.
		if mkErr := os.MkdirAll(filepath.Dir(dir), 0o755); mkErr != nil {
			return fmt.Errorf("keel-dev: preparing the cache for gate tool %q at %s: %w", pin.name, pin.install.version, mkErr)
		}
		staging, err = os.MkdirTemp(filepath.Dir(dir), ".install-"+strconv.Itoa(os.Getpid())+"-")
		if err != nil {
			return fmt.Errorf("keel-dev: preparing the cache for gate tool %q at %s: %w", pin.name, pin.install.version, err)
		}
	}
	defer func() {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			logger.Warn("could not remove tool install staging dir", "tool", pin.name, "dir", staging, "error", rmErr.Error())
		}
	}()

	logger.Info("installing pinned gate tool", "tool", pin.name, "version", pin.install.version, "package", pin.install.pkg, "cache", dir)
	lines := newLineLogWriter(logger, "tool-install:"+pin.name, "stdout")
	proc, startErr := procexec.ProcessStart(ctx, procexec.Request{
		Program: "go",
		Args:    []string{"install", spec},
		// Installed from a neutral directory with the workspace disabled, so the
		// caller's module or go.work cannot change what lands in the cache.
		Dir:    os.TempDir(),
		Env:    goInstallEnv(staging),
		Stdout: lines,
		Logger: logger,
	})
	if startErr != nil {
		return fmt.Errorf("keel-dev: installing gate tool %q at version %s failed to start (%s): %w", pin.name, pin.install.version, command, startErr)
	}
	res, waitErr := proc.Wait()
	lines.Flush()
	if waitErr != nil || res.ExitCode != 0 {
		reason := fmt.Sprintf("exit %d", res.ExitCode)
		if waitErr != nil {
			reason = waitErr.Error()
		}
		return fmt.Errorf("keel-dev: installing gate tool %q at version %s failed (%s): %s\n%s",
			pin.name, pin.install.version, command, reason, strings.TrimSpace(res.Stdout+res.Stderr))
	}

	built := filepath.Join(staging, pin.name)
	if !isExecutableFile(built) {
		return fmt.Errorf("keel-dev: installing gate tool %q at version %s (%s) produced no %s binary in %s",
			pin.name, pin.install.version, command, pin.name, staging)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("keel-dev: creating the cache entry for gate tool %q at %s: %w", pin.name, pin.install.version, err)
	}
	// Rename over any existing entry: a concurrent worktree installing the same
	// pin is a race both sides win, because both wrote the same version.
	if err := os.Rename(built, binary); err != nil {
		return fmt.Errorf("keel-dev: publishing the cache entry for gate tool %q at %s: %w", pin.name, pin.install.version, err)
	}
	logger.Info("pinned gate tool installed", "tool", pin.name, "version", pin.install.version, "path", binary)
	return nil
}

// goInstallEnv returns the child environment for the install: the caller's
// environment with GOBIN pointed at the staging dir and the workspace disabled,
// so the installed binary depends only on the pin.
func goInstallEnv(gobin string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		if strings.HasPrefix(kv, "GOBIN=") || strings.HasPrefix(kv, "GOWORK=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GOBIN="+gobin, "GOWORK=off")
}
