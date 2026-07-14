package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

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

// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestKnownRedManifestIsExplicitAndVisible(t *testing.T) {
	manifest := readExpectedRedManifest(t, filepath.Join("..", "..", "testdata", "red"+"list.json"))
	entries := validateExpectedRedManifest(t, manifest)
	t.Logf("EXPECTED RED manifest contains %d entries", len(entries))
	for _, entry := range entries {
		t.Logf("EXPECTED RED %s: %s fixed by %s - %s", entry.ID, entry.Requirement, entry.FixingCR, entry.Reason)
	}
}

// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestKnownRedManifestIsShrinkOnly(t *testing.T) {
	current := validateExpectedRedManifest(t, readExpectedRedManifest(t, filepath.Join("..", "..", "testdata", "red"+"list.json")))
	baseline := validateExpectedRedManifest(t, readExpectedRedManifest(t, filepath.Join("..", "..", "testdata", "red"+"list.baseline.json")))

	authorized := map[string]expectedRedEntry{}
	for _, entry := range baseline {
		authorized[entry.ID] = entry
	}
	for _, entry := range current {
		baselineEntry, ok := authorized[entry.ID]
		if !ok {
			t.Fatalf("expected-red entry %q is not in the authorized baseline; new known-red entries need a new approved record", entry.ID)
		}
		if entry.Requirement != baselineEntry.Requirement {
			t.Fatalf("expected-red entry %q requirement changed from %q to %q", entry.ID, baselineEntry.Requirement, entry.Requirement)
		}
		if entry.FixingCR != baselineEntry.FixingCR {
			t.Fatalf("expected-red entry %q fixing_cr changed from %q to %q", entry.ID, baselineEntry.FixingCR, entry.FixingCR)
		}
	}
}

// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestKnownRedManifestAllowsEmptyTargetState(t *testing.T) {
	entries := validateExpectedRedManifest(t, expectedRedManifest{Version: 1, Entries: nil})
	if len(entries) != 0 {
		t.Fatalf("empty target manifest returned %d entries, want 0", len(entries))
	}
}

func readExpectedRedManifest(t *testing.T, name string) expectedRedManifest {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read expected-red manifest: %v", err)
	}
	var manifest expectedRedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse expected-red manifest: %v", err)
	}
	return manifest
}

func validateExpectedRedManifest(t *testing.T, manifest expectedRedManifest) []expectedRedEntry {
	t.Helper()
	if manifest.Version != 1 {
		t.Fatalf("expected-red manifest version = %d, want 1", manifest.Version)
	}
	requirementRef := regexp.MustCompile(`^keel/requirement-[0-9]+$`)
	changeRequestRef := regexp.MustCompile(`^keel/change_request-[0-9]+$`)
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		if entry.ID == "" || entry.Requirement == "" || entry.FixingCR == "" || entry.Reason == "" {
			t.Fatalf("expected-red entry has blank field: %+v", entry)
		}
		if seen[entry.ID] {
			t.Fatalf("duplicate expected-red entry id %q", entry.ID)
		}
		seen[entry.ID] = true
		if !requirementRef.MatchString(entry.Requirement) {
			t.Fatalf("expected-red entry %q requirement = %q, want keel/requirement-N", entry.ID, entry.Requirement)
		}
		if !changeRequestRef.MatchString(entry.FixingCR) {
			t.Fatalf("expected-red entry %q fixing_cr = %q, want keel/change_request-N", entry.ID, entry.FixingCR)
		}
	}
	return manifest.Entries
}
