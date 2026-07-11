package vscode

import "testing"

// DHF-TEST: keel/requirement-23
func TestGoRunEventTestIDUsesModulePathInsteadOfDomainHeuristic(t *testing.T) {
	selection := GoSelection{Kind: "test", Pkg: "internal/sample", TestName: "TestSelected"}
	event := GoTestJSONEvent{Package: "example.org/openbrain/internal/sample", Test: "TestSelected"}
	got := GoRunEventTestID(selection, event, "go::test::internal/sample::TestSelected", "example.org/openbrain")
	if got != "go::test::internal/sample::TestSelected" {
		t.Fatalf("GoRunEventTestID = %q", got)
	}

	external := GoTestJSONEvent{Package: "gopkg.in/check.v1", Test: "TestSuite"}
	got = GoRunEventTestID(selection, external, "go::test::internal/sample::TestSelected", "example.org/openbrain")
	if got != "go::test::internal/sample::TestSuite" {
		t.Fatalf("external package should fall back to selected package, got %q", got)
	}
}
