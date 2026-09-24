//go:build linux || darwin

package term_test

import (
	"os"
	"testing"

	"github.com/david-aggeler/keel/term"
)

// DHF-TEST: keel/requirement-165
// TestLiveTerminalSmoke is the terminal-attached regression guard for the
// ioctl on the process's own controlling terminal. It is gated on
// KEEL_TERM_LIVE_SMOKE: CI never sets that variable, so the test always SKIPs
// in the gate. Run it locally from an interactive terminal:
//
//	KEEL_TERM_LIVE_SMOKE=1 go test ./term -run TestLiveTerminalSmoke -v
//
// It resizes the controlling terminal by one row and one column, reads the
// size through the same Capability, and restores the original size.
func TestLiveTerminalSmoke(t *testing.T) {
	if os.Getenv("KEEL_TERM_LIVE_SMOKE") == "" {
		t.Skip("set KEEL_TERM_LIVE_SMOKE=1 from an interactive terminal to run the live terminal smoke test")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open /dev/tty (run from an interactive terminal): %v", err)
	}
	defer func() { _ = tty.Close() }()

	if !term.IsTerminal(tty) {
		t.Fatal("IsTerminal(/dev/tty) = false on the controlling terminal")
	}
	c := term.New(term.Config{Stream: term.Stdout, Probe: term.FileProbe(nil, tty, nil)})
	if !c.Terminal() {
		t.Fatal("Terminal() = false on the controlling terminal")
	}
	before, ok := term.FileSize(tty)
	if !ok {
		t.Fatal("FileSize(/dev/tty) failed on the controlling terminal")
	}
	t.Logf("controlling terminal size %v", before)

	want := term.Size{Rows: before.Rows + 1, Cols: before.Cols + 1}
	setWinsize(t, tty, uint16(want.Rows), uint16(want.Cols))
	defer setWinsize(t, tty, uint16(before.Rows), uint16(before.Cols))
	if got := c.Size(); got != want {
		t.Errorf("Size() after resize = %v, want %v", got, want)
	}
}
