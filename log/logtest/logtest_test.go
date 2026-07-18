package logtest_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/log/logtest"
)

// DHF-TEST: keel/requirement-56
func TestCaptureRecordsJSONThroughStandardNew(t *testing.T) {
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Info("first", "answer", 42)
	logger.Warn("second")

	last := capture.LastJSON()
	if last == nil {
		t.Fatal("LastJSON returned nil")
	}
	if got := last["msg"]; got != "second" {
		t.Fatalf("LastJSON msg = %v, want second", got)
	}

	all := capture.AllJSON()
	if len(all) != 2 {
		t.Fatalf("AllJSON returned %d records, want 2", len(all))
	}
	if got := all[0]["msg"]; got != "first" {
		t.Fatalf("AllJSON[0] msg = %v, want first", got)
	}
	if got := all[0]["service"]; got != "svc" {
		t.Fatalf("AllJSON[0] service = %v, want svc", got)
	}
	if got := all[0]["answer"]; got != float64(42) {
		t.Fatalf("AllJSON[0] answer = %v, want 42", got)
	}

	capture.Reset()
	if got := capture.LastJSON(); got != nil {
		t.Fatalf("LastJSON after Reset = %v, want nil", got)
	}
	if got := capture.AllJSON(); len(got) != 0 {
		t.Fatalf("AllJSON after Reset returned %d records, want 0", len(got))
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-56
func TestCaptureWithGroupNestsFutureAttributes(t *testing.T) {
	capture := logtest.NewCapture()
	grouped := slog.New(capture.WithGroup("request")).With("id", "req-1")
	grouped.Info("handled")

	last := capture.LastJSON()
	request, ok := last["request"].(map[string]any)
	if !ok || request["id"] != "req-1" {
		t.Fatalf("request group = %#v in record %#v, want id=req-1", last["request"], last)
	}
}

// DHF-TEST: keel/requirement-56
func TestCaptureOnlyConfigDoesNotWriteConsoleAndStillRedacts(t *testing.T) {
	var console bytes.Buffer
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		Writer:   &console,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	secret := "Bearer capture-only-token"
	logger.Info("login failed "+secret, "token", secret, "dsn", "postgres://user:password@db/app")

	if got := strings.TrimSpace(console.String()); got != "" {
		t.Fatalf("console output = %q, want empty capture-only config", got)
	}
	last := capture.LastJSON()
	if last == nil {
		t.Fatal("LastJSON returned nil")
	}
	for name, value := range map[string]string{
		"msg":   stringValue(last["msg"]),
		"token": stringValue(last["token"]),
		"dsn":   stringValue(last["dsn"]),
	} {
		if strings.Contains(value, "capture-only-token") || strings.Contains(value, "password") {
			t.Fatalf("%s leaked secret in captured record: %q", name, value)
		}
	}
	if !strings.Contains(stringValue(last["token"]), "[REDACTED]") {
		t.Fatalf("token attr = %q, want redaction marker", stringValue(last["token"]))
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// DHF-TEST: keel/requirement-56
func TestLogtestPublicSurfaceIsCaptureOnly(t *testing.T) {
	exports := logtestExports(t)
	for _, name := range []string{"NewForTesting", "NewConsoleForTesting", "RecordCapture"} {
		if exports[name] {
			t.Fatalf("%s is exported from log/logtest; capture must be wired through log.New", name)
		}
	}
	for _, name := range []string{"Capture", "NewCapture"} {
		if !exports[name] {
			t.Fatalf("%s is missing from log/logtest public surface", name)
		}
	}
}

func logtestExports(t *testing.T) map[string]bool {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob logtest package: %v", err)
	}
	exports := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil && decl.Name.IsExported() {
					exports[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.TypeSpec); ok && spec.Name.IsExported() {
						exports[spec.Name.Name] = true
					}
				}
			}
		}
	}
	return exports
}
