package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// repoRootForTest walks up from the test's working directory to the module
// root, so the pin can be exercised against the committed protocol.ts.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found above the test working directory")
		}
		dir = parent
	}
}

func coveredSchemaBytes(t *testing.T) map[vscode.SchemaName][]byte {
	t.Helper()
	schemas := map[vscode.SchemaName][]byte{}
	for _, covered := range vsixProtocolDriftCoverage {
		body, err := vscode.SchemaBytes(covered.schema)
		if err != nil {
			t.Fatalf("read schema %s: %v", covered.schema, err)
		}
		schemas[covered.schema] = body
	}
	return schemas
}

func committedProtocolSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootForTest(t), filepath.FromSlash(vsixProtocolRel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestVSIXProtocolPinIsGreenAtHead is the baseline the mutation tests move
// away from: the committed protocol.ts and the embedded schemas agree, so the
// pin reports nothing.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinIsGreenAtHead(t *testing.T) {
	findings, err := vsixProtocolDriftFindings(coveredSchemaBytes(t), committedProtocolSource(t))
	if err != nil {
		t.Fatalf("pin failed to run: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("committed protocol.ts already drifts from the embedded schemas:\n  %s", strings.Join(findings, "\n  "))
	}
}

// addRequiredSchemaProperty performs keel/ac-483's mutation on a raw schema
// document: a new required property appears in the schema and protocol.ts is
// left untouched.
func addRequiredSchemaProperty(t *testing.T, body []byte, objectPath []string, property string) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	node := document
	for _, step := range objectPath {
		next, ok := node[step].(map[string]any)
		if !ok {
			t.Fatalf("schema path %v has no object at %q", objectPath, step)
		}
		node = next
	}
	properties, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema path %v declares no properties", objectPath)
	}
	properties[property] = map[string]any{"type": "string"}
	required, _ := node["required"].([]any)
	node["required"] = append(required, property)
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return mutated
}

// TestVSIXProtocolPinRedsOnSchemaPropertyAbsentFromProtocolTS proves
// keel/ac-483 on the schema side: a required property added to an embedded
// family that protocol.ts does not declare is a finding, and the finding names
// both the property and the document family.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinRedsOnSchemaPropertyAbsentFromProtocolTS(t *testing.T) {
	source := committedProtocolSource(t)

	for _, tc := range []struct {
		name     string
		schema   vscode.SchemaName
		path     []string
		property string
	}{
		{"discovery root", vscode.SchemaDiscovery, nil, "reconcile_token"},
		{"discovery test item", vscode.SchemaDiscovery, []string{"$defs", "test_item"}, "owning_lane"},
		{"desired-state row", vscode.SchemaDesiredState, []string{"$defs", "desired_state"}, "reconcile_hint"},
		{"run-event root", vscode.SchemaRunEvent, nil, "attempt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schemas := coveredSchemaBytes(t)
			schemas[tc.schema] = addRequiredSchemaProperty(t, schemas[tc.schema], tc.path, tc.property)

			findings, err := vsixProtocolDriftFindings(schemas, source)
			if err != nil {
				t.Fatalf("pin failed to run: %v", err)
			}
			if len(findings) == 0 {
				t.Fatalf("pin stayed green after %q was added to the %s schema", tc.property, tc.schema)
			}
			joined := strings.Join(findings, "\n")
			if !strings.Contains(joined, tc.property) {
				t.Fatalf("finding does not name the drifted property %q:\n%s", tc.property, joined)
			}
			if !strings.Contains(joined, string(tc.schema)) {
				t.Fatalf("finding does not name the document family %q:\n%s", tc.schema, joined)
			}
		})
	}
}

