package worktree

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DownOutcome names which of tear-down's determinate ends was reached.
type DownOutcome string

const (
	// DownRemoved: the checkout was inspected, found reclaimable, and removed.
	DownRemoved DownOutcome = "removed"
	// DownNoop: the checkout was already gone and unregistered. This is success,
	// and it stays distinguishable from a first-time removal so a caller can
	// still account for what it actually did.
	DownNoop DownOutcome = "noop"
)

// DownOptions carries the caller's tear-down policy.
type DownOptions struct {
	// Force removes the checkout even though it holds work — uncommitted
	// changes, untracked files, or commits on no remote. It is deliberately
	// per-condition and never cascades: it does not delete the branch, and it
	// cannot clear content the process is unable to unlink, because that needs
	// privilege rather than permission.
	Force bool
}

// DownResult reports what tear-down did.
type DownResult struct {
	// Name is the work-item name as supplied.
	Name string
	// Path is the absolute worktree directory the call addressed.
	Path string
	// Branch is the branch name for the work item. Tear-down never deletes it.
	Branch string
	// Outcome distinguishes a removal from an already-gone no-op.
	Outcome DownOutcome
}

// Down removes the worktree for name. Its only input is inspectable state: the
// checkout is removed when it holds nothing that removal would destroy, and
// refused with [CodeBlocked] and a [StaleReport] naming every offending path or
// commit when it does. No merge-status or base-comparison query takes part in
// the decision, so tear-down is callable before a merge, after one, or when no
// merge will ever happen — the branch survives either way.
//
// An already-removed, unregistered path is success with [DownNoop] and no
// mutating git command, so a reconciliation loop can re-invoke Down after a
// crash, or after a peer tore the same checkout down, and converge.
//
// DHF-REQ: keel/requirement-113 (keel/ac-401, keel/ac-402, keel/ac-403, keel/ac-405)
func (m *Manager) Down(ctx context.Context, name string, opts DownOptions) (DownResult, error) {
	const op = "down"
	path, branch, err := m.Resolve(name)
	if err != nil {
		return DownResult{}, err
	}
	result := DownResult{Name: name, Path: path, Branch: branch}

	regs, err := m.registrations(ctx, op)
	if err != nil {
		return result, err
	}
	reg, registered := lookup(regs, path)
	_, statErr := os.Stat(path)
	switch {
	case os.IsNotExist(statErr) && !registered:
		result.Outcome = DownNoop
		return result, nil
	case statErr != nil && !os.IsNotExist(statErr):
		return result, wrapError(op, CodeGit, path, statErr, "inspect %s", path)
	}

	report := m.inspect(ctx, path, reg, registered)
	if blocking := blockingItems(report, opts.Force); !blocking.Empty() {
		err := newError(op, CodeBlocked, path, "%s cannot be removed: %s", path, summarize(blocking))
		err.Report = &blocking
		return result, err
	}

	args := []string{"worktree", "remove"}
	if opts.Force {
		args = append(args, "--force")
	}
	if _, err := m.run(ctx, op, m.repoRoot, append(args, reg.path)...); err != nil {
		return result, err
	}
	// Removal keys off the registered path; pruning after it keeps a stale entry
	// from outliving the directory.
	if _, err := m.run(ctx, op, m.repoRoot, "worktree", "prune"); err != nil {
		return result, err
	}
	result.Outcome = DownRemoved
	return result, nil
}

// blockingItems filters a report down to what still stands in the way given the
// caller's force. Force covers held work only: a bad registration is a git-side
// problem to fix, standing inside the checkout is a caller-side one, and content
// that cannot be unlinked is a filesystem fact no permission changes.
func blockingItems(report StaleReport, force bool) StaleReport {
	if !force {
		return report
	}
	var kept StaleReport
	for _, b := range report.Blockers {
		switch b.Kind {
		case BlockerUncommittedChange, BlockerUntrackedFile, BlockerUnpushedCommit, BlockerCurrentDirectory:
			// Deliberately overridden.
		default:
			kept.add(b)
		}
	}
	return kept
}

