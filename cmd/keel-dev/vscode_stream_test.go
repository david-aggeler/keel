package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	procexec "github.com/david-aggeler/keel/exec"
	"github.com/david-aggeler/keel/vscode"
)

// streamFixtureRoot lays out a module with a `log` and an `exec` package and a
// lane over both, which is the smallest tree the lane Go run path resolves.
func streamFixtureRoot(t *testing.T, lanes string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	writeFile(t, root, "go.sum", "")
	for _, dir := range []string{"log", "exec", ".vscode"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, filepath.Join("log", "logging_test.go"), "package log\n\nimport \"testing\"\n\nfunc TestLog(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join("exec", "exec_test.go"), "package exec\n\nimport \"testing\"\n\nfunc TestExec(t *testing.T) {}\n")
	writeFile(t, root, filepath.Join(".vscode", "test-lanes.json"), lanes)
	return root
}

// The stub `go` scripts below print, then block on a handshake file the
// run-event writer creates when it receives the first event, then print again.
// The wait is bounded inside the stub and stamps an expiry file when it runs
// out, so a buffered implementation FAILS the assertion instead of hanging the
// suite. No wall-clock threshold appears in any assertion.

// A lane whose packages settle at different times delivers the first package
// terminal to the run-event writer while the `go test` child is still running.
//
// DHF-TEST: keel/requirement-131 (keel/ac-507)
func TestVSCodeLaneRunStreamsPackageTerminalBeforeChildExits(t *testing.T) {
	root := streamFixtureRoot(t, `{"version":1,"lanes":[{"id":"two","label":"two","order":"b.40","members":[{"go":"./log/..."},{"go":"./exec/..."}]}]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	handshake := filepath.Join(bin, "handshake")
	expiry := filepath.Join(bin, "expired")
	stub(t, bin, callsFile, "go", `
printf '{"Action":"pass","Package":"`+modulePath+`/exec","Elapsed":0.01}\n'
i=0
while [ $i -lt 100 ]; do
  if [ -f "`+handshake+`" ]; then break; fi
  sleep 0.05
  i=$((i+1))
done
if [ ! -f "`+handshake+`" ]; then : > "`+expiry+`"; fi
printf '{"Action":"pass","Package":"`+modulePath+`/log","Elapsed":0.02}\n'
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var got []vscode.RunEvent
	writer := func(event vscode.RunEvent) {
		got = append(got, event)
		if len(got) == 1 {
			if err := os.WriteFile(handshake, []byte("1"), 0o644); err != nil {
				t.Errorf("write handshake: %v", err)
			}
		}
	}

	if err := runVSCodeFileLane(context.Background(), discardLogger(), root, "keel::lane::two", "run-1", procexec.DefaultMaxOutputBytes, writer); err != nil {
		t.Fatalf("lane run: %v\ncalls:\n%s", err, calls(t, callsFile))
	}
	if _, err := os.Stat(expiry); err == nil {
		t.Fatalf("stub `go` timed out waiting for the first run event: the terminal for the earlier package did not reach the writer before the child exited\nevents: %+v", got)
	}
	if len(got) != 2 || got[0].TestID != "go::pkg::exec" || got[1].TestID != "go::pkg::log" {
		t.Fatalf("lane run events = %+v, want go::pkg::exec then go::pkg::log", got)
	}
}

// The direct Go selection path streams on the same terms as the lane path: the
// first result-bearing event reaches the writer before the child exits.
//
// DHF-TEST: keel/requirement-131 (keel/ac-510)
func TestVSCodeGoSelectionStreamsFirstEventBeforeChildExits(t *testing.T) {
	root := streamFixtureRoot(t, `{"version":1,"lanes":[]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	handshake := filepath.Join(bin, "handshake")
	expiry := filepath.Join(bin, "expired")
	stub(t, bin, callsFile, "go", `
printf '{"Action":"pass","Package":"`+modulePath+`/log","Test":"TestLog","Elapsed":0.01}\n'
i=0
while [ $i -lt 100 ]; do
  if [ -f "`+handshake+`" ]; then break; fi
  sleep 0.05
  i=$((i+1))
done
if [ ! -f "`+handshake+`" ]; then : > "`+expiry+`"; fi
printf '{"Action":"pass","Package":"`+modulePath+`/log","Elapsed":0.02}\n'
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var got []vscode.RunEvent
	writer := func(event vscode.RunEvent) {
		got = append(got, event)
		if len(got) == 1 {
			if err := os.WriteFile(handshake, []byte("1"), 0o644); err != nil {
				t.Errorf("write handshake: %v", err)
			}
		}
	}

	selection := vscode.GoSelection{Kind: "package", Pkg: "log"}
	if err := runVSCodeGoSelection(context.Background(), discardLogger(), root, "go::pkg::log", selection, procexec.DefaultMaxOutputBytes, writer); err != nil {
		t.Fatalf("go selection run: %v\ncalls:\n%s", err, calls(t, callsFile))
	}
	if _, err := os.Stat(expiry); err == nil {
		t.Fatalf("stub `go` timed out waiting for the first run event: the direct selection path did not deliver before the child exited\nevents: %+v", got)
	}
	if len(got) != 2 || got[0].TestID != "go::test::log::TestLog" || got[1].TestID != "go::pkg::log" {
		t.Fatalf("go selection events = %+v, want the per-test terminal then the package terminal", got)
	}
}

// The writer receives events in exactly the order the producer reported the
// lines that carry them, with no reordering and no duplicates.
//
// DHF-TEST: keel/requirement-131 (keel/ac-508)
func TestVSCodeLaneRunPreservesProducerEventOrder(t *testing.T) {
	root := streamFixtureRoot(t, `{"version":1,"lanes":[{"id":"log-only","label":"log","order":"b.40","members":[{"go":"./log/..."}]}]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
printf '{"Action":"pass","Package":"`+modulePath+`/log","Test":"TestA","Elapsed":0.01}\n'
printf '{"Action":"skip","Package":"`+modulePath+`/log","Test":"TestB","Elapsed":0}\n'
printf '{"Action":"pass","Package":"`+modulePath+`/log","Test":"TestC","Elapsed":0.03}\n'
printf '{"Action":"pass","Package":"`+modulePath+`/log","Elapsed":0.04}\n'
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var got []string
	writer := func(event vscode.RunEvent) {
		got = append(got, event.Event+" "+event.TestID)
	}
	if err := runVSCodeFileLane(context.Background(), discardLogger(), root, "keel::lane::log-only", "run-1", procexec.DefaultMaxOutputBytes, writer); err != nil {
		t.Fatalf("lane run: %v\ncalls:\n%s", err, calls(t, callsFile))
	}
	want := []string{
		"passed go::test::log::TestA",
		"skipped go::test::log::TestB",
		"passed go::test::log::TestC",
		"passed go::pkg::log",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("event order = %v, want %v", got, want)
	}
}

// A logical producer line split across two child writes is delivered once, in
// order, when both halves have arrived — the remainder handling the streaming
// seam depends on for keel/ac-508.
//
// DHF-TEST: keel/requirement-131 (keel/ac-508)
func TestLineFuncWriterJoinsSplitLinesInOrder(t *testing.T) {
	var got []string
	w := newLineFuncWriter(func(line []byte) { got = append(got, string(line)) })

	for _, chunk := range []string{"first\nsec", "ond\nthird\r\n", "trailing"} {
		n, err := w.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = (%d, %v), want (%d, nil)", chunk, n, err, len(chunk))
		}
	}
	if strings.Join(got, "|") != "first|second|third" {
		t.Fatalf("lines before Flush = %v, want first, second, third", got)
	}
	w.Flush()
	if strings.Join(got, "|") != "first|second|third|trailing" {
		t.Fatalf("lines after Flush = %v, want the trailing unterminated line appended", got)
	}
}

// A child that writes past the caller-supplied output ceiling fails the run on
// the streaming path, even though run events were already delivered — the
// keel/issue-160 shape the rewrite must not reintroduce.
//
// DHF-TEST: keel/requirement-131 (keel/ac-509)
func TestVSCodeLaneRunFailsOnOutputCeilingAfterStreamingEvents(t *testing.T) {
	root := streamFixtureRoot(t, `{"version":1,"lanes":[{"id":"log-only","label":"log","order":"b.40","members":[{"go":"./log/..."}]}]}`+"\n")

	bin := t.TempDir()
	callsFile := filepath.Join(bin, "calls.log")
	stub(t, bin, callsFile, "go", `
printf '{"Action":"pass","Package":"`+modulePath+`/log","Elapsed":0.01}\n'
sleep 0.1
i=0
while [ $i -lt 200 ]; do
  printf '{"Action":"output","Package":"`+modulePath+`/log","Output":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n'
  i=$((i+1))
done
exit 0`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var got []vscode.RunEvent
	writer := func(event vscode.RunEvent) { got = append(got, event) }

	err := runVSCodeFileLane(context.Background(), discardLogger(), root, "keel::lane::log-only", "run-1", 256, writer)
	if err == nil {
		t.Fatalf("lane run over the output ceiling returned nil, want the ceiling error\nevents: %+v", got)
	}
	if !errors.Is(err, procexec.ErrOutputLimitExceeded) {
		t.Fatalf("lane run error = %v, want it to carry %v", err, procexec.ErrOutputLimitExceeded)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one run event delivered before the ceiling was reached, got none")
	}
}
