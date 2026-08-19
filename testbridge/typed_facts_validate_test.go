package testbridge

import (
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

// TestValidateDiscoveryRefusesTypedFactsEncodedAsProse holds keel/ac-552 at the
// boundary a document actually passes through: ValidateDocument is what every
// producer's discovery document is stamped by before it is written, so the
// refusal has to red there and not only in the check it delegates to.
//
// DHF-TEST: keel/requirement-138
func TestValidateDiscoveryRefusesTypedFactsEncodedAsProse(t *testing.T) {
	doc := typedFactsDiscoveryDoc(vscode.TestItem{
		ID:          "demo::group::data-set",
		Label:       "app-db data set",
		Kind:        "group",
		Description: "mutually_exclusive=true",
	})
	err := ValidateDocument(doc)
	if err == nil {
		t.Fatal("ValidateDocument accepted a description that re-encodes a typed fact")
	}
	for _, want := range []string{"mutually_exclusive=", "desired_state_group.mutually_exclusive"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateDocument err = %v, want it to name %q", err, want)
		}
	}
}

// TestValidateDiscoveryRefusesUnknownFindingSeverity holds the closed-enum half
// of keel/ac-551: severity is the member a consumer selects a surface by, so an
// off-enum value must not reach the wire.
//
// DHF-TEST: keel/requirement-138
func TestValidateDiscoveryRefusesUnknownFindingSeverity(t *testing.T) {
	doc := typedFactsDiscoveryDoc(vscode.TestItem{
		ID:       "demo::lane::one",
		Label:    "one",
		Kind:     "lane",
		Findings: []vscode.Finding{{Rule: "V6", Severity: "nagging", Message: "no test-bearing packages"}},
	})
	err := ValidateDocument(doc)
	if err == nil {
		t.Fatal("ValidateDocument accepted an off-enum finding severity")
	}
	if !strings.Contains(err.Error(), "nagging") {
		t.Errorf("ValidateDocument err = %v, want it to name the invalid severity", err)
	}
}

// TestValidateDiscoveryAcceptsTypedFacts guards against the refusals above
// turning into a blanket rejection of the carriage they police.
//
// DHF-TEST: keel/requirement-138
func TestValidateDiscoveryAcceptsTypedFacts(t *testing.T) {
	measured := int64(9800)
	exitOne := 1
	doc := typedFactsDiscoveryDoc(vscode.TestItem{
		ID:          "demo::lane::one",
		Label:       "one",
		Kind:        "lane",
		Description: "the keel/log package",
		Findings:    []vscode.Finding{{Rule: "V6", Severity: "warning", Message: "no test-bearing packages"}},
		LastRun:     &vscode.LastRunFacts{At: time.Now().UTC(), DurationMS: &measured, ExitCode: &exitOne},
	})
	if err := ValidateDocument(doc); err != nil {
		t.Fatalf("ValidateDocument rejected a well-formed typed-fact item: %v", err)
	}
}

func typedFactsDiscoveryDoc(items ...vscode.TestItem) vscode.DiscoveryDocument {
	return vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   "workspace",
		ModulePath:  "example.com/mod",
		GeneratedAt: time.Now().UTC(),
		Items:       items,
	}
}
