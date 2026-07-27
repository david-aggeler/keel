package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/david-aggeler/keel/cli"
	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/worktree"
)

// worktreeCommandSpec is keel-dev's binding over keel/worktree: one namespace
// whose leaves cover bring-up (up, resume), tear-down (down), branch removal,
// and the two read-only reports (status, compare). The leaves carry argument
// parsing, keel/log-routed rendering, and the exit-code mapping and nothing
// else — every lifecycle decision belongs to keel/worktree.
//
// DHF-REQ: keel/requirement-114 (keel/ac-408, keel/ac-415)
func worktreeCommandSpec() *cli.CommandSpec {
	var downForce bool
	var branchDeleteForce bool
	var statusGlob string
	exitCodes := worktreeExitCodeSpecs()
	return &cli.CommandSpec{
		Name:      "worktree",
		Short:     "Bring worktrees up and down and report their state.",
		ExitCodes: exitCodes,
		Subcommands: []*cli.CommandSpec{
			{
				Name:        "up",
				Use:         "worktree up <name>",
				Short:       "Create the worktree for a work item, or reuse the one already there.",
				Group:       "Lifecycle",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 1, Max: 1}},
				Handler:     handleWorktreeUp,
			},
			{
				Name:        "resume",
				Use:         "worktree resume <name>",
				Short:       "Re-attach a worktree to a branch that already exists.",
				Group:       "Lifecycle",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 1, Max: 1}},
				Handler:     handleWorktreeResume,
			},
			{
				Name:        "down",
				Use:         "worktree down [--force] <name>",
				Short:       "Remove a work item's worktree, keeping its branch.",
				Group:       "Lifecycle",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 1, Max: 1}},
				Flags: []cli.FlagSpec{{
					Name:       "force",
					Short:      "Remove the checkout even though it still holds work.",
					BoolTarget: &downForce,
				}},
				Handler: handleWorktreeDown(&downForce),
			},
			{
				Name:        "branch-delete",
				Use:         "worktree branch-delete [--force] <name>",
				Short:       "Delete a work item's branch after its checkout is gone.",
				Group:       "Lifecycle",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 1, Max: 1}},
				Flags: []cli.FlagSpec{{
					Name:       "force",
					Short:      "Delete the branch even when it is not merged.",
					BoolTarget: &branchDeleteForce,
				}},
				Handler: handleWorktreeBranchDelete(&branchDeleteForce),
			},
			{
				Name:        "status",
				Use:         "worktree status <name> | worktree status --glob <pattern>",
				Short:       "Report a work item's checkout, or every checkout matching a pattern.",
				Group:       "Reports",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 0, Max: 1}},
				Flags: []cli.FlagSpec{{
					Name:         "glob",
					Value:        "pattern",
					Short:        "Report every registered checkout whose directory matches the pattern.",
					StringTarget: &statusGlob,
				}},
				Handler: handleWorktreeStatus(&statusGlob),
			},
			{
				Name:        "compare",
				Use:         "worktree compare <name>",
				Short:       "Report a branch against its base ref, without rendering a verdict.",
				Group:       "Reports",
				ExitCodes:   exitCodes,
				Positionals: []cli.PositionalSpec{{Name: "name", Min: 1, Max: 1}},
				Handler:     handleWorktreeCompare,
			},
		},
	}
}

func worktreeExitCodeSpecs() []cli.ExitCodeSpec {
	docs := worktree.ExitCodeTaxonomy()
	specs := make([]cli.ExitCodeSpec, 0, len(docs))
	for _, doc := range docs {
		specs = append(specs, cli.ExitCodeSpec{Code: int(doc.Code), Meaning: doc.Meaning})
	}
	return specs
}

func handleWorktreeUp(ctx context.Context, args []string) error {
	binding, err := newWorktreeBinding(ctx)
	if err != nil {
		return err
	}
	return binding.up(ctx, args[0])
}

func handleWorktreeResume(ctx context.Context, args []string) error {
	binding, err := newWorktreeBinding(ctx)
	if err != nil {
		return err
	}
	return binding.resume(ctx, args[0])
}

