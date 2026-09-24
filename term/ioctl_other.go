//go:build !linux && !darwin

package term

// isTerminal reports false: this platform has no ioctl binding here, and an
// unanswerable probe means not-a-terminal.
func isTerminal(uintptr) bool { return false }

// windowSize reports no size, so Size falls back to LINES, COLUMNS and
// DefaultSize.
func windowSize(uintptr) (Size, bool) { return Size{}, false }
