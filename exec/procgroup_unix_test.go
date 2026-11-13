//go:build unix

package exec_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

// TestKillProcessGroupOnCancelKillsGrandchild: a child shell that starts a
// long-lived grandchild holding the capture pipes must not outlive a cancel.
// Wait returns promptly and the grandchild is gone.
//
// DHF-TEST: keel/requirement-190 (keel/ac-837)
func TestKillProcessGroupOnCancelKillsGrandchild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program:   "/bin/sh",
		Args:      []string{"-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile},
		Logger:    logging.Discard(),
		Configure: procexec.KillProcessGroupOnCancel(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	pid := waitForPID(t, pidFile)
	cancel()
	start := time.Now()
	_, _ = proc.Wait()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("wait returned %v after cancel; want < 500ms", elapsed)
	}
	// The killed grandchild passes to init, which reaps it; allow that
	// reap a moment before calling it a survivor.
	gone := time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(gone) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d is still alive after cancel", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild pid never appeared in %s", path)
	return 0
}
