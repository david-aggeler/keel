//go:build linux

package term_test

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"unsafe"
)

// openPTY allocates a real pseudo-terminal pair through /dev/ptmx and returns
// its controller and its terminal end. The terminal end is a genuine terminal,
// so the ioctl path runs against the positive branch without a human at a
// keyboard. Both ends close on test cleanup.
func openPTY(t *testing.T) (controller, tty *os.File) {
	t.Helper()
	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { _ = controller.Close() })

	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, controller.Fd(), uintptr(syscall.TIOCSPTLCK), uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Fatalf("TIOCSPTLCK: %v", errno)
	}
	var n uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, controller.Fd(), uintptr(syscall.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		t.Fatalf("TIOCGPTN: %v", errno)
	}
	name := "/dev/pts/" + strconv.FormatUint(uint64(n), 10)
	tty, err = os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	return controller, tty
}
