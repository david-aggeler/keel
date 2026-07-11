package log_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-33
func TestPublicAPISurfaceMatchesMinimalLoggerContract(t *testing.T) {
	pkg := parseLogPackage(t)

	rejectedExports := []string{
		"ResolveGitCommit",
		"NewHumanFileHandler",
		"NewJSONFileHandler",
		"HumanLogPath",
		"JSONLogPath",
		"PerRunJSONLogPath",
		"RecentBuffer",
		"RecentEntry",
		"NewRecentBuffer",
		"TeeRecent",
		"RecordCapture",
		"DefaultRecentCapacity",
		"LogBuildIdentity",
		"StartDailyBuildIdentity",
		"Discard",
		"ParseLevel",
		"RedactString",
		"FromContext",
		"WithLogger",
		"Metric",
		"MetricKind",
		"EventToolCall",
		"Header",
		"Section",
		"Field",
		"Fields",
		"Emit",
	}
	for _, name := range rejectedExports {
		if pkg.exports[name] {
			t.Errorf("%s is still exported from keel/log", name)
		}
	}

	for _, field := range []string{"HumanFileHandler", "JSONFileHandler"} {
		if pkg.configFields[field] {
			t.Errorf("Config.%s is still exported; public construction should use TextDir/JSONLDir", field)
		}
	}
	for _, field := range []string{"TextDir", "JSONLDir"} {
		if !pkg.configFields[field] {
			t.Errorf("Config.%s is missing from the public file-sink configuration", field)
		}
	}

	requiredLoggerMethods := []string{
		"Header",
		"Section",
		"Field",
		"Fields",
		"Emit",
		"LogBuildIdentity",
	}
	for _, name := range requiredLoggerMethods {
		if !pkg.loggerMethods[name] {
			t.Errorf("Logger.%s is not available", name)
		}
	}
}

type logPackageSurface struct {
	exports       map[string]bool
	configFields  map[string]bool
	loggerMethods map[string]bool
}

func parseLogPackage(t *testing.T) logPackageSurface {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob log package: %v", err)
	}

	fset := token.NewFileSet()
	surface := logPackageSurface{
		exports:       map[string]bool{},
		configFields:  map[string]bool{},
		loggerMethods: map[string]bool{},
	}
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
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(spec.Name.Name) {
							surface.exports[spec.Name.Name] = true
						}
						if spec.Name.Name == "Config" {
							fields, ok := spec.Type.(*ast.StructType)
							if !ok {
								t.Fatalf("Config is %T, want struct", spec.Type)
							}
							for _, field := range fields.Fields.List {
								for _, name := range field.Names {
									if ast.IsExported(name.Name) {
										surface.configFields[name.Name] = true
									}
								}
							}
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if ast.IsExported(name.Name) {
								surface.exports[name.Name] = true
							}
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Recv == nil {
					if ast.IsExported(decl.Name.Name) {
						surface.exports[decl.Name.Name] = true
					}
					continue
				}
				if receiverName(decl.Recv.List[0].Type) == "Logger" && ast.IsExported(decl.Name.Name) {
					surface.loggerMethods[decl.Name.Name] = true
				}
			}
		}
	}
	return surface
}

func receiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return receiverName(expr.X)
	default:
		return ""
	}
}
