package vscode

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// donorModulePrefix is the import-path prefix the neutral engine must never
// carry. The engine is donation-ready to keel/vscode only while every source
// in this package imports nothing under it.
var donorModulePrefix = "go.aggeler" + ".com/vela"

// TestEngineImportsNoVelaPackage is the req-201 AC2 hard bar: the extracted VS
// Code run engine and its per-framework projector sources import no
// the donor module package. It parses every Go source in this package
// (production and test) and fails on any import spec under the donor module.
// String literals that merely mention the module path (e.g. test fixtures) are
// not imports and are ignored — only import specs are inspected.
//
// DHF-TEST: keel/requirement-23
func TestEngineImportsNoVelaPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	inspected := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		inspected++
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
			}
			if path == donorModulePrefix || strings.HasPrefix(path, donorModulePrefix+"/") {
				t.Errorf("%s imports %q; the neutral engine must import no %s package", name, path, donorModulePrefix)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("import-set guard inspected no Go sources; the guard is not actually running")
	}
}
