package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-12 (keel/ac-462, keel/ac-463)
func TestSetupUserConvergesPinnedDeadcodeAndDocumentsFloatingGopls(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "scripts", "setup_user.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)

	deadcodeBlock := scriptBlock(t, script, "DEADCODE_VERSION=", "ensure_local_bin_link \"deadcode\"")
	if strings.Contains(deadcodeBlock, "[[ -x \"$DEADCODE_BIN\" ]]") {
		t.Fatal("deadcode pin must converge an existing off-pin binary, not short-circuit on executable presence")
	}
	if !strings.Contains(deadcodeBlock, "go install \"golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION}\"") {
		t.Fatal("deadcode block must install the declared DEADCODE_VERSION pin")
	}

	goplsBlock := scriptBlock(t, script, "# --- gopls", "# --- golangci-lint")
	for _, want := range []string{"deliberately floating", "not part of the gate"} {
		if !strings.Contains(goplsBlock, want) {
			t.Fatalf("gopls declaration site must explain the floating @latest exemption; missing %q", want)
		}
	}
}

func scriptBlock(t *testing.T, script, start, end string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(start) + `(?s).*?` + regexp.QuoteMeta(end))
	match := re.FindString(script)
	if match == "" {
		t.Fatalf("script block %q..%q not found", start, end)
	}
	return match
}
