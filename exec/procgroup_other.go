//go:build !unix

package exec

import (
	"os/exec"
	"time"
)

// KillProcessGroupOnCancel returns a [Request.Configure] function. Process
// groups exist on unix only, so on this platform the cancel stays the os/exec
// default (kill the direct child) and only waitDelay is applied.
//
// DHF-REQ: keel/requirement-190
func KillProcessGroupOnCancel(waitDelay time.Duration) func(*exec.Cmd) {
	return func(cmd *exec.Cmd) {
		cmd.WaitDelay = waitDelay
	}
}
