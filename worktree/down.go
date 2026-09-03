package worktree

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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
	// DownPruned: the directory was already gone but its registration survived,
	// so tear-down dropped the registration. Nothing on disk was destroyed; the
	// outcome is distinct from a removal because nothing was removed, and from a
	// no-op because git state did change.
	DownPruned DownOutcome = "pruned"
)

// DownPolicy names which safety gates count as blockers for tear-down.
//
// DHF-REQ: keel/requirement-114 (keel/ac-439)
type DownPolicy string

const (
	// DownPolicyDefault is the package default: any uncommitted change,
	// untracked file, or commit absent from both every remote and the local
	// default branch blocks tear-down, preserving the ac-401 safety contract for
	// callers that opt into nothing.
	DownPolicyDefault DownPolicy = ""
	// DownPolicyKeepBranchCommits waives only [BlockerUnpushedCommit]. Down
	// never deletes the branch, so callers using this policy treat commits on
	// the branch as still reachable while uncommitted and untracked work remain
	// blockers unless the caller also chooses [DownOptions.Force].
	DownPolicyKeepBranchCommits DownPolicy = "keep_branch_commits"
)

// DownOptions carries the caller's tear-down policy.
type DownOptions struct {
	// Policy selects the named tear-down safety policy. The zero value is
	// [DownPolicyDefault].
	Policy DownPolicy
	// Force removes the checkout even though it holds work — uncommitted
	// changes, untracked files, or commits no other ref keeps. It is deliberately
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
// commit when it does. No merge-status or ahead/behind-base query takes part in
// the decision, so tear-down is callable before a merge, after one, or when no
// merge will ever happen — the branch survives either way. The one place the
// default branch is named is the reachability probe for unreachable commits,
// which asks only whether some surviving ref keeps a commit; it exempts, and can
// never make removal conditional on a merge having happened.
//
// An already-removed, unregistered path is success with [DownNoop] and no
// mutating git command, so a reconciliation loop can re-invoke Down after a
// crash, or after a peer tore the same checkout down, and converge. A path
// whose directory is gone while its registration survives is the same story
// half-finished: Down prunes the registration and reports [DownPruned].
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
	case os.IsNotExist(statErr):
		return m.pruneAbsent(ctx, op, result, path)
	case statErr != nil:
		return result, wrapError(op, CodeGit, path, statErr, "inspect %s", path)
	}

	if !validDownPolicy(opts.Policy) {
		return result, newError(op, CodeInvalidArgument, path, "unknown down policy %q", opts.Policy)
	}
	report := m.inspect(ctx, path, reg, registered)
	if blocking := blockingItems(report, opts.Policy, opts.Force); !blocking.Empty() {
		err := newError(op, CodeBlocked, path, "%s cannot be removed: %s", path, summarize(blocking))
		err.Report = &blocking
		return result, err
	}

	// The checkout is cleared for removal, so this package's own replication
	// links go first. git's removal check counts them as untracked files, and it
	// is right to: a link standing in for a directory is not what a trailing-slash
	// ignore rule matches, so the link is visible where the directory it replaces
	// is not. Unlinking one destroys nothing — the content it points at lives in
	// the primary checkout and bring-up recreates the link on demand.
	if err := m.clearReplicationLinks(path); err != nil {
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

// pruneAbsent handles the inverse of the no-op: the directory is gone but the
// registration survives. Inspecting a checkout that is not there could only
// report the failure to read it, so tear-down drops the registration instead —
// there is nothing on disk left for a removal to destroy, and the branch is
// untouched either way. A registration a prune cannot clear (git skips locked
// ones) stays a reported condition rather than a silent success, so the caller
// is never told the checkout is gone while git still lists it.
func (m *Manager) pruneAbsent(ctx context.Context, op string, result DownResult, path string) (DownResult, error) {
	if _, err := m.run(ctx, op, m.repoRoot, "worktree", "prune"); err != nil {
		return result, err
	}
	regs, err := m.registrations(ctx, op)
	if err != nil {
		return result, err
	}
	if _, stillRegistered := lookup(regs, path); stillRegistered {
		var report StaleReport
		report.add(Blocker{
			Kind:        BlockerStaleRegistration,
			Path:        path,
			Detail:      "the directory is gone but the registration survived a prune, which git skips for a locked worktree",
			Remediation: "run `git worktree unlock " + path + "` once the reason for the lock is gone, then tear down again",
		})
		blocked := newError(op, CodeBlocked, path, "%s cannot be removed: %s", path, summarize(report))
		blocked.Report = &report
		return result, blocked
	}
	result.Outcome = DownPruned
	return result, nil
}

// clearReplicationLinks unlinks every replication link in the checkout. It runs
// only after the checkout has been found reclaimable, so it can never destroy
// work: what it removes is a pointer into the primary checkout, not content.
//
// DHF-REQ: keel/requirement-160 (keel/ac-674)
func (m *Manager) clearReplicationLinks(path string) error {
	const op = "down"
	var links []string
	// WalkDir never follows a symlink, so the walk stays inside the checkout
	// however many links into the primary checkout replication left in it.
	err := filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !m.isReplicationLink(current, path) {
			return nil
		}
		links = append(links, current)
		return nil
	})
	if err != nil {
		return wrapError(op, CodeReplicateFailed, path, err, "walk %s for replicated links", path)
	}
	for _, link := range links {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return wrapError(op, CodeReplicateFailed, link, err, "unlink replicated %s", link)
		}
	}
	return nil
}

