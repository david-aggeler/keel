package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/david-aggeler/keel/vscode"
)

// The schema half of the protocol pin used to hold the opposite line from the
// TypeScript half: protocolSchema modelled nine keywords and every other
// keyword unmarshalled into nothing and was dropped without a trace, so a
// covered family could carry a wire property the pin never compared while the
// gate reported green. vsix_protocol_ts.go:9-14 states the posture both halves
// owe the reader — a construct the reader cannot model must red the gate rather
// than be skipped into a false green — and this file is the schema half of it.
//
// The scan below is deliberately independent of the type walk in
// vsix_protocol_drift.go. The walk descends only where it can type a node, so a
// keyword sitting in a subtree the walk never enters would stay invisible to a
// check hung off the walk; the scan reads the whole document instead.

// protocolModelledKeywords is the single enumeration of the JSON Schema
// keywords the pin compares. It is pinned to protocolSchema's json tags by
// TestProtocolModelledKeywordsMatchTheModelledStruct, so a field added to the
// struct without an entry here — or the reverse — reds the gate rather than
// letting the guard and the reader drift apart.
//
// DHF-REQ: keel/requirement-128
var protocolModelledKeywords = map[string]bool{
	"type":                 true,
	"$ref":                 true,
	"const":                true,
	"enum":                 true,
	"required":             true,
	"properties":           true,
	"additionalProperties": true,
	"items":                true,
	"$defs":                true,
}

// protocolDataKeywords names the modelled keywords whose value is document
// data rather than a nested schema. The scan must not walk into them, or a
// const object's own field names would be read as keywords.
var protocolDataKeywords = map[string]bool{
	"type":     true,
	"const":    true,
	"enum":     true,
	"required": true,
	"$ref":     true,
}

// protocolSchemaMapKeywords names the keywords whose value is a map from an
// author-chosen name to a schema. Their keys are names, never keywords, so the
// scan steps through them rather than reading them as a node. A keyword of this
// shape that is not listed here still reds the gate on its own name; the noise
// its generic descent then produces is the reader's cue, not a silent pass.
var protocolSchemaMapKeywords = map[string]bool{
	"properties":        true,
	"$defs":             true,
	"patternProperties": true,
	"dependentSchemas":  true,
	"definitions":       true,
}

// vsixProtocolKeywordExemption disposes of one keyword the pin does not model.
// An entry with an empty family and path exempts the keyword across every
// covered family; an entry naming both exempts only that site, so the same
// keyword appearing anywhere else still reds the gate.
//
// The distinction is the point. A keyword that constrains a value inside a JSON
// type has no TypeScript counterpart anywhere, so pinning its location buys
// nothing. A keyword that states a rule about the wire — the conditional
// constraint desired-state.json places on a mutually-exclusive group — is
// exempt only because that one instance has nothing to compare against; a
// second instance is a new decision and must be made, not inherited.
type vsixProtocolKeywordExemption struct {
	keyword string
	family  vscode.SchemaName
	path    string
	reason  string
}

