package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
//   - keel-dev-config-docs: every property in the keel-dev config object and
//     committed config file carries explanatory commentary (keel/ac-450).
//
//   - no-desired-state-label-coupling: the desired-state group's display label
//     must not appear in keel/testbridge at all — derivation keys on the
//     exported marker — and each consumer surface states it at most once
//     (keel/requirement-126, keel/ac-479).
//
//   - no-bare-forwarder: an unexported package-level function whose whole body
//     forwards to a same-package exported function, with no non-test caller, is
//     the residue of an unexport-then-re-export round trip and fails the gate
//     (keel/requirement-33, keel/ac-497).
//
//   - no-inline-discard-construction: log.Discard is the only site permitted to
//     build a discard logger by hand (keel/ac-493). Held here rather than in a
//     log/ test so its evaluated set is the gate's tracked-files selector.
//
//   - no-test-owned-tree-walk: a tracked _test.go file must not select files by
//     walking the filesystem from outside its own package — that walk reaches
//     gitignored scratch and reds the gate on content no change owns
//     (keel/requirement-85, keel/ac-501).
//
// DHF-REQ: keel/requirement-10, keel/requirement-11, keel/requirement-85 (keel/ac-453, keel/ac-454, keel/ac-455, keel/ac-501), keel/requirement-118 (keel/ac-450), keel/requirement-126 (keel/ac-479), keel/requirement-33 (keel/ac-497), keel/requirement-122 (keel/ac-493)
func runLint(dir string, files []string) error {
	var violations []string

	v, err := scanNoStdlibLog(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawFmtOutput(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRawStdoutStream(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoUndocumentedExports(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoRetiredDesiredStateVocabulary(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanKeelDevConfigDocumentation(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanDesiredStateGroupLabelCoupling(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoBareForwarders(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanInlineDiscardConstruction(dir, files)
	if err != nil {
		return err
	}
	violations = append(violations, v...)

	v, err = scanNoTestOwnedTreeWalk(dir, files)
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
// names in the protocol and devtool surfaces. Test files are scanned too: the
// policy's own planted-occurrence tests build the terms from bytes, so the
// guard never has a literal to trip on.
//
// DHF-REQ: keel/requirement-77
func scanNoRetiredDesiredStateVocabulary(root string, files []string) ([]string, error) {
	var violations []string
	for _, file := range files {
		if !retiredDesiredStateVocabularyPath(file) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		text := string(body)
		for _, term := range retiredDesiredStateVocabularyTerms {
			if line := firstLineContaining(text, term); line > 0 {
				violations = append(violations, fmt.Sprintf("  no-retired-desired-state-vocabulary: %s:%d contains %q — use DesiredState-rooted naming only (keel/requirement-77)", filepath.FromSlash(file), line, term))
			}
		}
	}
	return violations, nil
}

// desiredStateGroupLabel is the display string of the desired-state group,
// built from bytes: this policy lives inside a surface it scans, so a literal
// here would count as an occurrence of what it forbids.
var desiredStateGroupLabel = string([]byte{66, 32, 45, 32, 68, 101, 115, 105, 114, 101, 100, 32, 83, 116, 97, 116, 101})

// desiredStateGroupLabelBudget bounds where the display label may appear.
// keel/testbridge derives on DesiredStateGroupIDSuffix and must not mention the
// label at all; each consumer surface declares it once and no more, so a rename
// has exactly one site per surface (keel/ac-479).
var desiredStateGroupLabelBudget = []struct {
	dir   string
	limit int
}{
	{dir: "testbridge", limit: 0},
	{dir: "testdata", limit: 0},
	{dir: filepath.Join("cmd", "keel-dev"), limit: 1},
	{dir: filepath.Join("cmd", "keel-demo-dev"), limit: 1},
	{dir: filepath.Join("vsix", "src"), limit: 1},
}

// scanDesiredStateGroupLabelCoupling reports surfaces that spend more of their
// display-label budget than keel/ac-479 allows. It counts occurrences rather
// than merely flagging them: the defect the policy guards is duplication — a
// label stated in many places has no single point at which a rename is correct.
//
// DHF-REQ: keel/requirement-126
func scanDesiredStateGroupLabelCoupling(root string, files []string) ([]string, error) {
	counts := make(map[string]int, len(desiredStateGroupLabelBudget))
	sites := make(map[string][]string, len(desiredStateGroupLabelBudget))
	for _, file := range files {
		surface, ok := desiredStateGroupLabelSurface(file)
		if !ok {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		text := string(body)
		found := strings.Count(text, desiredStateGroupLabel)
		if found == 0 {
			continue
		}
		counts[surface] += found
		sites[surface] = append(sites[surface], fmt.Sprintf("%s:%d", filepath.FromSlash(file), firstLineContaining(text, desiredStateGroupLabel)))
	}
	var violations []string
	for _, budget := range desiredStateGroupLabelBudget {
		got := counts[budget.dir]
		if got <= budget.limit {
			continue
		}
		violations = append(violations, fmt.Sprintf(
			"  no-desired-state-label-coupling: %s states the desired-state group's display label %d time(s), budget %d (%s) — derivation keys on testbridge.DesiredStateGroupIDSuffix; the label is display only (keel/requirement-126, keel/ac-479)",
			budget.dir, got, budget.limit, strings.Join(sites[budget.dir], ", ")))
	}
	return violations, nil
}

// desiredStateGroupLabelSurface returns the budgeted surface a file belongs to.
// The most specific budgeted directory wins, so cmd/keel-dev never absorbs a
// cmd/keel-demo-dev occurrence.
func desiredStateGroupLabelSurface(file string) (string, bool) {
	best := ""
	for _, budget := range desiredStateGroupLabelBudget {
		if hasAnyPathPrefix(file, budget.dir) && len(budget.dir) > len(best) {
			best = budget.dir
		}
	}
	return best, best != ""
}

func retiredDesiredStateVocabularyPath(file string) bool {
	if !hasAnyPathPrefix(file, retiredDesiredStateVocabularyDirs...) {
		return false
	}
	switch filepath.Ext(file) {
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
func scanNoStdlibLog(root string, files []string) ([]string, error) {
	var violations []string
	err := visitGoFiles(root, files, func(path string, file *ast.File, fset *token.FileSet) {
		for _, imp := range file.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "log" {
				pos := fset.Position(imp.Pos())
				violations = append(violations,
					fmt.Sprintf("  no-stdlib-log: %s:%d imports stdlib log — use keel/log (log/slog is allowed)", path, pos.Line))
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
func scanNoRawFmtOutput(root string, files []string) ([]string, error) {
	var violations []string
	for _, sub := range rawFmtDirs {
		err := visitGoFiles(root, filesWithPrefix(files, sub), func(path string, file *ast.File, fset *token.FileSet) {
			ast.Inspect(file, func(n ast.Node) bool {
				// Allowlist: the printUsage function and the unknown-flag refusal
				// in run() are static help/diagnostic text, not run output.
				if fn, ok := n.(*ast.FuncDecl); ok && path == filepath.Join("cmd", "keel-dev", "main.go") &&
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
					fmt.Sprintf("  no-raw-fmt-output: %s:%d calls fmt.%s — route run output through keel/log", path, pos.Line, sel.Sel.Name))
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
func scanNoRawStdoutStream(root string, files []string) ([]string, error) {
	var violations []string
	err := visitGoFiles(root, filesWithPrefix(files, filepath.Join("cmd", "keel-dev")), func(path string, file *ast.File, fset *token.FileSet) {
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
var libraryDocDirs = []string{"log", "exec", "cli", "worktree"}

// scanNoUndocumentedExports reports every exported identifier (function, type,
// method on an exported type, struct field, const, or var) in keel's library
// packages that lacks a doc comment. go doc is the sole behavioral contract keel
// offers its consumers (keel/ac-46), so an undocumented export is a hole in that
// contract; this machine-enforces the floor that a comment exists (keel/ac-49).
// Comment quality remains a review concern. Test files are skipped by visitGoFiles.
func scanNoUndocumentedExports(root string, files []string) ([]string, error) {
	var violations []string
	for _, sub := range libraryDocDirs {
		err := visitGoFiles(root, filesWithPrefix(files, sub), func(path string, file *ast.File, fset *token.FileSet) {
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
			path, p.Line, kind, name))
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
				path, p.Line, typeName, name.Name))
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

// visitGoFiles parses every listed non-test .go file and hands the AST to visit.
func visitGoFiles(root string, files []string, visit func(path string, file *ast.File, fset *token.FileSet)) error {
	fset := token.NewFileSet()
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(file))
		parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("lint: parse %s: %w", path, err)
		}
		visit(filepath.FromSlash(file), parsed, fset)
	}
	return nil
}

func filesWithPrefix(files []string, prefix string) []string {
	var out []string
	for _, file := range files {
		if hasAnyPathPrefix(file, prefix) {
			out = append(out, file)
		}
	}
	return out
}

func hasAnyPathPrefix(file string, prefixes ...string) bool {
	file = filepath.ToSlash(file)
	for _, prefix := range prefixes {
		prefix = filepath.ToSlash(prefix)
		if file == prefix || strings.HasPrefix(file, prefix+"/") {
			return true
		}
	}
	return false
}
