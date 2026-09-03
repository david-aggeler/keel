package claude

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

// claudeRateLimitEvent builds one claude `rate_limit_event` line in the shape a
// live `claude -p --output-format stream-json --verbose` transcript emits
// (captured from claude 2.1.245), sibling fields included so the decoder is
// exercised against the real envelope rather than a convenient subset.
func claudeRateLimitEvent(status string, fiveHour, sevenDay float64, resetsAt int64) string {
	return `{"type":"rate_limit_event","rate_limit_info":{"status":"` + status + `",` +
		`"resetsAt":` + strconv.FormatInt(resetsAt, 10) + `,"rateLimitType":"five_hour",` +
		`"overageStatus":"rejected","overageDisabledReason":"out_of_credits","isUsingOverage":false,` +
		`"unifiedWindows":{"five_hour":{"utilization":` + strconv.FormatFloat(fiveHour, 'f', 2, 64) + `,"resetsAt":` + strconv.FormatInt(resetsAt, 10) + `},` +
		`"seven_day":{"utilization":` + strconv.FormatFloat(sevenDay, 'f', 2, 64) + `,"resetsAt":` + strconv.FormatInt(resetsAt+100, 10) + `}}},` +
		`"session_id":"s-1","uuid":"u-1"}`
}

const claudeResultLine = `{"type":"result","subtype":"success","is_error":false,"result":"ok","num_turns":1,"duration_ms":10,"total_cost_usd":0.01,"usage":{"input_tokens":1,"output_tokens":1}}`

// sharedLimitType fails to compile unless Result.Limit is keel/exec's shared
// LimitState — the "same type codex uses" leg of keel/ac-677.
func sharedLimitType(r Result) procexec.LimitState { return r.Limit }

func runClaudeStub(t *testing.T, lines []string, exitCode int) (*Result, error) {
	t.Helper()
	stub := writeStub(t, strings.Join(lines, "\n"), exitCode)
	return Run(context.Background(), Request{Prompt: "x", Bin: stub})
}

// TestRun_ResultCarriesLastReportedLimit is keel/ac-677: a claude stream
// carrying a rate-limit object leaves the newest value readable on Result, in
// the same keel/exec type codex reports.
//
// DHF-TEST: keel/requirement-161
func TestRun_ResultCarriesLastReportedLimit(t *testing.T) {
	res, err := runClaudeStub(t, []string{
		`{"type":"system","subtype":"init","session_id":"s-1"}`,
		claudeRateLimitEvent("allowed", 0.21, 0.19, 1788474600),
		claudeRateLimitEvent("allowed", 0.42, 0.19, 1788474600),
		claudeResultLine,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The type is keel/exec's, not a claude-local one: a caller reads one shape
	// for either executor. sharedLimitType would not compile otherwise.
	_ = sharedLimitType(*res)
	if !res.Limit.Reported {
		t.Fatalf("Limit.Reported = false; the stream carried two rate_limit_event lines")
	}
	if got, want := res.Limit.UsedFraction, 0.42; got != want {
		t.Errorf("Limit.UsedFraction = %v, want %v (the later event, fullest window)", got, want)
	}
	if got, want := res.Limit.Window, 5*time.Hour; got != want {
		t.Errorf("Limit.Window = %v, want %v", got, want)
	}
	if got, want := res.Limit.ResetsAt, time.Unix(1788474600, 0).UTC(); !got.Equal(want) {
		t.Errorf("Limit.ResetsAt = %v, want %v", got, want)
	}
	if res.Limit.Reached() {
		t.Errorf("Limit.Reached() = true at 42%% used, want false")
	}
}

// TestRun_LimitReadsTheFullestWindow proves the reported fraction is the
// binding constraint rather than a fixed window: whichever window is fullest is
// the one that will stop the next dispatch.
//
// DHF-TEST: keel/requirement-161
func TestRun_LimitReadsTheFullestWindow(t *testing.T) {
	res, err := runClaudeStub(t, []string{
		claudeRateLimitEvent("allowed", 0.10, 0.87, 1788474600),
		claudeResultLine,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := res.Limit.UsedFraction, 0.87; got != want {
		t.Errorf("Limit.UsedFraction = %v, want %v (seven_day is the fuller window)", got, want)
	}
	if got, want := res.Limit.Window, 7*24*time.Hour; got != want {
		t.Errorf("Limit.Window = %v, want %v", got, want)
	}
}

// TestRun_ResultLimitAbsentWhenStreamReportsNone is keel/ac-676 for claude.
//
// DHF-TEST: keel/requirement-161
func TestRun_ResultLimitAbsentWhenStreamReportsNone(t *testing.T) {
	res, err := runClaudeStub(t, []string{
		`{"type":"system","subtype":"init","session_id":"s-1"}`,
		claudeResultLine,
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

// TestRun_FailureAtReportedLimitMatchesQuotaSentinel is keel/ac-678 for claude,
// for both shapes a quota-exhausted child takes: one that still emits its
// terminal result event, and one that dies without it — the shape the
// 2026-09-03 drain actually met.
//
// DHF-TEST: keel/requirement-161
func TestRun_FailureAtReportedLimitMatchesQuotaSentinel(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"executor declared the limit reached", []string{
			claudeRateLimitEvent("rejected", 0.99, 0.19, 1788474600),
			claudeResultLine,
		}},
		{"window reported full", []string{
			claudeRateLimitEvent("allowed", 1.00, 0.19, 1788474600),
			claudeResultLine,
		}},
		{"child died before its result event", []string{
			claudeRateLimitEvent("rejected", 0.99, 0.19, 1788474600),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runClaudeStub(t, tc.lines, 1)
			if err == nil {
				t.Fatalf("Run returned nil error for an exit-1 child")
			}
			if !errors.Is(err, procexec.ErrQuotaExhausted) {
				t.Errorf("errors.Is(err, ErrQuotaExhausted) = false at the reported limit; err = %v", err)
			}
			if !strings.Contains(err.Error(), "stderr:") {
				t.Errorf("error message lost the stderr forensics clause: %v", err)
			}
		})
	}
}

// TestRun_FailureWithoutReportedLimitIsNotQuota is keel/ac-679 for claude.
//
// DHF-TEST: keel/requirement-161
func TestRun_FailureWithoutReportedLimitIsNotQuota(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
	}{
		{"no limit reported", []string{claudeResultLine}},
		{"limit reported below the threshold", []string{
			claudeRateLimitEvent("allowed", 0.99, 0.19, 1788474600),
			claudeResultLine,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runClaudeStub(t, tc.lines, 1)
			if err == nil {
				t.Fatalf("Run returned nil error for an exit-1 child")
			}
			if errors.Is(err, procexec.ErrQuotaExhausted) {
				t.Errorf("errors.Is(err, ErrQuotaExhausted) = true without a reached limit; err = %v", err)
			}
		})
	}
}

// TestRun_MalformedLimitLeavesRunUnaffected pins the strictness decision for
// the claude leg.
//
// DHF-TEST: keel/requirement-161
func TestRun_MalformedLimitLeavesRunUnaffected(t *testing.T) {
	res, err := runClaudeStub(t, []string{
		`{"type":"rate_limit_event","rate_limit_info":"not-an-object"}`,
		claudeResultLine,
	}, 0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Limit.Reported {
		t.Errorf("Limit.Reported = true from an undecodable rate_limit_info value")
	}
}