// vsixProtocolKeywordExemptions is the disposal decision for every unmodelled
// keyword the embedded schemas carry today. Each entry names one keyword and
// says why it has no counterpart in vsix/src/protocol.ts. There is deliberately
// no wildcard entry: a keyword absent from this table reds the gate, which is
// the whole of keel/ac-499.
//
// DHF-REQ: keel/requirement-128
var vsixProtocolKeywordExemptions = []vsixProtocolKeywordExemption{
	// Identity and annotation. These describe the document, not the wire shape
	// it carries, so protocol.ts declares nothing they could drift from.
	{keyword: "$schema", reason: "names the JSON Schema dialect the document is written in; it constrains no instance, so protocol.ts has nothing to declare against it"},
	{keyword: "$id", reason: "the document's own identity, pinned to the schema set by vscode/schema_drift_test.go; it constrains no instance"},
	{keyword: "title", reason: "prose annotation; it constrains no instance"},
	{keyword: "description", reason: "prose annotation; it constrains no instance"},
	{keyword: "$comment", reason: "prose annotation addressed to schema readers; it constrains no instance"},

	// Value-domain constraints. Each narrows a value inside a JSON type the pin
	// already compares. TypeScript's type system expresses no value ranges,
	// lengths, formats, or element uniqueness, so there is no declaration in
	// protocol.ts any of these could be pinned to — narrowing string to a
	// shorter string is still string.
	{keyword: "format", reason: "narrows a string's value domain (date-time, uri); TypeScript declares string either way, so there is no counterpart to pin"},
	{keyword: "pattern", reason: "narrows a string to a regular expression; TypeScript declares string either way, so there is no counterpart to pin"},
	{keyword: "minLength", reason: "narrows a string's length; TypeScript declares string either way, so there is no counterpart to pin"},
	{keyword: "minimum", reason: "narrows a number's range; TypeScript declares number either way, so there is no counterpart to pin"},
	{keyword: "minItems", reason: "narrows an array's length; TypeScript declares T[] either way, so there is no counterpart to pin"},
	{keyword: "uniqueItems", reason: "requires an array's elements to differ; TypeScript declares T[] either way, so there is no counterpart to pin"},

	// The one-active-row constraint on a mutually-exclusive desired-state group
	// (keel/issue-166). vsix/src/protocol.ts declares DesiredStateGroup.rows as
	// DesiredState[] with an optional active?: boolean — TypeScript cannot
	// express "exactly one element has active: true", so the constraint has no
	// counterpart to drift from. Checked against protocol.ts before this
	// exemption was recorded, per the unit's plan-time gate; exempted at these
	// sites only, so the same keywords elsewhere on the wire still red.
	{keyword: "allOf", family: vscode.SchemaDesiredState, path: "#/$defs/group",
		reason: "carries the conditional one-active-row constraint below; TypeScript declares no cardinality rule over rows, so there is nothing to pin it to"},
	{keyword: "if", family: vscode.SchemaDesiredState, path: "#/$defs/group/allOf/0",
		reason: "guards the constraint on mutually_exclusive: true; a validation rule, with no TypeScript counterpart"},
	{keyword: "then", family: vscode.SchemaDesiredState, path: "#/$defs/group/allOf/0",
		reason: "states the constraint the guard applies; a validation rule, with no TypeScript counterpart"},
	{keyword: "contains", family: vscode.SchemaDesiredState, path: "#/$defs/group/allOf/0/then/properties/rows",
		reason: "selects the active row inside rows; TypeScript declares active?: boolean per row and no rule across them"},
	{keyword: "minContains", family: vscode.SchemaDesiredState, path: "#/$defs/group/allOf/0/then/properties/rows",
		reason: "requires at least one active row; TypeScript's type system expresses no array cardinality"},
	{keyword: "maxContains", family: vscode.SchemaDesiredState, path: "#/$defs/group/allOf/0/then/properties/rows",
		reason: "allows at most one active row; TypeScript's type system expresses no array cardinality"},
}

// vsixProtocolKeywordExempt reports whether the keyword is disposed of at this
// site by the table above.
func vsixProtocolKeywordExempt(family vscode.SchemaName, path, keyword string) bool {
	for _, exemption := range vsixProtocolKeywordExemptions {
		if exemption.keyword != keyword {
			continue
		}
		if exemption.family != "" && exemption.family != family {
			continue
		}
		if exemption.path != "" && exemption.path != path {
			continue
		}
		return true
	}
	return false
}

