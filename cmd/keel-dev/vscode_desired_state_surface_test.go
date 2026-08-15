package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// desiredStateConstructionPrefix names the function class this file guards:
// every top-level constructor of a desired-state surface in cmd/keel-dev. The
// check is stated over the class, not over one symbol, so it stays meaningful
// after the test-only twin is deleted — a re-added second surface trips it.
const desiredStateConstructionPrefix = "buildVSCodeDesiredState"

// TestDesiredStateConstructionFunctionsHaveNonTestCallers proves cmd/keel-dev
// keeps exactly one desired-state construction path: the one production
// reaches. A constructor whose only references are test-file lines is a
// divergence sink — nothing forces it to agree with the live path, and the
// coverage it earns is spent on code no consumer can reach.
//
// DHF-TEST: keel/requirement-125 (keel/ac-476)
func TestDesiredStateConstructionFunctionsHaveNonTestCallers(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read cmd/keel-dev: %v", err)
	}

	fset := token.NewFileSet()
	constructors := map[string]token.Pos{}
	nonTestRefs := map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, desiredStateConstructionPrefix) {
				continue
			}
			constructors[fn.Name.Name] = fn.Name.Pos()
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok || !strings.HasPrefix(ident.Name, desiredStateConstructionPrefix) {
				return true
			}
			nonTestRefs[ident.Name]++
			return true
		})
	}

	if len(constructors) == 0 {
		t.Fatalf("no %s* constructor found in cmd/keel-dev — the check would be vacuous", desiredStateConstructionPrefix)
	}

	for name, pos := range constructors {
		// The declaration itself contributes one non-test reference; a caller
		// outside _test.go files is anything beyond that.
		if nonTestRefs[name] < 2 {
			t.Fatalf("%s (%s) has no non-test caller: a desired-state constructor reachable only from tests drifts unchecked beside the live path",
				name, fset.Position(pos))
		}
	}
}
