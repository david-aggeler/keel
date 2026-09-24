//go:build darwin

package term

import "syscall"

// ioctlReadTermios is the request that reads terminal attributes on Darwin.
const ioctlReadTermios = uintptr(syscall.TIOCGETA)
