package worktree

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	procexec "github.com/david-aggeler/keel/exec"
)

// Logger receives the START/END lifecycle records keel/exec emits for every git
// invocation this package makes. It is satisfied by *slog.Logger.
type Logger interface {
	// Debug records a diagnostic detail, including each captured stdout line.
	Debug(msg string, args ...any)
	// Error records a failure, including each captured stderr line.
	Error(msg string, args ...any)
	// Info records the lifecycle end of a git invocation.
	Info(msg string, args ...any)
	// InfoContext records the lifecycle start of a git invocation.
	InfoContext(ctx context.Context, msg string, args ...any)
}

// run executes one git command in dir (empty means the manager's repository
// root) and returns its trimmed stdout. A non-zero exit becomes an [*Error]
// carrying git's stderr verbatim, so the cause is never swallowed.
func (m *Manager) run(ctx context.Context, op, dir string, args ...string) (string, error) {
	if dir == "" {
		dir = m.repoRoot
	}
	req := procexec.Request{
		Program: m.gitBin,
		Args:    args,
		Dir:     dir,
		Env:     m.env,
	}
	if m.logger != nil {
		req.Logger = m.logger
	}
	proc, err := procexec.ProcessStart(ctx, req)
	if err != nil {
		return "", wrapError(op, CodeGit, dir, err, "start git %s", strings.Join(args, " "))
	}
	res, waitErr := proc.Wait()
	if waitErr != nil || res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return res.Stdout, wrapError(op, CodeGit, dir, waitErr, "git %s: exit %d: %s", strings.Join(args, " "), res.ExitCode, msg)
	}
	return res.Stdout, nil
}

// runQuiet is run for a command whose failure is itself the answer (a ref that
// does not resolve, a branch that does not exist). It reports success only.
func (m *Manager) runQuiet(ctx context.Context, dir string, args ...string) bool {
	_, err := m.run(ctx, "probe", dir, args...)
	return err == nil
}

// registration is one block of `git worktree list --porcelain` output.
type registration struct {
	path     string
	branch   string
	detached bool
	locked   bool
	prunable bool
}

// registrations parses every worktree this repository has registered. Paths are
// canonicalized so a comparison never turns on a symlinked or unclean spelling.
func (m *Manager) registrations(ctx context.Context, op string) ([]registration, error) {
	out, err := m.run(ctx, op, m.repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []registration
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, registration{path: canonical(strings.TrimSpace(strings.TrimPrefix(line, "worktree ")))})
		case len(list) == 0:
			// Leading blank or unexpected line before any block: nothing to attach it to.
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			list[len(list)-1].branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			list[len(list)-1].detached = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			list[len(list)-1].locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			list[len(list)-1].prunable = true
		}
	}
	return list, nil
}

// lookup finds the registration for path, if this repository has one.
func lookup(list []registration, path string) (registration, bool) {
	want := canonical(path)
	for _, reg := range list {
		if reg.path == want {
			return reg, true
		}
	}
	return registration{}, false
}

// canonical returns the cleanest absolute spelling of path that can be computed
// without touching the filesystem, so registrations and caller-supplied paths
// compare like for like.
func canonical(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

// resolvedPath is canonical with symlinks followed, for paths that exist.
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return canonical(path)
}

func parseCount(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// nonEmptyLines splits git output into its non-blank lines.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
