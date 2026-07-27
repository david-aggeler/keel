package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Outcome names which of bring-up's idempotent paths [Manager.Up] took.
type Outcome string

const (
	// OutcomeCreated: neither branch nor worktree existed; the branch was cut
	// from the base ref and a worktree added on it.
	OutcomeCreated Outcome = "created"
	// OutcomeAttached: the branch already existed without a checkout, and a
	// worktree was attached to it. This is what resuming interrupted work looks
	// like — an outcome, not a separate operation.
	OutcomeAttached Outcome = "attached"
	// OutcomeReused: a registered worktree on the requested branch was already
	// at the path and was reused in place; no git command mutated anything.
	OutcomeReused Outcome = "reused"
)

// baseFallbacks are the local refs tried, in order, when a caller declares no
// base and no remote default names a local branch. The first that resolves
// locally wins; none resolving is a typed error rather than a hardcoded guess.
var baseFallbacks = []string{"main", "master", "trunk"}

// Config configures a [Manager]. The zero value is usable: an empty RepoRoot
// resolves the repository from the current directory and every other field
// takes its documented default.
type Config struct {
	// RepoRoot is the repository the worktrees are anchored to. Empty resolves
	// `git rev-parse --show-toplevel` from the current working directory.
	RepoRoot string
	// WorktreesDir is the parent directory new worktrees are created under.
	// Empty defaults to <RepoRoot>/worktrees, except when RepoRoot itself sits
	// directly inside a worktrees/ directory — then siblings land under that
	// shared root rather than nesting.
	WorktreesDir string
	// BranchPrefix is prepended to the work-item name to form the branch name.
	// Empty (the default) makes the branch name equal the work-item name.
	BranchPrefix string
	// Base is the ref a newly created branch is cut from. Empty resolves the
	// repository's local default branch, falling back through main, master,
	// trunk when no remote default names one.
	Base string
	// GitBin is the git binary to execute. Empty resolves "git" through PATH.
	GitBin string
	// Env is the child git process environment as "KEY=VALUE" entries. Nil
	// inherits this process's environment unchanged.
	Env []string
	// Logger receives the START/END lifecycle record of every git invocation.
	// Nil uses keel/exec's default logger.
	Logger Logger
	// ExcludePaths are worktree-relative pathspecs whose contents never count as
	// work held by the checkout — the hook for a caller's own bookkeeping stamp.
	// The caller owns the format; this package only needs to be told to ignore it.
	ExcludePaths []string
}

// Manager runs the worktree lifecycle against one repository. Construct it with
// [New]. It holds no state across calls and starts no goroutines, so a single
// Manager may drive many concurrent worktrees; two calls for the SAME work item
// must not overlap, because each is a check-then-act sequence git does not lock.
type Manager struct {
	repoRoot     string
	worktreesDir string
	branchPrefix string
	base         string
	gitBin       string
	env          []string
	logger       Logger
	excludePaths []string
}

// Worktree is the rooting metadata bring-up returns.
type Worktree struct {
	// Name is the work-item name as supplied to [Manager.Up].
	Name string
	// Path is the absolute worktree directory.
	Path string
	// Branch is the branch checked out in the worktree.
	Branch string
	// Base is the ref the branch was cut from. Empty unless Outcome is
	// [OutcomeCreated] — an attached or reused worktree created no branch.
	Base string
	// BaseSHA is the commit Base resolved to when the branch was cut. Empty unless
	// Outcome is [OutcomeCreated].
	BaseSHA string
	// Outcome records which idempotent path bring-up took.
	Outcome Outcome
}

