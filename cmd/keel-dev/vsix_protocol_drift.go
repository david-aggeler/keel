package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/david-aggeler/keel/vscode"
)

// vsixProtocolRel is the VSIX's hand-written declaration of the Test Bridge
// wire, the third declaration of a protocol whose other two — the Go structs
// and the embedded schemas — are pinned to each other by
// vscode/schema_drift_test.go.
const vsixProtocolRel = "vsix/src/protocol.ts"

// protocolPinMaxDepth bounds the walk so a future self-referential schema
// cannot spin the gate.
const protocolPinMaxDepth = 32

// vsixProtocolDriftCoverage is the pin's coverage set: one embedded schema
// document family per entry, paired with the protocol.ts declaration the VSIX
// parses that family through. Every nested object, array element, and $ref
// below these roots is reached by the walk, so the set is stated as roots
// rather than as a type list — but the families themselves are enumerated
// deliberately, following the Go-side precedent, so a new family cannot join
// the wire and be silently exempt.
//
// DHF-REQ: keel/requirement-128
var vsixProtocolDriftCoverage = []struct {
	schema vscode.SchemaName
	tsType string
}{
	{vscode.SchemaDiscovery, "DiscoveryDocument"},
	{vscode.SchemaDesiredState, "DesiredStateDocument"},
	{vscode.SchemaRunEvent, "RunEvent"},
}

// vsixProtocolDriftExemptions names every embedded schema family deliberately
// left out of the coverage set above, with the reason it is out. keel/ac-484
// requires the exemption to be stated here rather than inferred from the
// absence of an entry.
var vsixProtocolDriftExemptions = map[vscode.SchemaName]string{
	vscode.SchemaRunLock:          "producer-owned state under .devtools/locks/: the VSIX asks the devtool to unlock through the maintenance test id and never reads or parses the lock file itself, so protocol.ts declares no counterpart to pin",
	vscode.SchemaTestBridgeConfig: "read by vsix/src/bridgeAdapter.ts through BridgeAdapterConfig, an in-memory adapter type that deliberately differs from the wire — it resolves the command to an absolute path and adds the derived outputChannel field — so pinning it would pin a shape that is not the document's",
}

// validateVSIXProtocolDrift is the gate step: it compares the committed
// protocol.ts against the embedded schemas and reports every drifted property
// in one run. Accumulating rather than failing on the first mismatch keeps a
// schema change from turning into a guessing loop.
//
// DHF-REQ: keel/requirement-128
func validateVSIXProtocolDrift(dir string) error {
	protocolPath := filepath.Join(dir, filepath.FromSlash(vsixProtocolRel))
	source, err := os.ReadFile(protocolPath)
	if err != nil {
		return fmt.Errorf("keel-dev vsix protocol pin: read %s: %w", protocolPath, err)
	}
	schemas := map[vscode.SchemaName][]byte{}
	for _, covered := range vsixProtocolDriftCoverage {
		body, err := vscode.SchemaBytes(covered.schema)
		if err != nil {
			return fmt.Errorf("keel-dev vsix protocol pin: %w", err)
		}
		schemas[covered.schema] = body
	}
	families, err := embeddedSchemaFamilies()
	if err != nil {
		return fmt.Errorf("keel-dev vsix protocol pin: %w", err)
	}
	findings, err := vsixProtocolDriftFindings(schemas, string(source))
	if err != nil {
		return fmt.Errorf("keel-dev vsix protocol pin: %w", err)
	}
	findings = append(vsixProtocolCoverageGaps(families), findings...)
	if len(findings) > 0 {
		return fmt.Errorf("keel-dev vsix protocol pin: %s has drifted from the embedded keel/vscode schemas (keel/requirement-128):\n  %s",
			vsixProtocolRel, strings.Join(findings, "\n  "))
	}
	return nil
}