func summarize(report StaleReport) string {
	seen := make(map[BlockerKind]int, len(report.Blockers))
	var order []BlockerKind
	for _, b := range report.Blockers {
		if _, ok := seen[b.Kind]; !ok {
			order = append(order, b.Kind)
		}
		seen[b.Kind]++
	}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		parts = append(parts, string(kind)+" x"+itoa(seen[kind]))
	}
	return strings.Join(parts, ", ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// inspect accumulates every blocking item for a checkout that exists on disk.
// It never stops at the first, and it never issues a mutating git command, so
// [Manager.State] can hand the same report to a verifier that must not remediate.
func (m *Manager) inspect(ctx context.Context, path string, reg registration, registered bool) StaleReport {
	const op = "inspect"
	var report StaleReport

	if !registered {
		report.add(Blocker{
			Kind:        BlockerStaleRegistration,
			Path:        path,
			Detail:      "the directory exists but " + m.repoRoot + " has no worktree registered at it",
			Remediation: "run `git worktree prune` to drop stale entries, then remove the directory directly",
		})
		// Nothing below can be trusted for an unregistered path, but the
		// filesystem-level check still tells the caller something useful.
		m.appendUndeletable(&report, path)
		return report
	}
	if reg.locked {
		report.add(Blocker{
			Kind:        BlockerLockedRegistration,
			Path:        path,
			Detail:      "the worktree is locked",
			Remediation: "run `git worktree unlock " + path + "` once the reason for the lock is gone",
		})
	}
	if reg.prunable {
		report.add(Blocker{
			Kind:        BlockerStaleRegistration,
			Path:        path,
			Detail:      "git reports the registration as prunable",
			Remediation: "run `git worktree prune` to drop stale entries",
		})
	}

	m.appendStatus(ctx, op, &report, path)
	m.appendUnpushed(ctx, op, &report, path, reg)
	m.appendUndeletable(&report, path)

	if inside, err := cwdInside(path); err != nil {
		report.add(Blocker{
			Kind:        BlockerInspectionFailed,
			Path:        path,
			Detail:      "could not resolve the current directory: " + err.Error(),
			Remediation: "re-run from a readable working directory",
		})
	} else if inside {
		report.add(Blocker{
			Kind:        BlockerCurrentDirectory,
			Path:        path,
			Detail:      "the calling process is standing inside this checkout",
			Remediation: "change to a directory outside " + path + " first",
		})
	}
	return report
}

// appendStatus turns porcelain status into one blocker per offending path.
// Ignored files are excluded by git's own default; the caller's declared
// exclusions are excluded by pathspec, so tooling bookkeeping never reads as work.
func (m *Manager) appendStatus(ctx context.Context, op string, report *StaleReport, path string) {
	args := []string{"status", "--porcelain", "--untracked-files=normal", "--", "."}
	for _, exclude := range m.excludePaths {
		args = append(args, ":(exclude)"+exclude)
	}
	out, err := m.run(ctx, op, path, args...)
	if err != nil {
		report.add(Blocker{
			Kind:        BlockerInspectionFailed,
			Path:        path,
			Detail:      "could not read the working tree status: " + err.Error(),
			Remediation: "resolve the git failure, then re-inspect",
		})
		return
	}
	// Porcelain status lines are "XY PATH" — the two status columns are
	// significant whitespace, so the line is split by position, never trimmed.
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 4 {
			continue
		}
		code, entry := line[:2], strings.TrimSpace(line[3:])
		// A rename reports "old -> new"; the new path is the one that exists.
		if idx := strings.LastIndex(entry, " -> "); idx >= 0 {
			entry = entry[idx+4:]
		}
		entry = strings.Trim(entry, `"`)
		if code == "??" {
			report.add(Blocker{
				Kind:        BlockerUntrackedFile,
				Path:        entry,
				Detail:      code,
				Remediation: "add and commit it, or delete it",
			})
			continue
		}
		report.add(Blocker{
			Kind:        BlockerUncommittedChange,
			Path:        entry,
			Detail:      code,
			Remediation: "commit or stash the change",
		})
	}
}

