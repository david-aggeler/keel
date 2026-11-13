//go:build unix

package exec

import (
	"os/exec"
	"syscall"
	"time"
)

// KillProcessGroupOnCancel returns a [Request.Configure] function that starts
// the child in its own process group and, when the request's context is
// canceled or its deadline passes, sends SIGKILL to that whole group. A
// descendant that inherited the capture pipes therefore dies with the child,
// so Wait returns at the cancel rather than when the descendant exits.
// waitDelay bounds how long Wait then waits for the pipes to close; it is the
// fallback after the group kill, not the primary bound.
//
// DHF-REQ: keel/requirement-190
func KillProcessGroupOnCancel(waitDelay time.Duration) func(*exec.Cmd) {
	return func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		cmd.WaitDelay = waitDelay
	}
}
