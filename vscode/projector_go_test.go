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

// DHF-TEST: keel/requirement-71
func TestGoRunEventTestIDKeysPackageEventsForRootSelection(t *testing.T) {
	selection := GoSelection{Kind: "root", Pkg: "..."}
	event := GoTestJSONEvent{Package: "example.org/openbrain/internal/sample"}
	if got := GoRunEventTestID(selection, event, "go::root", "example.org/openbrain"); got != "go::pkg::internal/sample" {
		t.Fatalf("root-selection package event = %q, want go::pkg::internal/sample", got)
	}

	external := GoTestJSONEvent{Package: "gopkg.in/check.v1"}
	if got := GoRunEventTestID(selection, external, "go::root", "example.org/openbrain"); got != "go::root" {
		t.Fatalf("external package should fall back to the selected id, got %q", got)
	}
}

// DHF-TEST: keel/requirement-71
func TestGoJSONResultBelongsToSelectionAcceptsPerTestResultsForRootAndPackage(t *testing.T) {
	perTest := GoTestJSONEvent{Package: "example.org/openbrain/internal/sample", Test: "TestSample"}
	perPackage := GoTestJSONEvent{Package: "example.org/openbrain/internal/sample"}
	for _, selection := range []GoSelection{{Kind: "root", Pkg: "..."}, {Kind: "package", Pkg: "internal/sample"}} {
		if !GoJSONResultBelongsToSelection(selection, perTest) {
			t.Fatalf("%s selection must own per-test results", selection.Kind)
		}
		if !GoJSONResultBelongsToSelection(selection, perPackage) {
			t.Fatalf("%s selection must own package-level results", selection.Kind)
		}
	}

	fileSelection := GoSelection{Kind: "file", Pkg: "internal/sample", TestNames: []string{"TestOther"}}
	if GoJSONResultBelongsToSelection(fileSelection, perTest) {
		t.Fatal("file selection must not own results for tests outside the file")
	}
	testSelection := GoSelection{Kind: "test", Pkg: "internal/sample", TestName: "TestOther"}
	if GoJSONResultBelongsToSelection(testSelection, perTest) {
		t.Fatal("test selection must not own results for other tests")
	}
}
