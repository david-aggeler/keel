package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// versionFileRelPath and vsixManifestRelPath are the two committed carriers of
// the one-version invariant ("One tag. One version across the Go module and
// VSIX"). They are written slash-separated because they are quoted verbatim in
// the failure text, which must read the same on every host (keel/ac-570).
const (
	versionFileRelPath  = "VERSION"
	vsixManifestRelPath = "vsix/package.json"
)

// runVersionParity is the authoring-time defence of the one-version invariant:
// it compares the root VERSION file with the version field of the VSIX manifest
// and refuses the checkout when they differ.
//
// Two other mechanisms already guard the invariant, and both sit downstream of
// the moment it breaks: `keel-dev release` stamps the manifest from VERSION,
// which holds only when the release verb runs, and the VSIX bridge's
// assertVersionMatch compares the manifest against the devtool at test runtime,
// three subsystems away from the commit that caused the skew. This stage closes
// the authoring-time hole between them (keel/issue-187); it replaces neither.
//
// The comparison is a pure function of two committed files — no Node, no VS
// Code, no network — so it is hermetic and belongs early in the battery, ahead
// of the stages that cost minutes.
//
// DHF-REQ: keel/requirement-141 (keel/ac-569, keel/ac-570, keel/ac-572)
func runVersionParity(_ context.Context, _ *slog.Logger, dir string) error {
	moduleVersion, moduleErr := readVersionFile(filepath.Join(dir, versionFileRelPath))
	manifestVersion, manifestErr := readVSIXManifestVersion(filepath.Join(dir, filepath.FromSlash(vsixManifestRelPath)))

	moduleAbsent := errors.Is(moduleErr, os.ErrNotExist)
	manifestAbsent := errors.Is(manifestErr, os.ErrNotExist)
	// A tree carrying neither file is not a keel checkout and has nothing to
	// gate. The stage stays present and green rather than disappearing, so the
	// declared command surface is checkout-independent (keel/ac-544).
	if moduleAbsent && manifestAbsent {
		return nil
	}
	// Exactly one carrier present is the sharpest form of the skew the stage
	// exists to catch, so it is reported the same way: both paths, both states.
	if moduleAbsent || manifestAbsent {
		return fmt.Errorf("version parity: %s and %s must both declare the version, but one is missing: %s = %s, %s = %s",
			versionFileRelPath, vsixManifestRelPath,
			versionFileRelPath, describeVersionRead(moduleVersion, moduleErr),
			vsixManifestRelPath, describeVersionRead(manifestVersion, manifestErr))
	}
	if moduleErr != nil {
		return fmt.Errorf("version parity: read %s: %w", versionFileRelPath, moduleErr)
	}
	if manifestErr != nil {
		return fmt.Errorf("version parity: read %s: %w", vsixManifestRelPath, manifestErr)
	}
	if moduleVersion != manifestVersion {
		return fmt.Errorf("version parity: %s and %s disagree: %s = %q, %s = %q; keel ships one version across the Go module and the VSIX — stamp the manifest from %s (`keel-dev release` does this) or correct the file that is wrong",
			versionFileRelPath, vsixManifestRelPath,
			versionFileRelPath, moduleVersion,
			vsixManifestRelPath, manifestVersion,
			versionFileRelPath)
	}
	return nil
}

// describeVersionRead renders one carrier's outcome for the missing-file
// report: the version it declared, or why it declared none.
func describeVersionRead(version string, err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "absent"
	case err != nil:
		return "unreadable (" + err.Error() + ")"
	default:
		return fmt.Sprintf("%q", version)
	}
}

// readVersionFile reads the root version-of-record. VERSION holds a bare semver
// and nothing else (keel/requirement-110), so the whole trimmed body is the
// version.
func readVersionFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", errors.New("file is empty; it must hold the bare semver")
	}
	return version, nil
}

// readVSIXManifestVersion reads the manifest's top-level version field. The
// manifest is parsed as JSON rather than pattern-matched: this stage only reads,
// so it has no reason to preserve the hand-maintained formatting the release
// verb's stamping step goes out of its way to keep.
func readVSIXManifestVersion(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		return "", errors.New("manifest declares no top-level version field")
	}
	return version, nil
}
