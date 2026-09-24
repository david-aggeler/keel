package main

import (
	"strings"
	"testing"
)

func runUsageDiagnostic(t *testing.T, args ...string) string {
	t.Helper()
	var code int
	stdout, stderr := captureProcessStreams(t, func() {
		code = run(append([]string{"--no-header"}, args...))
	})
	if code != 2 {
		t.Fatalf("run(%q) exit = %d, want 2\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("run(%q) wrote a usage diagnostic to stdout: %q", args, stdout)
	}
	return stderr
}

// DHF-TEST: keel/requirement-169 (keel/ac-719)
func TestKeelDevUnknownFlagIsReportedAsFlag(t *testing.T) {
	stderr := runUsageDiagnostic(t, "--frob")
	if !strings.Contains(stderr, `unknown flag "--frob"`) {
		t.Fatalf("stderr = %q, want unknown flag", stderr)
	}
	if strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q, must not say unknown command", stderr)
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-720)
func TestKeelDevNearMissCommandSuggestsSibling(t *testing.T) {
	stderr := runUsageDiagnostic(t, "worktre")
	if !strings.Contains(stderr, "worktree") || !strings.Contains(stderr, "did you mean") {
		t.Fatalf("stderr = %q, want a worktree suggestion", stderr)
	}
	stderr = runUsageDiagnostic(t, "frobnicate")
	if strings.Contains(stderr, "did you mean") {
		t.Fatalf("stderr = %q, want no suggestion", stderr)
	}
}

// DHF-TEST: keel/requirement-169 (keel/ac-721)
func TestKeelDevBareGroupsShowConciseHelp(t *testing.T) {
	tree := commandTree()
	for _, group := range []string{"gate", "test-bridge", "vsix"} {
		node, ok := tree.Child(group)
		if !ok || node.Handler != nil || len(node.Subcommands) == 0 {
			t.Fatalf("%s is not a handler-less group", group)
		}
		stderr := runUsageDiagnostic(t, group)
		if !strings.Contains(stderr, "Subcommands:") {
			t.Fatalf("%s stderr = %q, want concise help", group, stderr)
		}
		for _, child := range node.Subcommands {
			if !strings.Contains(stderr, child.Name) || !strings.Contains(stderr, child.Short) {
				t.Fatalf("%s stderr lacks %s row %q:\n%s", group, child.Name, child.Short, stderr)
			}
		}
	}
}
