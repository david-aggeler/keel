package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
)

// DHF-TEST: keel/requirement-107
func TestKeelDevCommandTreeUsesCLIPositionalsAndValidateTree(t *testing.T) {
	tree := commandTree()
	if err := tree.ValidateTree(); err != nil {
		t.Fatalf("keel-dev command tree failed ValidateTree: %v", err)
	}

	for _, tc := range []struct {
		path []string
		want cli.PositionalSpec
	}{
		{path: []string{"ci"}, want: cli.PositionalSpec{Name: "args", Min: 0, Max: 0}},
		{path: []string{"release"}, want: cli.PositionalSpec{Name: "version", Min: 1, Max: 1}},
		{path: []string{"verify"}, want: cli.PositionalSpec{Name: "version", Min: 1, Max: 1}},
	} {
		spec := commandSpecByPath(tree, tc.path...)
		if spec == nil {
			t.Fatalf("missing command %s", strings.Join(tc.path, " "))
		}
		if len(spec.Positionals) != 1 || spec.Positionals[0] != tc.want {
			t.Fatalf("%s positionals = %+v, want %+v", strings.Join(tc.path, " "), spec.Positionals, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-107
func TestTestBridgeCommandTreeIsFlatAndUsesCLIBoundFlags(t *testing.T) {
	tree := commandTree()
	testBridge := commandSpecByPath(tree, "test-bridge")
	if testBridge == nil {
		t.Fatal("missing test-bridge command")
	}

	got := commandLeafUses(testBridge)
	want := []string{
		"test-bridge config-init",
		"test-bridge config-upgrade",
		"test-bridge discover [--format json]",
		"test-bridge desired-state [--format json] [--id test-id]",
		"test-bridge run [--dry-run] --id test-id",
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("test-bridge leaves = %#v, want %#v", got, want)
	}

	assertStringFlagTarget(t, commandSpecByPath(tree, "test-bridge", "discover"), "format", []string{"json"}, false)
	assertStringFlagTarget(t, commandSpecByPath(tree, "test-bridge", "desired-state"), "format", []string{"json"}, false)
	assertStringSliceFlagTarget(t, commandSpecByPath(tree, "test-bridge", "desired-state"), "id", true, false)
	assertStringSliceFlagTarget(t, commandSpecByPath(tree, "test-bridge", "run"), "id", true, true)
	assertBoolFlagTarget(t, commandSpecByPath(tree, "test-bridge", "run"), "dry-run")
}

// DHF-TEST: keel/requirement-107
func TestKeelDevMigrationRemovesHandRolledArgParsing(t *testing.T) {
	for _, fn := range []string{"handleCI", "handleRelease", "handleVerify"} {
		if callsBuiltinLenOnArgs(t, "commands.go", fn) {
			t.Fatalf("%s still performs a hand-rolled len(args) arity check", fn)
		}
	}
	for _, fn := range []string{"handleDiscover", "handleDesiredState", "handleRun"} {
		if scansFlagShapedArgs(t, "../../testbridge/testbridge.go", fn) {
			t.Fatalf("%s still scans flag-shaped args manually", fn)
		}
	}
}

func assertStringFlagTarget(t *testing.T, spec *cli.CommandSpec, name string, enum []string, required bool) {
	t.Helper()
	flag, ok := flagByName(spec, name)
	if !ok {
		t.Fatalf("%s missing --%s", spec.Use, name)
	}
	if flag.StringTarget == nil {
		t.Fatalf("%s --%s StringTarget is nil", spec.Use, name)
	}
	if !stringSlicesEqual(flag.Enum, enum) {
		t.Fatalf("%s --%s enum = %#v, want %#v", spec.Use, name, flag.Enum, enum)
	}
	if flag.Required != required {
		t.Fatalf("%s --%s required = %t, want %t", spec.Use, name, flag.Required, required)
	}
}

func assertStringSliceFlagTarget(t *testing.T, spec *cli.CommandSpec, name string, repeatable, required bool) {
	t.Helper()
	flag, ok := flagByName(spec, name)
	if !ok {
		t.Fatalf("%s missing --%s", spec.Use, name)
	}
	if flag.StringSliceTarget == nil {
		t.Fatalf("%s --%s StringSliceTarget is nil", spec.Use, name)
	}
	if flag.Repeatable != repeatable {
		t.Fatalf("%s --%s repeatable = %t, want %t", spec.Use, name, flag.Repeatable, repeatable)
	}
	if flag.Required != required {
		t.Fatalf("%s --%s required = %t, want %t", spec.Use, name, flag.Required, required)
	}
}

func assertBoolFlagTarget(t *testing.T, spec *cli.CommandSpec, name string) {
	t.Helper()
	flag, ok := flagByName(spec, name)
	if !ok {
		t.Fatalf("%s missing --%s", spec.Use, name)
	}
	if flag.BoolTarget == nil {
		t.Fatalf("%s --%s BoolTarget is nil", spec.Use, name)
	}
}

func flagByName(spec *cli.CommandSpec, name string) (cli.FlagSpec, bool) {
	if spec == nil {
		return cli.FlagSpec{}, false
	}
	for _, flag := range spec.Flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return cli.FlagSpec{}, false
}

func callsBuiltinLenOnArgs(t *testing.T, path, fnName string) bool {
	t.Helper()
	fn := parseFuncDecl(t, path, fnName)
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "len" || len(call.Args) != 1 {
			return true
		}
		arg, ok := call.Args[0].(*ast.Ident)
		if ok && arg.Name == "args" {
			found = true
			return false
		}
		return true
	})
	return found
}

func scansFlagShapedArgs(t *testing.T, path, fnName string) bool {
	t.Helper()
	fn := parseFuncDecl(t, path, fnName)
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HasPrefix" || len(call.Args) != 2 {
			return true
		}
		prefix, ok := call.Args[1].(*ast.BasicLit)
		if ok && prefix.Value == `"--"` {
			found = true
			return false
		}
		return true
	})
	return found
}

func parseFuncDecl(t *testing.T, path, fnName string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == fnName {
			return fn
		}
	}
	t.Fatalf("missing function %s in %s", fnName, path)
	return nil
}