// New resolves and validates a [Manager] from cfg. It runs a git command only
// when RepoRoot is empty.
func New(cfg Config) (*Manager, error) {
	m := &Manager{
		gitBin:       cfg.GitBin,
		branchPrefix: cfg.BranchPrefix,
		base:         cfg.Base,
		env:          cfg.Env,
		logger:       cfg.Logger,
		excludePaths: append([]string(nil), cfg.ExcludePaths...),
	}
	if m.gitBin == "" {
		m.gitBin = "git"
	}

	repoRoot := cfg.RepoRoot
	if repoRoot == "" {
		out, err := m.run(context.Background(), "new", workingDirectory(), "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, wrapError("new", CodeNotInRepository, "", err, "resolve repository root")
		}
		repoRoot = strings.TrimSpace(out)
		if repoRoot == "" {
			return nil, newError("new", CodeNotInRepository, "", "repository top level resolved to an empty path")
		}
	}
	m.repoRoot = canonical(repoRoot)

	m.worktreesDir = cfg.WorktreesDir
	if m.worktreesDir == "" {
		m.worktreesDir = defaultWorktreesDir(m.repoRoot)
	}
	m.worktreesDir = canonical(m.worktreesDir)

	// A ref beginning with "-" would be read by git as a flag, not a ref: the
	// one option-injection vector in an otherwise validated-or-absolute argv.
	if strings.HasPrefix(m.base, "-") {
		return nil, newError("new", CodeInvalidArgument, "", "base ref %q must not start with %q", m.base, "-")
	}
	if strings.HasPrefix(m.branchPrefix, "-") {
		return nil, newError("new", CodeInvalidArgument, "", "branch prefix %q must not start with %q", m.branchPrefix, "-")
	}
	return m, nil
}

// RepoRoot returns the absolute repository root the manager is anchored to.
func (m *Manager) RepoRoot() string { return m.repoRoot }

// Resolve derives the absolute worktree path and branch name for a work item
// without touching git, rejecting an empty or unsafe name. Callers can pre-flight
// with it, and every operation below runs it before the first git command so a
// bad name never half-creates a directory or a ref.
func (m *Manager) Resolve(name string) (path, branch string, err error) {
	if err := validateName(name); err != nil {
		return "", "", err
	}
	return filepath.Join(m.worktreesDir, name), m.branchPrefix + name, nil
}

// Up makes a worktree for name exist on its matching branch, idempotently:
// it cuts the branch from the base ref and adds a worktree when neither exists
// ([OutcomeCreated]), attaches a worktree when the branch exists without one
// ([OutcomeAttached]), or reuses the registered worktree in place
// ([OutcomeReused]).
//
// It refuses, changing nothing, when the target path exists but is not a
// directory, is not a worktree registered with this repository, or is
// registered on a different branch, and when the requested branch is already
// checked out in another registered worktree — the last refusal names that
// checkout so the caller can attach there instead of silently growing a second
// root for the same branch. Every refusal is [CodeConflict].
//
// DHF-REQ: keel/requirement-113 (keel/ac-400, keel/ac-406, keel/ac-416, keel/ac-417)
func (m *Manager) Up(ctx context.Context, name string) (*Worktree, error) {
	const op = "up"
	path, branch, err := m.Resolve(name)
	if err != nil {
		return nil, err
	}

	regs, err := m.registrations(ctx, op)
	if err != nil {
		return nil, err
	}

	if info, statErr := os.Stat(path); statErr == nil {
		// Something is already at the path. Only a directory this repository
		// lists as a worktree on the matching branch is a safe reuse; a file, a
		// stray directory, a nested independent repository, or the right path on
		// the wrong branch is a collision, and adopting it would let a session
		// commit into a checkout it does not own.
		if !info.IsDir() {
			return nil, newError(op, CodeConflict, path, "%s already exists and is not a directory — refusing to reuse it", path)
		}
		reg, registered := lookup(regs, path)
		if !registered {
			return nil, newError(op, CodeConflict, path, "%s already exists but is not a worktree registered with %s — refusing to reuse the wrong checkout", path, m.repoRoot)
		}
		if reg.branch != branch {
			return nil, newError(op, CodeConflict, path, "%s is registered on branch %q, want %q — refusing to reuse the wrong checkout", path, reg.branch, branch)
		}
		return &Worktree{Name: name, Path: path, Branch: branch, Outcome: OutcomeReused}, nil
	} else if !os.IsNotExist(statErr) {
		return nil, wrapError(op, CodeGit, path, statErr, "inspect %s", path)
	}

	// The path is free, but the branch may already be rooted somewhere else.
	// git refuses that itself; naming the occupying checkout is what turns the
	// refusal into something the caller can act on.
	if occupied, ok := branchCheckout(regs, branch); ok && occupied != canonical(path) {
		return nil, newError(op, CodeConflict, path, "branch %q is already checked out in the worktree at %s — refusing to create a second checkout at %s", branch, occupied, path)
	}

	if m.branchExists(ctx, branch) {
		if _, err := m.run(ctx, op, m.repoRoot, "worktree", "add", path, branch); err != nil {
			return nil, err
		}
		return &Worktree{Name: name, Path: path, Branch: branch, Outcome: OutcomeAttached}, nil
	}

	base, err := m.resolveBase(ctx, op)
	if err != nil {
		return nil, err
	}
	baseHead, err := m.revParse(ctx, op, base)
	if err != nil {
		return nil, err
	}
	if _, err := m.run(ctx, op, m.repoRoot, "worktree", "add", "-b", branch, path, baseHead); err != nil {
		return nil, err
	}
	if err := m.storeBranchBase(ctx, op, branch, base); err != nil {
		return nil, err
	}
	return &Worktree{Name: name, Path: path, Branch: branch, Base: base, BaseSHA: baseHead, Outcome: OutcomeCreated}, nil
}