func handleWorktreeDown(force *bool) cli.Handler {
	return func(ctx context.Context, args []string) error {
		binding, err := newWorktreeBinding(ctx)
		if err != nil {
			return err
		}
		return binding.down(ctx, args[0], *force)
	}
}

func handleWorktreeBranchDelete(force *bool) cli.Handler {
	return func(ctx context.Context, args []string) error {
		binding, err := newWorktreeBinding(ctx)
		if err != nil {
			return err
		}
		return binding.branchDelete(ctx, args[0], *force)
	}
}

func handleWorktreeStatus(glob *string) cli.Handler {
	return func(ctx context.Context, args []string) error {
		pattern := strings.TrimSpace(*glob)
		switch {
		case pattern != "" && len(args) > 0:
			return worktreeInvalidArg("status", "keel-dev worktree status takes a name or --glob, not both")
		case pattern == "" && len(args) == 0:
			return worktreeInvalidArg("status", "keel-dev worktree status needs a name or --glob <pattern>")
		}
		binding, err := newWorktreeBinding(ctx)
		if err != nil {
			return err
		}
		if pattern != "" {
			return binding.statusGlob(ctx, pattern)
		}
		return binding.status(ctx, args[0])
	}
}

func handleWorktreeCompare(ctx context.Context, args []string) error {
	binding, err := newWorktreeBinding(ctx)
	if err != nil {
		return err
	}
	return binding.compare(ctx, args[0])
}

// dispatchKeelDev applies keel-dev's command-family exit-code taxonomy after
// shared CLI dispatch. Generic usage errors still exit 2; malformed worktree
// argv exits through the worktree invalid-argument row.
//
// DHF-REQ: keel/requirement-114 (keel/ac-414)
func dispatchKeelDev(ctx context.Context, tree *cli.CommandSpec, words []string) error {
	err := tree.Dispatch(ctx, words)
	if len(words) == 0 || words[0] != "worktree" {
		return err
	}
	return classifyWorktreeArgvError(tree, words, err)
}

func classifyWorktreeArgvError(tree *cli.CommandSpec, words []string, err error) error {
	if err == nil {
		return nil
	}
	var usage cli.UsageError
	if !errors.As(err, &usage) {
		return err
	}
	return worktreeInvalidArg("argv", worktreeArgvMessage(tree, words, usage.Error()))
}

func worktreeArgvMessage(tree *cli.CommandSpec, words []string, fallback string) string {
	namespace, ok := tree.Child("worktree")
	if !ok {
		return fallback
	}
	usage := namespace.Usage([]string{"worktree"})
	if len(words) == 1 {
		return fmt.Sprintf("missing worktree command\n%s", usage)
	}
	if _, ok := namespace.Child(words[1]); !ok {
		return fmt.Sprintf("unknown worktree command %q\n%s", words[1], usage)
	}
	return fallback
}

// worktreeBinding is one resolved worktree session: the manager, the primary
// checkout the worktrees hang off, and the two output streams — narrative
// through keel/log, the single result line on the protocol stream.
type worktreeBinding struct {
	manager      *worktree.Manager
	primary      string
	worktreesDir string
	// worktreesDirPresent records whether the worktrees parent existed at
	// resolution time. The reports name a path even when it does not.
	worktreesDirPresent bool
	logger              *slog.Logger
	out                 io.Writer
}

// worktreeBaseDefault is the worktrees parent used when no marker file declares
// one, matching the shell contract the skill scripts established.
const worktreeBaseDefault = "worktrees/"

// worktreeMarkerFile is the optional marker whose placeholders.worktree_base
// row relocates the worktrees parent.
const worktreeMarkerFile = "openbrain-client.local.yaml"

