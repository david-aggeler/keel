package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestKeelDemoDevConfigVerbsReportOutcomeOnStderrOnly runs the built binary
// with -v, which lowers keel-demo-dev's Warn console floor so its Info records
// reach stderr. Each config verb reports its outcome there; stdout stays empty.
//
// DHF-TEST: keel/requirement-168 (keel/ac-717, keel/ac-718)
func TestKeelDemoDevConfigVerbsReportOutcomeOnStderrOnly(t *testing.T) {
	exe := buildDemoDev(t)
	root := t.TempDir()
	for _, step := range []struct {
		verb    string
		outcome string
	}{
		{"config-init", "outcome=created"},
		{"config-init", "outcome=already present"},
		{"config-upgrade", "outcome=already current"},
	} {
		cmd := exec.Command(exe, "-v", "test-bridge", step.verb)
		cmd.Dir = root
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: %v\nstderr:\n%s", step.verb, err, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("%s: stdout = %q, want empty", step.verb, stdout.String())
		}
		if n := strings.Count(stderr.String(), "testbridge config verb="+step.verb); n != 1 {
			t.Fatalf("%s: %d outcome records on stderr, want 1\nstderr:\n%s", step.verb, n, stderr.String())
		}
		if !strings.Contains(stderr.String(), step.outcome) || !strings.Contains(stderr.String(), "test-bridge.json") {
			t.Fatalf("%s: stderr missing %q with the config path\nstderr:\n%s", step.verb, step.outcome, stderr.String())
		}
	}
}
