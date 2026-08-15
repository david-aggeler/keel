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

// DHF-TEST: keel/requirement-122
func TestRequestLoggerGodocStatesTheSilentDefault(t *testing.T) {
	doc := requestFieldDoc(t, "Logger")

	if strings.Contains(doc, "slog.Default") {
		t.Errorf("Request.Logger godoc still claims a slog.Default fallback:\n%s", doc)
	}
	if !strings.Contains(strings.ToLower(doc), "no output") {
		t.Errorf("Request.Logger godoc does not state that a nil value produces no output:\n%s", doc)
	}
}

func requestFieldDoc(t *testing.T, field string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	source := filepath.Join(filepath.Dir(thisFile), "procexec.go")

	parsed, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", source, err)
	}

	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "Request" {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("Request is %T, want struct", typeSpec.Type)
			}
			for _, f := range structType.Fields.List {
				for _, name := range f.Names {
					if name.Name == field && f.Doc != nil {
						return f.Doc.Text()
					}
				}
			}
		}
	}
	t.Fatalf("no doc comment found for Request.%s", field)
	return ""
}
