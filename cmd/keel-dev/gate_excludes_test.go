package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-118 (keel/ac-449)
func TestGateExcludeConfigRejectsMalformedPatterns(t *testing.T) {
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
			if err := os.WriteFile(filepath.Join(dir, keelDevConfigFile), []byte("gate:\n  excludes:\n    - "+strconv.Quote(strings.TrimSpace(tc.pattern))+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := loadKeelDevConfig(dir)
			if err == nil || !strings.Contains(err.Error(), "gate.excludes") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadKeelDevConfig error = %v, want gate.excludes and %q", err, tc.want)
			}
		})
	}
}
