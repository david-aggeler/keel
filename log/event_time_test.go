package log_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	logging "github.com/david-aggeler/keel/log"
)

// eventTimeProfile is one output profile of keel/requirement-170, expressed as
// the logging.Config amendments the keel/cli flags select: -v lowers the
// console floor to Debug, --quiet raises it to Warn, --plain and color-off
// disable color, color-on forces it, and redirected sends the console to a
// file instead of an in-memory writer.
type eventTimeProfile struct {
	name         string
	level        slog.Level
	forceColor   bool
	disableColor bool
	redirected   bool
}

var eventTimeMachineProfiles = []eventTimeProfile{
	{name: "default", level: slog.LevelInfo},
	{name: "verbose", level: slog.LevelDebug},
	{name: "quiet", level: slog.LevelWarn},
	{name: "plain", level: slog.LevelInfo, disableColor: true},
	{name: "redirected", level: slog.LevelInfo, redirected: true},
}

var eventTimeHumanProfiles = append(slicesCloneProfiles(eventTimeMachineProfiles),
	eventTimeProfile{name: "color-on", level: slog.LevelInfo, forceColor: true},
	eventTimeProfile{name: "color-off", level: slog.LevelInfo, disableColor: true},
)

func slicesCloneProfiles(in []eventTimeProfile) []eventTimeProfile {
	return append([]eventTimeProfile(nil), in...)
}

// runEventTimeProfile drives the ac-725/726/727 emission (Header banner,
// Section banner, Info record, Warn record) under one console mode and one
// output profile. It returns the console lines and the wall-clock window the
// records were stamped in.
func runEventTimeProfile(t *testing.T, console logging.Console, p eventTimeProfile) (lines []string, before, after time.Time) {
	t.Helper()
	var buf bytes.Buffer
	var w io.Writer = &buf
	var file *os.File
	if p.redirected {
		f, err := os.Create(filepath.Join(t.TempDir(), "console.out"))
		if err != nil {
			t.Fatalf("create redirect target: %v", err)
		}
		file = f
		w = f
	}
	logger := mustNewLogger(t, logging.Config{
		Service:          "svc",
		Console:          console,
		ConsoleVerbosity: p.level,
		Writer:           w,
		ForceColor:       p.forceColor,
		DisableColor:     p.disableColor,
	})

	before = time.Now()
	logger.Header("event-time probe", "1.2.3")
	logger.Section("records")
	logger.Info("info record", "k", "v")
	logger.Warn("warn record", "k", "v")
	after = time.Now()

	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	out := buf.String()
	if file != nil {
		if err := file.Close(); err != nil {
			t.Fatalf("close redirect target: %v", err)
		}
		b, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatalf("read redirect target: %v", err)
		}
		out = string(b)
	}
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		t.Fatalf("profile %s wrote no console lines", p.name)
	}
	lines = strings.Split(trimmed, "\n")
	if p.level >= slog.LevelWarn {
		if len(lines) != 1 {
			t.Fatalf("quiet profile lines = %q, want exactly the Warn record", lines)
		}
	} else if len(lines) < 4 {
		t.Fatalf("profile %s lines = %q, want header, section, info and warn", p.name, lines)
	}
	return lines, before, after
}

// assertMachineTS decodes line into a raw map — never a struct, so a missing
// key cannot hide behind a zero value — and requires a top-level RFC 3339 ts
// inside the emission window to the second.
func assertMachineTS(t *testing.T, line string, before, after time.Time) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("console line is not a JSON object: %q: %v", line, err)
	}
	raw, ok := obj["ts"]
	if !ok {
		t.Fatalf("console line has no top-level ts: %q", line)
	}
	s, ok := raw.(string)
	if !ok {
		t.Fatalf("ts = %#v, want RFC 3339 string: %q", raw, line)
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("ts %q does not parse as RFC 3339: %v", s, err)
	}
	if ts.Before(before.Truncate(time.Second)) || ts.After(after) {
		t.Fatalf("ts %s outside record time window [%s, %s]", ts, before.Truncate(time.Second), after)
	}
	return obj
}

// DHF-TEST: keel/requirement-170, keel/requirement-17, keel/ac-726
func TestAIConsoleEveryEventCarriesTSUnderEveryProfile(t *testing.T) {
	for _, p := range eventTimeMachineProfiles {
		t.Run(p.name, func(t *testing.T) {
			lines, before, after := runEventTimeProfile(t, logging.ConsoleSparseAI, p)
			events := map[string]bool{}
			for _, line := range lines {
				obj := assertMachineTS(t, line, before, after)
				ev, _ := obj["event"].(string)
				events[ev] = true
			}
			if p.level < slog.LevelWarn && (!events["header"] || !events["section"]) {
				t.Fatalf("banner events missing from %q; want header and section to carry ts", lines)
			}
		})
	}
}

// DHF-TEST: keel/requirement-170, keel/ac-727
func TestJSONConsoleEveryRecordCarriesTSUnderEveryProfile(t *testing.T) {
	for _, p := range eventTimeMachineProfiles {
		t.Run(p.name, func(t *testing.T) {
			lines, before, after := runEventTimeProfile(t, logging.ConsoleJSON, p)
			banners := 0
			for _, line := range lines {
				obj := assertMachineTS(t, line, before, after)
				if _, ok := obj["banner"]; ok {
					banners++
				}
			}
			if p.level < slog.LevelWarn && banners < 2 {
				t.Fatalf("banner records missing from %q; want header and section to carry ts", lines)
			}
		})
	}
}

var (
	ansiEscape    = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	humanLineTime = regexp.MustCompile(`^(\d{2}:\d{2}:\d{2})`)
)

// DHF-TEST: keel/requirement-170, keel/ac-725
func TestHumanConsoleEveryLineBeginsWithEventTimeUnderEveryProfile(t *testing.T) {
	for _, p := range eventTimeHumanProfiles {
		t.Run(p.name, func(t *testing.T) {
			lines, before, after := runEventTimeProfile(t, logging.ConsolePlain, p)
			lo := before.Truncate(time.Second)
			hi := after.Truncate(time.Second)
			for _, line := range lines {
				visible := ansiEscape.ReplaceAllString(line, "")
				m := humanLineTime.FindStringSubmatch(visible)
				if m == nil {
					t.Fatalf("human console line does not begin with HH:MM:SS: %q", line)
				}
				if m[1] != lo.Format(time.TimeOnly) && m[1] != hi.Format(time.TimeOnly) {
					t.Fatalf("line time %s, want record time %s..%s: %q", m[1], lo.Format(time.TimeOnly), hi.Format(time.TimeOnly), line)
				}
			}
		})
	}
}
