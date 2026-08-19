package vscode

import (
	"strings"
	"testing"
)

// TestRenderDescriptionOmitsErrorSeverityFindings holds the negative half of
// keel/ac-559, which is the half that matters: an error-severity finding is
// routed to the error surface, and a text on both surfaces would be reported
// twice. The warning stays where it was — it is the one severity the composed
// description carries.
//
// DHF-TEST: keel/requirement-140
func TestRenderDescriptionOmitsErrorSeverityFindings(t *testing.T) {
	item := TestItem{
		ID:    "keel::lane::a",
		Label: "a",
		Kind:  "lane",
		Findings: []Finding{
			{Rule: "lane-prereq", Severity: "error", Message: "resource is unavailable"},
			{Rule: "lane-order", Severity: "warning", Message: "order drifted"},
		},
	}

	got := RenderDescription(item, DefaultDisplayConfig())

	if strings.Contains(got, "resource is unavailable") {
		t.Fatalf("RenderDescription() = %q, want the error-severity finding routed away from the description (keel/ac-559)", got)
	}
	if !strings.Contains(got, "lane-order warning: order drifted") {
		t.Fatalf("RenderDescription() = %q, want the warning-severity finding in the description (keel/ac-559)", got)
	}
}

// TestHasRenderableFactsIgnoresErrorSeverityFindings keeps the two answers
// consistent: a fact the renderer will not render is not a renderable fact, or
// a consumer would read "the display is switched off" from an item whose only
// finding routes elsewhere.
//
// DHF-TEST: keel/requirement-140
func TestHasRenderableFactsIgnoresErrorSeverityFindings(t *testing.T) {
	item := TestItem{
		ID:       "keel::lane::a",
		Label:    "a",
		Kind:     "lane",
		Findings: []Finding{{Rule: "lane-prereq", Severity: "error", Message: "resource is unavailable"}},
	}

	if HasRenderableFacts(item) {
		t.Fatalf("HasRenderableFacts() = true, want false for an item whose only finding routes to the error surface (keel/ac-559)")
	}
}
