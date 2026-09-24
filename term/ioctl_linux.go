//go:build linux

package term

import "syscall"

// ioctlReadTermios is the request that reads terminal attributes on Linux.
const ioctlReadTermios = uintptr(syscall.TCGETS)