// embeddedSchemaFamilies lists every schema document family keel/vscode
// embeds, read from the embedded filesystem rather than from a second hand-kept
// list, so a family added to the wire cannot miss the coverage decision below.
func embeddedSchemaFamilies() ([]vscode.SchemaName, error) {
	entries, err := vscode.SchemasFS.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("read the embedded schema set: %w", err)
	}
	families := make([]vscode.SchemaName, 0, len(entries))
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok {
			continue
		}
		families = append(families, vscode.SchemaName(name))
	}
	return families, nil
}

// vsixProtocolCoverageGaps reports every embedded family that is neither
// covered by the pin nor named in its exemptions. keel/ac-484 asks for a
// coverage set that cannot silently shrink; this is what makes the exemption
// table load-bearing rather than a comment — a new schema family reds the gate
// until someone decides whether the VSIX parses it.
//
// DHF-REQ: keel/requirement-128
func vsixProtocolCoverageGaps(families []vscode.SchemaName) []string {
	covered := map[vscode.SchemaName]bool{}
	for _, entry := range vsixProtocolDriftCoverage {
		covered[entry.schema] = true
	}
	var gaps []string
	for _, family := range families {
		if covered[family] {
			continue
		}
		if _, exempt := vsixProtocolDriftExemptions[family]; exempt {
			continue
		}
		gaps = append(gaps, fmt.Sprintf("%s: the schema family is neither covered by the protocol pin nor named in its exemptions (keel/ac-484)", family))
	}
	sort.Strings(gaps)
	return gaps
}

// protocolSchema is the subset of JSON Schema the pin compares against. It is
// the same reading vscode/schema_drift_test.go takes on the Go side:
// structural contract only — property names, required-ness, declared type,
// closed enums, and const — not a full evaluator.
type protocolSchema struct {
	Type                 string                    `json:"type"`
	Ref                  string                    `json:"$ref"`
	Const                json.RawMessage           `json:"const"`
	Enum                 []json.RawMessage         `json:"enum"`
	Required             []string                  `json:"required"`
	Properties           map[string]protocolSchema `json:"properties"`
	AdditionalProperties json.RawMessage           `json:"additionalProperties"`
	Items                *protocolSchema           `json:"items"`
	Defs                 map[string]protocolSchema `json:"$defs"`
}

// protocolPin carries one family's comparison state.
type protocolPin struct {
	family       string
	root         protocolSchema
	declarations map[string]tsType
	findings     []string
}

// vsixProtocolDriftFindings compares each covered schema family against the
// protocol.ts declaration that parses it. A finding names the drifted property
// and the document family it belongs to (keel/ac-483); an error means the pin
// could not run at all, which is never a pass.
//
// DHF-REQ: keel/requirement-128
func vsixProtocolDriftFindings(schemas map[vscode.SchemaName][]byte, protocolSource string) ([]string, error) {
	declarations, err := parseTypeScriptDeclarations(protocolSource)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", vsixProtocolRel, err)
	}
	var findings []string
	for _, covered := range vsixProtocolDriftCoverage {
		body, ok := schemas[covered.schema]
		if !ok {
			return nil, fmt.Errorf("no embedded schema supplied for the %s family", covered.schema)
		}
		var root protocolSchema
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, fmt.Errorf("parse the %s schema: %w", covered.schema, err)
		}
		declared, ok := declarations[covered.tsType]
		if !ok {
			return nil, fmt.Errorf("%s declares no %s for the %s family", vsixProtocolRel, covered.tsType, covered.schema)
		}
		pin := &protocolPin{family: string(covered.schema), root: root, declarations: declarations}
		pin.compare("#", root, declared, covered.tsType, 0)
		findings = append(findings, pin.findings...)
	}
	return findings, nil
}

func (p *protocolPin) add(format string, args ...any) {
	p.findings = append(p.findings, p.family+": "+fmt.Sprintf(format, args...))
}

