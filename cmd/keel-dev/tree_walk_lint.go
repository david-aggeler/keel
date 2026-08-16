package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// treeWalkSelectors are the filesystem-selection calls a test can use to build
// its own evaluated set. Each one takes the directory or pattern it selects
// from as its first argument, which is what the policy inspects.
var treeWalkSelectors = map[string]map[string]bool{
	"filepath": {"WalkDir": true, "Walk": true, "Glob": true},
	"os":       {"ReadDir": true},
}

// scanNoTestOwnedTreeWalk reports a tracked _test.go file that selects files by
// walking the filesystem from a root outside its own package directory.
//
// This is the leg that closes the class rather than the instance. keel/ac-265,
// keel/ac-268 and keel/ac-455 each bound the file-selecting sites that existed
// when they were written — gate steps, then in-process lint policies — and an
// assertion owned by a test was outside all of them. keel/issue-171 is what
// that gap cost: a test-owned walk rooted at the module root evaluated
// gitignored worktrees/ and scratchpad/ content and reded the gate on main.
// A gate-reached evaluation must take its file set from the shared
// tracked-files-then-excludes selector, not from a walk of the working tree
// (keel/requirement-85).
//
// Scope is deliberate. A walk that stays inside the test's own package cannot
// reach the sanctioned scratch directories, so `filepath.WalkDir(".")` and
// `filepath.Glob(filepath.Join(dir, "*.go"))` over a runtime.Caller-derived
// package dir are left alone — vscode/traceability_test.go and
// log/api_surface_test.go are the calibration set for that. So is a walk over a
// fixture tree the test built itself, whose root reaches the call as a variable
// (cmd/keel-dev/keeldev_test.go's lintFixtureFiles).
//
// Residual: the policy resolves a walk root only when it is statically
// determinable — a string literal, a filepath.Join of literals, or a local
// assigned one. A root computed at run time is not reported. That is the shape
// keel/change_request-198 actually wrote (`moduleRoot := ".."`) and the shape
// the next hand-rolled structural assertion will write, but it is a floor, not
// a proof of absence.
//
// DHF-REQ: keel/requirement-85 (keel/ac-501)
func scanNoTestOwnedTreeWalk(root string, files []string) ([]string, error) {
	var violations []string
	fset := token.NewFileSet()
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			continue
		}
		rel := filepath.FromSlash(file)
		parsed, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("lint: parse %s: %w", rel, err)
		}
		bindings := pathBindings(parsed)
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			pkg, fn, ok := selectorCall(call)
			if !ok || !treeWalkSelectors[pkg][fn] {
				return true
			}
			start, ok := staticPathArg(call.Args[0], bindings, map[string]bool{})
			if !ok || !pathEscapesPackage(start) {
				return true
			}
			pos := fset.Position(call.Pos())
			violations = append(violations, fmt.Sprintf(
				"  no-test-owned-tree-walk: %s:%d calls %s.%s rooted at %q, outside its own package — a gate-reached walk of the working tree evaluates gitignored worktrees/ and scratchpad/ content; take the file set from the gate's tracked-files selector instead (keel/requirement-85, keel/ac-501)",
				rel, pos.Line, pkg, fn, start))
			return true
		})
	}
	return violations, nil
}

// selectorCall decomposes pkg.Fn(...) into its package and function name.
func selectorCall(call *ast.CallExpr) (string, string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", "", false
	}
	return pkg.Name, sel.Sel.Name, true
}

// pathBindings collects every expression the file binds to each identifier. It
// is deliberately file-wide and ignores scope: resolution below requires all of
// an identifier's bindings to agree, so a name that is assigned different paths
// on different branches resolves to nothing rather than to the wrong one.
func pathBindings(file *ast.File) map[string][]ast.Expr {
	bound := map[string][]ast.Expr{}
	record := func(names []ast.Expr, values []ast.Expr) {
		if len(names) != len(values) {
			return
		}
		for i, name := range names {
			if id, ok := name.(*ast.Ident); ok {
				bound[id.Name] = append(bound[id.Name], values[i])
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.AssignStmt:
			record(d.Lhs, d.Rhs)
		case *ast.ValueSpec:
			names := make([]ast.Expr, 0, len(d.Names))
			for _, name := range d.Names {
				names = append(names, name)
			}
			record(names, d.Values)
		}
		return true
	})
	return bound
}

// staticPathArg resolves a walk-root expression to a path when it is
// statically determinable: a string literal, a filepath.Join whose every
// argument resolves, or an identifier whose bindings all resolve to the same
// path. seen breaks self- and mutually-referential bindings (root = root+"/x").
func staticPathArg(expr ast.Expr, bindings map[string][]ast.Expr, seen map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return stringLiteral(e)
	case *ast.Ident:
		if seen[e.Name] {
			return "", false
		}
		values, ok := bindings[e.Name]
		if !ok || len(values) == 0 {
			return "", false
		}
		seen[e.Name] = true
		defer delete(seen, e.Name)
		resolved := ""
		for i, value := range values {
			path, ok := staticPathArg(value, bindings, seen)
			if !ok || (i > 0 && path != resolved) {
				return "", false
			}
			resolved = path
		}
		return resolved, true
	case *ast.CallExpr:
		pkg, fn, ok := selectorCall(e)
		if !ok || pkg != "filepath" || fn != "Join" || len(e.Args) == 0 {
			return "", false
		}
		parts := make([]string, 0, len(e.Args))
		for _, arg := range e.Args {
			part, ok := staticPathArg(arg, bindings, seen)
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		return filepath.Join(parts...), true
	}
	return "", false
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// pathEscapesPackage reports whether a walk root leaves the directory of the
// test that names it — an absolute path, or one that climbs out with "..".
func pathEscapesPackage(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	cleaned := filepath.ToSlash(filepath.Clean(p))
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}
