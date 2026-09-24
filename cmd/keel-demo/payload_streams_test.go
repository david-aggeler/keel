package main

import (
	"strings"
	"testing"
)

// TestKeelDemoWorkflowVerbSeparatesResultFromDiagnostics runs each payload verb
// of the built binary with stdout and stderr captured apart. stdout must be the
// result line and nothing else; stderr must carry the verb's log record and
// never the result line. A verb that routed its result through the logger would
// put the line on stderr with a level prefix, and fail both halves.
//
// DHF-TEST: keel/requirement-164 (keel/ac-699)
func TestKeelDemoWorkflowVerbSeparatesResultFromDiagnostics(t *testing.T) {
	exe := buildDemoExe(t)
	for _, tc := range []struct {
		args   []string
		result string
		event  string
	}{
		{
			args:   []string{"workflow", "inspect", "--format", "json", "run-123"},
			result: "workflow inspect run_id=run-123 format=json\n",
			event:  "workflow_inspect",
		},
		{
			args:   []string{"workflow", "replay", "--speed", "fast", "demo.transcript"},
			result: "workflow replay transcript=demo.transcript speed=fast\n",
			event:  "workflow_replay",
		},
	} {
		for _, mode := range []string{"human", "ai", "json"} {
			t.Run(tc.event+"/"+mode, func(t *testing.T) {
				args := append([]string{"--mode", mode}, tc.args...)
				stdout, stderr, code := runDemoStreams(t, exe, args...)
				if code != 0 {
					t.Fatalf("keel-demo %v exit = %d, want 0\nstderr:\n%s", args, code, stderr)
				}
				if stdout != tc.result {
					t.Fatalf("keel-demo %v stdout = %q, want exactly the result line %q", args, stdout, tc.result)
				}
				if !strings.Contains(stderr, tc.event) {
					t.Fatalf("keel-demo %v stderr lacks the verb's log record %q:\n%s", args, tc.event, stderr)
				}
				if strings.Contains(stderr, strings.TrimSpace(tc.result)) {
					t.Fatalf("keel-demo %v stderr carries the result line:\n%s", args, stderr)
				}
			})
		}
	}
}