// isReplicationLink reports whether abs is a symlink standing in for content in
// the primary checkout. A link pointing back inside the worktree is the
// caller's own and is deliberately excluded — it is content this checkout
// holds, so tear-down must keep counting it as work.
func (m *Manager) isReplicationLink(abs, worktreePath string) bool {
	info, err := os.Lstat(abs)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(abs)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(abs), target)
	}
	target = filepath.Clean(target)
	return underPath(target, m.repoRoot) && !underPath(target, filepath.Clean(worktreePath))
}

// underPath reports whether candidate is root or nested under it.
func underPath(candidate, root string) bool {
	return candidate == root || strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// blockingItems filters a report down to what still stands in the way given the
// caller's policy and force. Force covers held work only: a bad registration is
// a git-side problem to fix, standing inside the checkout is a caller-side one,
// and content that cannot be unlinked is a filesystem fact no permission
// changes.
func blockingItems(report StaleReport, policy DownPolicy, force bool) StaleReport {
	if policy == DownPolicyDefault && !force {
		return report
	}
	var kept StaleReport
	for _, b := range report.Blockers {
		if !downPolicyWaives(policy, force, b.Kind) {
			kept.add(b)
		}
	}
	return kept
}

func validDownPolicy(policy DownPolicy) bool {
	return policy == DownPolicyDefault || policy == DownPolicyKeepBranchCommits
}

func downPolicyWaives(policy DownPolicy, force bool, kind BlockerKind) bool {
	switch kind {
	case BlockerUnpushedCommit:
		return force || policy == DownPolicyKeepBranchCommits
	case BlockerUncommittedChange, BlockerUntrackedFile, BlockerCurrentDirectory:
		return force
	default:
		return false
	}
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
		parts = append(parts, string(kind)+" x"+strconv.Itoa(seen[kind]))
	}
	return strings.Join(parts, ", ")
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
			// A replication link is this package's own bookkeeping, not work the
			// checkout holds. It has to be excluded here rather than left to
			// git's ignore rules, because substituting a symlink for a directory
			// changes what those rules match.
			if m.isReplicationLink(filepath.Join(path, filepath.FromSlash(entry)), path) {
				continue
			}
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

// appendUnpushed reports commits that exist in this checkout and nowhere that
// would keep them once the checkout is gone.
//
// Two cases differ in what removal would actually cost. A detached checkout's
// commits are reachable from no ref at all, so removing it orphans them
// immediately — those are always reported. Commits on a branch stay reachable
// from the branch ref, which tear-down never deletes, so they are reported as
// the belt-and-suspenders case only when the repository has somewhere else to
// measure "exists elsewhere" against.
//
// That elsewhere is two refs, not one: a remote ref, and the repository's own
// local default branch. Both keep a commit, and the second is what bring-up
// already branches from (keel/ac-417), so measuring tear-down against remotes
// alone made the symmetric pair disagree — on a checkout whose default branch
// runs ahead of every remote, a unit inherits that whole span and can never
// probe clean, merged or not. With neither available there is nowhere for the
// commits to be, and the branch ref is what keeps them.
//
// DHF-REQ: keel/requirement-113 (keel/ac-523)
func (m *Manager) appendUnpushed(ctx context.Context, op string, report *StaleReport, path string, reg registration) {
	var args []string
	switch {
	case reg.detached || reg.branch == "":
		args = []string{"rev-list", "HEAD", "--not", "--branches", "--remotes"}
	case m.hasRemoteRefs(ctx, op):
		args = []string{"rev-list", reg.branch, "--not", "--remotes"}
		if keeper := m.defaultBranchKeeper(ctx, op, reg.branch); keeper != "" {
			args = append(args, keeper)
		}
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
			Detail:      "reachable from this checkout, from no remote ref, and from no local default branch",
			Remediation: "merge the branch into the default branch, push it, or force the tear-down deliberately",
		})
	}
}

// defaultBranchKeeper names the local ref that keeps a commit after this
// checkout is gone, or "" when there is none to name. It reuses bring-up's own
// base resolution rather than deriving a second answer, which is what keeps the
// two halves of the pair pointed at the same branch instead of merely both
// plausible.
//
// Two answers are withheld deliberately, because a wrong one silently empties
// the probe rather than loosening it by a commit: a base that does not resolve
// leaves the guard measuring remotes alone, and a base that IS the branch under
// tear-down would exclude the branch from itself and exempt everything.
func (m *Manager) defaultBranchKeeper(ctx context.Context, op, branch string) string {
	base, err := m.resolveBase(ctx, op)
	if err != nil || base == "" {
		return ""
	}
	if base == branch || base == "refs/heads/"+branch {
		return ""
	}
	return base
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
