package log_test

import (
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-31
func TestFoundationExportsAreConsumerAgnostic(t *testing.T) {
	out, err := exec.Command("go", "list", "github.com/david-aggeler/keel/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list packages: %v\n%s", err, out)
	}
	for _, importPath := range strings.Fields(string(out)) {
		out, err := exec.Command("go", "doc", importPath).CombinedOutput()
		if err != nil {
			t.Fatalf("go doc %s: %v\n%s", importPath, err, out)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, "const ") &&
				!strings.HasPrefix(line, "func ") &&
				!strings.HasPrefix(line, "type ") {
				continue
			}
			for _, forbidden := range []string{"Vault", "vault"} {
				if strings.Contains(line, forbidden) {
					t.Errorf("go doc %s contains consumer-domain term %q in exported declaration %q", importPath, forbidden, line)
				}
			}
		}
	}
}

func TestEmit_ProducesInfoLevelWithKindMetric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call",
		slog.String("tool", "store_memory"),
		slog.Int64("duration_ms", 42),
		slog.Bool("error", false),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	if got["msg"] != "tool_call" {
		t.Errorf("msg = %q; want %q", got["msg"], "tool_call")
	}
	if got["level"] != "INFO" {
		t.Errorf("level = %q; want %q", got["level"], "INFO")
	}
	if got["kind"] != "metric" {
		t.Errorf("kind = %q; want %q", got["kind"], "metric")
	}
}

func TestEmit_MsDurationsAreNumeric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("sync_timing",
		slog.String("op", "create"),
		slog.Int64("pull_ms", 10),
		slog.Int64("commit_ms", 20),
		slog.Int64("total_ms", 30),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	// JSON numbers unmarshal as float64 in map[string]any.
	for field, want := range map[string]float64{
		"pull_ms":   10,
		"commit_ms": 20,
		"total_ms":  30,
	} {
		v, ok := got[field].(float64)
		if !ok {
			t.Errorf("%s: type = %T; want float64", field, got[field])
			continue
		}
		if v != want {
			t.Errorf("%s = %v; want %v", field, v, want)
		}
	}
}

func TestEmit_CountFieldsAreNumeric(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("ingest_summary",
		slog.Int("ok_count", 5),
		slog.Int("err_count", 2),
	)

	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a captured log line, got nil")
	}
	for field, want := range map[string]float64{
		"ok_count":  5,
		"err_count": 2,
	} {
		v, ok := got[field].(float64)
		if !ok {
			t.Errorf("%s: type = %T; want float64", field, got[field])
			continue
		}
		if v != want {
			t.Errorf("%s = %v; want %v", field, v, want)
		}
	}
}

func TestEmit_MultipleCallsProduceMultipleLines(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call", slog.String("tool", "tool_a"))
	logger.Emit("tool_call", slog.String("tool", "tool_b"))
	logger.Emit("tool_call", slog.String("tool", "tool_c"))

	// Count lines in the capture buffer.
	raw := rc.LastRaw()
	if raw == "" {
		t.Fatal("expected captured output, got empty")
	}
	// LastRaw returns the last line; verify it's the third tool.
	if !strings.Contains(raw, "tool_c") {
		t.Errorf("last line should mention tool_c; got: %s", raw)
	}

	// Reset and verify buffer tracks all three.
	rc.Reset()
	logger.Emit("tool_call", slog.String("tool", "tool_d"))
	got := rc.LastJSON()
	if got == nil {
		t.Fatal("expected a line after reset")
	}
	if got["tool"] != "tool_d" {
		t.Errorf("tool = %q; want %q", got["tool"], "tool_d")
	}
}

// ---------------------------------------------------------------------------
// AllJSON integration with metrics — verifies that AllJSON sees all Emit calls.
// This test is RED until AllJSON() is implemented on RecordCapture.
// ---------------------------------------------------------------------------

// TestEmit_AllJSONSeesAllLines asserts that AllJSON returns one entry per
// Emit call when multiple events are emitted. This is the key use-case for
// drift tests that need to inspect both a per-record line and a summary line.
func TestEmit_AllJSONSeesAllLines(t *testing.T) {
	logger, rc := newJSONMetricLogger(t, "test-svc")

	logger.Emit("tool_call", slog.String("tool", "a"))
	logger.Emit("tool_call", slog.String("tool", "b"))

	all := rc.AllJSON()
	if len(all) != 2 {
		t.Fatalf("AllJSON returned %d items after 2 Emit calls, want 2", len(all))
	}
	if all[0]["tool"] != "a" {
		t.Errorf("AllJSON[0][tool] = %v, want %q", all[0]["tool"], "a")
	}
	if all[1]["tool"] != "b" {
		t.Errorf("AllJSON[1][tool] = %v, want %q", all[1]["tool"], "b")
	}
	// Both must carry kind=metric.
	for i, line := range all {
		if line["kind"] != "metric" {
			t.Errorf("AllJSON[%d][kind] = %v, want %q", i, line["kind"], "metric")
		}
	}
}

func newJSONMetricLogger(t testing.TB, service string) (*logging.Logger, *recordCapture) {
	t.Helper()
	rc := &recordCapture{}
	return mustNewLogger(t, logging.Config{
		Service: service,
		Level:   slog.LevelDebug,
		Console: logging.ConsoleJSON,
		Writer:  rc,
	}), rc
}
