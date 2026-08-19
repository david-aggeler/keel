package vscode

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// desiredStateRowFactsRef is where the discovery schema describes the typed
// desired-state facts a row item carries.
const desiredStateRowFactsRef = "#/$defs/test_item/properties/desired_state_row"

// desiredStateGroupFactsRef is where the discovery schema describes the typed
// desired-state facts a group item carries.
const desiredStateGroupFactsRef = "#/$defs/test_item/properties/desired_state_group"

type jsonSchema struct {
	ID                   string                `json:"$id"`
	Type                 string                `json:"type"`
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
		{"discovery reconcile result", reflect.TypeOf(ReconcileResult{}), "#/$defs/capabilities/properties/reconcile_results/items"},
		{"discovery test item", reflect.TypeOf(TestItem{}), "#/$defs/test_item"},
		{"discovery range", reflect.TypeOf(Range{}), "#/$defs/test_item/properties/range"},
		{"discovery desired-state group facts", reflect.TypeOf(DesiredStateGroupFacts{}), desiredStateGroupFactsRef},
		{"discovery desired-state row facts", reflect.TypeOf(DesiredStateRowFacts{}), desiredStateRowFactsRef},
		{"discovery last-run facts", reflect.TypeOf(LastRunFacts{}), lastRunFactsRef},
		{"discovery finding", reflect.TypeOf(Finding{}), findingRef},
		{"discovery condition", reflect.TypeOf(Condition{}), conditionRef},
		{"desired-state", reflect.TypeOf(DesiredStateDocument{}), ""},
		{"desired-state devtool", reflect.TypeOf(DevtoolMetadata{}), "#/properties/devtool"},
		{"desired-state group", reflect.TypeOf(DesiredStateGroup{}), "#/$defs/group"},
		{"desired-state desired_state", reflect.TypeOf(DesiredState{}), "#/$defs/desired_state"},
		{"run-event", reflect.TypeOf(RunEvent{}), ""},
		{"run-event location", reflect.TypeOf(RunLocation{}), "#/properties/location"},
		{"run-event artifact", reflect.TypeOf(RunArtifact{}), "#/properties/artifact"},
		{"run-lock", reflect.TypeOf(RunLockFile{}), ""},
		{"test-bridge-config", reflect.TypeOf(TestBridgeConfig{}), ""},
	}

	loaded := loadSchemas(t)

	for _, check := range checks {
		root := loaded[schemaNameForCheck(check.name)]
		schema := schemaAtRef(root, check.ref)
		if !additionalPropertiesClosed(schema.AdditionalProperties) {
			t.Fatalf("%s does not set additionalProperties:false", check.name)
		}
		if err := compareSchemaToType(schema, check.typ); err != nil {
			t.Fatalf("%s drift: %v", check.name, err)
		}
	}

	assertEnumMatches(t, loaded["run-event"].Properties["event"].Enum, sortedKeys(knownRunEvents))
	assertEnumMatches(t, loaded["run-event"].Properties["source"].Enum, sortedKeys(runEventSources))
	assertEnumMatches(t, loaded["run-event"].Properties["artifact"].Properties["kind"].Enum, sortedKeys(artifactKinds))
	assertEnumMatches(t, schemaAtRef(loaded["discovery"], desiredStateRowFactsRef).Properties["action"].Enum, sortedKeys(desiredStateActions))
	assertEnumMatches(t, schemaAtRef(loaded["discovery"], findingRef).Properties["severity"].Enum, sortedKeys(findingSeverities))
	assertEnumMatches(t, schemaAtRef(loaded["discovery"], conditionRef).Properties["kind"].Enum, sortedKeys(conditionKinds))
	assertEnumMatches(t, schemaAtRef(loaded["desired-state"], "#/$defs/desired_state").Properties["action"].Enum, sortedKeys(desiredStateActions))
}

