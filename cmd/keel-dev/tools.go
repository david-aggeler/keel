package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	procexec "github.com/david-aggeler/keel/exec"
)

// toolPin is one external gate tool pinned to an exact version. The ci gate
// verifies presence AND version before it runs the tool, so a missing or
// drifted tool fails loud with a named remediation instead of silently
// skipping the check it was supposed to enforce.
//
// DHF-REQ: keel/requirement-12 (keel/ac-42)
type toolPin struct {
	name string // binary name, and the file name of its cache entry
	// versionArgs prints the tool's version (e.g. --version). Empty means the
	// tool exposes no stable version probe, so the pin is presence-only.
	versionArgs []string
	// want is the exact version substring that MUST appear in the probe output.
	// Empty pairs with an empty versionArgs for a presence-only pin.
	want string
	// install declares how this pin is materialized — a version-keyed cache
	// entry keel installs on demand, or an explicitly host-global PATH tool.
	// It is what gives the branch-committed pin and the binary it verifies the
	// same scope (keel/ac-465).
	install toolInstall
}

// verifyResolvedToolPin confirms that the binary the pin resolved to reports
// the pinned version, unless the pin is presence-only. Every failure is loud
// and names the tool plus the expected version — a gate tool that is absent or
// the wrong version must never be a silent skip.
//
// DHF-REQ: keel/requirement-12 (keel/ac-42)
func verifyResolvedToolPin(ctx context.Context, logger *slog.Logger, t toolPin, path string) error {
	if t.want == "" {
		logger.Debug("gate tool present (presence-only pin)", "tool", t.name, "path", path)
		return nil
	}

	out, err := probeVersion(ctx, logger, t, path)
	if err != nil {
		return fmt.Errorf("keel-dev: probing %q version at %s: %w", t.name, path, err)
	}
	if !strings.Contains(out, t.want) {
		return fmt.Errorf("keel-dev: gate tool %q version mismatch: want %s, %s (resolved at %s) reported:\n%s",
			t.name, t.want, strings.Join(append([]string{t.name}, t.versionArgs...), " "), path, strings.TrimSpace(out))
	}
	logger.Debug("gate tool pinned version verified", "tool", t.name, "want", t.want, "path", path)
	return nil
}

// wantDesc renders the pinned version for error messages, or "any version" for
// a presence-only pin.
func (t toolPin) wantDesc() string {
	if t.want == "" {
		return "any version"
	}
	return "version " + t.want
}

// probeVersion runs the resolved binary's version command through keel/exec and
// returns its combined stdout+stderr. The exit code is ignored: some tools print
// their version to stderr and/or exit non-zero, and the version substring match
// is the real signal. The probe runs the binary the gate will run — never a
// same-named one from PATH.
func probeVersion(ctx context.Context, logger *slog.Logger, t toolPin, path string) (string, error) {
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: path,
		Args:    t.versionArgs,
		Logger:  logger,
	})
	if err != nil {
		return "", err
	}
	res, waitErr := proc.Wait()
	if waitErr != nil {
		return "", waitErr
	}
	return res.Stdout + res.Stderr, nil
}
