package log

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

const unknownGitCommit = "unknown"

// LogBuildIdentity logs a single "build identity" record carrying the binary's
// version and resolved git commit. An empty version renders as "dev"; the commit
// is resolved via [ResolveGitCommit]. A nil logger falls back to slog.Default.
//
// DHF-REQ: openbrain/requirement-108
func LogBuildIdentity(logger *slog.Logger, version, gitCommit string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("build identity", "version", versionOrDev(version), "git_commit", ResolveGitCommit(gitCommit))
}

// StartDailyBuildIdentity spawns a background goroutine that calls
// [LogBuildIdentity] just after each local midnight, so a long-running process
// re-stamps its build identity once per day in the log. The goroutine runs until
// ctx is cancelled; StartDailyBuildIdentity itself returns immediately and does
// not block.
//
// DHF-REQ: openbrain/requirement-108
func StartDailyBuildIdentity(ctx context.Context, logger *slog.Logger, version, gitCommit string) {
	go func() {
		timer := time.NewTimer(durationUntilNextBuildIdentityHeartbeat(time.Now()))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				LogBuildIdentity(logger, version, gitCommit)
				timer.Reset(durationUntilNextBuildIdentityHeartbeat(time.Now()))
			}
		}
	}()
}

// ResolveGitCommit returns the commit to report for the running binary. A
// non-empty gitCommit other than "dev" (typically injected at build time via
// -ldflags) is returned as-is. Otherwise it falls back to the commit embedded by
// the Go toolchain in the build info, appending "-modified" when the working
// tree was dirty at build time, and returns "unknown" when no revision is
// available.
func ResolveGitCommit(gitCommit string) string {
	if gitCommit != "" && gitCommit != "dev" {
		return gitCommit
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		var revision string
		modified := false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if revision != "" {
			if modified {
				return revision + "-modified"
			}
			return revision
		}
	}
	return unknownGitCommit
}

func versionOrDev(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

func durationUntilNextBuildIdentityHeartbeat(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 1, 0, now.Location())
	return next.Sub(now)
}
