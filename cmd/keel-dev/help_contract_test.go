package main

import (
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// helpContractModes are the console modes requested help must not vary with.
var helpContractModes = []string{"human", "ai", "json"}

// helpContractRequests are the requested-help invocations keel/cli serves for
// keel-dev. None names a handler: every keel-dev handler runs the gate or a
// release step, and the empty stderr and working directory already show one ran.
var helpContractRequests = [][]string{
	{"--help"},
	{"help", "ci"},
	{"--help-all"},
	{"--help-json"},
	{"--version"},
}

// helpContractUsageErrors are the usage errors whose help text keel/cli writes
// to stderr.
var helpContractUsageErrors = [][]string{
	{"help", "no-such-topic"},
	{"--no-such-flag"},
}

// eventTimePrefix matches a console line that opens with an HH:MM:SS event time.
var eventTimePrefix = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}`)

// DHF-TEST: keel/requirement-172 (keel/ac-737), keel/requirement-170 (keel/ac-729), keel/requirement-28 (keel/ac-735)
func TestKeelDevRequestedHelpIsServedByKeelCLIIdenticallyInEveryMode(t *testing.T) {
	for _, request := range helpContractRequests {
		t.Run(strings.Join(request, " "), func(t *testing.T) {
			var human string
			for _, mode := range helpContractModes {
				stdout, stderr, code := runHelpContract(t, append([]string{"--mode", mode}, request...))
				if code != 0 || stderr != "" {
					t.Fatalf("--mode %s %q: exit %d, stderr %q; want exit 0 and empty stderr", mode, request, code, stderr)
				}
				if stdout == "" {
					t.Fatalf("--mode %s %q: empty stdout", mode, request)
				}
				if mode == "human" {
					human = stdout
				} else if stdout != human {
					t.Fatalf("--mode %s %q stdout differs from --mode human:\n--- human\n%s\n--- %s\n%s", mode, request, human, mode, stdout)
				}
				assertHelpCarriesNoEventTime(t, request, stdout)
			}
		})
	}
}

// DHF-TEST: keel/requirement-172 (keel/ac-738)
func TestKeelDevUsageErrorHelpIsWrittenByKeelCLIToStderr(t *testing.T) {
	for _, request := range helpContractUsageErrors {
		t.Run(strings.Join(request, " "), func(t *testing.T) {
			var human string
			for _, mode := range helpContractModes {
				stdout, stderr, code := runHelpContract(t, append([]string{"--mode", mode}, request...))
				if code != 2 || stdout != "" {
					t.Fatalf("--mode %s %q: exit %d, stdout %q; want exit 2 and empty stdout", mode, request, code, stdout)
				}
				if !strings.Contains(stderr, "Usage:") {
					t.Fatalf("--mode %s %q: stderr carries no help text:\n%s", mode, request, stderr)
				}
				if mode == "human" {
					human = stderr
				} else if stderr != human {
					t.Fatalf("--mode %s %q stderr differs from --mode human:\n--- human\n%s\n--- %s\n%s", mode, request, human, mode, stderr)
				}
				assertNoJSONObjectLine(t, request, stderr)
			}
		})
	}
}

// assertHelpCarriesNoEventTime fails when a help line opens with an event time
// or, for --help-json, when an inventory element carries a ts field; for the
// text forms it also fails on any line that is a JSON log record.
func assertHelpCarriesNoEventTime(t *testing.T, request []string, stdout string) {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		if eventTimePrefix.MatchString(line) {
			t.Fatalf("%q help line carries an event time: %q", request, line)
		}
	}
	if request[0] != "--help-json" {
		if request[0] != "--version" {
			assertNoJSONObjectLine(t, request, stdout)
		}
		return
	}
	var inventory []map[string]any
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatalf("--help-json stdout is not a JSON array: %v\n%s", err, stdout)
	}
	for _, element := range inventory {
		if _, ok := element["ts"]; ok {
			t.Fatalf("--help-json element carries ts: %v", element)
		}
	}
}

func assertNoJSONObjectLine(t *testing.T, request []string, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil {
			t.Fatalf("%q output line is a JSON log record: %q", request, line)
		}
	}
}

// runHelpContract runs keel-dev in an empty working directory with stdout and
// stderr redirected to pipes. It fails the test when the run leaves anything
// behind in that directory: no .logs record and no handler side effect.
func runHelpContract(t *testing.T, argv []string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdoutDone := readAllAsync(stdoutR)
	stderrDone := readAllAsync(stderrR)
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	code := run(argv)
	os.Stdout, os.Stderr = oldStdout, oldStderr
	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdout, stderr := <-stdoutDone, <-stderrDone
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%q left %d entries in the working directory (first %q); help must write no .logs record and run no handler", argv, len(entries), entries[0].Name())
	}
	return stdout, stderr, code
}

func readAllAsync(r *os.File) <-chan string {
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		_ = r.Close()
		done <- string(data)
	}()
	return done
}

// keelDevHelp runs keel-dev with argv and returns the stdout of a help request
// that exited 0: keel/cli writes help only to the process streams.
func keelDevHelp(t *testing.T, argv ...string) string {
	t.Helper()
	stdout, stderr := captureProcessStreams(t, func() {
		if code := run(argv); code != 0 {
			t.Fatalf("keel-dev %q exit = %d, want 0", argv, code)
		}
	})
	if stderr != "" {
		t.Fatalf("keel-dev %q stderr = %q, want empty", argv, stderr)
	}
	return stdout
}
