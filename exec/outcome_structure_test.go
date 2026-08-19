package exec_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// adapterSources are the two CLI adapters that must reach their success-or-
// failure verdict through the shared contract rather than through a rule of
// their own.
var adapterSources = map[string]string{
	"keel/exec/codex":  filepath.Join("codex", "codexcli.go"),
	"keel/exec/claude": filepath.Join("claude", "claudecli.go"),
}

// outcomeFactFields are the two per-CLI facts the shared contract consumes. An
// adapter may read and expose them; it may not branch on them, because a branch
// on either field is a second, independent expression of the outcome rule — the
// drift that produced keel/issue-162.
var outcomeFactFields = map[string]bool{
	"ExitCode": true,
	"IsError":  true,
}

// TestAdaptersReachTheirVerdictThroughTheSharedOutcomeContract proves each CLI
// adapter calls the one shared keel/exec outcome function, and that neither
// adapter carries a second expression of the rule: no branch anywhere in the
// adapter may be conditioned on the child's exit code or on the terminal
// event's failure flag.
//
// The DecideCLIOutcome assertion is also this test's positive control: if the
// parse or the AST walk below silently stopped matching, that assertion fails
// before the absence assertions can report a false clean.
//
// DHF-TEST: keel/requirement-134
func TestAdaptersReachTheirVerdictThroughTheSharedOutcomeContract(t *testing.T) {
	for pkg, rel := range adapterSources {
		t.Run(pkg, func(t *testing.T) {
			file, fset := parseAdapterSource(t, rel)

			var sharedCalls int
			var ownRules []string
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CallExpr:
					if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "DecideCLIOutcome" {
						sharedCalls++
					}
				case *ast.IfStmt:
					ast.Inspect(node.Cond, func(c ast.Node) bool {
						sel, ok := c.(*ast.SelectorExpr)
						if ok && outcomeFactFields[sel.Sel.Name] {
							ownRules = append(ownRules,
								fset.Position(sel.Pos()).String()+": branch on "+sel.Sel.Name)
						}
						return true
					})
				}
				return true
			})

			if sharedCalls != 1 {
				t.Fatalf("%s calls the shared DecideCLIOutcome %d times, want exactly 1", rel, sharedCalls)
			}
			if len(ownRules) > 0 {
				t.Fatalf("%s still decides the outcome itself:\n\t%s", rel, strings.Join(ownRules, "\n\t"))
			}
		})
	}
}

func parseAdapterSource(t *testing.T, rel string) (*ast.File, *token.FileSet) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	source := filepath.Join(filepath.Dir(thisFile), rel)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, source, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}
	return parsed, fset
}