// TestVSIXProtocolPinRedsOnProtocolTSDrift covers the other side of the same
// contract: protocol.ts moving away from an unchanged schema. Each mutation is
// one of the four drift shapes keel/requirement-128 names — removed, renamed,
// retyped, and re-required.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinRedsOnProtocolTSDrift(t *testing.T) {
	schemas := coveredSchemaBytes(t)
	source := committedProtocolSource(t)

	for _, tc := range []struct {
		name    string
		old     string
		new     string
		wantAll []string
	}{
		{
			name:    "property removed",
			old:     "  canonical_id?: string;\n",
			new:     "",
			wantAll: []string{"canonical_id", "discovery"},
		},
		{
			name:    "property renamed",
			old:     "  teardown_policy?: string;",
			new:     "  teardown_policies?: string;",
			wantAll: []string{"teardown_policy", "desired-state"},
		},
		{
			name:    "property retyped",
			old:     "  exit_code?: number;",
			new:     "  exit_code?: string;",
			wantAll: []string{"exit_code", "run-event"},
		},
		{
			name:    "optional property declared required",
			old:     "  detail?: string;",
			new:     "  detail: string;",
			wantAll: []string{"detail", "desired-state"},
		},
		{
			name:    "enum member dropped from the union",
			old:     "  state: 'passed' | 'skipped';",
			new:     "  state: 'passed';",
			wantAll: []string{"state", "discovery"},
		},
		{
			name:    "version literal no longer matches the schema const",
			old:     "export interface RunEvent {\n  version: 1;",
			new:     "export interface RunEvent {\n  version: 2;",
			wantAll: []string{"version", "run-event"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(source, tc.old) {
				t.Fatalf("committed protocol.ts no longer contains the mutation anchor %q", tc.old)
			}
			mutated := strings.Replace(source, tc.old, tc.new, 1)

			findings, err := vsixProtocolDriftFindings(schemas, mutated)
			if err != nil {
				t.Fatalf("pin failed to run: %v", err)
			}
			if len(findings) == 0 {
				t.Fatal("pin stayed green after protocol.ts drifted")
			}
			joined := strings.Join(findings, "\n")
			for _, want := range tc.wantAll {
				if !strings.Contains(joined, want) {
					t.Fatalf("finding does not name %q:\n%s", want, joined)
				}
			}
		})
	}
}

// TestVSIXProtocolPinAccumulatesFindings holds the change request's decision
// that the gate reports every drifted property in one run: a gate that
// surfaces one mismatch per invocation turns a schema change into a guessing
// loop.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinAccumulatesFindings(t *testing.T) {
	schemas := coveredSchemaBytes(t)
	schemas[vscode.SchemaDiscovery] = addRequiredSchemaProperty(t, schemas[vscode.SchemaDiscovery], nil, "reconcile_token")
	schemas[vscode.SchemaRunEvent] = addRequiredSchemaProperty(t, schemas[vscode.SchemaRunEvent], nil, "attempt")

	findings, err := vsixProtocolDriftFindings(schemas, committedProtocolSource(t))
	if err != nil {
		t.Fatalf("pin failed to run: %v", err)
	}
	joined := strings.Join(findings, "\n")
	if !strings.Contains(joined, "reconcile_token") || !strings.Contains(joined, "attempt") {
		t.Fatalf("pin stopped at the first drifted family instead of accumulating:\n%s", joined)
	}
}

// TestVSIXProtocolPinRejectsUnreadableSources keeps the pin loud: a
// TypeScript construct it cannot parse, or a family whose TypeScript
// counterpart is missing, must surface rather than silently pass.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinRejectsUnreadableSources(t *testing.T) {
	schemas := coveredSchemaBytes(t)

	if _, err := vsixProtocolDriftFindings(schemas, "export interface RunEvent { version: 1; }"); err == nil {
		t.Fatal("pin accepted a protocol source missing the discovery declarations")
	}
	if _, err := vsixProtocolDriftFindings(schemas, committedProtocolSource(t)+"\nexport const answer = 42;\n"); err == nil {
		t.Fatal("pin accepted a TypeScript construct it cannot parse")
	}
}

// TestVSIXProtocolPinCoversEveryParsedSchemaFamily proves keel/ac-484: the
// coverage set names every embedded family the VSIX parses, and each family
// left out is named in the gate's own source with a reason.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinCoversEveryParsedSchemaFamily(t *testing.T) {
	covered := map[vscode.SchemaName]bool{}
	for _, entry := range vsixProtocolDriftCoverage {
		if entry.tsType == "" {
			t.Fatalf("coverage entry %s names no TypeScript declaration", entry.schema)
		}
		covered[entry.schema] = true
	}
	for _, want := range []vscode.SchemaName{vscode.SchemaDiscovery, vscode.SchemaDesiredState, vscode.SchemaRunEvent} {
		if !covered[want] {
			t.Fatalf("the pin does not cover the %s family the VSIX parses at runtime", want)
		}
	}

	for _, name := range []vscode.SchemaName{
		vscode.SchemaDiscovery, vscode.SchemaDesiredState, vscode.SchemaRunEvent,
		vscode.SchemaRunLock, vscode.SchemaTestBridgeConfig,
	} {
		reason, exempt := vsixProtocolDriftExemptions[name]
		if covered[name] == exempt {
			t.Fatalf("schema family %s is neither covered nor exempt, or is both", name)
		}
		if exempt && len(reason) < 40 {
			t.Fatalf("exemption for %s does not state why it is out: %q", name, reason)
		}
	}
}

