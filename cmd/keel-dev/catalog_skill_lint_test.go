package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-114 (keel/ac-433)
func TestKeelDevSourcesDoNotReferenceCatalogOwnedSkillPaths(t *testing.T) {
	dotClaude := string([]byte{'.', 'c', 'l', 'a', 'u', 'd', 'e'})
	blockedPath := filepath.ToSlash(filepath.Join(dotClaude, "skills", "change-request"))

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		literals := map[string]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s has an invalid string literal %s: %v", entry.Name(), lit.Value, err)
			}
			literals[value] = true
			if strings.Contains(filepath.ToSlash(value), blockedPath) {
				t.Fatalf("%s references the catalog-owned change-request skill path", entry.Name())
			}
			return true
		})
		if literals[dotClaude] && literals["skills"] && literals["change-request"] {
			t.Fatalf("%s composes the catalog-owned change-request skill path", entry.Name())
		}
	}
}
