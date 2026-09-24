//go:build linux || darwin

package term

import (
	"syscall"
	"unsafe"
)

// isTerminal asks the terminal driver for fd's attributes. Only a terminal
// answers; every other descriptor — /dev/null included — fails with ENOTTY.
func isTerminal(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlReadTermios, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// winsize mirrors the kernel's struct winsize.
type winsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// windowSize reads fd's rows and columns with one TIOCGWINSZ.
func windowSize(fd uintptr) (Size, bool) {
	var ws winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return Size{}, false
	}
	return Size{Rows: int(ws.Row), Cols: int(ws.Col)}, true
}
