package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
)

// rawDiscoveryItems encodes the discovery document the producer would write and
// reads it back as untyped JSON. The assertions below are about the wire the
// extension parses, so they read the document rather than the Go struct: a
// field the producer never wrote cannot be hidden behind a zero value.
func rawDiscoveryItems(t *testing.T, root string) []map[string]any {
	t.Helper()
	built, err := buildVSCodeDiscovery(root)
	if err != nil {
		t.Fatalf("buildVSCodeDiscovery: %v", err)
	}
	var encoded bytes.Buffer
	if err := testbridge.EncodeDocument(&encoded, built); err != nil {
		t.Fatalf("encode protocol document: %v", err)
	}
	var doc struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &doc); err != nil {
		t.Fatalf("discovery JSON: %v\n%s", err, encoded.String())
	}
	return doc.Items
}

func rawDiscoveryItemByID(t *testing.T, items []map[string]any, id string) map[string]any {
	t.Helper()
	for _, item := range items {
		if item["id"] == id {
			return item
		}
	}
	t.Fatalf("discovery carries no item %q: %+v", id, items)
	return nil
}

// TestVSCodeDiscoveryRoutesGoParseFailureToTheConditionChannel holds
// keel/ac-557: a Go test file that fails to parse is a discovery-time
// condition, not prose. Its text travels on the persistent-condition channel,
// the item's description carries none of it, and the item stays non-runnable.
//
// DHF-TEST: keel/requirement-140
func TestVSCodeDiscoveryRoutesGoParseFailureToTheConditionChannel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("broken", "broken_test.go"), "package broken\n\nfunc TestBroken(\n")

	item := rawDiscoveryItemByID(t, rawDiscoveryItems(t, root), "go::file::broken/broken_test.go")

	if runnable, _ := item["runnable"].(bool); runnable {
		t.Fatalf("parse-failed file item = %+v, want a non-runnable item (keel/ac-557)", item)
	}
	conditions, ok := item["conditions"].([]any)
	if !ok || len(conditions) != 1 {
		t.Fatalf("parse-failed file conditions = %v, want exactly one persistent condition (keel/ac-557)", item["conditions"])
	}
	condition, ok := conditions[0].(map[string]any)
	if !ok {
		t.Fatalf("condition = %v, want a typed object (keel/ac-557)", conditions[0])
	}
	if condition["kind"] != "parse_error" {
		t.Fatalf("condition kind = %v, want \"parse_error\" (keel/ac-557)", condition["kind"])
	}
	message, _ := condition["message"].(string)
	if !strings.Contains(message, "expected") {
		t.Fatalf("condition message = %q, want the parse error text (keel/ac-557)", message)
	}
	description, _ := item["description"].(string)
	if description != "" {
		t.Fatalf("parse-failed file description = %q, want the prose channel empty of the parse text (keel/ac-557)", description)
	}
}

// TestVSCodeDiscoveryRoutesPackageParseFailureToTheConditionChannel holds the
// same criterion for the sibling case: a test file that parses but sits in a
// package with an invalid non-test file. The condition belongs to the same
// channel — the item is not runnable for a discovery-time reason either way.
//
// DHF-TEST: keel/requirement-140
func TestVSCodeDiscoveryRoutesPackageParseFailureToTheConditionChannel(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	if err := os.MkdirAll(filepath.Join(root, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, filepath.Join("broken", "broken.go"), "package broken\n\nfunc Broken(\n")
	writeFile(t, root, filepath.Join("broken", "ok_test.go"), "package broken\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")

	item := rawDiscoveryItemByID(t, rawDiscoveryItems(t, root), "go::file::broken/ok_test.go")

	conditions, ok := item["conditions"].([]any)
	if !ok || len(conditions) != 1 {
		t.Fatalf("package-parse-failed file conditions = %v, want exactly one persistent condition (keel/ac-557)", item["conditions"])
	}
	condition, _ := conditions[0].(map[string]any)
	message, _ := condition["message"].(string)
	if condition["kind"] != "parse_error" || !strings.Contains(message, "broken.go") {
		t.Fatalf("condition = %v, want the package parse error on the condition channel (keel/ac-557)", condition)
	}
	if description, _ := item["description"].(string); description != "" {
		t.Fatalf("package-parse-failed file description = %q, want the prose channel empty of the parse text (keel/ac-557)", description)
	}
}
