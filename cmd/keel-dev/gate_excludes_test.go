package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-114 (keel/ac-433), keel/requirement-85 (keel/ac-435)
func TestCommittedGateExcludeListOwnsCatalogMaterializedPaths(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	patterns, err := readGateExcludePatterns(root)
	if err != nil {
		t.Fatalf("readGateExcludePatterns: %v", err)
	}
	if !gatePathExcluded(".claude/skills/change-request/SKILL.md", patterns) {
		t.Fatal("committed gate exclude list must exclude catalog-materialized .claude paths")
	}
	if gatePathExcluded("docs/.claude-notes.md", patterns) {
		t.Fatal("committed gate exclude list must not exclude unrelated paths that merely contain .claude")
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-435)
func TestGateExcludeListAbsentMeansNoExcludes(t *testing.T) {
	patterns, err := readGateExcludePatterns(t.TempDir())
	if err != nil {
		t.Fatalf("readGateExcludePatterns: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("patterns = %v, want none", patterns)
	}
	if gatePathExcluded(".claude/skills/generated.md", patterns) {
		t.Fatal("absent gate exclude list must not exclude any path")
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-435)
func TestGateExcludeListRejectsMalformedPatterns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "absolute", pattern: "/tmp/**\n", want: "repo-relative"},
		{name: "backslash", pattern: `.claude\**` + "\n", want: "slash separators"},
		{name: "recursive wildcard in middle", pattern: ".claude/**/generated.md\n", want: "must end in /**"},
		{name: "negated", pattern: "!.claude/**\n", want: "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, gateExcludeFile), []byte(tc.pattern), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := readGateExcludePatterns(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readGateExcludePatterns error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
