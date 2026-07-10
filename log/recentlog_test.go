package log

import (
	"strings"
	"testing"
)

func TestRecentBuffer_NewestFirstAndWraparound(t *testing.T) {
	b := NewRecentBuffer(3)
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		b.Add(RecentEntry{Level: "WARN", Message: m})
	}
	got := b.Entries(0, "")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (capacity bound)", len(got))
	}
	// Newest-first: e, d, c — a and b evicted.
	want := []string{"e", "d", "c"}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("entry[%d] = %q, want %q", i, got[i].Message, w)
		}
	}
}

// DHF-TEST: keel/requirement-20
func TestRecentBuffer_LevelFilterAndLimit(t *testing.T) {
	b := NewRecentBuffer(10)
	b.Add(RecentEntry{Level: "WARN", Message: "w1"})
	b.Add(RecentEntry{Level: "ERROR", Message: "e1"})
	b.Add(RecentEntry{Level: "WARN", Message: "w2"})
	b.Add(RecentEntry{Level: "ERROR", Message: "e2"})

	errs := b.Entries(0, "error")
	if len(errs) != 2 || errs[0].Message != "e2" || errs[1].Message != "e1" {
		t.Fatalf("error filter = %+v, want [e2 e1]", errs)
	}
	// Case-insensitive level filter.
	warns := b.Entries(0, "WARN")
	if len(warns) != 2 {
		t.Fatalf("warn filter len = %d, want 2", len(warns))
	}
	// Limit caps newest-first.
	one := b.Entries(1, "")
	if len(one) != 1 || one[0].Message != "e2" {
		t.Fatalf("limit 1 = %+v, want [e2]", one)
	}
}

// DHF-TEST: keel/requirement-20
func TestRecentBuffer_AddNormalizesLevelForFiltering(t *testing.T) {
	b := NewRecentBuffer(10)
	b.Add(RecentEntry{Level: "warn", Message: "lower-warn"})
	b.Add(RecentEntry{Level: "ErRoR", Message: "mixed-error"})

	errs := b.Entries(0, "ERROR")
	if len(errs) != 1 || errs[0].Message != "mixed-error" {
		t.Fatalf("error filter = %+v, want mixed-error entry", errs)
	}
	if errs[0].Level != "ERROR" {
		t.Fatalf("error level = %q, want ERROR", errs[0].Level)
	}

	warns := b.Entries(0, "warn")
	if len(warns) != 1 || warns[0].Message != "lower-warn" {
		t.Fatalf("warn filter = %+v, want lower-warn entry", warns)
	}
	if warns[0].Level != "WARN" {
		t.Fatalf("warn level = %q, want WARN", warns[0].Level)
	}
}

func TestRecentBuffer_EmptyAndClamp(t *testing.T) {
	b := NewRecentBuffer(0) // clamped to 1, must not panic
	if got := b.Entries(0, ""); len(got) != 0 {
		t.Fatalf("empty buffer = %+v, want []", got)
	}
	b.Add(RecentEntry{Level: "WARN", Message: "x"})
	b.Add(RecentEntry{Level: "WARN", Message: "y"})
	got := b.Entries(0, "")
	if len(got) != 1 || got[0].Message != "y" {
		t.Fatalf("cap-1 buffer = %+v, want [y]", got)
	}
}

// DHF-TEST: keel/requirement-20
func TestTeeRecent_CapturesWarnErrorOnly(t *testing.T) {
	base, _ := newForTesting("mcp-server")
	buf := NewRecentBuffer(100)
	logger := TeeRecent(base, buf, "mcp-server")

	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	got := buf.Entries(0, "")
	if len(got) != 2 {
		t.Fatalf("captured %d entries, want 2 (warn+error only): %+v", len(got), got)
	}
	if got[0].Level != "ERROR" || got[0].Message != "error msg" {
		t.Errorf("newest = %+v, want ERROR/error msg", got[0])
	}
	if got[1].Level != "WARN" || got[1].Message != "warn msg" {
		t.Errorf("oldest = %+v, want WARN/warn msg", got[1])
	}
	if got[0].Service != "mcp-server" {
		t.Errorf("service = %q, want mcp-server", got[0].Service)
	}
}

func TestTeeRecent_RedactsAtIngest(t *testing.T) {
	base, _ := newForTesting("web-ui")
	buf := NewRecentBuffer(10)
	logger := TeeRecent(base, buf, "web-ui")

	// Secret in the message (DSN form) and in attr values (sensitive key + DSN).
	logger.Error("clone failed for postgres://user:hunter2@db:5432/ob",
		"token", "ghp_supersecretvalue",
		"target", "https://ghp_pat@gitea.local/repo.git")

	got := buf.Entries(1, "")
	if len(got) != 1 {
		t.Fatalf("captured %d, want 1", len(got))
	}
	e := got[0]
	if strings.Contains(e.Message, "hunter2") {
		t.Errorf("message leaked DSN password: %q", e.Message)
	}
	if e.Attrs["token"] != "[REDACTED]" {
		t.Errorf("sensitive attr token = %q, want [REDACTED]", e.Attrs["token"])
	}
	if strings.Contains(e.Attrs["target"], "ghp_pat") {
		t.Errorf("target attr leaked PAT: %q", e.Attrs["target"])
	}
	if _, ok := e.Attrs["service"]; ok {
		t.Errorf("service attr must not be duplicated into Attrs")
	}
}

// TestTeeRecent_ForwardsToInner asserts the tee does not swallow records: the
// wrapped handler still sees every level.
func TestTeeRecent_ForwardsToInner(t *testing.T) {
	base, capture := newForTesting("svc")
	buf := NewRecentBuffer(10)
	logger := TeeRecent(base, buf, "svc")
	logger.Info("forwarded")
	logger.Warn("forwarded-warn")
	all := capture.AllJSON()
	if len(all) != 2 {
		t.Fatalf("inner handler saw %d records, want 2", len(all))
	}
}