// resolveSchema follows a local $ref. Only the "#/$defs/<name>" form the
// embedded schemas use is understood; any other form is a finding.
func (p *protocolPin) resolveSchema(node protocolSchema) (protocolSchema, bool) {
	for hops := 0; node.Ref != ""; hops++ {
		if hops > protocolPinMaxDepth {
			return node, false
		}
		name, ok := strings.CutPrefix(node.Ref, "#/$defs/")
		if !ok {
			return node, false
		}
		next, ok := p.root.Defs[name]
		if !ok {
			return node, false
		}
		node = next
	}
	return node, true
}

// resolveTS follows a named declaration back to its definition.
func (p *protocolPin) resolveTS(declared tsType) (tsType, bool) {
	for hops := 0; declared.kind == tsRefKind; hops++ {
		if hops > protocolPinMaxDepth {
			return declared, false
		}
		next, ok := p.declarations[declared.name]
		if !ok {
			return declared, false
		}
		declared = next
	}
	return declared, true
}

// compare walks one schema node against the TypeScript type that reads it.
func (p *protocolPin) compare(path string, schema protocolSchema, declared tsType, tsPath string, depth int) {
	if depth > protocolPinMaxDepth {
		p.add("gave up at %s: the schema nests deeper than the pin walks", path)
		return
	}
	resolvedSchema, ok := p.resolveSchema(schema)
	if !ok {
		p.add("cannot resolve the schema reference %q at %s", schema.Ref, path)
		return
	}
	resolvedTS, ok := p.resolveTS(declared)
	if !ok {
		p.add("%s reads %s but declares no type %q", vsixProtocolRel, path, declared.name)
		return
	}
	if declared.kind == tsRefKind {
		// A named declaration is the clearest anchor a reader can jump to, so
		// it replaces the access path once the walk crosses into it.
		tsPath = declared.name
	}

	want := schemaJSONType(resolvedSchema)
	got, err := tsJSONType(resolvedTS)
	if err != nil {
		p.add("cannot read the TypeScript type of %s at %s: %v", tsPath, path, err)
		return
	}
	if want != "" && !jsonTypesAgree(want, got) {
		p.add("type drift on %s: the schema declares %q at %s, TypeScript declares %q", tsPath, want, path, got)
		return
	}

	p.compareEnum(path, resolvedSchema, resolvedTS, tsPath)
	p.compareConst(path, resolvedSchema, resolvedTS, tsPath)

	switch want {
	case "object":
		p.compareObject(path, resolvedSchema, resolvedTS, tsPath, depth)
	case "array":
		if resolvedSchema.Items != nil && resolvedTS.elem != nil {
			p.compare(path+"[]", *resolvedSchema.Items, *resolvedTS.elem, tsPath+"[]", depth+1)
		}
	}
}