// TestValidateVSIXProtocolDriftGatesTheWorktree exercises the gate entry
// point on the committed tree and on a mutated copy of it.
//
// DHF-TEST: keel/requirement-128
func TestValidateVSIXProtocolDriftGatesTheWorktree(t *testing.T) {
	if err := validateVSIXProtocolDrift(repoRootForTest(t)); err != nil {
		t.Fatalf("committed worktree fails the protocol pin: %v", err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vsix", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(committedProtocolSource(t), "  canonical_id?: string;\n", "", 1)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(vsixProtocolRel)), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateVSIXProtocolDrift(root)
	if err == nil {
		t.Fatal("validateVSIXProtocolDrift accepted a drifted protocol.ts")
	}
	if !strings.Contains(err.Error(), "canonical_id") || !strings.Contains(err.Error(), "discovery") {
		t.Fatalf("gate error names neither the property nor the family: %v", err)
	}

	if err := validateVSIXProtocolDrift(t.TempDir()); err == nil {
		t.Fatal("validateVSIXProtocolDrift accepted a worktree with no protocol.ts")
	}
}

// TestRunVSIXGateRunsTheProtocolPin proves keel/ac-483's outer clause: the pin
// is wired into `keel-dev vsix ci`, so a drifted protocol.ts makes the gate
// exit non-zero before any Node-backed step runs.
//
// DHF-TEST: keel/requirement-128
func TestRunVSIXGateRunsTheProtocolPin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vsix", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(committedProtocolSource(t), "  canonical_id?: string;\n", "", 1)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(vsixProtocolRel)), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}

	stubBin := t.TempDir()
	for _, tool := range []string{"node", "pnpm", "xvfb-run"} {
		if err := os.WriteFile(filepath.Join(stubBin, tool), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", stubBin)

	err := runVSIXGate(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), root)
	if err == nil {
		t.Fatal("runVSIXGate stayed green with a drifted protocol.ts")
	}
	if !strings.Contains(err.Error(), "canonical_id") {
		t.Fatalf("vsix gate error does not name the drifted property: %v", err)
	}
}

// TestParseTypeScriptDeclarationsReadsTheProtocolSubset covers the reader's
// accepted grammar and, more importantly, its refusals: a construct it cannot
// model must be an error, because a silent skip is a false green.
//
// DHF-TEST: keel/requirement-128
func TestParseTypeScriptDeclarationsReadsTheProtocolSubset(t *testing.T) {
	declarations, err := parseTypeScriptDeclarations(`
// a line comment
/* a block comment */
export type Kind = 'a' | 'b';
export interface Doc {
  /** doc comment */
  'quoted': string,
  nested: { id: string; label: number }[];
  listed: Array<string>;
  kind: Kind;
  flag?: boolean;
}
`)
	if err != nil {
		t.Fatalf("reader rejected the protocol subset: %v", err)
	}
	doc := declarations["Doc"]
	if len(doc.members) != 5 {
		t.Fatalf("read %d members, want 5", len(doc.members))
	}
	quoted, ok := doc.member("quoted")
	if !ok || quoted.optional {
		t.Fatalf("quoted member = %+v, want a required member", quoted)
	}
	flag, ok := doc.member("flag")
	if !ok || !flag.optional {
		t.Fatalf("flag member = %+v, want an optional member", flag)
	}
	nested, _ := doc.member("nested")
	if nested.typ.kind != tsArrayKind || nested.typ.elem.kind != tsObjectKind {
		t.Fatalf("nested member = %+v, want an array of objects", nested.typ)
	}

	for _, tc := range []struct {
		name   string
		source string
	}{
		{"unexported declaration", "interface Doc { a: string; }"},
		{"unsupported export", "export const answer = 42;"},
		{"truncated declaration", "export interface"},
		{"missing declaration name", "export interface 'Doc' {}"},
		{"missing alias body", "export type Kind = ;"},
		{"missing member type", "export interface Doc { a: }"},
		{"unterminated object", "export interface Doc { a: string"},
		{"unterminated block comment", "/* forever"},
		{"unterminated string literal", "export type Kind = 'a"},
		{"unreadable character", "export interface Doc { a: string; }\n@decorator"},
		{"generic without argument", "export interface Doc { a: Array; }"},
		{"unterminated generic", "export interface Doc { a: Array<string; }"},
		{"unreadable member name", "export interface Doc { 1: string; }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseTypeScriptDeclarations(tc.source); err == nil {
				t.Fatalf("reader accepted %q", tc.source)
			}
		})
	}
}