// ResetFresh returns an already-registered worktree to its base ref for a fresh
// pickup: it refuses while the branch carries commits the base does not have,
// then resets the checkout hard and removes untracked files. It stays separate
// from [Manager.Up] because only the caller knows whether a pickup is fresh work
// or a resume that must preserve what the branch already holds.
func (m *Manager) ResetFresh(ctx context.Context, name string) (*Worktree, error) {
	const op = "reset"
	path, branch, err := m.Resolve(name)
	if err != nil {
		return nil, err
	}
	regs, err := m.registrations(ctx, op)
	if err != nil {
		return nil, err
	}
	reg, registered := lookup(regs, path)
	if !registered {
		return nil, newError(op, CodeConflict, path, "%s is not a worktree registered with %s", path, m.repoRoot)
	}
	if reg.branch != branch {
		return nil, newError(op, CodeConflict, path, "%s is registered on branch %q, want %q", path, reg.branch, branch)
	}

	base, err := m.resolveBase(ctx, op)
	if err != nil {
		return nil, err
	}
	baseHead, err := m.revParse(ctx, op, base)
	if err != nil {
		return nil, err
	}
	ahead, err := m.countCommits(ctx, op, baseHead+".."+branch)
	if err != nil {
		return nil, err
	}
	if ahead != 0 {
		return nil, newError(op, CodeBlocked, path, "branch %q has %d commit(s) not in base %q (%s) — refusing to discard them", branch, ahead, base, baseHead)
	}
	if _, err := m.run(ctx, op, path, "reset", "--hard", baseHead); err != nil {
		return nil, err
	}
	if _, err := m.run(ctx, op, path, "clean", "-ffd"); err != nil {
		return nil, err
	}
	return &Worktree{Name: name, Path: path, Branch: branch, Base: base, BaseSHA: baseHead, Outcome: OutcomeReused}, nil
}

// DeleteBranch removes the branch for name with git's safe-delete semantics:
// a branch not fully merged into HEAD is refused and preserved, with git's own
// message surfaced. This and [Manager.ForceDeleteBranch] are the only calls in
// the package that consult merge state, and they are deliberately separate from
// tear-down — the branch outlives its checkout until a caller says otherwise.
func (m *Manager) DeleteBranch(ctx context.Context, name string) error {
	return m.deleteBranch(ctx, name, "-d")
}

