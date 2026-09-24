package log_test

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// humanLevelLabel matches the level label directly after the HH:MM:SS event
// time of an ANSI-stripped human console line.
var humanLevelLabel = regexp.MustCompile(`^\d{2}:\d{2}:\d{2} (DEBUG|INFO|WARN|ERROR)  `)

// DHF-TEST: keel/requirement-171, keel/ac-732
func TestHumanConsoleEveryLineCarriesItsLevelLabelUnderEveryProfile(t *testing.T) {
	profiles := []struct {
		name         string
		level        slog.Level
		forceColor   bool
		disableColor bool
	}{
		{name: "default", level: slog.LevelInfo},
		{name: "verbose", level: slog.LevelDebug},
		{name: "quiet", level: slog.LevelWarn},
		{name: "color-on", level: slog.LevelInfo, forceColor: true},
		{name: "color-off", level: slog.LevelInfo, disableColor: true},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := mustNewLogger(t, logging.Config{
				Service:          "svc",
				Console:          logging.ConsolePlain,
				ConsoleVerbosity: p.level,
				Writer:           &buf,
				ForceColor:       p.forceColor,
				DisableColor:     p.disableColor,
			})
			logger.Header("level-label probe", "1.2.3")
			logger.Info("info record")
			logger.Warn("warn record")
			logger.Error("error record")
			if err := logger.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
			want := map[string]string{"info record": "INFO", "warn record": "WARN", "error record": "ERROR", "level-label probe": "INFO"}
			seen := map[string]bool{}
			for _, line := range lines {
				visible := ansiEscape.ReplaceAllString(line, "")
				m := humanLevelLabel.FindStringSubmatch(visible)
				if m == nil {
					t.Fatalf("human console line has no level label after the event time: %q", line)
				}
				for msg, label := range want {
					if strings.Contains(visible, msg) {
						seen[msg] = true
						if m[1] != label {
							t.Fatalf("line %q label = %s, want %s", visible, m[1], label)
						}
					}
				}
			}
			if !seen["warn record"] || !seen["error record"] {
				t.Fatalf("warn/error records missing from console lines %q", lines)
			}
			if p.level < slog.LevelWarn && (!seen["info record"] || !seen["level-label probe"]) {
				t.Fatalf("header/info lines missing from console lines %q", lines)
			}
		})
	}
}
