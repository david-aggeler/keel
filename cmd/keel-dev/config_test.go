package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-118 (keel/ac-446)
func TestKeelDevConfigAbsentFileUsesCompleteDefaults(t *testing.T) {
	cfg, err := loadKeelDevConfig(t.TempDir())
	if err != nil {
		t.Fatalf("loadKeelDevConfig absent file: %v", err)
	}
	want := defaultKeelDevConfig()
	if !gateExcludePatternsEqual(cfg.Gate.Excludes, want.Gate.Excludes) {
		t.Fatalf("default excludes = %#v, want %#v", cfg.Gate.Excludes, want.Gate.Excludes)
	}
	if !toolPinsEqual(cfg.toolPins(), want.toolPins()) {
		t.Fatalf("default tool pins = %#v, want %#v", cfg.toolPins(), want.toolPins())
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-447)
func TestKeelDevConfigPartialFileOverlaysDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - scratch/**\n")

	cfg, err := loadKeelDevConfig(dir)
	if err != nil {
		t.Fatalf("loadKeelDevConfig partial file: %v", err)
	}
	if !gatePathExcluded("scratch/output.md", cfg.Gate.Excludes) {
		t.Fatal("partial config did not override gate.excludes")
	}
	pins := cfg.toolPins()
	if pins["golangci-lint"].want != "2.12.2" {
		t.Fatalf("partial config zeroed tools.pins; golangci-lint pin = %#v", pins["golangci-lint"])
	}
	if pins["deadcode"].want != "" || len(pins["deadcode"].versionArgs) != 0 {
		t.Fatalf("partial config changed presence-only deadcode pin: %#v", pins["deadcode"])
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-448)
func TestKeelDevConfigUnknownPropertyFailsLoud(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, keelDevConfigFile, "gate:\n  unknown_key: true\n")

	_, err := loadKeelDevConfig(dir)
	if err == nil {
		t.Fatal("unknown property should fail")
	}
	if !strings.Contains(err.Error(), filepath.Join(dir, keelDevConfigFile)) || !strings.Contains(err.Error(), "unknown_key") {
		t.Fatalf("unknown-property error = %v, want file path and property name", err)
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-449)
func TestKeelDevConfigInvalidPropertyNamesDottedPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, keelDevConfigFile, "gate:\n  excludes:\n    - .claude\\**\n")

	_, err := loadKeelDevConfig(dir)
	if err == nil {
		t.Fatal("invalid exclude pattern should fail")
	}
	if !strings.Contains(err.Error(), "gate.excludes") || !strings.Contains(err.Error(), "slash separators") {
		t.Fatalf("invalid-property error = %v, want dotted path and validation detail", err)
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-446)
func TestExplicitMissingKeelDevConfigFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	_, err := loadKeelDevConfigFile(missing, true)
	if err == nil {
		t.Fatal("explicit missing config path should fail")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit missing config error = %v, want os.ErrNotExist", err)
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-450)
func TestKeelDevConfigPropertiesAreDocumentedInTypeAndFile(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	cfg, err := loadKeelDevConfig(root)
	if err != nil {
		t.Fatalf("loadKeelDevConfig: %v", err)
	}
	files, err := trackedLintFiles(context.Background(), discardLogger(), root, cfg.Gate.Excludes)
	if err != nil {
		t.Fatal(err)
	}
	if violations, err := scanKeelDevConfigDocumentation(root, files); err != nil {
		t.Fatal(err)
	} else if len(violations) != 0 {
		t.Fatalf("config documentation violations:\n%s", strings.Join(violations, "\n"))
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-451)
func TestCommittedKeelDevConfigOwnsGateExcludes(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	cfg, err := loadKeelDevConfig(root)
	if err != nil {
		t.Fatalf("loadKeelDevConfig: %v", err)
	}
	if !gatePathExcluded(".claude/skills/change-request/SKILL.md", cfg.Gate.Excludes) {
		t.Fatal("committed config must exclude catalog-materialized .claude paths")
	}
	if !gatePathExcluded("docs/handoffs/change_request-163.md", cfg.Gate.Excludes) {
		t.Fatal("committed config must exclude tracked handoff prose")
	}
	if gatePathExcluded("docs/.claude-notes.md", cfg.Gate.Excludes) {
		t.Fatal("committed config must not exclude unrelated paths that merely contain .claude")
	}
}

// DHF-TEST: keel/requirement-118 (keel/ac-451)
func TestPinnedToolsComeFromKeelDevConfig(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	cfg, err := loadKeelDevConfig(root)
	if err != nil {
		t.Fatalf("loadKeelDevConfig: %v", err)
	}
	pins := cfg.toolPins()
	for _, want := range []string{"golangci-lint", "govulncheck", "cspell", "shellcheck", "shfmt", "deadcode", "gitleaks"} {
		if _, ok := pins[want]; !ok {
			t.Fatalf("config-derived pins missing %q", want)
		}
	}
	if pins["gitleaks"].want != "" || len(pins["gitleaks"].versionArgs) != 0 {
		t.Fatalf("gitleaks pin must remain presence-only, got %#v", pins["gitleaks"])
	}
}

func gateExcludePatternsEqual(a, b gateExcludePatterns) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toolPinsEqual(a, b map[string]toolPin) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || av.name != bv.name || av.want != bv.want || av.install != bv.install || !stringSliceEqual(av.versionArgs, bv.versionArgs) {
			return false
		}
	}
	return true
}

func TestKeelDevConfigStructFieldDocScanCatchesMissingComment(t *testing.T) {
	dir := t.TempDir()
	keeldev := filepath.Join(dir, "cmd", "keel-dev")
	if err := os.MkdirAll(keeldev, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, keeldev, "config.go", "package main\n\ntype sample struct {\n\tGood string `yaml:\"good\"` // explains what and why\n\tBad string `yaml:\"bad\"`\n}\n")
	violations, err := scanConfigStructFieldDocs(dir, lintFixtureFiles(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "sample.Bad") {
		t.Fatalf("violations = %v, want sample.Bad", violations)
	}
}

func TestKeelDevConfigFileCommentScanCatchesMissingWhy(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, keelDevConfigFile, "# too short\nroot:\n")
	violations, err := scanConfigFileKeyComments(filepath.Join(dir, keelDevConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || !strings.Contains(violations[0], "root") {
		t.Fatalf("violations = %v, want root", violations)
	}
}

func TestKeelDevConfigHasNoPinnedToolsMapLiteral(t *testing.T) {
	src, err := parser.ParseFile(token.NewFileSet(), "tools.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range src.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if name.Name == "pinnedTools" {
					t.Fatal("pinnedTools map literal must not remain as a second source of truth")
				}
			}
		}
	}
}
