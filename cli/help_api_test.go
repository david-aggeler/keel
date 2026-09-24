package cli_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestExportedAPIGivesConsumersNoHelpOutputControl type-checks keel/cli's
// non-test sources and enumerates every exported function, method on an
// exported type, exported struct field and exported variable. It fails on any
// that carries an io.Writer, *os.File, keel/log logger or *slog.Logger in a
// parameter, result or field, and on any exported help-request or help-format
// field (a Help*- or Version-named field that is not help content text).
//
// DHF-TEST: keel/requirement-172 (keel/ac-736)
func TestExportedAPIGivesConsumersNoHelpOutputControl(t *testing.T) {
	pkg := typeCheckCLI(t)
	var violations []string
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *types.Func:
			violations = append(violations, outputControlIn(obj.Name(), obj.Type())...)
		case *types.Var:
			violations = append(violations, outputControlIn(obj.Name(), obj.Type())...)
		case *types.TypeName:
			named, ok := obj.Type().(*types.Named)
			if !ok {
				continue
			}
			for i := 0; i < named.NumMethods(); i++ {
				method := named.Method(i)
				if method.Exported() {
					violations = append(violations, outputControlIn(obj.Name()+"."+method.Name(), method.Type())...)
				}
			}
			if st, ok := named.Underlying().(*types.Struct); ok {
				for i := 0; i < st.NumFields(); i++ {
					field := st.Field(i)
					if !field.Exported() {
						continue
					}
					label := obj.Name() + "." + field.Name()
					violations = append(violations, outputControlIn(label, field.Type())...)
					if selectsHelp(field) {
						violations = append(violations, label+": exported field selects a help request or the help format")
					}
				}
			}
		}
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("keel/cli exports help output control a consumer can use:\n  %s", strings.Join(violations, "\n  "))
	}
}

// TestExportedAPICheckDetectsWriterTakingHelp is the positive control for the
// exported-API check: a writer-taking renderer and a help-request field are
// reported, so an empty result above is evidence, not a blind matcher.
//
// DHF-TEST: keel/requirement-172 (keel/ac-736)
func TestExportedAPICheckDetectsWriterTakingHelp(t *testing.T) {
	src := `package probe
import ("io"; "log/slog"; "os")
type Spec struct{ HelpWriter io.Writer; HelpWidth int; Help bool; HelpUsage string; Log *slog.Logger }
func (Spec) RenderRootHelp(w io.Writer) {}
func PrintFlagRows(f *os.File) {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "probe.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("probe", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	named := pkg.Scope().Lookup("Spec").Type().(*types.Named)
	var got []string
	got = append(got, outputControlIn("Spec.RenderRootHelp", named.Method(0).Type())...)
	got = append(got, outputControlIn("PrintFlagRows", pkg.Scope().Lookup("PrintFlagRows").Type())...)
	st := named.Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		got = append(got, outputControlIn(st.Field(i).Name(), st.Field(i).Type())...)
		if selectsHelp(st.Field(i)) {
			got = append(got, st.Field(i).Name()+": selects help")
		}
	}
	want := []string{"Spec.RenderRootHelp", "PrintFlagRows", "HelpWriter", "HelpWidth", "Help:", "Log"}
	joined := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Fatalf("check missed %q; reported:\n%s", w, joined)
		}
	}
	if strings.Contains(joined, "HelpUsage") {
		t.Fatalf("check flagged help content text HelpUsage:\n%s", joined)
	}
}

// typeCheckCLI type-checks the keel/cli package from its non-test sources, so
// the enumeration sees the API a consumer compiles against and nothing a test
// file adds.
func typeCheckCLI(t *testing.T) *types.Package {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("github.com/david-aggeler/keel/cli", fset, files, nil)
	if err != nil {
		t.Fatalf("type-check keel/cli: %v", err)
	}
	return pkg
}

// outputControlIn reports every writer, file or logger type reachable from typ
// through signatures, pointers, slices, maps and struct fields.
func outputControlIn(label string, typ types.Type) []string {
	var found []string
	seen := map[types.Type]bool{}
	var walk func(types.Type)
	walk = func(typ types.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		if name := outputControlType(typ); name != "" {
			found = append(found, label+": "+name)
			return
		}
		switch typ := typ.(type) {
		case *types.Signature:
			for i := 0; i < typ.Params().Len(); i++ {
				walk(typ.Params().At(i).Type())
			}
			for i := 0; i < typ.Results().Len(); i++ {
				walk(typ.Results().At(i).Type())
			}
		case *types.Pointer:
			walk(typ.Elem())
		case *types.Slice:
			walk(typ.Elem())
		case *types.Array:
			walk(typ.Elem())
		case *types.Map:
			walk(typ.Key())
			walk(typ.Elem())
		case *types.Struct:
			for i := 0; i < typ.NumFields(); i++ {
				if typ.Field(i).Exported() {
					walk(typ.Field(i).Type())
				}
			}
		}
	}
	walk(typ)
	return found
}

// outputControlType names typ when it is an output destination a consumer
// could hand to a help renderer.
func outputControlType(typ types.Type) string {
	if ptr, ok := typ.(*types.Pointer); ok {
		if named, ok := ptr.Elem().(*types.Named); ok && named.Obj().Pkg() != nil {
			switch named.Obj().Pkg().Path() + "." + named.Obj().Name() {
			case "os.File", "log/slog.Logger", "github.com/david-aggeler/keel/log.Logger":
				return "*" + named.Obj().Pkg().Path() + "." + named.Obj().Name()
			}
		}
		return ""
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	if named.Obj().Pkg().Path() == "io" && named.Obj().Name() == "Writer" {
		return "io.Writer"
	}
	return ""
}

// selectsHelp reports an exported field that requests help or selects its
// format: a Help*- or Version-named field whose type is not help content text
// (a string or a list of strings).
func selectsHelp(field *types.Var) bool {
	name := field.Name()
	if !strings.HasPrefix(name, "Help") && name != "Version" {
		return false
	}
	switch typ := field.Type().Underlying().(type) {
	case *types.Basic:
		return typ.Kind() != types.String
	case *types.Slice:
		basic, ok := typ.Elem().Underlying().(*types.Basic)
		return !ok || basic.Kind() != types.String
	}
	return true
}
