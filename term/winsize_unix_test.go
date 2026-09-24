//go:build linux || darwin

package term_test

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// setWinsize resizes the terminal behind f with TIOCSWINSZ, the same call a
// terminal emulator makes when its window is dragged.
func setWinsize(t *testing.T, f *os.File, rows, cols uint16) {
	t.Helper()
	ws := struct{ Row, Col, Xpixel, Ypixel uint16 }{Row: rows, Col: cols}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws))); errno != 0 {
		t.Fatalf("TIOCSWINSZ %dx%d on %s: %v", rows, cols, f.Name(), errno)
	}
}
