package worktree

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
)

// DHF-TEST: keel/requirement-114 (keel/ac-413)
func TestExitCodeTaxonomyCoversEveryDeclaredErrorCode(t *testing.T) {
	declared := declaredErrorCodes(t)
	documented := map[ErrorCode]string{}
	for _, row := range ExitCodeTaxonomy() {
		documented[row.Code] = row.Meaning
	}

	for name, code := range declared {
		if documented[code] == "" {
			t.Fatalf("ExitCodeTaxonomy missing %s=%d", name, code)
		}
	}
	for code := range documented {
		found := false
		for _, declaredCode := range declared {
			if code == declaredCode {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ExitCodeTaxonomy documents %d, but no ErrorCode constant declares it", code)
		}
	}
}

func declaredErrorCodes(t *testing.T) map[string]ErrorCode {
	t.Helper()
	body, err := os.ReadFile("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", body, 0)
	if err != nil {
		t.Fatal(err)
	}

	codes := map[string]ErrorCode{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			name := value.Names[0].Name
			if len(name) < len("Code") || name[:len("Code")] != "Code" {
				continue
			}
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "ErrorCode" {
				continue
			}
			lit, ok := value.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				t.Fatalf("%s must declare an explicit integer ErrorCode value", name)
			}
			n, err := strconv.Atoi(lit.Value)
			if err != nil {
				t.Fatalf("parse %s value %q: %v", name, lit.Value, err)
			}
			codes[name] = ErrorCode(n)
		}
	}
	if len(codes) == 0 {
		t.Fatal("no ErrorCode constants found in errors.go")
	}
	return codes
}