// loadSchemas parses every embedded schema and asserts the whole-document
// invariants that hold for all of them.
func loadSchemas(t *testing.T) map[string]jsonSchema {
	t.Helper()
	loaded := map[string]jsonSchema{}
	for _, name := range []SchemaName{SchemaDiscovery, SchemaDesiredState, SchemaRunEvent, SchemaRunLock, SchemaTestBridgeConfig} {
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
	return loaded
}

// compareSchemaToType reports the structural drift between one schema object
// and the Go wire struct it describes: property names, required-vs-omitempty,
// and the JSON type each property carries. A renamed or retyped Go field is a
// non-nil error.
//
// DHF-REQ: keel/requirement-127
func compareSchemaToType(schema jsonSchema, typ reflect.Type) error {
	wantProps, wantRequired := jsonFields(typ)
	gotProps := sortedKeys(schema.Properties)
	gotRequired := append([]string(nil), schema.Required...)
	sort.Strings(gotRequired)
	if strings.Join(gotProps, ",") != strings.Join(wantProps, ",") {
		return fmt.Errorf("property drift:\n got: %v\nwant: %v", gotProps, wantProps)
	}
	if strings.Join(gotRequired, ",") != strings.Join(wantRequired, ",") {
		return fmt.Errorf("required drift:\n got: %v\nwant: %v", gotRequired, wantRequired)
	}
	for name, want := range jsonFieldTypes(typ) {
		got := schema.Properties[name].Type
		if got == "" {
			// The property constrains its values by enum or $ref instead of by
			// a declared type; there is nothing to compare.
			continue
		}
		if got != want {
			return fmt.Errorf("type drift on %q: schema says %q, Go type is %q", name, got, want)
		}
	}
	return nil
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
	case strings.HasPrefix(name, "desired-state"):
		return "desired-state"
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
		case "items":
			if cur.Items != nil {
				cur = *cur.Items
			}
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

// jsonFieldTypes maps each JSON property name of a wire struct to the JSON
// type its Go field serializes as.
func jsonFieldTypes(typ reflect.Type) map[string]string {
	types := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		types[name] = jsonTypeOf(field.Type)
	}
	return types
}

func jsonTypeOf(typ reflect.Type) string {
	if typ == reflect.TypeOf(time.Time{}) {
		return "string"
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return jsonTypeOf(typ.Elem())
	case reflect.Bool:
		return "boolean"
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "object"
	}
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

// lastRunFactsRef is where the discovery schema describes the typed last-run
// facts a lane item carries.
const lastRunFactsRef = "#/$defs/test_item/properties/last_run"

// findingRef is where the discovery schema describes one typed lane validation
// finding.
const findingRef = "#/$defs/test_item/properties/findings/items"

// conditionRef is where the discovery schema describes one persistent
// non-result condition standing against an item.
const conditionRef = "#/$defs/test_item/properties/conditions/items"

// TestDiscoveryItemCarriesTypedFactsAndScalarDescription pins the carriage
// keel/requirement-138 introduces: the prose channel is a scalar `description`
// string, and the two machine facts that rode the `limitations` array — the
// measured last-run duration and the lane validation findings — are typed,
// schema-covered properties with a closed severity enum.
//
// DHF-TEST: keel/requirement-138
func TestDiscoveryItemCarriesTypedFactsAndScalarDescription(t *testing.T) {
	item := schemaAtRef(loadSchemas(t)["discovery"], "#/$defs/test_item")

	if got := item.Properties["description"].Type; got != "string" {
		t.Errorf("discovery test_item.description type = %q, want %q (keel/ac-549)", got, "string")
	}
	if got := item.Properties["findings"].Type; got != "array" {
		t.Errorf("discovery test_item.findings type = %q, want %q (keel/ac-551)", got, "array")
	}
	if got := item.Properties["last_run"].Type; got != "object" {
		t.Errorf("discovery test_item.last_run type = %q, want %q (keel/ac-550)", got, "object")
	}
	for _, name := range []string{"rule", "severity", "message"} {
		if _, ok := schemaAtRef(loadSchemas(t)["discovery"], findingRef).Properties[name]; !ok {
			t.Errorf("discovery finding declares no %q member (keel/ac-551)", name)
		}
	}
	for _, name := range []string{"at", "duration_ms", "exit_code"} {
		if _, ok := schemaAtRef(loadSchemas(t)["discovery"], lastRunFactsRef).Properties[name]; !ok {
			t.Errorf("discovery last_run declares no %q member (keel/ac-550)", name)
		}
	}
	// duration_ms and exit_code stay optional so that a lane with no
	// attributable run stream carries no measurement at all rather than a
	// zero standing in for "never measured" (keel/ac-564).
	for _, name := range schemaAtRef(loadSchemas(t)["discovery"], lastRunFactsRef).Required {
		if name != "at" {
			t.Errorf("discovery last_run requires %q; only %q may be required (keel/ac-564)", name, "at")
		}
	}
}
