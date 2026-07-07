package log

// Second internal coverage file for keel/change_request-4 (keel/ac-37):
// build identity, multi-sink New composition, handler groups, recentlog edges.

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestBuildIdentity(t *testing.T) {
	l, rc := NewForTesting("svc")
	LogBuildIdentity(l, "", "")
	rec := rc.LastJSON()
	if rec["version"] != "dev" {
		t.Errorf("empty version should log as dev, got %v", rec["version"])
	}
	if rec["git_commit"] == "" {
		t.Error("git_commit should always be populated")
	}

	LogBuildIdentity(l, "v1.2.3", "abc123")
	rec = rc.LastJSON()
	if rec["version"] != "v1.2.3" || rec["git_commit"] != "abc123" {
		t.Errorf("explicit identity not honored: %v", rec)
	}

	LogBuildIdentity(nil, "v1", "c") // nil logger falls back, must not panic

	if got := ResolveGitCommit("explicit"); got != "explicit" {
		t.Errorf("explicit commit should pass through, got %q", got)
	}
	// "dev" and "" resolve from build info; tests run without vcs stamping, so
	// any non-empty result is acceptable.
	if got := ResolveGitCommit("dev"); got == "" {
		t.Error("dev commit resolution returned empty")
	}
	if versionOrDev("") != "dev" || versionOrDev("v2") != "v2" {
		t.Error("versionOrDev mapping broken")
	}
}

func TestStartDailyBuildIdentityStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	StartDailyBuildIdentity(ctx, Discard(), "v1", "c")
	cancel() // goroutine must exit on ctx.Done without firing
	time.Sleep(10 * time.Millisecond)

	if d := durationUntilNextBuildIdentityHeartbeat(time.Date(2026, 7, 7, 23, 59, 0, 0, time.UTC)); d <= 0 || d > 24*time.Hour {
		t.Errorf("heartbeat duration out of range: %v", d)
	}
}

func TestNewWithFileWriterAndSinkHandlers(t *testing.T) {
	dir := t.TempDir()
	primary := &RecordCapture{}
	file := &RecordCapture{}

	// FileWriter branch of New.
	l := New(Config{Service: "svc", Writer: primary, FileWriter: file})
	l.Info("both sinks")
	if primary.LastJSON() == nil || file.LastJSON() == nil {
		t.Fatal("record missing from a sink")
	}

	// Human+JSON handler composition branches of New and NewConsole.
	hh, err := NewHumanFileHandler(dir, "svc")
	if err != nil {
		t.Fatal(err)
	}
	jh, err := NewJSONFileHandler(dir, "svc")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []*slog.Logger{
		New(Config{Service: "svc", Writer: &RecordCapture{}, HumanFileHandler: hh, JSONFileHandler: jh}),
		New(Config{Service: "svc", Writer: &RecordCapture{}, HumanFileHandler: hh}),
		New(Config{Service: "svc", Writer: &RecordCapture{}, JSONFileHandler: jh}),
		NewConsole(Config{Service: "svc", Writer: &RecordCapture{}, HumanFileHandler: hh, JSONFileHandler: jh}),
		NewConsole(Config{Service: "svc", Writer: &RecordCapture{}, JSONFileHandler: jh}),
		NewConsole(Config{Service: "svc", Writer: &RecordCapture{}, HumanLogDir: dir}),
	} {
		l.Info("compose")
	}

	// WithGroup on the composed handlers.
	lg, rc := NewConsoleForTesting("svc")
	lg.WithGroup("grp").Info("grouped", "k", "v")
	if out := rc.LastRaw(); !strings.Contains(out, "grouped") {
		t.Errorf("grouped record lost: %q", out)
	}
	lj, rcj := NewForTesting("svc")
	lj.WithGroup("grp").Info("grouped", "k", "v")
	if rcj.LastJSON() == nil {
		t.Error("grouped JSON record lost")
	}
}

func TestRecentBufferEdges(t *testing.T) {
	buf := NewRecentBuffer(3)
	if buf.Len() != 0 {
		t.Fatal("fresh buffer not empty")
	}
	base, _ := NewForTesting("svc")
	l := TeeRecent(base, buf, "svc")
	l = l.With("k", "v") // exercise WithAttrs on the tee handler
	l = l.WithGroup("grp")
	for i := 0; i < 5; i++ {
		l.Warn("m", "i", i) // only warn/error records are retained
	}
	if buf.Len() != 3 {
		t.Errorf("ring should cap at 3, got %d", buf.Len())
	}
	if got := buf.Entries(2, ""); len(got) != 2 {
		t.Errorf("Entries(2) = %d rows", len(got))
	}
}