// TestVSIXProtocolPinIsLoudOnWhatItCannotCompare pins the guard's own failure
// posture: every shape the walk cannot model is reported, never skipped into
// silence.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolPinIsLoudOnWhatItCannotCompare(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		source string
		want   string
	}{
		{
			name:   "unreadable schema reference",
			schema: `{"type":"object","properties":{"a":{"$ref":"https://example.test/a.json"}}}`,
			source: "export interface Doc { a: string; }",
			want:   "cannot resolve the schema reference",
		},
		{
			name:   "self-referential schema definition",
			schema: `{"type":"object","properties":{"a":{"$ref":"#/$defs/loop"}},"$defs":{"loop":{"$ref":"#/$defs/loop"}}}`,
			source: "export interface Doc { a: string; }",
			want:   "cannot resolve the schema reference",
		},
		{
			name:   "undeclared TypeScript type",
			schema: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			source: "export interface Doc { a: Missing; }",
			want:   `declares no type "Missing"`,
		},
		{
			name:   "closed object declaring no properties",
			schema: `{"type":"object","properties":{"a":{"type":"object"}}}`,
			source: "export interface Doc { a: { b: string }; }",
			want:   "declares no properties",
		},
		{
			name:   "union mixing JSON types",
			schema: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			source: "export interface Doc { a: string | 1; }",
			want:   "cannot read the TypeScript type",
		},
		{
			name:   "TypeScript narrowing an open string",
			schema: `{"type":"object","properties":{"a":{"type":"string"}}}`,
			source: "export interface Doc { a: 'x' | 'y'; }",
			want:   "narrows",
		},
		{
			name:   "TypeScript narrowing against a non-string enum",
			schema: `{"type":"object","properties":{"a":{"enum":[1,2]}}}`,
			source: "export interface Doc { a: 'x'; }",
			want:   "no closed enum",
		},
		{
			name:   "literal pinned where the schema declares no const",
			schema: `{"type":"object","properties":{"a":{"type":"number"}}}`,
			source: "export interface Doc { a: 7; }",
			want:   "declares no const",
		},
		{
			name:   "property TypeScript does not declare",
			schema: `{"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}`,
			source: "export interface Doc { }",
			want:   `schema property "a"`,
		},
		{
			name:   "property the schema does not define",
			schema: `{"type":"object","properties":{}, "additionalProperties":false}`,
			source: "export interface Doc { a: string; }",
			want:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var root protocolSchema
			if err := json.Unmarshal([]byte(tc.schema), &root); err != nil {
				t.Fatal(err)
			}
			declarations, err := parseTypeScriptDeclarations(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			pin := &protocolPin{family: "discovery", root: root, declarations: declarations}
			pin.compare("#", root, declarations["Doc"], "Doc", 0)
			joined := strings.Join(pin.findings, "\n")
			if tc.want == "" {
				if len(pin.findings) != 0 {
					t.Fatalf("pin reported findings it should not have:\n%s", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("findings do not contain %q:\n%s", tc.want, joined)
			}
		})
	}
}

// TestSchemaJSONTypeInfersUndeclaredTypes covers the inference the walk leans
// on where a schema node states its shape by enum, properties, or items
// instead of by "type".
//
// DHF-TEST: keel/requirement-128
func TestSchemaJSONTypeInfersUndeclaredTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		want   string
	}{
		{"declared", `{"type":"boolean"}`, "boolean"},
		{"closed enum", `{"enum":["a","b"]}`, "string"},
		{"properties only", `{"properties":{"a":{"type":"string"}}}`, "object"},
		{"items only", `{"items":{"type":"string"}}`, "array"},
		{"nothing to infer from", `{"minLength":1}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema protocolSchema
			if err := json.Unmarshal([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			if got := schemaJSONType(schema); got != tc.want {
				t.Fatalf("schemaJSONType = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVSIXProtocolCoverageGapsRedOnAnUndecidedFamily proves the second half of
// keel/ac-484: the coverage decision is enforced against the embedded schema
// set, so a family added to the wire cannot be silently exempt.
//
// DHF-TEST: keel/requirement-128
func TestVSIXProtocolCoverageGapsRedOnAnUndecidedFamily(t *testing.T) {
	families, err := embeddedSchemaFamilies()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != len(vsixProtocolDriftCoverage)+len(vsixProtocolDriftExemptions) {
		t.Fatalf("embedded families %v are not fully accounted for by the coverage set and exemptions", families)
	}
	if gaps := vsixProtocolCoverageGaps(families); len(gaps) != 0 {
		t.Fatalf("committed coverage decision has gaps:\n  %s", strings.Join(gaps, "\n  "))
	}

	gaps := vsixProtocolCoverageGaps(append(families, "unaccounted-family"))
	if len(gaps) != 1 || !strings.Contains(gaps[0], "unaccounted-family") {
		t.Fatalf("a new schema family did not red the coverage decision: %v", gaps)
	}
}
