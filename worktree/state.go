package worktree

import (
	"context"
	"os"
	"strings"
)

// State is the read-only view of one work item's checkout: whether it exists
// and is registered, what it is on, how it stands against the base ref, and
// every item that would block a tear-down. Producing it runs no mutating git
// command, so a role forbidden from remediation can call it freely.
type State struct {
	// Name is the work-item name as supplied.
	Name string
	// Path is the absolute worktree directory.
	Path string
	// Branch is the branch this repository has registered at the path. Empty
	// when the path is unregistered or the checkout is detached.
	Branch string
	// Base is the ref the counts below were measured against. Empty when no
	// base could be resolved.
	Base string
	// Exists reports whether anything is present at the path.
	Exists bool
	// Registered reports whether this repository lists a worktree at the path.
	Registered bool
	// Detached reports a registered checkout that is on no branch.
	Detached bool
	// Locked reports a registration git has marked locked.
	Locked bool
	// Ahead is the number of commits the checkout has that Base does not.
	Ahead int
	// Behind is the number of commits Base has that the checkout does not.
	Behind int
	// CurrentDirectoryInside reports whether the calling process is standing in
	// the checkout.
	CurrentDirectoryInside bool
	// Stale lists every item that would block a tear-down, empty when none.
	Stale StaleReport
}

// ReasonKind classifies one finding of the branch comparison.
type ReasonKind string

const (
	// ReasonOnBaseBranch: the checkout is on the base ref itself, or on no
	// branch at all, so there is nothing to compare.
	ReasonOnBaseBranch ReasonKind = "on_base_branch"
	// ReasonBaseUnresolvable: the base ref does not resolve locally — a typo, or
	// a ref never fetched. Never folded into [ReasonNoCommitsAhead], because
	// "your base does not exist" and "you have committed nothing" call for
	// different fixes.
	ReasonBaseUnresolvable ReasonKind = "base_unresolvable"
	// ReasonNoCommitsAhead: the base resolves and the branch adds nothing to it.
	ReasonNoCommitsAhead ReasonKind = "no_commits_ahead"
	// ReasonWorkingTreeDirty: the checkout holds changes that are not committed.
	ReasonWorkingTreeDirty ReasonKind = "working_tree_dirty"
	// ReasonInspectionFailed: a check could not be evaluated. It is reported as
	// a reason rather than passed over, so an uninspectable repository never
	// reads as a clean one.
	ReasonInspectionFailed ReasonKind = "inspection_failed"
)

// Reason is one finding of the branch comparison.
type Reason struct {
	// Kind classifies the finding.
	Kind ReasonKind
	// Detail states it in the caller's terms.
	Detail string
}

// Comparison is the git-fact view of a branch against its base: the counts, and
// one reason per applicable condition. It renders no verdict — there is
// deliberately no "ready" field, because merge readiness is the caller's
// conjunction of these facts with whatever policy clauses it adds, and a
// boolean here would smuggle that policy into the mechanism.
//
// Reasons accumulate rather than short-circuit, so fixing one condition does not
// require another round trip to discover the next, and they fail closed: a check
// that could not be evaluated yields [ReasonInspectionFailed], never silence.
type Comparison struct {
	// Name is the work-item name as supplied.
	Name string
	// Branch is the branch the checkout is on. Empty when it is detached.
	Branch string
	// Base is the ref compared against, as declared or resolved.
	Base string
	// Ahead is the number of commits Branch has that Base does not. Meaningful
	// only when no [ReasonBaseUnresolvable] is present.
	Ahead int
	// Behind is the number of commits Base has that Branch does not.
	Behind int
	// Reasons are every applicable finding, in inspection order. Empty means the
	// comparison found nothing to report — not that anything is approved.
	Reasons []Reason
}

