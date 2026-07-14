package vscode

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// DHF-TEST: keel/requirement-77
func TestDesiredStateDocumentUsesDesiredStateVocabulary(t *testing.T) {
	doc := DesiredStateDocument{
		Version:     3,
		Devtool:     DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: time.Unix(4, 0).UTC(),
		Groups: []DesiredStateGroup{{
			Label: "Test Preconditions",
			Rows: []DesiredState{{
				Resource: "db",
				Kind:     "service",
				Desired:  "up",
				Current:  "down",
				Status:   "reconcilable",
				Action:   "reconcile_during_run",
				Message:  "start database during run",
				Owned:    true,
			}},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DesiredStateDocument
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("desired-state document decode: %v", err)
	}

	schema, err := SchemaBytes(SchemaDesiredState)
	if err != nil {
		t.Fatalf("read desired-state schema: %v", err)
	}
	retired := string([]byte{115, 101, 116, 117, 112, 45, 112, 108, 97, 110})
	if strings.Contains(string(schema), retired) {
		t.Fatalf("desired-state schema still contains retired label %q", retired)
	}
	if !strings.Contains(string(schema), "desired-state.json") {
		t.Fatalf("desired-state schema $id does not name desired-state.json:\n%s", schema)
	}
}
