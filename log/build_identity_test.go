package log

import (
	"testing"
	"time"
)

// DHF-TEST: openbrain/requirement-108
func TestLogBuildIdentityEmitsVersionAndGitCommit(t *testing.T) {
	logger, capture := newForTesting("build-test")

	LogBuildIdentity(logger, "1.2.3", "abc1234")

	got := capture.LastJSON()
	if got["msg"] != "build identity" {
		t.Fatalf("msg = %v, want build identity", got["msg"])
	}
	if got["version"] != "1.2.3" {
		t.Fatalf("version = %v, want 1.2.3", got["version"])
	}
	if got["git_commit"] != "abc1234" {
		t.Fatalf("git_commit = %v, want abc1234", got["git_commit"])
	}
}

func TestDurationUntilNextBuildIdentityHeartbeat(t *testing.T) {
	now := time.Date(2026, 6, 14, 23, 59, 59, 0, time.UTC)
	got := durationUntilNextBuildIdentityHeartbeat(now)
	if got != 2*time.Second {
		t.Fatalf("duration = %s, want 2s", got)
	}
}
