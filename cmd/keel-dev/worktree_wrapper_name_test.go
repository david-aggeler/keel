package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRecordingDelegate writes a stub delegate that records the argv it was
// handed and exits 0, so a script that reaches it can be told apart from one
// that rejected the arguments itself.
func newRecordingDelegate(t *testing.T) (bin, record string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "keel-dev-stub")
	record = filepath.Join(dir, "argv")
	body := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>" + record + "\nexit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, record
}

// TestWorktreeWrappersDelegateNameRejection proves the first clause of the
// criterion: a malformed work-item name reaches the delegate. A script that
// still decided the grammar itself would exit 64 without ever invoking it.
//
// DHF-TEST: keel/requirement-114 (keel/ac-421)
func TestWorktreeWrappersDelegateNameRejection(t *testing.T) {
	repo := newWorktreeScriptEnv(t, "").repo
	for _, script := range worktreeWrapperScripts {
		for _, tc := range []struct {
			label string
			args  []string
			want  string
		}{
			{"bad kind", []string{"nope", "1", "alpha"}, "nope-1-alpha"},
			{"bad slug", []string{"cr", "1", "Alpha"}, "cr-1-Alpha"},
			{"bad seq", []string{"cr", "x", "alpha"}, "cr-x-alpha"},
			{"slug too long", []string{"cr", "1", strings.Repeat("a", 101)}, "cr-1-" + strings.Repeat("a", 101)},
		} {
			bin, record := newRecordingDelegate(t)
			got := runWorktreeScriptWithEnv(t, repo, script, []string{"KEEL_DEV_BIN=" + bin}, tc.args...)
			label := script + " " + tc.label
			if got.exitCode != 0 {
				t.Errorf("%s: exit %d, want the stub delegate's 0\nstderr: %s", label, got.exitCode, got.stderr)
			}
			argv, err := os.ReadFile(record)
			if err != nil {
				t.Errorf("%s: the script rejected the name itself; the delegate was never invoked (stderr: %s)", label, got.stderr)
				continue
			}
			if !strings.Contains(string(argv), tc.want) {
				t.Errorf("%s: delegate argv %q does not carry the composed name %q", label, strings.TrimSpace(string(argv)), tc.want)
			}
		}
	}
}

// stripShellComment drops a trailing shell comment, leaving parameter
// expansions that contain a '#' — ${#SLUG} is a length check, not a comment.
func stripShellComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
			return line[:i]
		}
	}
	return line
}

// TestWorktreeWrappersEncodeNoNameGrammar proves the second clause: no script
// body carries a pattern match or a length check deciding whether kind, seq, or
// slug is well formed. This is what makes the criterion checkable by inspection
// rather than by a coincidence of two implementations agreeing.
//
// DHF-TEST: keel/requirement-114 (keel/ac-421)
func TestWorktreeWrappersEncodeNoNameGrammar(t *testing.T) {
	for _, script := range worktreeWrapperScripts {
		path := filepath.Join(worktreeScriptDir, script)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			code := stripShellComment(line)
			named := strings.Contains(code, "KIND") || strings.Contains(code, "SEQ") || strings.Contains(code, "SLUG")
			switch {
			case named && strings.Contains(code, "=~"):
				t.Errorf("%s:%d decides work-item-name validity with a pattern match: %s", script, i+1, strings.TrimSpace(line))
			case named && strings.Contains(code, "${#"):
				t.Errorf("%s:%d decides work-item-name validity with a length check: %s", script, i+1, strings.TrimSpace(line))
			}
		}
	}
}
