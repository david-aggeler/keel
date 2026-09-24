//go:build linux

package term_test

import (
	"testing"

	"github.com/david-aggeler/keel/term"
)

// DHF-TEST: keel/requirement-165
// Positive control for the ioctl: a real pty is a terminal. Without it, every
// false answer above could come from an ioctl that never says true.
func TestPTYIsATerminal(t *testing.T) {
	controller, tty := openPTY(t)
	if !term.IsTerminal(tty) {
		t.Errorf("IsTerminal(%s) = false, want true", tty.Name())
	}
	if !term.IsTerminal(controller) {
		t.Errorf("IsTerminal(ptmx) = false, want true")
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-701 on real file descriptors: stdout on a pipe and stderr on a pty
// answer differently.
func TestPipedStdoutTerminalStderr(t *testing.T) {
	_, tty := openPTY(t)
	probe := term.FileProbe(nil, openPipe(t), tty)
	env := envOf(colorTerm)
	out := term.New(term.Config{Stream: term.Stdout, Getenv: env, Probe: probe})
	errc := term.New(term.Config{Stream: term.Stderr, Getenv: env, Probe: probe})
	if out.Terminal() || out.Color() {
		t.Errorf("stdout on a pipe: Terminal=%v Color=%v, want false false", out.Terminal(), out.Color())
	}
	if !errc.Terminal() || !errc.Color() || !errc.Animation() {
		t.Errorf("stderr on a pty: Terminal=%v Color=%v Animation=%v, want true true true",
			errc.Terminal(), errc.Color(), errc.Animation())
	}
}

// DHF-TEST: keel/requirement-165
// keel/ac-704 on a real pty: resizing rows and columns independently is seen
// by the next Size call on the same value.
func TestPTYResizeIsSeenLive(t *testing.T) {
	controller, tty := openPTY(t)
	setWinsize(t, controller, 30, 120)
	c := term.New(term.Config{Stream: term.Stdout, Getenv: envOf(colorTerm), Probe: term.FileProbe(nil, tty, nil)})
	if got := c.Size(); got != (term.Size{Rows: 30, Cols: 120}) {
		t.Fatalf("Size() = %v, want {30 120}", got)
	}
	setWinsize(t, controller, 30, 175)
	if got := c.Size(); got != (term.Size{Rows: 30, Cols: 175}) {
		t.Errorf("Size() after column resize = %v, want {30 175}", got)
	}
	setWinsize(t, controller, 48, 175)
	if got := c.Size(); got != (term.Size{Rows: 48, Cols: 175}) {
		t.Errorf("Size() after row resize = %v, want {48 175}", got)
	}
	if size, ok := term.FileSize(tty); !ok || size != (term.Size{Rows: 48, Cols: 175}) {
		t.Errorf("FileSize = %v, %v; want {48 175}, true", size, ok)
	}
}
