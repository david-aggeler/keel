package log

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

const unknownGitCommit = "unknown"

// DHF-REQ: openbrain/requirement-108
func LogBuildIdentity(logger *slog.Logger, version, gitCommit string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("build identity", "version", versionOrDev(version), "git_commit", ResolveGitCommit(gitCommit))
}

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