// vsixProtocolKeywordFindings reports every keyword one covered schema document
// carries that the pin neither models nor exempts, naming the keyword and the
// schema path. It reads the raw document rather than protocolSchema, because
// the whole defect is that protocolSchema drops what it has no field for.
//
// DHF-REQ: keel/requirement-128
func vsixProtocolKeywordFindings(family vscode.SchemaName, document []byte) ([]string, error) {
	var root any
	if err := json.Unmarshal(document, &root); err != nil {
		return nil, fmt.Errorf("parse the %s schema: %w", family, err)
	}
	var findings []string
	var walkNode func(path string, node any, depth int)

	walkNode = func(path string, node any, depth int) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if depth > protocolPinMaxDepth {
			findings = append(findings, fmt.Sprintf("%s: gave up at %s: the schema nests deeper than the pin walks", family, path))
			return
		}
		names := make([]string, 0, len(object))
		for name := range object {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !protocolModelledKeywords[name] && !vsixProtocolKeywordExempt(family, path, name) {
				findings = append(findings, fmt.Sprintf(
					"%s: the schema node at %s carries the keyword %q, which the protocol pin does not model, so %s is not checked against it (keel/ac-499)",
					family, path, name, vsixProtocolRel))
			}
			if protocolDataKeywords[name] {
				continue
			}
			value := object[name]
			if protocolSchemaMapKeywords[name] {
				members, ok := value.(map[string]any)
				if !ok {
					continue
				}
				memberNames := make([]string, 0, len(members))
				for member := range members {
					memberNames = append(memberNames, member)
				}
				sort.Strings(memberNames)
				for _, member := range memberNames {
					walkNode(path+"/"+name+"/"+member, members[member], depth+1)
				}
				continue
			}
			switch typed := value.(type) {
			case map[string]any:
				walkNode(path+"/"+name, typed, depth+1)
			case []any:
				for index, element := range typed {
					walkNode(fmt.Sprintf("%s/%s/%d", path, name, index), element, depth+1)
				}
			}
		}
	}

	walkNode("#", root, 0)
	return findings, nil
}

// vsixProtocolStaleKeywordExemptions reports every site-scoped exemption whose
// site no longer carries the keyword it excuses. An exemption that outlives the
// construct it was written for is a standing hole in the guard, so it reds the
// gate the same way an undecided keyword does.
//
// DHF-REQ: keel/requirement-128
func vsixProtocolStaleKeywordExemptions(schemas map[vscode.SchemaName][]byte) ([]string, error) {
	live := map[string]bool{}
	for family, document := range schemas {
		var root any
		if err := json.Unmarshal(document, &root); err != nil {
			return nil, fmt.Errorf("parse the %s schema: %w", family, err)
		}
		var collect func(path string, node any, depth int)
		collect = func(path string, node any, depth int) {
			object, ok := node.(map[string]any)
			if !ok || depth > protocolPinMaxDepth {
				return
			}
			for name, value := range object {
				live[string(family)+" "+path+" "+name] = true
				if protocolDataKeywords[name] {
					continue
				}
				if protocolSchemaMapKeywords[name] {
					members, ok := value.(map[string]any)
					if !ok {
						continue
					}
					for member, memberValue := range members {
						collect(path+"/"+name+"/"+member, memberValue, depth+1)
					}
					continue
				}
				switch typed := value.(type) {
				case map[string]any:
					collect(path+"/"+name, typed, depth+1)
				case []any:
					for index, element := range typed {
						collect(fmt.Sprintf("%s/%s/%d", path, name, index), element, depth+1)
					}
				}
			}
		}
		collect("#", root, 0)
	}
	var stale []string
	for _, exemption := range vsixProtocolKeywordExemptions {
		if exemption.family == "" || exemption.path == "" {
			continue
		}
		if live[string(exemption.family)+" "+exemption.path+" "+exemption.keyword] {
			continue
		}
		stale = append(stale, fmt.Sprintf(
			"%s: the keyword exemption for %q at %s no longer matches any schema node, so it excuses nothing and must be removed (keel/ac-499)",
			exemption.family, exemption.keyword, exemption.path))
	}
	sort.Strings(stale)
	return stale, nil
}
