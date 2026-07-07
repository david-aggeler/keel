package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// runLint executes keel's in-process lint policies over the module rooted at
// dir. Like openbrain's devtool lints, these are AST scans compiled into the
// gate itself — no external lint binary, so CI stays hermetic.
//
// Policies:
//
//   - no-stdlib-log: the stdlib "log" package must not be imported anywhere in
//     the module (log/slog is fine) — diagnostics flow through keel/log.
//
//   - no-raw-fmt-output: cmd/keel-dev must not print run output via
//     fmt.Print*/Fprint* (ac-29: no raw fmt fallback); the static usage text in
//     main.go is the single allowlisted exception.
//
//   - no-raw-stdout-stream: cmd/keel-dev must not reference os.Stdout/os.Stderr
//     outside logger construction and the usage-text printer (keel/ac-36) —
//     handing the raw stream to a child bypasses the keel/log console sink and
//     its redaction path (keel/issue-2).
//
// DHF-REQ: keel/requirement-10, keel/requirement-11
func runLint(dir string) error {
	var violations []string

	v, err := scanNoStdlibLog(dir)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawFmtOutput(filepath.Join(dir, "cmd", "keel-dev"))
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawStdoutStream(filepath.Join(dir, "cmd", "keel-dev"))
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	if len(violations) > 0 {
		sort.Strings(violations)
		return fmt.Errorf("lint: %d violation(s):\n%s", len(violations), strings.Join(violations, "\n"))
	}
	return nil
}

// scanNoStdlibLog reports every import of the stdlib "log" package in the
// module. keel/log is the logging surface; stdlib log bypasses redaction and
// the G1 schema.
func scanNoStdlibLog(root string) ([]string, error) {
	var violations []string
	err := walkGoFiles(root, func(path string, file *ast.File, fset *token.FileSet) {
		for _, imp := range file.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "log" {
				pos := fset.Position(imp.Pos())
				violations = append(violations,
					fmt.Sprintf("  no-stdlib-log: %s:%d imports stdlib log — use keel/log (log/slog is allowed)", relPath(root, path), pos.Line))
			}
		}
	})
	return violations, err
}

// rawFmtFuncs are the fmt functions that write program output. Sprint* and
// Errorf construct values and are fine.
var rawFmtFuncs = map[string]bool{
	"Print": true, "Println": true, "Printf": true,
	"Fprint": true, "Fprintln": true, "Fprintf": true,
}

// scanNoRawFmtOutput reports fmt print calls in keel-dev outside the usage-text
// allowlist (printUsage in main.go, which emits static help, not run output).
// A tree without cmd/keel-dev (e.g. lint fixtures in tests) has nothing to scan.
func scanNoRawFmtOutput(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	var violations []string
	err := walkGoFiles(dir, func(path string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			// Allowlist: the printUsage function and the unknown-flag refusal in
			// run() are static help/diagnostic text, not run output.
			if fn, ok := n.(*ast.FuncDecl); ok && filepath.Base(path) == "main.go" &&
				(fn.Name.Name == "printUsage" || fn.Name.Name == "run") {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" || !rawFmtFuncs[sel.Sel.Name] {
				return true
			}
			pos := fset.Position(call.Pos())
			violations = append(violations,
				fmt.Sprintf("  no-raw-fmt-output: %s:%d calls fmt.%s — route run output through keel/log", filepath.Base(path), pos.Line, sel.Sel.Name))
			return true
		})
	})
	return violations, err
}

// stdoutAllowlist names the only functions in cmd/keel-dev permitted to touch
// os.Stdout/os.Stderr: logger construction (the writers keel/log wraps) and the
// static usage-text printer. Everything else must go through the logger.
var stdoutAllowlist = map[string]bool{
	"newLogger":          true,
	"newLoggerWithFiles": true,
	"printUsage":         true,
	"run":                true, // unknown-flag refusal precedes logger construction
}

// scanNoRawStdoutStream reports os.Stdout/os.Stderr references in cmd/keel-dev
// outside the allowlist (keel/ac-36). A tree without cmd/keel-dev has nothing
// to scan.
func scanNoRawStdoutStream(dir string) ([]string, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	var violations []string
	err := walkGoFiles(dir, func(path string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if filepath.Base(path) == "main.go" && stdoutAllowlist[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "os" || (sel.Sel.Name != "Stdout" && sel.Sel.Name != "Stderr") {
					return true
				}
				pos := fset.Position(sel.Pos())
				violations = append(violations,
					fmt.Sprintf("  no-raw-stdout-stream: %s:%d references os.%s in %s — surface output through keel/log (lineLogWriter)", filepath.Base(path), pos.Line, sel.Sel.Name, fn.Name.Name))
				return true
			})
		}
	})
	return violations, err
}

// walkGoFiles parses every non-test .go file under root (skipping vendor,
// testdata, and hidden directories) and hands the AST to visit.
func walkGoFiles(root string, visit func(path string, file *ast.File, fset *token.FileSet)) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "bin") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("lint: parse %s: %w", path, err)
		}
		visit(path, file, fset)
		return nil
	})
}

func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
