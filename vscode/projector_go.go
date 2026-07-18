package vscode

import "path/filepath"

// GoTestJSONEvent is one decoded line of `go test -json` output.
type GoTestJSONEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// GoRunEventTestID resolves the VS Code test item id a `go test -json` event
// belongs to, given the selection it was produced for and the module path.
//
// DHF-REQ: keel/requirement-23
func GoRunEventTestID(selection GoSelection, event GoTestJSONEvent, selectedID, modulePath string) string {
	if event.Test != "" {
		pkg := GoEventPackageRel(event.Package, modulePath)
		if pkg == "" {
			pkg = selection.Pkg
		}
		return "go::test::" + filepath.ToSlash(pkg) + "::" + event.Test
	}
	if selection.TestName == "" && event.Package != "" {
		pkg := GoEventPackageRel(event.Package, modulePath)
		if pkg != "" && (selection.Kind == "package" || selection.Kind == "root") {
			return "go::pkg::" + filepath.ToSlash(pkg)
		}
	}
	return selectedID
}

// OutputBelongsToGoSelection reports whether a `go test -json` output line
// should be surfaced for the given selection.
//
// DHF-REQ: keel/requirement-23
func OutputBelongsToGoSelection(selection GoSelection, event GoTestJSONEvent) bool {
	if selection.Kind == "file" && event.Test != "" {
		return StringInSlice(selection.TestNames, event.Test)
	}
	return selection.TestName == "" || event.Test == "" || event.Test == selection.TestName
}

// GoJSONResultBelongsToSelection reports whether a `go test -json` result event
// counts toward the given selection. Root and package selections own every
// result their run produced — package-level and per-test alike — so that each
// started descendant settles under its own id (requirement-71 AC 6).
//
// DHF-REQ: keel/requirement-23, keel/requirement-71
func GoJSONResultBelongsToSelection(selection GoSelection, event GoTestJSONEvent) bool {
	if selection.Kind == "root" || selection.Kind == "package" {
		return true
	}
	if selection.Kind == "file" {
		return event.Test != "" && StringInSlice(selection.TestNames, event.Test)
	}
	if selection.TestName != "" {
		return event.Test == selection.TestName
	}
	return event.Test == ""
}
