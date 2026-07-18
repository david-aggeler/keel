package main

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestKnownRedManifestIsExplicitAndVisible(t *testing.T) {
	entries, err := loadExpectedRedManifest(filepath.Join("..", "..", "testdata", "red"+"list.json"))
	if err != nil {
		t.Fatalf("load expected-red manifest: %v", err)
	}
	t.Logf("EXPECTED RED manifest contains %d entries", len(entries))
	for _, entry := range entries {
		t.Logf("EXPECTED RED %s: %s fixed by %s - %s", entry.ID, entry.Requirement, entry.FixingCR, entry.Reason)
	}
}

// The declared ci gate must surface the debt on every run (CR-74 visibility
// decision, formal_review-78): logExpectedRed emits one count record plus one
// record per manifest entry through the gate logger.
//
// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestExpectedRedDebtIsVisibleInGateOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	if err := logExpectedRed(logger, filepath.Join("..", "..")); err != nil {
		t.Fatalf("logExpectedRed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "expected-red manifest") {
		t.Fatalf("gate log output missing the expected-red count record:\n%s", output)
	}
	entries, err := loadExpectedRedManifest(filepath.Join("..", "..", "testdata", "red"+"list.json"))
	if err != nil {
		t.Fatalf("load expected-red manifest: %v", err)
	}
	for _, entry := range entries {
		if !strings.Contains(output, entry.ID) {
			t.Fatalf("gate log output missing expected-red entry %q:\n%s", entry.ID, output)
		}
	}
}

// DHF-TEST: keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestKnownRedManifestIsShrinkOnly(t *testing.T) {
	current, err := loadExpectedRedManifest(filepath.Join("..", "..", "testdata", "red"+"list.json"))
	if err != nil {
		t.Fatalf("load expected-red manifest: %v", err)
	}
	baseline, err := loadExpectedRedManifest(filepath.Join("..", "..", "testdata", "red"+"list.baseline.json"))
	if err != nil {
		t.Fatalf("load expected-red baseline: %v", err)
	}

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
	entries, err := validateExpectedRedManifest(expectedRedManifest{Version: 1, Entries: nil})
	if err != nil {
		t.Fatalf("empty target manifest rejected: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty target manifest returned %d entries, want 0", len(entries))
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-69, keel/requirement-70, keel/requirement-71, keel/requirement-72
func TestExpectedRedManifestRejectsInvalidEntries(t *testing.T) {
	valid := expectedRedEntry{ID: "go::test::pkg::TestRed", Requirement: "keel/requirement-11", FixingCR: "keel/change_request-126", Reason: "known red"}
	for _, tc := range []struct {
		name     string
		manifest expectedRedManifest
		want     string
	}{
		{name: "version", manifest: expectedRedManifest{Version: 2}, want: "version"},
		{name: "blank", manifest: expectedRedManifest{Version: 1, Entries: []expectedRedEntry{{ID: "x"}}}, want: "blank field"},
		{name: "duplicate", manifest: expectedRedManifest{Version: 1, Entries: []expectedRedEntry{valid, valid}}, want: "duplicate"},
		{name: "requirement", manifest: expectedRedManifest{Version: 1, Entries: []expectedRedEntry{{ID: "x", Requirement: "openbrain/requirement-1", FixingCR: "keel/change_request-126", Reason: "bad req"}}}, want: "requirement"},
		{name: "fixing cr", manifest: expectedRedManifest{Version: 1, Entries: []expectedRedEntry{{ID: "x", Requirement: "keel/requirement-11", FixingCR: "keel/issue-1", Reason: "bad cr"}}}, want: "fixing_cr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateExpectedRedManifest(tc.manifest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateExpectedRedManifest err = %v, want containing %q", err, tc.want)
			}
		})
	}
}
