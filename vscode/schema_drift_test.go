package vscode

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type jsonSchema struct {
	ID                   string                `json:"$id"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
	Required             []string              `json:"required"`
	Properties           map[string]jsonSchema `json:"properties"`
	Defs                 map[string]jsonSchema `json:"$defs"`
	Items                *jsonSchema           `json:"items"`
	Ref                  string                `json:"$ref"`
	Enum                 []string              `json:"enum"`
}

// DHF-TEST: keel/requirement-34
func TestSchemasDriftAgainstGoTypes(t *testing.T) {
	checks := []struct {
		name string
		typ  reflect.Type
		ref  string
	}{
		{"discovery", reflect.TypeOf(DiscoveryDocument{}), ""},
		{"discovery capabilities", reflect.TypeOf(DiscoveryCapabilities{}), "#/$defs/capabilities"},
		{"discovery test item", reflect.TypeOf(TestItem{}), "#/$defs/test_item"},
		{"discovery range", reflect.TypeOf(Range{}), "#/$defs/test_item/properties/range"},
		{"setup-plan", reflect.TypeOf(SetupPlan{}), ""},
		{"setup-plan devtool", reflect.TypeOf(DevtoolMetadata{}), "#/properties/devtool"},
		{"setup-plan item", reflect.TypeOf(SetupPlanItem{}), "#/$defs/item"},
		{"setup-plan group", reflect.TypeOf(DesiredStateGroup{}), "#/$defs/group"},
		{"setup-plan desired_state", reflect.TypeOf(DesiredState{}), "#/$defs/desired_state"},
		{"setup-plan check", reflect.TypeOf(PrereqCheck{}), "#/$defs/check"},
		{"setup-plan action", reflect.TypeOf(SetupPlanAction{}), "#/$defs/action"},
		{"setup-plan teardown", reflect.TypeOf(SetupPlanTeardown{}), "#/$defs/teardown"},
		{"run-event", reflect.TypeOf(RunEvent{}), ""},
		{"run-event location", reflect.TypeOf(RunLocation{}), "#/properties/location"},
		{"run-event artifact", reflect.TypeOf(RunArtifact{}), "#/properties/artifact"},
		{"run-lock", reflect.TypeOf(RunLockFile{}), ""},
		{"test-bridge-config", reflect.TypeOf(TestBridgeConfig{}), ""},
	}

	loaded := map[string]jsonSchema{}
	for _, name := range []SchemaName{SchemaDiscovery, SchemaSetupPlan, SchemaRunEvent, SchemaRunLock, SchemaTestBridgeConfig} {
		body, err := SchemaBytes(name)
		if err != nil {
			t.Fatalf("read schema %s: %v", name, err)
		}
		var schema jsonSchema
		if err := json.Unmarshal(body, &schema); err != nil {
			t.Fatalf("parse schema %s: %v", name, err)
		}
		if !strings.Contains(schema.ID, "github.com/david-aggeler/keel/vscode/schemas/") {
			t.Fatalf("%s $id is not keel-anchored: %q", name, schema.ID)
		}
		if !additionalPropertiesClosed(schema.AdditionalProperties) {
			t.Fatalf("%s does not set additionalProperties:false", name)
		}
		loaded[string(name)] = schema
	}

	for _, check := range checks {
		root := loaded[schemaNameForCheck(check.name)]
		schema := schemaAtRef(root, check.ref)
		if !additionalPropertiesClosed(schema.AdditionalProperties) {
			t.Fatalf("%s does not set additionalProperties:false", check.name)
		}
		wantProps, wantRequired := jsonFields(check.typ)
		gotProps := sortedKeys(schema.Properties)
		gotRequired := append([]string(nil), schema.Required...)
		sort.Strings(gotRequired)
		if strings.Join(gotProps, ",") != strings.Join(wantProps, ",") {
			t.Fatalf("%s property drift:\n got: %v\nwant: %v", check.name, gotProps, wantProps)
		}
		if strings.Join(gotRequired, ",") != strings.Join(wantRequired, ",") {
			t.Fatalf("%s required drift:\n got: %v\nwant: %v", check.name, gotRequired, wantRequired)
		}
	}

	assertEnumMatches(t, loaded["run-event"].Properties["event"].Enum, sortedKeys(knownRunEvents))
	assertEnumMatches(t, loaded["run-event"].Properties["source"].Enum, sortedKeys(runEventSources))
	assertEnumMatches(t, loaded["run-event"].Properties["artifact"].Properties["kind"].Enum, sortedKeys(artifactKinds))
}

func additionalPropertiesClosed(raw json.RawMessage) bool {
	var b bool
	return json.Unmarshal(raw, &b) == nil && !b
}

// DHF-TEST: keel/requirement-34
func TestSchemaDriftKnownLimitsAreCoveredByEventStamper(t *testing.T) {
	// This stdlib drift test deliberately checks only structural contract drift:
	// property names, required-vs-omitempty, closed enum sets,
	// additionalProperties:false, and keel-anchored $id values. It does not
	// implement a full JSON Schema evaluator for const version, minLength,
	// minimum, or date-time format. EventStamper enforces the producer-side
	// value constraints before events are written.
	var logs []string
	stamped := EventStamper{
		Now:       func() time.Time { return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC) },
		RunID:     "run-1",
		Source:    "vscode",
		Workspace: "cr-38",
		Logf:      func(message string) { logs = append(logs, message) },
	}.Stamp(RunEvent{Event: "passed", DurationMS: -1, TestID: "go::root"})
	if stamped.Event != "output" || !strings.Contains(stamped.Message, "duration_ms") {
		t.Fatalf("invalid duration was not demoted by EventStamper: %+v", stamped)
	}
	if len(logs) == 0 {
		t.Fatal("EventStamper did not log the invalid event")
	}
}

func schemaNameForCheck(name string) string {
	switch {
	case strings.HasPrefix(name, "discovery"):
		return "discovery"
	case strings.HasPrefix(name, "setup-plan"):
		return "setup-plan"
	case strings.HasPrefix(name, "run-event"):
		return "run-event"
	case strings.HasPrefix(name, "run-lock"):
		return "run-lock"
	default:
		return name
	}
}

func schemaAtRef(root jsonSchema, ref string) jsonSchema {
	if ref == "" {
		return root
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	cur := root
	for _, part := range parts {
		switch part {
		case "$defs":
			continue
		case "properties":
			continue
		default:
			if next, ok := cur.Defs[part]; ok {
				cur = next
				continue
			}
			cur = cur.Properties[part]
		}
	}
	return cur
}

func jsonFields(typ reflect.Type) ([]string, []string) {
	var props []string
	var required []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		props = append(props, name)
		if !strings.Contains(opts, "omitempty") {
			required = append(required, name)
		}
	}
	sort.Strings(props)
	sort.Strings(required)
	return props, required
}

func sortedKeys[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	return keys
}

func assertEnumMatches(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("enum drift:\n got: %v\nwant: %v", got, want)
	}
}
