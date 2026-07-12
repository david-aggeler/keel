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
// is resolved from build info when gitCommit is empty or "dev". A nil logger
// falls back to slog.Default.
//
// DHF-REQ: openbrain/requirement-108
func logBuildIdentity(logger *slog.Logger, version, gitCommit string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("build identity", "version", versionOrDev(version), "git_commit", ResolveGitCommit(gitCommit))
}

// DHF-REQ: openbrain/requirement-108
func startDailyBuildIdentity(ctx context.Context, logger *slog.Logger, version, gitCommit string) {
	go func() {
		timer := time.NewTimer(durationUntilNextBuildIdentityHeartbeat(time.Now()))
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				logBuildIdentity(logger, version, gitCommit)
				timer.Reset(durationUntilNextBuildIdentityHeartbeat(time.Now()))
			}
		}
	}()
}

// ResolveGitCommit resolves the git commit stamped into build-identity records.
// An explicit non-"dev" value wins; otherwise Go build info vcs.revision is
// used, with "-modified" appended when vcs.modified is true. If no revision is
// available, it returns "unknown".
//
// DHF-REQ: keel/requirement-56
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

func resolveGitCommit(gitCommit string) string {
	return ResolveGitCommit(gitCommit)
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
