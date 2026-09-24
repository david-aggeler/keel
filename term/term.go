package term

import (
	"os"
	"strconv"
)

// Stream names one of the process's three standard streams.
type Stream int

// The standard streams.
const (
	Stdin Stream = iota
	Stdout
	Stderr
)

// String returns the conventional stream name.
func (s Stream) String() string {
	switch s {
	case Stdin:
		return "stdin"
	case Stdout:
		return "stdout"
	case Stderr:
		return "stderr"
	}
	return "stream(" + strconv.Itoa(int(s)) + ")"
}

// ColorPolicy is the explicit color policy a caller supplies for one
// invocation, typically parsed from a --color flag.
type ColorPolicy int

// Color policies. The zero value is [ColorAuto].
const (
	// ColorAuto permits color when the stream is a terminal and the
	// environment does not forbid it.
	ColorAuto ColorPolicy = iota
	// ColorAlways permits color regardless of detection and environment.
	ColorAlways
	// ColorNever forbids color regardless of detection and environment.
	ColorNever
)

// Size is a terminal size in character cells.
type Size struct {
	// Rows is the number of lines.
	Rows int
	// Cols is the number of columns.
	Cols int
}

// DefaultSize is the size reported when neither the terminal nor the LINES
// and COLUMNS variables supply one.
var DefaultSize = Size{Rows: 24, Cols: 80}

// Probe answers the detection questions for the standard streams. It is the
// injection seam: [OSProbe] asks the operating system, a test supplies its
// own answer.
type Probe interface {
	// IsTerminal reports whether s is connected to a terminal.
	IsTerminal(s Stream) bool
	// Size reports the terminal size of s. ok is false when s is not a
	// terminal or the size cannot be read.
	Size(s Stream) (size Size, ok bool)
}

// Config carries the inputs [New] resolves a [Capability] from. The zero
// value describes stdin with automatic color, the process environment, and
// the operating system's answer.
type Config struct {
	// Stream is the stream the capability describes: the target of Color and
	// Size.
	Stream Stream
	// Color is the explicit color policy. It overrides the environment and
	// detection.
	Color ColorPolicy
	// NoInput forbids prompting, as a --no-input flag would.
	NoInput bool
	// Getenv reads the environment. Nil means [os.Getenv].
	Getenv func(string) string
	// Probe answers detection. Nil means [OSProbe].
	Probe Probe
}

// Capability is the resolved, read-only terminal capability of one stream.
// Build it with [New]; it has no setters. Every field except size is fixed at
// construction.
type Capability struct {
	stream    Stream
	terminal  bool
	color     bool
	animation bool
	prompt    bool
	probe     Probe
	getenv    func(string) string
}

// New resolves the capability described by cfg. Detection and environment
// are read once, here; only [Capability.Size] reads again later.
//
// DHF-REQ: keel/requirement-165
func New(cfg Config) Capability {
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	probe := cfg.Probe
	if probe == nil {
		probe = OSProbe()
	}
	terminal := probe.IsTerminal(cfg.Stream)
	termUsable := termUsable(getenv("TERM"))

	var color bool
	switch cfg.Color {
	case ColorAlways:
		color = true
	case ColorNever:
		color = false
	default:
		color = terminal && getenv("NO_COLOR") == "" && termUsable
	}

	return Capability{
		stream:    cfg.Stream,
		terminal:  terminal,
		color:     color,
		animation: termUsable && probe.IsTerminal(Stderr),
		prompt:    !cfg.NoInput && probe.IsTerminal(Stdin),
		probe:     probe,
		getenv:    getenv,
	}
}

// termUsable reports whether TERM names a terminal that renders escapes: set,
// non-empty, and not "dumb".
func termUsable(v string) bool {
	return v != "" && v != "dumb"
}

// Stream returns the stream this capability describes.
func (c Capability) Stream() Stream { return c.stream }

// Terminal reports whether the stream is connected to a terminal. It is
// detection only: neither policy nor environment changes it.
func (c Capability) Terminal() bool { return c.terminal }

// Color reports whether output on the stream may carry color escapes:
// [ColorAlways] and [ColorNever] decide outright; under [ColorAuto] the stream
// must be a terminal, NO_COLOR must be unset or empty, and TERM must be set
// and not "dumb".
func (c Capability) Color() bool { return c.color }

// Animation reports whether spinners and redrawn progress may be drawn: stderr
// must be a terminal and TERM must be set and not "dumb". No color policy
// forces it, so animation never reaches a pipe.
func (c Capability) Animation() bool { return c.animation }

// Prompt reports whether the process may prompt for input: stdin must be a
// terminal and [Config.NoInput] must be false.
func (c Capability) Prompt() bool { return c.prompt }

// Size returns the stream's terminal size, read live on every call. Each
// dimension falls back independently: the terminal's answer, then LINES or
// COLUMNS, then [DefaultSize]. LINES and COLUMNS are weak fallbacks — a child
// inherits a frozen copy, so neither tracks a resize.
func (c Capability) Size() Size {
	var detected Size
	if c.probe != nil {
		detected, _ = c.probe.Size(c.stream)
	}
	getenv := c.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return Size{
		Rows: dimension(detected.Rows, getenv("LINES"), DefaultSize.Rows),
		Cols: dimension(detected.Cols, getenv("COLUMNS"), DefaultSize.Cols),
	}
}

// dimension picks the first positive of a detected value, an environment
// value, and a default.
func dimension(detected int, env string, def int) int {
	if detected > 0 {
		return detected
	}
	if n, err := strconv.Atoi(env); err == nil && n > 0 {
		return n
	}
	return def
}

// IsTerminal reports whether f is connected to a terminal, by ioctl — never by
// file mode, so /dev/null is not a terminal. A nil file, a closed file, and
// any file the ioctl rejects report false.
//
// DHF-REQ: keel/requirement-165
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fd, ok := rawFd(f)
	return ok && isTerminal(fd)
}

// FileSize reads the terminal size of f by TIOCGWINSZ. ok is false when f is
// not a terminal or the ioctl fails.
//
// DHF-REQ: keel/requirement-165
func FileSize(f *os.File) (size Size, ok bool) {
	if f == nil {
		return Size{}, false
	}
	fd, ok := rawFd(f)
	if !ok {
		return Size{}, false
	}
	return windowSize(fd)
}

// rawFd returns f's descriptor without switching it to blocking mode, as
// f.Fd would. ok is false for a closed file.
func rawFd(f *os.File) (fd uintptr, ok bool) {
	sc, err := f.SyscallConn()
	if err != nil {
		return 0, false
	}
	if err := sc.Control(func(d uintptr) { fd = d }); err != nil {
		return 0, false
	}
	return fd, true
}

// OSProbe returns a [Probe] over the process's own os.Stdin, os.Stdout and
// os.Stderr.
func OSProbe() Probe { return FileProbe(os.Stdin, os.Stdout, os.Stderr) }

// FileProbe returns a [Probe] over the given files, each answered by its own
// ioctl. A nil file is not a terminal.
func FileProbe(stdin, stdout, stderr *os.File) Probe {
	return fileProbe{files: [3]*os.File{stdin, stdout, stderr}}
}

type fileProbe struct{ files [3]*os.File }

func (p fileProbe) file(s Stream) *os.File {
	if s < Stdin || s > Stderr {
		return nil
	}
	return p.files[s]
}

func (p fileProbe) IsTerminal(s Stream) bool { return IsTerminal(p.file(s)) }

func (p fileProbe) Size(s Stream) (Size, bool) { return FileSize(p.file(s)) }
