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
//   - no-raw-fmt-output: cmd/keel-dev plus the library surface (log, exec)
//     must not print run output via fmt.Print*/Fprint* (ac-29, ac-54: no raw
//     fmt fallback); the static usage text in main.go is the single
//     allowlisted exception.
//
//   - no-raw-stdout-stream: cmd/keel-dev must not reference os.Stdout/os.Stderr
//     outside logger construction and the usage-text printer (keel/ac-36) —
//     handing the raw stream to a child bypasses the keel/log console sink and
//     its redaction path (keel/issue-2).
//
//   - no-undocumented-exports: every exported identifier in the library
//     packages (log, exec, exec/claude, exec/codex) must carry a doc comment —
//     go doc is keel's sole consumer-facing behavioral contract, so an
//     undocumented export is a hole in it (keel/ac-46, keel/ac-49).
//
//   - no-retired-desired-state-vocabulary: the desired-state protocol and
//     devtool surfaces must not reintroduce the retired pre-rename vocabulary
//     (keel/requirement-77).
//
// DHF-REQ: keel/requirement-10, keel/requirement-11
func runLint(dir string) error {
	var violations []string

	v, err := scanNoStdlibLog(dir)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawFmtOutput(dir)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawStdoutStream(filepath.Join(dir, "cmd", "keel-dev"))
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoUndocumentedExports(dir)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRetiredDesiredStateVocabulary(dir)
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

var retiredDesiredStateVocabularyDirs = []string{
	"vscode",
	"testbridge",
	filepath.Join("cmd", "keel-dev"),
	filepath.Join("cmd", "keel-demo-dev"),
	filepath.Join("vsix", "src"),
}

var retiredDesiredStateVocabularyTerms = []string{
	string([]byte{83, 101, 116, 117, 112, 80, 108, 97, 110}),
	string([]byte{98, 117, 105, 108, 100, 86, 83, 67, 111, 100, 101, 80, 108, 97, 110}),
	string([]byte{115, 101, 116, 117, 112, 45, 112, 108, 97, 110}),
}

// scanNoRetiredDesiredStateVocabulary rejects the retired desired-state document
// names in the protocol and devtool surfaces. Test files are skipped so the
// policy can carry planted-occurrence tests without failing itself.
//
// DHF-REQ: keel/requirement-77
func scanNoRetiredDesiredStateVocabulary(root string) ([]string, error) {
	var violations []string
	for _, sub := range retiredDesiredStateVocabularyDirs {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "bin" || name == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".test.ts") {
				return nil
			}
			if !retiredDesiredStateVocabularyFile(path) {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(body)
			for _, term := range retiredDesiredStateVocabularyTerms {
				if line := firstLineContaining(text, term); line > 0 {
					violations = append(violations, fmt.Sprintf("  no-retired-desired-state-vocabulary: %s:%d contains %q — use DesiredState-rooted naming only (keel/requirement-77)", relPath(root, path), line, term))
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

func retiredDesiredStateVocabularyFile(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".js", ".ts", ".json":
		return true
	default:
		return false
	}
}

func firstLineContaining(text, term string) int {
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, term) {
			return i + 1
		}
	}
	return 0
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

var rawFmtDirs = []string{filepath.Join("cmd", "keel-dev"), "log", "exec"}

// scanNoRawFmtOutput reports fmt print calls in keel-dev and library packages
// outside the usage-text allowlist (printUsage in main.go, which emits static
// help, not run output). Missing roots are ignored for small lint fixtures.
func scanNoRawFmtOutput(root string) ([]string, error) {
	var violations []string
	for _, sub := range rawFmtDirs {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := walkGoFiles(dir, func(path string, file *ast.File, fset *token.FileSet) {
			ast.Inspect(file, func(n ast.Node) bool {
				// Allowlist: the printUsage function and the unknown-flag refusal
				// in run() are static help/diagnostic text, not run output.
				if fn, ok := n.(*ast.FuncDecl); ok && relPath(root, path) == filepath.Join("cmd", "keel-dev", "main.go") &&
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
					fmt.Sprintf("  no-raw-fmt-output: %s:%d calls fmt.%s — route run output through keel/log", relPath(root, path), pos.Line, sel.Sel.Name))
				return true
			})
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

// stdoutAllowlist names the only (file, function) pairs in cmd/keel-dev
// permitted to touch os.Stdout/os.Stderr: logger construction (the writers
// keel/log wraps), the static usage-text printer, and the sole VS Code protocol
// JSONL stream. Everything else must go through the logger.
var stdoutAllowlist = map[string]bool{
	fileFunc("main.go", "buildLogger"):       true,
	fileFunc("main.go", "loggerConfig"):      true, // base logger config (console writer)
	fileFunc("main.go", "newLogger"):         true,
	fileFunc("main.go", "newProtocolStream"): true,
	fileFunc("main.go", "printUsage"):        true,
	fileFunc("main.go", "run"):               true, // unknown-flag refusal precedes logger construction
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
			if stdoutAllowlist[fileFunc(filepath.Base(path), fn.Name.Name)] {
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

func fileFunc(file, fn string) string {
	return file + ":" + fn
}

// libraryDocDirs are the module-root-relative library package roots whose
// exported identifiers must each carry a doc comment (keel/ac-49). cmd/keel-dev
// is intentionally excluded: it is the internal devtool, not part of keel's
// consumer-facing API. exec is walked recursively, so exec/claude and exec/codex
// are covered.
var libraryDocDirs = []string{"log", "exec", "cli"}

// scanNoUndocumentedExports reports every exported identifier (function, type,
// method on an exported type, struct field, const, or var) in keel's library
// packages that lacks a doc comment. go doc is the sole behavioral contract keel
// offers its consumers (keel/ac-46), so an undocumented export is a hole in that
// contract; this machine-enforces the floor that a comment exists (keel/ac-49).
// Comment quality remains a review concern. Test files are skipped by walkGoFiles.
func scanNoUndocumentedExports(root string) ([]string, error) {
	var violations []string
	for _, sub := range libraryDocDirs {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := walkGoFiles(dir, func(path string, file *ast.File, fset *token.FileSet) {
			for _, decl := range file.Decls {
				violations = append(violations, undocumentedInDecl(root, path, fset, decl)...)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

// undocumentedInDecl reports the exported identifiers declared by one top-level
// declaration that carry no doc comment.
func undocumentedInDecl(root, path string, fset *token.FileSet, decl ast.Decl) []string {
	var out []string
	report := func(pos token.Pos, kind, name string) {
		p := fset.Position(pos)
		out = append(out, fmt.Sprintf("  no-undocumented-exports: %s:%d exported %s %s has no doc comment — go doc is keel's consumer contract (keel/ac-46, keel/ac-49)",
			relPath(root, path), p.Line, kind, name))
	}
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if !d.Name.IsExported() {
			return nil
		}
		if d.Recv != nil {
			// Methods on an unexported type are not part of the API surface, so
			// only require a doc when the receiver type is exported.
			if !receiverTypeExported(d.Recv) {
				return nil
			}
			if !hasDoc(d.Doc) {
				report(d.Pos(), "method", receiverTypeName(d.Recv)+"."+d.Name.Name)
			}
			return out
		}
		if !hasDoc(d.Doc) {
			report(d.Pos(), "func", d.Name.Name)
		}
	case *ast.GenDecl:
		// A doc comment on the GenDecl itself (d.Doc) covers every spec in the
		// group — the conventional carrier for grouped const/var/type blocks.
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !s.Name.IsExported() {
					continue
				}
				if !hasDoc(s.Doc) && !hasDoc(d.Doc) {
					report(s.Pos(), "type", s.Name.Name)
				}
				if st, ok := s.Type.(*ast.StructType); ok {
					out = append(out, undocumentedFields(root, path, fset, s.Name.Name, st)...)
				}
			case *ast.ValueSpec:
				kind := "var"
				if d.Tok == token.CONST {
					kind = "const"
				}
				for _, name := range s.Names {
					if !name.IsExported() {
						continue
					}
					if !hasDoc(s.Doc) && !hasDoc(s.Comment) && !hasDoc(d.Doc) {
						report(name.Pos(), kind, name.Name)
					}
				}
			}
		}
	}
	return out
}

// undocumentedFields reports exported struct fields with no doc comment. A field
// is documented by either a preceding doc comment or a trailing line comment;
// embedded (anonymous) fields are skipped — their own type carries the doc.
func undocumentedFields(root, path string, fset *token.FileSet, typeName string, st *ast.StructType) []string {
	var out []string
	if st.Fields == nil {
		return nil
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}
		documented := hasDoc(field.Doc) || hasDoc(field.Comment)
		for _, name := range field.Names {
			if !name.IsExported() || documented {
				continue
			}
			p := fset.Position(name.Pos())
			out = append(out, fmt.Sprintf("  no-undocumented-exports: %s:%d exported field %s.%s has no doc comment — go doc is keel's consumer contract (keel/ac-46, keel/ac-49)",
				relPath(root, path), p.Line, typeName, name.Name))
		}
	}
	return out
}

// hasDoc reports whether a comment group carries any non-whitespace text.
func hasDoc(cg *ast.CommentGroup) bool {
	return cg != nil && strings.TrimSpace(cg.Text()) != ""
}

// receiverBaseType unwraps a method receiver expression to its base type
// identifier, stripping a leading pointer and any generic type parameters
// (*Foo[T] → Foo). Returns nil when the base is not a plain identifier.
func receiverBaseType(recv *ast.FieldList) *ast.Ident {
	if recv == nil || len(recv.List) == 0 {
		return nil
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch e := t.(type) {
	case *ast.IndexExpr:
		t = e.X
	case *ast.IndexListExpr:
		t = e.X
	}
	id, _ := t.(*ast.Ident)
	return id
}

// receiverTypeExported reports whether a method's receiver base type is exported.
func receiverTypeExported(recv *ast.FieldList) bool {
	id := receiverBaseType(recv)
	return id != nil && id.IsExported()
}

// receiverTypeName returns the method receiver's base type name (without pointer
// or type parameters), or "" when it is not a plain identifier.
func receiverTypeName(recv *ast.FieldList) string {
	if id := receiverBaseType(recv); id != nil {
		return id.Name
	}
	return ""
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
