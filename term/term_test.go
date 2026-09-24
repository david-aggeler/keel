package term_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/term"
)

// fakeProbe is an injected detection answer. size is read through a pointer so
// a test can change it under an already-constructed Capability.
type fakeProbe struct {
	terminal map[term.Stream]bool
	size     *term.Size
}

func (p fakeProbe) IsTerminal(s term.Stream) bool { return p.terminal[s] }

func (p fakeProbe) Size(s term.Stream) (term.Size, bool) {
	if !p.terminal[s] || p.size == nil {
		return term.Size{}, false
	}
	return *p.size, true
}

// allTerminals answers "terminal" on every stream.
func allTerminals() fakeProbe {
	return fakeProbe{terminal: map[term.Stream]bool{term.Stdin: true, term.Stdout: true, term.Stderr: true}}
}

// envOf returns a Getenv over a fixed map; absent keys read as unset.
func envOf(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// colorTerm is an environment that forbids nothing.
var colorTerm = map[string]string{"TERM": "xterm-256color"}

// openDevNull opens /dev/null, the character device that is not a terminal.
func openDevNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// openPipe returns the write end of a fresh pipe.
func openPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return w
}

// DHF-TEST: keel/requirement-165
// keel/ac-700: a stream on /dev/null is not a terminal and color is not
// permitted, although /dev/null is a character device.
func TestDevNullIsNotATerminal(t *testing.T) {
	devNull := openDevNull(t)
	st, err := devNull.Stat()
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device here; the case does not discriminate", os.DevNull)
	}
	if term.IsTerminal(devNull) {
		t.Fatalf("IsTerminal(%s) = true, want false", os.DevNull)
	}
	c := term.New(term.Config{
		Stream: term.Stdout,
		Getenv: envOf(colorTerm),
		Probe:  term.FileProbe(nil, devNull, nil),
	})
	if c.Terminal() {
		t.Errorf("Terminal() = true on %s, want false", os.DevNull)
	}
	if c.Color() {
		t.Errorf("Color() = true on %s, want false", os.DevNull)
	}
}

