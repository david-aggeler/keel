package term_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ioctlFuncs are the package's descriptor-taking ioctl wrappers. Each call to
// one must run inside a SyscallConn Control callback.
var ioctlFuncs = []string{"isTerminal", "windowSize"}

// TestIoctlRunsInsideControlCallback pins that every ioctl in term/ receives
// its descriptor inside the Control callback, so the file holds the descriptor
// open for the whole call. A descriptor copied out of the callback can be
// released by a finalizer or a concurrent Close and reused by another file.
// The race cannot be forced hermetically, so the check is on the syntax tree:
// each call to an ioctl wrapper must sit in a function literal passed to a
// Control method, or to a package helper that forwards it to Control.
//
// DHF-TEST: keel/requirement-165 (keel/ac-730)
func TestIoctlRunsInsideControlCallback(t *testing.T) {
	fset := token.NewFileSet()
	var files []*ast.File
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		files = append(files, f)
	}

	sinks := controlSinks(files)
	found := map[string]int{}
	for _, f := range files {
		checkIoctlCalls(t, fset, f, sinks, found)
	}
	// Positive control: an empty match must not pass.
	for _, name := range ioctlFuncs {
		if found[name] == 0 {
			t.Errorf("found no call to %s in term/; the check matched nothing", name)
		}
	}
}

// controlSinks returns, for each package function that forwards a func-typed
// parameter to a Control method, the index of that parameter.
func controlSinks(files []*ast.File) map[string]int {
	sinks := map[string]int{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			params := paramNames(fn.Type)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isControlCall(call) || len(call.Args) != 1 {
					return true
				}
				if id, ok := call.Args[0].(*ast.Ident); ok {
					if i, ok := params[id.Name]; ok {
						sinks[fn.Name.Name] = i
					}
				}
				return true
			})
		}
	}
	return sinks
}

func paramNames(ft *ast.FuncType) map[string]int {
	out := map[string]int{}
	i := 0
	for _, field := range ft.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, n := range field.Names {
			out[n.Name] = i
			i++
		}
	}
	return out
}

func isControlCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Control"
}

// checkIoctlCalls walks f with a stack of enclosing nodes and reports every
// ioctl-wrapper call that no Control callback encloses.
func checkIoctlCalls(t *testing.T, fset *token.FileSet, f *ast.File, sinks map[string]int, found map[string]int) {
	t.Helper()
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && isIoctlFunc(id.Name) {
				found[id.Name]++
				if !insideCallback(stack, sinks) {
					t.Errorf("%s: %s runs outside a SyscallConn Control callback", fset.Position(call.Pos()), id.Name)
				}
			}
		}
		stack = append(stack, n)
		return true
	})
}

func isIoctlFunc(name string) bool {
	for _, n := range ioctlFuncs {
		if n == name {
			return true
		}
	}
	return false
}

// insideCallback reports whether any function literal on the stack is passed
// as an argument to a Control call or to a sink helper at its callback index.
func insideCallback(stack []ast.Node, sinks map[string]int) bool {
	for i := len(stack) - 1; i > 0; i-- {
		lit, ok := stack[i].(*ast.FuncLit)
		if !ok {
			continue
		}
		call, ok := stack[i-1].(*ast.CallExpr)
		if !ok {
			continue
		}
		for argIdx, arg := range call.Args {
			if arg != lit {
				continue
			}
			if isControlCall(call) {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				if idx, ok := sinks[id.Name]; ok && idx == argIdx {
					return true
				}
			}
		}
	}
	return false
}
