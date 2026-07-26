package worktree_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentCLINames are the agent-CLI names keel knows. None of them may appear in
// keel/worktree's import path, exported identifiers, exported doc comments,
// default field values, or error strings.
var agentCLINames = []string{"codex", "claude"}

// TestPublicSurfaceNamesNoAgentCLI scans the package's own non-test sources for
// any agent-CLI name in an exported identifier, a doc comment, or a string
// literal (which covers both default field values and error text), and asserts
// the import path itself is neutral.
//
// DHF-TEST: keel/requirement-113 (keel/ac-399)
func TestPublicSurfaceNamesNoAgentCLI(t *testing.T) {
	const importPath = "github.com/david-aggeler/keel/worktree"
	for _, name := range agentCLINames {
		if strings.Contains(strings.ToLower(importPath), name) {
			t.Errorf("import path %q names the agent CLI %q", importPath, name)
		}
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	var scanned int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		report := func(pos token.Pos, kind, text string) {
			for _, agent := range agentCLINames {
				if strings.Contains(strings.ToLower(text), agent) {
					t.Errorf("%s: %s names the agent CLI %q: %q", fset.Position(pos), kind, agent, text)
				}
			}
		}
		for _, group := range file.Comments {
			report(group.Pos(), "doc comment", group.Text())
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				if node.IsExported() {
					report(node.Pos(), "exported identifier", node.Name)
				}
			case *ast.BasicLit:
				if node.Kind == token.STRING {
					report(node.Pos(), "string literal", node.Value)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no non-test Go sources found in the package directory")
	}
}
