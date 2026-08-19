package vscode

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDiscoveryItemCarriesOneProseFieldNamedDescription holds keel/ac-549: the
// item's prose channel is a single `description` string, the schema declares no
// `limitations` property, and a document still carrying the removed array is
// refused rather than silently tolerated.
//
// DHF-TEST: keel/requirement-138
func TestDiscoveryItemCarriesOneProseFieldNamedDescription(t *testing.T) {
	schema := loadSchemas(t)["discovery"]
	item := schemaAtRef(schema, "#/$defs/test_item")
	if _, ok := item.Properties["limitations"]; ok {
		t.Fatal("discovery schema still declares a limitations property")
	}
	prose, ok := item.Properties["description"]
	if !ok {
		t.Fatal("discovery schema declares no description property")
	}
	if prose.Type != "string" {
		t.Fatalf("description type = %q, want string", prose.Type)
	}

	var decoded TestItem
	err := json.Unmarshal([]byte(`{"id":"keel::lane::a","label":"a","kind":"lane","limitations":["prose"]}`), &decoded)
	if err == nil {
		t.Fatal("decoding an item carrying limitations succeeded; the removed field must be refused")
	}
	if !strings.Contains(err.Error(), "limitations") {
		t.Fatalf("error %q does not name the removed field", err)
	}

	// The field's absence is what a migrated producer looks like, and it still
	// decodes.
	if err := json.Unmarshal([]byte(`{"id":"keel::lane::a","label":"a","kind":"lane","description":"prose"}`), &decoded); err != nil {
		t.Fatalf("decoding a migrated item: %v", err)
	}
	if decoded.Description != "prose" {
		t.Fatalf("description = %q, want the producer's prose", decoded.Description)
	}
}