// ForceDeleteBranch removes the branch for name whether or not it is merged.
// It is the deliberate escape for a branch whose work is being abandoned; no
// other force in this package cascades into it.
func (m *Manager) ForceDeleteBranch(ctx context.Context, name string) error {
	return m.deleteBranch(ctx, name, "-D")
}

func (m *Manager) deleteBranch(ctx context.Context, name, flag string) error {
	const op = "delete-branch"
	_, branch, err := m.Resolve(name)
	if err != nil {
		return err
	}
	if !m.branchExists(ctx, branch) {
		return newError(op, CodeBranchMissing, "", "branch %q does not exist", branch)
	}
	if _, err := m.run(ctx, op, m.repoRoot, "branch", flag, branch); err != nil {
		return err
	}
	return nil
}

// resolveBase returns the configured base ref, or the local default/fallback
// base that resolves locally. Nothing resolving is a typed error, never a guess.
//
// DHF-REQ: keel/requirement-113 (keel/ac-416, keel/ac-417)
func (m *Manager) resolveBase(ctx context.Context, op string) (string, error) {
	if m.base != "" {
		if !m.refResolvable(ctx, m.base) {
			return m.base, newError(op, CodeBranchMissing, "", "base ref %q does not resolve in %s", m.base, m.repoRoot)
		}
		return m.base, nil
	}
	candidates := m.baseCandidates(ctx, op)
	for _, ref := range candidates {
		if m.refResolvable(ctx, ref) {
			return ref, nil
		}
	}
	return "", newError(op, CodeBranchMissing, "", "no base ref declared and none of %s resolves in %s", strings.Join(candidates, ", "), m.repoRoot)
}

func (m *Manager) baseCandidates(ctx context.Context, op string) []string {
	candidates := append([]string(nil), baseFallbacks...)
	if !m.refResolvable(ctx, "refs/remotes/origin/HEAD") {
		return candidates
	}
	out, err := m.run(ctx, op, m.repoRoot, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return candidates
	}
	remoteDefault := strings.TrimSpace(out)
	_, localDefault, ok := strings.Cut(remoteDefault, "/")
	if !ok || localDefault == "" {
		return candidates
	}
	deduped := []string{localDefault}
	for _, candidate := range candidates {
		if candidate != localDefault {
			deduped = append(deduped, candidate)
		}
	}
	return deduped
}

// BranchExists reports whether the branch for a work item exists locally,
// without touching the repository: it neither creates the branch (which
// [Manager.Up] would) nor consults a worktree registration (which is what makes
// [Manager.State] unable to answer for a path that carries none). It is the
// non-mutating question a caller would otherwise hand-roll as its own git ref
// probe.
//
// A git failure is returned as an error rather than folded into false, so a
// caller can tell "no such branch" apart from "git broke".
//
// DHF-REQ: keel/requirement-113 (keel/ac-420)
func (m *Manager) BranchExists(ctx context.Context, name string) (bool, error) {
	const op = "branch-exists"
	_, branch, err := m.Resolve(name)
	if err != nil {
		return false, err
	}
	return m.branchRefExists(ctx, op, branch)
}

// branchRefExists is the single implementation of the branch-existence question.
// for-each-ref is used rather than show-ref because it exits zero for a ref that
// is simply not there, leaving a non-zero exit to mean what it says: git failed.
// The pattern also matches refs BELOW the branch (refs/heads/x/y for branch x),
// so the answer is an exact refname match, not a non-empty result.
func (m *Manager) branchRefExists(ctx context.Context, op, branch string) (bool, error) {
	ref := "refs/heads/" + branch
	out, err := m.run(ctx, op, m.repoRoot, "for-each-ref", "--format=%(refname)", ref)
	if err != nil {
		return false, err
	}
	for _, line := range nonEmptyLines(out) {
		if line == ref {
			return true, nil
		}
	}
	return false, nil
}