// Has reports whether the comparison carries a reason of a kind.
func (c Comparison) Has(kind ReasonKind) bool {
	for _, r := range c.Reasons {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func (c *Comparison) add(kind ReasonKind, detail string) {
	c.Reasons = append(c.Reasons, Reason{Kind: kind, Detail: detail})
}

// State inspects the checkout for name and reports what it finds. A path that
// was never brought up, or that has already been torn down, is reported as
// absent rather than as an error — the report describes reality, it does not
// require one.
//
// DHF-REQ: keel/requirement-113 (keel/ac-407)
func (m *Manager) State(ctx context.Context, name string) (State, error) {
	const op = "state"
	path, _, err := m.Resolve(name)
	if err != nil {
		return State{}, err
	}
	state := State{Name: name, Path: path}

	regs, err := m.registrations(ctx, op)
	if err != nil {
		return state, err
	}
	reg, registered := lookup(regs, path)
	state.Registered = registered
	state.Branch = reg.branch
	state.Detached = reg.detached
	state.Locked = reg.locked

	if _, statErr := os.Stat(path); statErr == nil {
		state.Exists = true
	} else if !os.IsNotExist(statErr) {
		return state, wrapError(op, CodeGit, path, statErr, "inspect %s", path)
	}

	if inside, err := cwdInside(path); err == nil {
		state.CurrentDirectoryInside = inside
	}

	switch {
	case state.Exists:
		state.Stale = m.inspect(ctx, path, reg, registered)
	case registered:
		state.Stale.add(Blocker{
			Kind:        BlockerStaleRegistration,
			Path:        path,
			Detail:      "the registration survives but the directory is gone",
			Remediation: "run `git worktree prune` to drop stale entries",
		})
	}

	if base, err := m.resolveBase(ctx, op); err == nil && state.Branch != "" {
		state.Base = base
		if m.refResolvable(ctx, base) {
			state.Ahead, state.Behind, _ = m.aheadBehind(ctx, op, base, state.Branch)
		}
	}
	return state, nil
}

// Compare reports the git facts a merge-readiness policy is built from, without
// deciding anything. See [Comparison].
//
// DHF-REQ: keel/requirement-113 (keel/ac-404, keel/ac-407)
func (m *Manager) Compare(ctx context.Context, name string) (Comparison, error) {
	const op = "compare"
	path, wantBranch, err := m.Resolve(name)
	if err != nil {
		return Comparison{}, err
	}
	comparison := Comparison{Name: name}

	regs, err := m.registrations(ctx, op)
	if err != nil {
		return comparison, err
	}
	reg, registered := lookup(regs, path)
	if !registered {
		return comparison, newError(op, CodeConflict, path, "%s is not a worktree registered with %s", path, m.repoRoot)
	}
	comparison.Branch = reg.branch

	base, baseErr := m.resolveBase(ctx, op)
	comparison.Base = base

	if reg.detached || comparison.Branch == "" {
		comparison.add(ReasonOnBaseBranch, "the checkout is on no branch, so there is nothing to compare against "+base)
	} else if baseErr == nil && sameBranch(comparison.Branch, base) {
		comparison.add(ReasonOnBaseBranch, "the checkout is on the base ref "+base+" itself")
	}
	if comparison.Branch != "" && comparison.Branch != wantBranch {
		comparison.add(ReasonInspectionFailed, "the checkout is on branch "+comparison.Branch+", not the "+wantBranch+" this work item names")
	}

	switch {
	case baseErr != nil:
		comparison.add(ReasonBaseUnresolvable, "no base ref could be resolved: "+baseErr.Error())
	case !m.refResolvable(ctx, base):
		comparison.add(ReasonBaseUnresolvable, "base ref "+base+" does not resolve locally")
	case comparison.Branch == "":
		// Nothing to count against; the on-base-branch reason already says so.
	default:
		ahead, behind, err := m.aheadBehind(ctx, op, base, comparison.Branch)
		switch {
		case err != nil:
			comparison.add(ReasonInspectionFailed, "could not count commits against base "+base+": "+err.Error())
		case ahead == 0:
			comparison.Behind = behind
			comparison.add(ReasonNoCommitsAhead, "the branch adds no commits to base "+base)
		default:
			comparison.Ahead, comparison.Behind = ahead, behind
		}
	}

	// A working tree that could not be inspected is reported as a finding, not
	// passed over as clean.
	out, err := m.run(ctx, op, path, "status", "--porcelain")
	switch {
	case err != nil:
		comparison.add(ReasonInspectionFailed, "could not read the working tree status: "+err.Error())
	case len(nonEmptyLines(out)) > 0:
		comparison.add(ReasonWorkingTreeDirty, "the working tree has uncommitted changes")
	}
	return comparison, nil
}

// aheadBehind counts commits either side of base for branch, in one revision
// walk, from the repository root.
func (m *Manager) aheadBehind(ctx context.Context, op, base, branch string) (ahead, behind int, err error) {
	out, err := m.run(ctx, op, m.repoRoot, "rev-list", "--left-right", "--count", base+"..."+branch)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, newError(op, CodeGit, "", "unreadable commit counts %q for %s...%s", strings.TrimSpace(out), base, branch)
	}
	behind, okBehind := parseCount(fields[0])
	ahead, okAhead := parseCount(fields[1])
	if !okBehind || !okAhead {
		return 0, 0, newError(op, CodeGit, "", "unreadable commit counts %q for %s...%s", strings.TrimSpace(out), base, branch)
	}
	return ahead, behind, nil
}

// sameBranch reports whether branch is the base ref by another spelling. It
// compares against the ref as written and against its last segment, so a base
// declared as a remote-tracking ref still matches the local branch it tracks —
// without this package ever hardcoding a default branch name.
func sameBranch(branch, base string) bool {
	base = strings.TrimPrefix(base, "refs/heads/")
	if branch == base {
		return true
	}
	if idx := strings.Index(base, "/"); idx >= 0 {
		return branch == base[idx+1:]
	}
	return false
}
