package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-68
func TestContextLoggerExamplesAreDocumented(t *testing.T) {
	t.Helper()

	fset := token.NewFileSet()
	examples, err := parser.ParseFile(fset, "example_test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse examples: %v", err)
	}

	for _, name := range []string{"ExampleWithLogger", "ExampleFromContext"} {
		if !declaresFunc(examples, name) {
			t.Fatalf("missing runnable godoc example %s", name)
		}
	}

	doc, err := parser.ParseFile(fset, "doc.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse package doc: %v", err)
	}
	docText := doc.Doc.Text()
	for _, want := range []string{"WithLogger", "FromContext", "context.Context"} {
		if !strings.Contains(docText, want) {
			t.Fatalf("package doc does not mention %q in the request-scoped logger note", want)
		}
	}
}

func declaresFunc(file *ast.File, name string) bool {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return true
		}
	}
	return false
}

// ExampleNew shows constructing a JSON logger over a caller-supplied writer and
// reading back the single record it emits. Production callers leave Writer nil
// to log to os.Stdout; here a bytes.Buffer captures the output so the example is
// deterministic.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleNew() {
	var buf bytes.Buffer
	logger, err := logging.New(logging.Config{Service: "demo", Console: logging.ConsoleJSON, Writer: &buf})
	if err != nil {
		fmt.Println(err)
		return
	}

	logger.Info("service starting", "port", 8080)

	var rec map[string]any
	_ = json.Unmarshal(buf.Bytes(), &rec)
	fmt.Println(rec["service"], rec["level"], rec["msg"], rec["port"])
	// Output: demo INFO service starting 8080
}

// DHF-TEST: keel/requirement-68
func ExampleWithLogger() {
	var buf bytes.Buffer
	ctx := logging.WithLogger(context.Background(), exampleTextLogger(&buf).With("request_id", "req-123"))

	logging.FromContext(ctx).InfoContext(ctx, "handled")

	fmt.Println(strings.TrimSpace(buf.String()))
	// Output: level=INFO msg=handled request_id=req-123
}

// DHF-TEST: keel/requirement-68
func ExampleFromContext() {
	var buf bytes.Buffer
	ctx := logging.WithLogger(context.Background(), exampleTextLogger(&buf))

	logging.FromContext(ctx).InfoContext(ctx, "done", "attempt", 2)

	fmt.Println(strings.TrimSpace(buf.String()))
	// Output: level=INFO msg=done attempt=2
}

func exampleTextLogger(w *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}