// DHF-TEST: keel/requirement-165
// Regular files, pipes, nil and closed files are not terminals either.
func TestIsTerminalRejectsNonTerminals(t *testing.T) {
	regular, err := os.Create(filepath.Join(t.TempDir(), "regular"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = regular.Close() })
	closed, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = closed.Close()

	for name, f := range map[string]*os.File{
		"regular file": regular,
		"pipe":         openPipe(t),
		"nil":          nil,
		"closed":       closed,
	} {
		if term.IsTerminal(f) {
			t.Errorf("IsTerminal(%s) = true, want false", name)
		}
		if size, ok := term.FileSize(f); ok {
			t.Errorf("FileSize(%s) = %v, true; want ok=false", name, size)
		}
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-701 through the injected seam: each stream is answered from its own
// detection, never from one process-wide verdict.
func TestStreamsAreAnsweredIndependently(t *testing.T) {
	probe := fakeProbe{terminal: map[term.Stream]bool{term.Stderr: true}}
	out := term.New(term.Config{Stream: term.Stdout, Getenv: envOf(colorTerm), Probe: probe})
	diag := term.New(term.Config{Stream: term.Stderr, Getenv: envOf(colorTerm), Probe: probe})
	if out.Terminal() || out.Color() {
		t.Errorf("stdout on a pipe: Terminal=%v Color=%v, want false false", out.Terminal(), out.Color())
	}
	if !diag.Terminal() || !diag.Color() {
		t.Errorf("stderr on a terminal: Terminal=%v Color=%v, want true true", diag.Terminal(), diag.Color())
	}
	if out.Stream() != term.Stdout || diag.Stream() != term.Stderr {
		t.Errorf("Stream() = %v, %v; want stdout, stderr", out.Stream(), diag.Stream())
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-702: terminal-emulator markers in the environment never make a pipe
// report as a terminal.
func TestEnvironmentMarkersAreNotEvidence(t *testing.T) {
	env := envOf(map[string]string{
		"ZELLIJ":              "0",
		"ZELLIJ_SESSION_NAME": "main",
		"ZELLIJ_PANE_ID":      "3",
		"TERM_PROGRAM":        "WezTerm",
		"TERM":                "xterm-256color",
		"COLORTERM":           "truecolor",
		"FORCE_COLOR":         "1",
		"CLICOLOR_FORCE":      "1",
	})
	pipe := openPipe(t)
	for _, s := range []term.Stream{term.Stdin, term.Stdout, term.Stderr} {
		files := [3]*os.File{}
		files[s] = pipe
		c := term.New(term.Config{Stream: s, Getenv: env, Probe: term.FileProbe(files[0], files[1], files[2])})
		if c.Terminal() || c.Color() || c.Animation() || c.Prompt() {
			t.Errorf("%v on a pipe: Terminal=%v Color=%v Animation=%v Prompt=%v, want all false",
				s, c.Terminal(), c.Color(), c.Animation(), c.Prompt())
		}
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-703: explicit policy beats NO_COLOR; the rest of the precedence
// order is pinned beside it.
func TestColorPrecedence(t *testing.T) {
	noColor := map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}
	cases := []struct {
		name     string
		env      map[string]string
		policy   term.ColorPolicy
		terminal bool
		want     bool
	}{
		{"always beats NO_COLOR on a terminal", noColor, term.ColorAlways, true, true},
		{"always beats NO_COLOR and detection", noColor, term.ColorAlways, false, true},
		{"always beats TERM=dumb", map[string]string{"TERM": "dumb"}, term.ColorAlways, true, true},
		{"never beats a terminal", colorTerm, term.ColorNever, true, false},
		{"auto honors NO_COLOR", noColor, term.ColorAuto, true, false},
		{"auto ignores empty NO_COLOR", map[string]string{"TERM": "xterm", "NO_COLOR": ""}, term.ColorAuto, true, true},
		{"auto honors TERM=dumb", map[string]string{"TERM": "dumb"}, term.ColorAuto, true, false},
		{"auto honors TERM unset", map[string]string{}, term.ColorAuto, true, false},
		{"auto on a terminal", colorTerm, term.ColorAuto, true, true},
		{"auto off a terminal", colorTerm, term.ColorAuto, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := fakeProbe{terminal: map[term.Stream]bool{term.Stdout: tc.terminal}}
			c := term.New(term.Config{Stream: term.Stdout, Color: tc.policy, Getenv: envOf(tc.env), Probe: probe})
			if got := c.Color(); got != tc.want {
				t.Errorf("Color() = %v, want %v", got, tc.want)
			}
			if c.Terminal() != tc.terminal {
				t.Errorf("Terminal() = %v changed by policy or environment, want %v", c.Terminal(), tc.terminal)
			}
		})
	}
}

// DHF-TEST: keel/requirement-165 (keel/ac-731)
// Animation needs a terminal on stderr, a usable TERM, and color permitted on
// stderr; color policy cannot force it into a pipe. The color decision is the
// one for stderr, not for Config.Stream.
func TestAnimation(t *testing.T) {
	noColor := map[string]string{"TERM": "xterm-256color", "NO_COLOR": "1"}
	cases := []struct {
		name   string
		stdout bool
		stderr bool
		env    map[string]string
		policy term.ColorPolicy
		want   bool
	}{
		{"terminal stderr", true, true, colorTerm, term.ColorAuto, true},
		{"piped stderr", true, false, colorTerm, term.ColorAuto, false},
		{"piped stderr, color forced", true, false, colorTerm, term.ColorAlways, false},
		{"TERM=dumb", true, true, map[string]string{"TERM": "dumb"}, term.ColorAlways, false},
		{"TERM unset", true, true, map[string]string{}, term.ColorAuto, false},
		{"terminal stderr, color never", true, true, colorTerm, term.ColorNever, false},
		{"terminal stderr, NO_COLOR", true, true, noColor, term.ColorAuto, false},
		{"terminal stderr, NO_COLOR, color forced", true, true, noColor, term.ColorAlways, true},
		{"piped stdout, terminal stderr, color never", false, true, colorTerm, term.ColorNever, false},
		{"piped stdout, terminal stderr", false, true, colorTerm, term.ColorAuto, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := fakeProbe{terminal: map[term.Stream]bool{term.Stdout: tc.stdout, term.Stderr: tc.stderr}}
			c := term.New(term.Config{Stream: term.Stdout, Color: tc.policy, Getenv: envOf(tc.env), Probe: probe})
			if got := c.Animation(); got != tc.want {
				t.Errorf("Animation() = %v, want %v", got, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-166 (keel/ac-707)
// NoAnimation removes the permission a terminal stderr would otherwise grant.
func TestNoAnimationForbidsAnimationOnATerminal(t *testing.T) {
	c := term.New(term.Config{Stream: term.Stderr, NoAnimation: true, Getenv: envOf(colorTerm), Probe: allTerminals()})
	if c.Animation() {
		t.Fatal("NoAnimation on a terminal: Animation() = true")
	}
	if !term.New(term.Config{Stream: term.Stderr, Getenv: envOf(colorTerm), Probe: allTerminals()}).Animation() {
		t.Fatal("baseline on a terminal: Animation() = false")
	}
}

// DHF-TEST: keel/requirement-165
// Prompting needs a terminal on stdin and no --no-input.
func TestPrompt(t *testing.T) {
	cases := []struct {
		name    string
		stdin   bool
		noInput bool
		want    bool
	}{
		{"terminal stdin", true, false, true},
		{"terminal stdin, no-input", true, true, false},
		{"piped stdin", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := fakeProbe{terminal: map[term.Stream]bool{term.Stdin: tc.stdin, term.Stderr: true}}
			c := term.New(term.Config{Stream: term.Stderr, NoInput: tc.noInput, Getenv: envOf(colorTerm), Probe: probe})
			if got := c.Prompt(); got != tc.want {
				t.Errorf("Prompt() = %v, want %v", got, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-704 through the injected seam: the same value reports a changed
// size in both dimensions.
func TestSizeIsLiveOnTheSameValue(t *testing.T) {
	size := &term.Size{Rows: 40, Cols: 120}
	probe := allTerminals()
	probe.size = size
	c := term.New(term.Config{Stream: term.Stderr, Getenv: envOf(colorTerm), Probe: probe})
	if got := c.Size(); got != *size {
		t.Fatalf("Size() = %v, want %v", got, *size)
	}
	*size = term.Size{Rows: 52, Cols: 175}
	if got := c.Size(); got != (term.Size{Rows: 52, Cols: 175}) {
		t.Errorf("Size() after resize = %v, want {52 175}", got)
	}
}

// DHF-TEST: keel/requirement-165
// Effective size falls back per dimension: terminal, then LINES and COLUMNS,
// then 80 by 24.
func TestSizeFallback(t *testing.T) {
	cases := []struct {
		name  string
		probe *term.Size
		env   map[string]string
		want  term.Size
	}{
		{"terminal answer wins", &term.Size{Rows: 30, Cols: 100}, map[string]string{"LINES": "10", "COLUMNS": "20"}, term.Size{Rows: 30, Cols: 100}},
		{"no terminal, env", nil, map[string]string{"LINES": "10", "COLUMNS": "20"}, term.Size{Rows: 10, Cols: 20}},
		{"no terminal, no env", nil, map[string]string{}, term.DefaultSize},
		{"garbage env", nil, map[string]string{"LINES": "x", "COLUMNS": "-3"}, term.DefaultSize},
		{"zero terminal dimension falls back", &term.Size{Rows: 0, Cols: 100}, map[string]string{"LINES": "33"}, term.Size{Rows: 33, Cols: 100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := allTerminals()
			probe.size = tc.probe
			c := term.New(term.Config{Stream: term.Stdout, Getenv: envOf(tc.env), Probe: probe})
			if got := c.Size(); got != tc.want {
				t.Errorf("Size() = %v, want %v", got, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-165
// The zero Config resolves against the real process: nothing panics and the
// answer matches the ioctl on os.Stdin.
func TestZeroConfigUsesTheProcess(t *testing.T) {
	c := term.New(term.Config{})
	if c.Stream() != term.Stdin {
		t.Errorf("Stream() = %v, want stdin", c.Stream())
	}
	if c.Terminal() != term.IsTerminal(os.Stdin) {
		t.Errorf("Terminal() = %v, IsTerminal(os.Stdin) = %v", c.Terminal(), term.IsTerminal(os.Stdin))
	}
	if got := c.Size(); got.Rows <= 0 || got.Cols <= 0 {
		t.Errorf("Size() = %v, want positive dimensions", got)
	}
	if term.OSProbe().IsTerminal(term.Stream(7)) {
		t.Error("an unknown stream reported as a terminal")
	}
}

// DHF-TEST: keel/requirement-165
func TestStreamString(t *testing.T) {
	for s, want := range map[term.Stream]string{term.Stdin: "stdin", term.Stdout: "stdout", term.Stderr: "stderr", term.Stream(9): "stream(9)"} {
		if got := s.String(); got != want {
			t.Errorf("Stream(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-705: go list -deps names no other keel package and nothing outside
// the standard library.
func TestPackageIsALeaf(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain not on PATH: %v", err)
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	const self = "github.com/david-aggeler/keel/term"
	var seen bool
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch line {
		case "":
		case self:
			seen = true
		default:
			t.Errorf("non-standard dependency %q", line)
		}
	}
	if !seen {
		t.Errorf("go list -deps did not report %s itself; output:\n%s", self, out)
	}
}
