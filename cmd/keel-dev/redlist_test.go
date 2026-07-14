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
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "red"+"list.json"))
	if err != nil {
		t.Fatalf("read expected-red manifest: %v", err)
	}
	var manifest expectedRedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse expected-red manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("expected-red manifest version = %d, want 1", manifest.Version)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("expected-red manifest is empty; remove the harness or keep at least one explicit entry")
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
		t.Logf("EXPECTED RED %s: %s fixed by %s - %s", entry.ID, entry.Requirement, entry.FixingCR, entry.Reason)
	}
}