// newWorktreeBinding resolves the primary checkout and the worktrees parent the
// same way the skill scripts do, so a delegated wrapper addresses exactly the
// paths its shell predecessor addressed.
func newWorktreeBinding(ctx context.Context) (*worktreeBinding, error) {
	state := stateFrom(ctx)
	primary, err := primaryCheckout(ctx, state.logger, state.root)
	if err != nil {
		return nil, err
	}
	base, err := worktreeBase(primary)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(primary, base)
	present := false
	if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
		present = true
		if resolved, resolveErr := filepath.EvalSymlinks(dir); resolveErr == nil {
			dir = resolved
		}
	}
	manager, err := worktree.New(worktree.Config{
		RepoRoot:     primary,
		WorktreesDir: dir,
		Logger:       state.logger,
	})
	if err != nil {
		return nil, worktreeExit("new", err)
	}
	return &worktreeBinding{
		manager:             manager,
		primary:             primary,
		worktreesDir:        dir,
		worktreesDirPresent: present,
		logger:              state.logger,
		out:                 state.protocol,
	}, nil
}

// primaryCheckout resolves the repository's primary worktree from the common
// git directory, so a verb invoked inside a linked worktree still anchors on the
// checkout the worktrees hang off. Reading git's own layout is plumbing, not
// lifecycle work.
func primaryCheckout(ctx context.Context, logger *slog.Logger, dir string) (string, error) {
	out, code, err := gitProbe(ctx, logger, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || code != 0 {
		return "", worktreeFailure("resolve", worktree.CodeNotInRepository, "%s is not inside a git repository", dir)
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return "", worktreeFailure("resolve", worktree.CodeNotInRepository, "git reported no common directory for %s", dir)
	}
	primary := filepath.Dir(filepath.Clean(common))
	if resolved, resolveErr := filepath.EvalSymlinks(primary); resolveErr == nil {
		primary = resolved
	}
	if info, statErr := os.Stat(primary); statErr != nil || !info.IsDir() {
		return "", worktreeFailure("resolve", worktree.CodeNotInRepository, "could not locate the primary worktree of %s", dir)
	}
	return primary, nil
}

// worktreeBase reads the worktrees parent from the marker file's
// placeholders.worktree_base row, falling back to the default. An absolute
// declaration is refused rather than silently anchoring outside the repository.
func worktreeBase(primary string) (string, error) {
	body, err := os.ReadFile(filepath.Join(primary, worktreeMarkerFile))
	base := worktreeBaseDefault
	if err == nil {
		if declared := markerWorktreeBase(string(body)); declared != "" {
			base = declared
		}
	}
	if filepath.IsAbs(base) {
		return "", worktreeFailure("resolve", worktree.CodeConflict, "worktree_base must be a relative path, not absolute: %s", base)
	}
	return base, nil
}

// markerWorktreeBase extracts placeholders.worktree_base from the marker file's
// body. The marker is read positionally rather than parsed as YAML: keel's core
// compile graph carries no external dependencies, and one nested scalar does not
// justify one.
func markerWorktreeBase(body string) string {
	inPlaceholders := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "placeholders:" {
			inPlaceholders = true
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "#") {
			inPlaceholders = false
			continue
		}
		if !inPlaceholders {
			continue
		}
		indented := strings.TrimLeft(line, " \t")
		if len(indented) == len(line) || !strings.HasPrefix(indented, "worktree_base:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(indented, "worktree_base:"))
		if idx := strings.Index(value, "#"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}

// up brings the work item's worktree up by delegating the lifecycle decision to
// keel/worktree, then rendering the outcome it returns.
//
// DHF-REQ: keel/requirement-113, keel/requirement-114 (keel/ac-408, keel/ac-409, keel/ac-411)
func (b *worktreeBinding) up(ctx context.Context, name string) error {
	if _, _, err := b.manager.Resolve(name); err != nil {
		return worktreeExit("up", err)
	}
	if err := b.ensureWorktreesDir(); err != nil {
		return err
	}
	created, err := b.manager.Up(ctx, name)
	if err != nil {
		return worktreeExit("up", err)
	}

	switch created.Outcome {
	case worktree.OutcomeCreated:
		b.logger.Info("worktree created", "worktree", created.Path, "branch", created.Branch, "base", created.Base, "outcome", string(created.Outcome))
		return b.emit("up", name, created.Path)
	case worktree.OutcomeAttached:
		b.logger.Info("worktree attached", "worktree", created.Path, "branch", created.Branch, "outcome", string(created.Outcome))
		return b.emit("up", name, created.Path)
	case worktree.OutcomeReused:
		b.logger.Info("worktree already exists; no-op", "worktree", created.Path, "branch", created.Branch, "outcome", string(created.Outcome))
		return b.emit("up-noop", name, created.Path)
	default:
		return worktreeFailure("up", worktree.CodeGit, "worktree up returned unknown outcome %q for %s", created.Outcome, name)
	}
}

// resume is the strict-alias compatibility verb for the attach outcome. It
// delegates to keel/worktree and then maps the returned outcome onto the legacy
// resume tokens.
//
// DHF-REQ: keel/requirement-113, keel/requirement-114 (keel/ac-409, keel/ac-411)
func (b *worktreeBinding) resume(ctx context.Context, name string) error {
	_, branch, err := b.manager.Resolve(name)
	if err != nil {
		return worktreeExit("resume", err)
	}
	// The strict-alias resume verb preserves the legacy branch-missing refusal
	// without letting the general bring-up path create that missing branch first.
	exists, err := b.branchExists(ctx, branch)
	if err != nil {
		return worktreeExit("resume", err)
	}
	if !exists {
		return worktreeFailure("resume", worktree.CodeBranchMissing, "branch %s does not exist", branch)
	}
	if err := b.ensureWorktreesDir(); err != nil {
		return err
	}
	attached, err := b.manager.Up(ctx, name)
	if err != nil {
		return worktreeExit("resume", err)
	}
	switch attached.Outcome {
	case worktree.OutcomeAttached:
		b.logger.Info("worktree attached", "worktree", attached.Path, "branch", attached.Branch, "outcome", string(attached.Outcome))
		return b.emit("resume", name, attached.Path)
	case worktree.OutcomeReused:
		b.logger.Info("worktree already registered; no-op", "worktree", attached.Path, "branch", attached.Branch, "outcome", string(attached.Outcome))
		return b.emit("resume-noop", name, attached.Path)
	default:
		b.logger.Info("worktree resume reached non-attach outcome", "worktree", attached.Path, "branch", attached.Branch, "outcome", string(attached.Outcome))
		return worktreeFailure("resume", worktree.CodeBranchMissing,
			"branch %s reached outcome %q, want %q", branch, attached.Outcome, worktree.OutcomeAttached)
	}
}

// down removes the work item's checkout, keeping its branch. An already-absent
// checkout is a no-op success; a checkout still holding work, or one whose
// registration git cannot be trusted with, is refused with every offending item
// named.
//
// Commits absent from every remote are deliberately NOT blocking: tear-down
// never deletes the branch, so the branch ref keeps them reachable, and the
// shell contract this verb backs tore such a checkout down without complaint.
//
// DHF-REQ: keel/requirement-114 (keel/ac-409)
func (b *worktreeBinding) down(ctx context.Context, name string, force bool) error {
	path, branch, err := b.manager.Resolve(name)
	if err != nil {
		return worktreeExit("down", err)
	}
	state, err := b.manager.State(ctx, name)
	if err != nil {
		return worktreeExit("down", err)
	}
	if !state.Exists && !state.Registered {
		b.logger.Info("worktree already gone; no-op", "worktree", path, "branch", branch)
		return b.emit("down-noop", name, path)
	}
	if !state.Exists {
		// The directory is gone but the registration survived; keel/worktree
		// prunes it. Nothing on disk is destroyed, so this stays a no-op to the
		// caller.
		if _, err := b.manager.Down(ctx, name, worktree.DownOptions{}); err != nil {
			b.reportBlockers(err)
			return worktreeExit("down", err)
		}
		b.logger.Info("stale worktree registration pruned", "worktree", path)
		return b.emit("down-noop", name, path)
	}

	if blocking := worktreeDownBlockers(state.Stale, force); len(blocking) > 0 {
		for _, blocker := range blocking {
			b.logBlocker(blocker)
		}
		return worktreeFailure("down", worktree.CodeBlocked,
			"worktree %s cannot be removed: %s", path, summarizeBlockers(blocking))
	}

	// Every condition this verb refuses on has been checked above, so the
	// removal is forced past keel/worktree's own remote-comparison gate, which
	// the shell contract never had.
	removed, err := b.manager.Down(ctx, name, worktree.DownOptions{Force: true})
	if err != nil {
		b.reportBlockers(err)
		return worktreeExit("down", err)
	}
	b.logger.Info("worktree removed", "worktree", removed.Path, "branch", removed.Branch, "outcome", string(removed.Outcome))
	return b.emit("down", name, removed.Path)
}

// branchDelete removes the work item's branch as a leaf separate from
// tear-down. The default path delegates to keel/worktree's safe delete; --force
// delegates to the explicit force escape.
//
// DHF-REQ: keel/requirement-114 (keel/ac-415)
func (b *worktreeBinding) branchDelete(ctx context.Context, name string, force bool) error {
	_, branch, err := b.manager.Resolve(name)
	if err != nil {
		return worktreeExit("branch-delete", err)
	}
	if force {
		if err := b.manager.ForceDeleteBranch(ctx, name); err != nil {
			return worktreeExit("branch-delete", err)
		}
		b.logger.Info("worktree branch force-deleted", "branch", branch, "outcome", "deleted")
		return b.write(fmt.Sprintf("branch-delete %s", name))
	}
	if err := b.manager.DeleteBranch(ctx, name); err != nil {
		return worktreeExit("branch-delete", err)
	}
	b.logger.Info("worktree branch deleted", "branch", branch, "outcome", "deleted")
	return b.write(fmt.Sprintf("branch-delete %s", name))
}

// worktreeDownBlockerKinds are the blocker kinds this verb refuses on. Commits
// absent from every remote are excluded deliberately (see [worktreeBinding.down]).
var worktreeDownBlockerKinds = map[worktree.BlockerKind]bool{
	worktree.BlockerUncommittedChange:  true,
	worktree.BlockerUntrackedFile:      true,
	worktree.BlockerLockedRegistration: true,
	worktree.BlockerStaleRegistration:  true,
	worktree.BlockerUndeletableContent: true,
	worktree.BlockerCurrentDirectory:   true,
	worktree.BlockerInspectionFailed:   true,
}

// worktreeForcedBlockerKinds are the kinds --force clears. A bad registration,
// content the process cannot unlink, and a check that could not be evaluated all
// survive a force: none of them is a safety gate the caller can simply overrule.
var worktreeForcedBlockerKinds = map[worktree.BlockerKind]bool{
	worktree.BlockerUncommittedChange: true,
	worktree.BlockerUntrackedFile:     true,
	worktree.BlockerCurrentDirectory:  true,
}

// worktreeDownBlockers filters an inspection down to the items this verb refuses
// on, honoring the caller's force.
func worktreeDownBlockers(report worktree.StaleReport, force bool) []worktree.Blocker {
	var blocking []worktree.Blocker
	for _, blocker := range report.Blockers {
		if !worktreeDownBlockerKinds[blocker.Kind] {
			continue
		}
		if force && worktreeForcedBlockerKinds[blocker.Kind] {
			continue
		}
		blocking = append(blocking, blocker)
	}
	return blocking
}

// summarizeBlockers renders one "kind xN" term per distinct kind, in inspection
// order, for the single-line refusal message.
func summarizeBlockers(blockers []worktree.Blocker) string {
	counts := make(map[worktree.BlockerKind]int, len(blockers))
	var order []worktree.BlockerKind
	for _, blocker := range blockers {
		if _, seen := counts[blocker.Kind]; !seen {
			order = append(order, blocker.Kind)
		}
		counts[blocker.Kind]++
	}
	terms := make([]string, 0, len(order))
	for _, kind := range order {
		terms = append(terms, fmt.Sprintf("%s x%d", kind, counts[kind]))
	}
	return strings.Join(terms, ", ")
}

// status reports one work item's checkout: the machine-readable line the skill
// scripts consume, plus the full inspection through keel/log.
//
// DHF-REQ: keel/requirement-114 (keel/ac-409)
func (b *worktreeBinding) status(ctx context.Context, name string) error {
	state, err := b.manager.State(ctx, name)
	if err != nil {
		return worktreeExit("status", err)
	}
	_, branch, err := b.manager.Resolve(name)
	if err != nil {
		return worktreeExit("status", err)
	}
	exists, err := b.branchExists(ctx, branch)
	if err != nil {
		return worktreeExit("status", err)
	}
	b.logger.Info("worktree state",
		"worktree", state.Path,
		"branch", branch,
		"exists", state.Exists,
		"registered", state.Registered,
		"detached", state.Detached,
		"locked", state.Locked,
		"base", state.Base,
		"ahead", state.Ahead,
		"behind", state.Behind,
		"blockers", len(state.Stale.Blockers),
	)
	for _, blocker := range state.Stale.Blockers {
		b.logBlocker(blocker)
	}
	return b.emitStatus(name, b.reportPath(name, state.Path), exists, state.Registered)
}

// statusGlob reports every directory under the worktrees parent whose name
// matches the pattern. A missing parent is zero matches, not a failure.
//
// DHF-REQ: keel/requirement-114 (keel/ac-409)
func (b *worktreeBinding) statusGlob(ctx context.Context, pattern string) error {
	if !validWorktreeGlob(pattern) {
		return worktreeFailure("status", worktree.CodeInvalidArgument, "invalid glob pattern %q", pattern)
	}
	if !b.worktreesDirPresent {
		b.logger.Info("no worktrees directory; no matches", "dir", b.worktreesDir)
		return nil
	}
	entries, err := os.ReadDir(b.worktreesDir)
	if err != nil {
		return worktreeFailure("status", worktree.CodeBranchMissing, "could not read %s: %v", b.worktreesDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if matched, matchErr := filepath.Match(pattern, entry.Name()); matchErr != nil || !matched {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(b.worktreesDir, name)
		exists, err := b.branchExists(ctx, name)
		if err != nil {
			return worktreeExit("status", err)
		}
		registered, err := b.registered(ctx, name)
		if err != nil {
			return worktreeExit("status", err)
		}
		if err := b.emitStatus(name, path, exists, registered); err != nil {
			return err
		}
	}
	return nil
}

// compare reports the branch against its base ref. It renders the git facts and
// every applicable reason, and deliberately no verdict — merge readiness is the
// caller's conjunction, not this verb's.
//
// DHF-REQ: keel/requirement-114 (keel/ac-408)
func (b *worktreeBinding) compare(ctx context.Context, name string) error {
	comparison, err := b.manager.Compare(ctx, name)
	if err != nil {
		return worktreeExit("compare", err)
	}
	for _, reason := range comparison.Reasons {
		b.logger.Info("branch comparison finding", "kind", string(reason.Kind), "detail", reason.Detail)
	}
	b.logger.Info("branch comparison",
		"branch", comparison.Branch,
		"base", comparison.Base,
		"ahead", comparison.Ahead,
		"behind", comparison.Behind,
		"reasons", len(comparison.Reasons),
	)
	return b.write(fmt.Sprintf("compare %s %s base=%s ahead=%d behind=%d reasons=%d",
		name, comparison.Branch, comparison.Base, comparison.Ahead, comparison.Behind, len(comparison.Reasons)))
}

// reportPath returns the path the reports name. The manager's path is used
// whenever the worktrees parent exists; when it does not, nothing can be there,
// and the report names the sibling path the shell contract named.
func (b *worktreeBinding) reportPath(name, managed string) string {
	if b.worktreesDirPresent {
		return managed
	}
	return filepath.Join(filepath.Dir(b.primary), name)
}

// registered reports whether this repository lists a worktree for the work item.
func (b *worktreeBinding) registered(ctx context.Context, name string) (bool, error) {
	state, err := b.manager.State(ctx, name)
	if err != nil {
		return false, err
	}
	return state.Registered, nil
}

// ensureWorktreesDir creates the worktrees parent so bring-up lands under it
// even on a checkout that has never had one.
func (b *worktreeBinding) ensureWorktreesDir() error {
	if err := os.MkdirAll(b.worktreesDir, 0o755); err != nil {
		return worktreeFailure("up", worktree.CodeGit, "could not create %s: %v", b.worktreesDir, err)
	}
	b.worktreesDirPresent = true
	return nil
}

// branchExists reports whether the local branch exists. This is a read-only ref
// probe, not lifecycle work: the reports have to answer "does the branch exist"
// for a path that carries no registration, which no state query can derive.
func (b *worktreeBinding) branchExists(ctx context.Context, branch string) (bool, error) {
	_, code, err := gitProbe(ctx, b.logger, b.primary, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, worktreeFailure("probe", worktree.CodeGit, "git show-ref for branch %s exited %d", branch, code)
	}
}

// gitProbe runs one read-only git command through keel/exec and returns its
// stdout and exit status. A non-zero status is an answer, not an error, so the
// caller decides what it means.
func gitProbe(ctx context.Context, logger *slog.Logger, dir string, args ...string) (string, int, error) {
	request := procexec.Request{Program: "git", Args: args, Dir: dir}
	if logger != nil {
		request.Logger = logger
	}
	process, err := procexec.ProcessStart(ctx, request)
	if err != nil {
		return "", -1, worktreeFailure("probe", worktree.CodeGit, "could not start git %s: %v", strings.Join(args, " "), err)
	}
	result, _ := process.Wait()
	return result.Stdout, result.ExitCode, nil
}

// validWorktreeGlob accepts the pattern charset the skill scripts accept, so a
// pattern that would have been refused in shell is still refused here.
func validWorktreeGlob(pattern string) bool {
	if pattern == "" {
		return false
	}
	first := pattern[0]
	if first < 'a' || first > 'z' {
		return false
	}
	for _, r := range pattern {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '*', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// emit writes one "<token> <name> <path>" result line.
func (b *worktreeBinding) emit(token, name, path string) error {
	return b.write(fmt.Sprintf("%s %s %s", token, name, path))
}

// emitStatus writes one status result line.
func (b *worktreeBinding) emitStatus(name, path string, branch, registered bool) error {
	return b.write(fmt.Sprintf("status %s %s branch=%t worktree=%t", name, path, branch, registered))
}

// write puts one result line on the protocol stream. Result lines are the
// machine-readable contract operators and skills consume; the narrative around
// them goes through keel/log.
func (b *worktreeBinding) write(line string) error {
	if b.out == nil {
		return nil
	}
	_, err := io.WriteString(b.out, line+"\n")
	return err
}

// logBlocker records one blocking item with its remediation, so the operator
// reads what stands in the way and what would clear it.
func (b *worktreeBinding) logBlocker(blocker worktree.Blocker) {
	b.logger.Error("worktree blocker",
		"kind", string(blocker.Kind),
		"path", blocker.Path,
		"commit", blocker.Commit,
		"detail", blocker.Detail,
		"remediation", blocker.Remediation,
	)
}

// reportBlockers surfaces the inspection carried by a keel/worktree refusal, if
// it carries one.
func (b *worktreeBinding) reportBlockers(err error) {
	var typed *worktree.Error
	if !errors.As(err, &typed) || typed.Report == nil {
		return
	}
	for _, blocker := range typed.Report.Blockers {
		b.logBlocker(blocker)
	}
}

// worktreeFailure builds a refusal carrying the taxonomy's exit status.
func worktreeFailure(op string, code worktree.ErrorCode, format string, args ...any) error {
	return &logging.OperationalError{
		Op:       "worktree " + op,
		Message:  fmt.Sprintf(format, args...),
		ExitCode: int(code),
	}
}

func worktreeInvalidArg(op, message string) error {
	return worktreeExit(op, &worktree.Error{Op: op, Code: worktree.CodeInvalidArgument, Message: message})
}

// worktreeExit maps a keel/worktree failure onto its exit status. A failure the
// package does not classify exits 1, the taxonomy's git-error status.
func worktreeExit(op string, err error) error {
	if err == nil {
		return nil
	}
	code := worktree.CodeOf(err)
	if code == 0 {
		code = worktree.CodeGit
	}
	return &logging.OperationalError{
		Op:       "worktree " + op,
		Err:      err,
		ExitCode: int(code),
	}
}
