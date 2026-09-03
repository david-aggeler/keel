package codex

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

// codexTokenCount builds one codex `token_count` event carrying a rate_limits
// object in the shape a live codex 0.147 rollout emits, verbatim down to the
// sibling fields the adapter must ignore.
func codexTokenCount(usedPercent float64, windowMinutes int, resetsAt int64, reachedType string) string {
	reached := "null"
	if reachedType != "" {
		reached = `"` + reachedType + `"`
	}
	return `{"type":"event_msg","payload":{"type":"token_count",` +
		`"info":{"total_token_usage":{"total_tokens":15300},"model_context_window":258400},` +
		`"rate_limits":{"limit_id":"codex","limit_name":null,` +
		`"primary":{"used_percent":` + strconv.FormatFloat(usedPercent, 'f', 1, 64) + `,"window_minutes":` + strconv.Itoa(windowMinutes) + `,"resets_at":` + strconv.FormatInt(resetsAt, 10) + `},` +
		`"secondary":null,"credits":{"has_credits":false,"unlimited":false,"balance":"0"},` +
		`"individual_limit":null,"spend_control_reached":null,"plan_type":"prolite",` +
		`"rate_limit_reached_type":` + reached + `}}}`
}

func runCodexStub(t *testing.T, lines []string, exitCode int) (*Result, error) {
	t.Helper()
	dir := t.TempDir()
	stub := writeStreamStub(t, filepath.Join(dir, "argv.txt"), filepath.Join(dir, "stdinlen.txt"), lines, exitCode)
	return Run(context.Background(), Request{Prompt: "x", Dir: dir, Bin: stub})
}

// TestRun_ResultCarriesLastReportedRateLimit is keel/ac-675: two token_count
// events with different used_percent values leave the later one readable as
// typed fields on Result, not only inside Event.Raw.
//
// DHF-TEST: keel/requirement-161
func TestRun_ResultCarriesLastReportedRateLimit(t *testing.T) {
	res, err := runCodexStub(t, []string{
		`{"type":"thread.started","thread_id":"t-1"}`,
		codexTokenCount(41, 10080, 1788859000, ""),
		codexTokenCount(80, 10080, 1788859323, ""),
		`{"type":"result","is_error":false}`,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Limit.Reported {
		t.Fatalf("Limit.Reported = false; the stream carried two rate_limits objects")
	}
	if got, want := res.Limit.UsedFraction, 0.80; got != want {
		t.Errorf("Limit.UsedFraction = %v, want %v (the later of the two events wins)", got, want)
	}
	if got, want := res.Limit.Window, 10080*time.Minute; got != want {
		t.Errorf("Limit.Window = %v, want %v", got, want)
	}
	if got, want := res.Limit.ResetsAt, time.Unix(1788859323, 0).UTC(); !got.Equal(want) {
		t.Errorf("Limit.ResetsAt = %v, want %v", got, want)
	}
	if res.Limit.Reached() {
		t.Errorf("Limit.Reached() = true at 80%% used, want false")
	}
}

// TestRun_ResultLimitAbsentWhenStreamReportsNone is keel/ac-676 for codex: a
// stream with no limit object leaves the state readable as not reported, which
// a caller can tell apart from a reported empty window.
//
// DHF-TEST: keel/requirement-161
func TestRun_ResultLimitAbsentWhenStreamReportsNone(t *testing.T) {
	res, err := runCodexStub(t, []string{
		`{"type":"thread.started","thread_id":"t-1"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}`,
		`{"type":"result","is_error":false}`,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Limit.Reported {
		t.Errorf("Limit.Reported = true; no event carried a limit object")
	}
	if res.Limit != (procexec.LimitState{}) {
		t.Errorf("Limit = %+v, want the zero state", res.Limit)
	}
}

// TestRun_MalformedRateLimitLeavesRunUnaffected pins the strictness decision: a
// telemetry field that will not decode never fails a run that otherwise
// succeeded, and never fabricates a reading.
//
// DHF-TEST: keel/requirement-161
func TestRun_MalformedRateLimitLeavesRunUnaffected(t *testing.T) {
	res, err := runCodexStub(t, []string{
		`{"type":"event_msg","payload":{"type":"token_count","rate_limits":"not-an-object"}}`,
		`{"type":"result","is_error":false}`,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Limit.Reported {
		t.Errorf("Limit.Reported = true from an undecodable rate_limits value")
	}
}

// TestRun_FailureAtReportedLimitMatchesQuotaSentinel is keel/ac-678 for codex:
// a run that fails while the reported state says the limit is reached returns
// an error matching the exported sentinel, with the exit code and stderr still
// in the message.
//
// DHF-TEST: keel/requirement-161
func TestRun_FailureAtReportedLimitMatchesQuotaSentinel(t *testing.T) {
	res, err := runCodexStub(t, []string{
		codexTokenCount(100, 10080, 1788859323, "weekly"),
		`{"type":"result","is_error":false}`,
	}, 1)
	if err == nil {
		t.Fatalf("Run returned nil error for an exit-1 child")
	}
	if !errors.Is(err, procexec.ErrQuotaExhausted) {
		t.Errorf("errors.Is(err, ErrQuotaExhausted) = false for a failure at the reported limit; err = %v", err)
	}
	if !strings.Contains(err.Error(), "exited 1") {
		t.Errorf("error message lost the child's exit code: %v", err)
	}
	if !strings.Contains(err.Error(), "stderr:") {
		t.Errorf("error message lost the stderr forensics clause: %v", err)
	}
	if res == nil || !res.Limit.Reached() {
		t.Errorf("Result did not carry the reached limit state alongside the error")
	}
}

// TestRun_FailureWithoutReportedLimitIsNotQuota is keel/ac-679 for codex: the
// negative leg. A failure with no reported limit, or one below the threshold,
// must not be attributed to quota — otherwise the sentinel misleads a
// supervisor exactly as bare stderr does, only with more authority.
//
// DHF-TEST: keel/requirement-161
func TestRun_FailureWithoutReportedLimitIsNotQuota(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"no limit reported", []string{`{"type":"result","is_error":false}`}},
		{"limit reported below the threshold", []string{
			codexTokenCount(99, 10080, 1788859323, ""),
			`{"type":"result","is_error":false}`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runCodexStub(t, tc.lines, 1)
			if err == nil {
				t.Fatalf("Run returned nil error for an exit-1 child")
			}
			if errors.Is(err, procexec.ErrQuotaExhausted) {
				t.Errorf("errors.Is(err, ErrQuotaExhausted) = true without a reached limit; err = %v", err)
			}
		})
	}
}

// TestRun_SuccessAtReportedLimitIsNotAnError keeps the sentinel an attribution
// rather than a verdict: reaching the limit does not by itself fail a run.
//
// DHF-TEST: keel/requirement-161
func TestRun_SuccessAtReportedLimitIsNotAnError(t *testing.T) {
	res, err := runCodexStub(t, []string{
		codexTokenCount(100, 10080, 1788859323, "weekly"),
		`{"type":"result","is_error":false}`,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Limit.Reached() {
		t.Errorf("Limit.Reached() = false for a 100%%-used reported state")
	}
}