func (p *protocolPin) compareObject(path string, schema protocolSchema, declared tsType, tsPath string, depth int) {
	if len(schema.Properties) == 0 {
		if len(schema.AdditionalProperties) == 0 {
			p.add("the schema object at %s declares no properties, so %s cannot be pinned to it", path, tsPath)
		}
		return
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	for _, name := range sortedSchemaProperties(schema) {
		member, ok := declared.member(name)
		if !ok {
			p.add("schema property %q at %s is not declared in TypeScript %s", name, path, tsPath)
			continue
		}
		if !required[name] && !member.optional {
			p.add("property %q at %s is optional in the schema but declared required in TypeScript %s", name, path, tsPath)
		}
		p.compare(path+"/"+name, schema.Properties[name], member.typ, tsPath+"."+name, depth+1)
	}
	for _, member := range declared.members {
		if _, ok := schema.Properties[member.name]; !ok {
			p.add("TypeScript %s declares property %q that the schema does not define at %s", tsPath, member.name, path)
		}
	}
}

// compareEnum holds the direction that matters for a consumer: where the
// TypeScript narrows a value to a set of literals, that set must be the
// schema's closed enum, or a producer that gains an enum member leaves the
// extension with a case it believes cannot occur. TypeScript that declares the
// plain primitive narrows nothing and is left alone.
func (p *protocolPin) compareEnum(path string, schema protocolSchema, declared tsType, tsPath string) {
	literals, narrowed := tsStringLiterals(declared)
	if !narrowed {
		return
	}
	members, closed := schemaEnumStrings(schema)
	if !closed {
		p.add("TypeScript %s narrows %s to %v, but the schema declares no closed enum there", tsPath, path, literals)
		return
	}
	sort.Strings(literals)
	sort.Strings(members)
	if strings.Join(literals, ",") != strings.Join(members, ",") {
		p.add("enum drift on %s: the schema declares %v at %s, TypeScript declares %v", tsPath, members, path, literals)
	}
}

// compareConst pins the version literals: a schema whose const moves without
// protocol.ts moving with it is a document the extension will reject or
// misread.
func (p *protocolPin) compareConst(path string, schema protocolSchema, declared tsType, tsPath string) {
	if declared.kind != tsStringLiteralKind && declared.kind != tsNumberLiteralKind {
		return
	}
	if len(schema.Const) == 0 {
		p.add("TypeScript %s pins %s to the literal %s, but the schema declares no const there", tsPath, path, declared.literal)
		return
	}
	want := strings.Trim(string(schema.Const), `"`)
	if want != declared.literal {
		p.add("const drift on %s: the schema declares %s at %s, TypeScript declares %s", tsPath, want, path, declared.literal)
	}
}

func sortedSchemaProperties(schema protocolSchema) []string {
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// schemaJSONType reports the JSON type a schema node describes, inferring it
// from a closed enum, declared properties, or declared items when the node
// leaves "type" out.
func schemaJSONType(schema protocolSchema) string {
	if schema.Type != "" {
		return schema.Type
	}
	if _, ok := schemaEnumStrings(schema); ok {
		return "string"
	}
	if len(schema.Properties) > 0 {
		return "object"
	}
	if schema.Items != nil {
		return "array"
	}
	return ""
}

func schemaEnumStrings(schema protocolSchema) ([]string, bool) {
	if len(schema.Enum) == 0 {
		return nil, false
	}
	members := make([]string, 0, len(schema.Enum))
	for _, raw := range schema.Enum {
		var member string
		if err := json.Unmarshal(raw, &member); err != nil {
			return nil, false
		}
		members = append(members, member)
	}
	return members, true
}

// tsJSONType reports the JSON type a TypeScript type expression reads.
func tsJSONType(declared tsType) (string, error) {
	switch declared.kind {
	case tsObjectKind:
		return "object", nil
	case tsArrayKind:
		return "array", nil
	case tsStringLiteralKind:
		return "string", nil
	case tsNumberLiteralKind:
		return "number", nil
	case tsPrimitiveKind:
		return declared.name, nil
	case tsUnionKind:
		var first string
		for _, option := range declared.options {
			optionType, err := tsJSONType(option)
			if err != nil {
				return "", err
			}
			if first == "" {
				first = optionType
			} else if !jsonTypesAgree(first, optionType) {
				return "", fmt.Errorf("union mixes %q and %q", first, optionType)
			}
		}
		return first, nil
	default:
		return "", fmt.Errorf("unresolved type reference %q", declared.name)
	}
}

// tsStringLiterals reports the literal set a TypeScript type narrows a string
// to, and whether it narrows at all.
func tsStringLiterals(declared tsType) ([]string, bool) {
	switch declared.kind {
	case tsStringLiteralKind:
		return []string{declared.literal}, true
	case tsUnionKind:
		literals := make([]string, 0, len(declared.options))
		for _, option := range declared.options {
			if option.kind != tsStringLiteralKind {
				return nil, false
			}
			literals = append(literals, option.literal)
		}
		return literals, len(literals) > 0
	default:
		return nil, false
	}
}

// jsonTypesAgree treats the schema's integer/number split as one TypeScript
// number, which is the only widening TypeScript forces.
func jsonTypesAgree(schemaType, tsType string) bool {
	if schemaType == "integer" {
		schemaType = "number"
	}
	return schemaType == tsType
}
