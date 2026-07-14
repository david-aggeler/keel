package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
)

// The expected-red manifest is the single source of known-red tests (CR-74):
// every entry names the failing test id, the requirement it specifies, and the
// CR whose merge deletes the entry. The ci gate prints the count and entries on
// every run so the debt is visible, never silent, and refuses an invalid file.
//
// DHF-REQ: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
type expectedRedManifest struct {
	Version int                `json:"version"`
	Entries []expectedRedEntry `json:"entries"`
}

type expectedRedEntry struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
	FixingCR    string `json:"fixing_cr"`
	Reason      string `json:"reason"`
}

var (
	expectedRedRequirementRef = regexp.MustCompile(`^keel/requirement-[0-9]+$`)
	expectedRedFixingCRRef    = regexp.MustCompile(`^keel/change_request-[0-9]+$`)
)

// loadExpectedRedManifest reads and validates an expected-red manifest file.
func loadExpectedRedManifest(path string) ([]expectedRedEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read expected-red manifest: %w", err)
	}
	var manifest expectedRedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse expected-red manifest %s: %w", path, err)
	}
	return validateExpectedRedManifest(manifest)
}

// validateExpectedRedManifest checks the manifest invariants: version 1, no
// blank fields, unique ids, and record-ref shapes for requirement + fixing CR.
// An empty entry list is the target state and is valid.
func validateExpectedRedManifest(manifest expectedRedManifest) ([]expectedRedEntry, error) {
	if manifest.Version != 1 {
		return nil, fmt.Errorf("expected-red manifest version = %d, want 1", manifest.Version)
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.ID == "" || entry.Requirement == "" || entry.FixingCR == "" || entry.Reason == "" {
			return nil, fmt.Errorf("expected-red entry has blank field: %+v", entry)
		}
		if seen[entry.ID] {
			return nil, fmt.Errorf("duplicate expected-red entry id %q", entry.ID)
		}
		seen[entry.ID] = true
		if !expectedRedRequirementRef.MatchString(entry.Requirement) {
			return nil, fmt.Errorf("expected-red entry %q requirement = %q, want keel/requirement-N", entry.ID, entry.Requirement)
		}
		if !expectedRedFixingCRRef.MatchString(entry.FixingCR) {
			return nil, fmt.Errorf("expected-red entry %q fixing_cr = %q, want keel/change_request-N", entry.ID, entry.FixingCR)
		}
	}
	return manifest.Entries, nil
}

// logExpectedRed prints the expected-red debt through keel/log. Called by the
// ci gate on every run (CR-74 visibility decision: count + entries, never silent).
// A missing manifest is the debt-free state (count 0) — same as an empty one —
// so hermetic test modules without a testdata/ tree still gate green; an
// invalid manifest stays a hard failure.
func logExpectedRed(logger *slog.Logger, dir string) error {
	path := filepath.Join(dir, "testdata", "red"+"list.json")
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		logger.Info("expected-red manifest", "count", 0, "manifest", "absent")
		return nil
	}
	entries, err := loadExpectedRedManifest(path)
	if err != nil {
		return err
	}
	logger.Info("expected-red manifest", "count", len(entries))
	for _, entry := range entries {
		logger.Info("expected-red entry", "id", entry.ID, "requirement", entry.Requirement, "fixing_cr", entry.FixingCR, "reason", entry.Reason)
	}
	return nil
}