// branchExists is the internal bool-only view for the lifecycle paths that
// already treat an unanswerable git as "not there" and fail on the next command.
func (m *Manager) branchExists(ctx context.Context, branch string) bool {
	exists, err := m.branchRefExists(ctx, "probe", branch)
	return err == nil && exists
}

func (m *Manager) refResolvable(ctx context.Context, ref string) bool {
	return m.runQuiet(ctx, m.repoRoot, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

func (m *Manager) revParse(ctx context.Context, op, ref string) (string, error) {
	out, err := m.run(ctx, op, m.repoRoot, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (m *Manager) storeBranchBase(ctx context.Context, op, branch, base string) error {
	_, err := m.run(ctx, op, m.repoRoot, "config", "branch."+branch+".keel-worktree-base", base)
	return err
}

func (m *Manager) branchBase(ctx context.Context, op, branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	out, err := m.run(ctx, op, m.repoRoot, "config", "--get", "--default", "", "branch."+branch+".keel-worktree-base")
	if err != nil {
		return "", false
	}
	base := strings.TrimSpace(out)
	return base, base != ""
}

func (m *Manager) countCommits(ctx context.Context, op, revRange string) (int, error) {
	out, err := m.run(ctx, op, m.repoRoot, "rev-list", "--count", revRange)
	if err != nil {
		return 0, err
	}
	n, ok := parseCount(out)
	if !ok {
		return 0, newError(op, CodeGit, "", "unreadable commit count %q for range %q", strings.TrimSpace(out), revRange)
	}
	return n, nil
}

// branchCheckout reports the canonical path of the registered worktree that has
// branch checked out, if any.
func branchCheckout(list []registration, branch string) (string, bool) {
	for _, reg := range list {
		if !reg.detached && reg.branch == branch {
			return reg.path, true
		}
	}
	return "", false
}

// defaultWorktreesDir returns <repoRoot>/worktrees, or the repository's own
// parent when the repository itself sits directly inside a worktrees directory —
// so operating from one linked worktree still lands siblings under the shared
// root rather than nesting.
func defaultWorktreesDir(repoRoot string) string {
	parent := filepath.Dir(repoRoot)
	if filepath.Base(parent) == "worktrees" {
		return parent
	}
	return filepath.Join(repoRoot, "worktrees")
}

func workingDirectory() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// validateName rejects an empty or unsafe work-item name before any git command
// runs. The name must be one safe path component AND a valid single-level git
// ref: no separators, no traversal, no leading dash (git would read it as a
// flag), only [A-Za-z0-9._-] runes, and none of the ref pitfalls that would
// otherwise pass path validation and fail only after the directory and ref were
// partly created.
func validateName(name string) error {
	const op = "resolve"
	switch {
	case strings.TrimSpace(name) == "":
		return newError(op, CodeInvalidArgument, "", "empty work-item name")
	case name != strings.TrimSpace(name):
		return newError(op, CodeInvalidArgument, "", "work-item name %q has leading or trailing whitespace", name)
	case name == "." || name == "..":
		return newError(op, CodeInvalidArgument, "", "unsafe work-item name %q", name)
	case strings.ContainsAny(name, `/\`) || strings.Contains(name, ".."):
		return newError(op, CodeInvalidArgument, "", "work-item name %q must not contain path separators or %q", name, "..")
	case strings.HasPrefix(name, "-"):
		return newError(op, CodeInvalidArgument, "", "work-item name %q must not start with %q", name, "-")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return newError(op, CodeInvalidArgument, "", "work-item name %q contains disallowed character %q", name, string(r))
		}
	}
	switch {
	case strings.HasPrefix(name, ".") || strings.HasSuffix(name, "."):
		return newError(op, CodeInvalidArgument, "", "work-item name %q must not begin or end with %q", name, ".")
	case strings.HasSuffix(name, ".lock"):
		return newError(op, CodeInvalidArgument, "", "work-item name %q must not end in %q", name, ".lock")
	case name == "HEAD":
		return newError(op, CodeInvalidArgument, "", "work-item name %q is reserved", name)
	}
	return nil
}
