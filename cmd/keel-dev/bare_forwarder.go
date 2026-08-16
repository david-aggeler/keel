package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// bareForwarder is one candidate: an unexported package-level function whose
// whole body forwards, unchanged, to an exported function of the same package.
type bareForwarder struct {
	name   string // the unexported function's name
	target string // the exported function it forwards to
	file   string // module-root-relative path of the declaring file
	line   int
	decl   *ast.Ident // the declaration's name node, excluded from the caller scan
}

// scanNoBareForwarders reports unexported package-level functions whose entire
// body is a call to a same-package exported function, forwarding their
// parameters unchanged, and which no non-test file in the package calls
// (keel/ac-497).
//
// The shape is the residue of an unexport-then-re-export round trip: an
// unexported name survives the re-export and stays alive only because tests in
// its own package still call it, which keeps the advisory deadcode step blind
// to it (keel/design_decision-1, keel/issue-9) and lets a test assert against a
// shim rather than the consumer-visible surface.
//
// The policy deliberately matches nothing else. A wrapper that adapts an
// argument, adds a statement, reorders parameters, targets another package or
// an unexported name, hangs off a receiver, or has a production caller is a
// real construct and is out of scope — over-matching here would delete working
// code, so every one of those is a near miss the tests pin.
//
// DHF-REQ: keel/requirement-33
func scanNoBareForwarders(root string, files []string) ([]string, error) {
	// Package scope is directory scope, so candidates and their callers are
	// resolved per directory. visitGoFiles already drops _test.go, which is
	// exactly the "no non-test caller" leg of the criterion.
	byDir := make(map[string][]string, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		byDir[filepath.Dir(filepath.FromSlash(file))] = append(byDir[filepath.Dir(filepath.FromSlash(file))], file)
	}

	var violations []string
	for _, dir := range sortedDirs(byDir) {
		var (
			exported   = map[string]bool{}
			candidates []bareForwarder
			parsed     []*ast.File
		)
		err := visitGoFiles(root, byDir[dir], func(path string, file *ast.File, fset *token.FileSet) {
			parsed = append(parsed, file)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				if fn.Name.IsExported() {
					exported[fn.Name.Name] = true
					continue
				}
				target, ok := forwardedTarget(fn)
				if !ok {
					continue
				}
				pos := fset.Position(fn.Pos())
				candidates = append(candidates, bareForwarder{
					name:   fn.Name.Name,
					target: target,
					file:   path,
					line:   pos.Line,
					decl:   fn.Name,
				})
			}
		})
		if err != nil {
			return nil, err
		}
		for _, c := range candidates {
			// The target must be declared in this same package; a bare
			// identifier that resolves elsewhere (dot import) is not the shape.
			if !exported[c.target] {
				continue
			}
			if referencedIn(parsed, c) {
				continue
			}
			violations = append(violations, fmt.Sprintf(
				"  no-bare-forwarder: %s:%d declares unexported %s whose whole body forwards to %s with no non-test caller — call %s directly and delete the wrapper (keel/requirement-33, keel/ac-497)",
				c.file, c.line, c.name, c.target, c.target))
		}
	}
	return violations, nil
}

// forwardedTarget reports the exported function a candidate forwards to. It
// holds only for a single-statement body that is one call — with or without a
// return — to an unqualified exported identifier, passing every declared
// parameter, in order, untouched.
func forwardedTarget(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return "", false
	}
	if fn.Type.TypeParams != nil {
		return "", false // a generic shim is not the shape
	}
	var call *ast.CallExpr
	switch stmt := fn.Body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(stmt.Results) != 1 {
			return "", false
		}
		c, ok := stmt.Results[0].(*ast.CallExpr)
		if !ok {
			return "", false
		}
		call = c
	case *ast.ExprStmt:
		c, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		call = c
	default:
		return "", false
	}
	target, ok := call.Fun.(*ast.Ident)
	if !ok || !target.IsExported() {
		return "", false
	}
	if !argsForwardParams(fn, call) {
		return "", false
	}
	return target.Name, true
}

// argsForwardParams reports whether the call passes exactly the declared
// parameters, in declaration order, as bare identifiers. A transformed,
// reordered, dropped, or added argument fails.
func argsForwardParams(fn *ast.FuncDecl, call *ast.CallExpr) bool {
	var params []string
	variadic := false
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			if len(field.Names) == 0 {
				return false // an unnamed parameter cannot be forwarded by name
			}
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				variadic = true
			}
			for _, name := range field.Names {
				if name.Name == "_" {
					return false
				}
				params = append(params, name.Name)
			}
		}
	}
	if len(call.Args) != len(params) {
		return false
	}
	// A variadic parameter must be spread; a non-variadic one must not be.
	if (call.Ellipsis != token.NoPos) != variadic {
		return false
	}
	for i, arg := range call.Args {
		id, ok := arg.(*ast.Ident)
		if !ok || id.Name != params[i] {
			return false
		}
	}
	return true
}

// referencedIn reports whether any non-test file of the package mentions the
// candidate other than at its own declaration.
func referencedIn(files []*ast.File, c bareForwarder) bool {
	found := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SelectorExpr:
				// A field or method of the same name is a different symbol.
				ast.Inspect(node.X, func(inner ast.Node) bool {
					if id, ok := inner.(*ast.Ident); ok && id.Name == c.name && id != c.decl {
						found = true
					}
					return !found
				})
				return false
			case *ast.Ident:
				if node.Name == c.name && node != c.decl {
					found = true
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// sortedDirs orders the scanned package directories so violations are reported
// deterministically.
func sortedDirs(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