// appendUnpushed reports commits that exist in this checkout and on no remote.
//
// Two cases differ in what removal would actually cost. A detached checkout's
// commits are reachable from no ref at all, so removing it orphans them
// immediately — those are always reported. Commits on a branch stay reachable
// from the branch ref, which tear-down never deletes, so they are reported as
// the belt-and-suspenders case only when the repository has remote refs to
// measure "exists elsewhere" against; with no remote configured there is
// nowhere for them to be, and the branch ref is what keeps them.
func (m *Manager) appendUnpushed(ctx context.Context, op string, report *StaleReport, path string, reg registration) {
	var args []string
	switch {
	case reg.detached || reg.branch == "":
		args = []string{"rev-list", "HEAD", "--not", "--branches", "--remotes"}
	case m.hasRemoteRefs(ctx, op):
		args = []string{"rev-list", reg.branch, "--not", "--remotes"}
	default:
		return
	}
	out, err := m.run(ctx, op, path, args...)
	if err != nil {
		report.add(Blocker{
			Kind:        BlockerInspectionFailed,
			Path:        path,
			Detail:      "could not list commits absent from every remote: " + err.Error(),
			Remediation: "resolve the git failure, then re-inspect",
		})
		return
	}
	for _, commit := range nonEmptyLines(out) {
		report.add(Blocker{
			Kind:        BlockerUnpushedCommit,
			Commit:      commit,
			Detail:      "reachable from this checkout and from no remote ref",
			Remediation: "push the branch, or force the tear-down deliberately",
		})
	}
}

func (m *Manager) hasRemoteRefs(ctx context.Context, op string) bool {
	out, err := m.run(ctx, op, m.repoRoot, "for-each-ref", "--format=%(refname)", "refs/remotes")
	return err == nil && len(nonEmptyLines(out)) > 0
}

// appendUndeletable walks the checkout for content the calling process cannot
// unlink. This is a filesystem fact, not a git one: the offending files are
// usually worthless residue left by a privileged build, and no force clears
// them, so they are reported as their own condition with the paths named rather
// than surfacing later as a raw permission error from the removal.
func (m *Manager) appendUndeletable(report *StaleReport, path string) {
	err := filepath.WalkDir(path, func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			report.add(Blocker{
				Kind:        BlockerInspectionFailed,
				Path:        current,
				Detail:      "could not read the directory: " + err.Error(),
				Remediation: "restore read access to the path, then re-inspect",
			})
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		if directoryWritable(current) {
			return nil
		}
		entries, readErr := os.ReadDir(current)
		if readErr != nil || len(entries) == 0 {
			report.add(Blocker{
				Kind:        BlockerUndeletableContent,
				Path:        current,
				Detail:      "the calling process cannot write this directory, so its entries cannot be unlinked",
				Remediation: "delete it with escalated privilege, or out of process",
			})
			return fs.SkipDir
		}
		for _, entry := range entries {
			report.add(Blocker{
				Kind:        BlockerUndeletableContent,
				Path:        filepath.Join(current, entry.Name()),
				Detail:      "the parent directory is not writable by the calling process, so this entry cannot be unlinked",
				Remediation: "delete it with escalated privilege, or out of process",
			})
		}
		return fs.SkipDir
	})
	if err != nil {
		report.add(Blocker{
			Kind:        BlockerInspectionFailed,
			Path:        path,
			Detail:      "could not walk the checkout: " + err.Error(),
			Remediation: "restore read access to the path, then re-inspect",
		})
	}
}

// cwdInside reports whether the calling process's working directory is the
// checkout or nested under it.
func cwdInside(path string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	cwd = resolvedPath(cwd)
	target := resolvedPath(path)
	return cwd == target || strings.HasPrefix(cwd, target+string(os.PathSeparator)), nil
}
