package worktree

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func normalizeUpOptions(opts []UpOptions) (UpOptions, error) {
	if len(opts) == 0 {
		return UpOptions{}, nil
	}
	if len(opts) > 1 {
		return UpOptions{}, newError("up", CodeInvalidArgument, "", "at most one UpOptions value may be supplied")
	}
	out := opts[0]
	switch out.Policy {
	case "", ReplicateMissingOnly:
		out.Policy = ReplicateMissingOnly
	case ReplicateRefresh, ReplicateOff:
	default:
		return UpOptions{}, newError("up", CodeInvalidArgument, "", "unknown replicate policy %q", out.Policy)
	}
	for _, item := range out.Replicate {
		switch item.Mode {
		case "", ReplicateCopy, ReplicateLink:
		default:
			return UpOptions{}, newError("up", CodeInvalidArgument, "", "unknown replicate mode %q for pattern %q", item.Mode, item.Pattern)
		}
	}
	return out, nil
}

func (m *Manager) validateReplicateItems(items []ReplicateItem) error {
	for _, item := range items {
		pattern := strings.TrimSpace(item.Pattern)
		if pattern == "" {
			return newError("up", CodeInvalidArgument, "", "replicate pattern must not be empty")
		}
		if filepath.IsAbs(pattern) {
			m.logReplicateHazard(item)
			return newError("up", CodeInvalidArgument, "", "replicate pattern %q must be relative to the repository root", item.Pattern)
		}
		clean := filepath.Clean(filepath.FromSlash(patternRoot(pattern)))
		if clean == "." || clean == string(filepath.Separator) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			m.logReplicateHazard(item)
			return newError("up", CodeInvalidArgument, "", "replicate pattern %q escapes the repository root", item.Pattern)
		}
		worktreesRel, ok := filepath.Rel(m.repoRoot, m.worktreesDir)
		if ok != nil {
			m.logReplicateHazard(item)
			return newError("up", CodeInvalidArgument, "", "worktrees directory %s is not relative to repository root %s", m.worktreesDir, m.repoRoot)
		}
		worktreesRel = filepath.Clean(worktreesRel)
		if worktreesRel == "." || strings.HasPrefix(worktreesRel, ".."+string(filepath.Separator)) || clean == worktreesRel || strings.HasPrefix(clean, worktreesRel+string(filepath.Separator)) {
			m.logReplicateHazard(item)
			return newError("up", CodeInvalidArgument, "", "replicate pattern %q reaches the worktrees parent %s", item.Pattern, worktreesRel)
		}
	}
	return nil
}

func patternRoot(pattern string) string {
	pattern = strings.TrimSpace(strings.TrimPrefix(filepath.ToSlash(pattern), "./"))
	cut := len(pattern)
	for _, marker := range []string{"**", "*", "?", "["} {
		if idx := strings.Index(pattern, marker); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	root := strings.TrimRight(pattern[:cut], "/")
	if root == "" {
		return pattern
	}
	return root
}

// DHF-REQ: keel/requirement-157
func (m *Manager) replicate(ctx context.Context, op string, wt *Worktree, opts UpOptions) error {
	if opts.Policy == ReplicateOff || len(opts.Replicate) == 0 {
		return nil
	}
	for _, item := range opts.Replicate {
		result, err := m.replicateItem(ctx, op, wt.Path, opts.Policy, item)
		wt.Replication = append(wt.Replication, result)
		m.logReplicateResult(result)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) replicateItem(ctx context.Context, op, worktreePath string, policy ReplicatePolicy, item ReplicateItem) (ReplicateResult, error) {
	mode := item.Mode
	if mode == "" {
		mode = ReplicateCopy
	}
	result := ReplicateResult{Pattern: item.Pattern, Mode: mode}

	sources, exists, ignored, tracked, err := m.classifyReplicateSource(ctx, op, item.Pattern)
	if err != nil {
		return result, err
	}
	if len(sources) > 0 {
		result.Path = m.materializationRoot(item.Pattern, sources[0])
	} else {
		result.Path = literalPattern(item.Pattern)
	}
	switch {
	case tracked:
		result.Outcome = ReplicateOutcomeSkippedTracked
		return result, nil
	case !exists:
		result.Outcome = ReplicateOutcomeSkippedAbsent
		return result, nil
	case !ignored:
		result.Outcome = ReplicateOutcomeSkippedNotIgnored
		return result, nil
	}

	if mode == ReplicateLink {
		src := filepath.Join(m.repoRoot, filepath.FromSlash(result.Path))
		dst := filepath.Join(worktreePath, filepath.FromSlash(result.Path))
		if err := materializeLink(op, src, dst, policy); err != nil {
			return result, err
		}
		result.Outcome = ReplicateOutcomeLinked
		return result, nil
	}
	for _, sourceRel := range sources {
		src := filepath.Join(m.repoRoot, filepath.FromSlash(sourceRel))
		dst := filepath.Join(worktreePath, filepath.FromSlash(sourceRel))
		if err := materializeCopy(op, src, dst, policy); err != nil {
			return result, err
		}
	}
	result.Outcome = ReplicateOutcomeCopied
	return result, nil
}

func materializeLink(op, src, dst string, policy ReplicatePolicy) error {
	if policy == ReplicateMissingOnly {
		if _, statErr := os.Lstat(dst); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return wrapError(op, CodeReplicateFailed, dst, statErr, "inspect replicated destination %s", dst)
		}
	}
	if policy == ReplicateRefresh {
		if err := os.RemoveAll(dst); err != nil {
			return wrapError(op, CodeReplicateFailed, dst, err, "refresh replicated destination %s", dst)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapError(op, CodeReplicateFailed, dst, err, "create parent for replicated destination %s", dst)
	}
	if err := os.Symlink(src, dst); err != nil {
		return wrapError(op, CodeReplicateFailed, dst, err, "link replicated destination %s", dst)
	}
	return nil
}

func materializeCopy(op, src, dst string, policy ReplicatePolicy) error {
	if policy == ReplicateMissingOnly {
		if _, statErr := os.Lstat(dst); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return wrapError(op, CodeReplicateFailed, dst, statErr, "inspect replicated destination %s", dst)
		}
	}
	if policy == ReplicateRefresh {
		if err := os.RemoveAll(dst); err != nil {
			return wrapError(op, CodeReplicateFailed, dst, err, "refresh replicated destination %s", dst)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapError(op, CodeReplicateFailed, dst, err, "create parent for replicated destination %s", dst)
	}
	if err := copyPath(src, dst); err != nil {
		return wrapError(op, CodeReplicateFailed, dst, err, "copy replicated destination %s", dst)
	}
	return nil
}

func (m *Manager) classifyReplicateSource(ctx context.Context, op, pattern string) (paths []string, exists, ignored, tracked bool, err error) {
	pattern = strings.TrimSpace(pattern)
	trackedOut, err := m.run(ctx, op, m.repoRoot, "ls-files", "--", pattern)
	if err != nil {
		return nil, false, false, false, err
	}
	trackedLines := nonEmptyLines(trackedOut)

	ignoredOut, err := m.run(ctx, op, m.repoRoot, "ls-files", "--others", "--ignored", "--exclude-standard", "--", pattern)
	if err != nil {
		return nil, false, false, false, err
	}
	if ignoredLines := nonEmptyLines(ignoredOut); len(ignoredLines) > 0 {
		return ignoredLines, true, true, false, nil
	}

	rel := literalPattern(pattern)
	if _, statErr := os.Lstat(filepath.Join(m.repoRoot, filepath.FromSlash(rel))); statErr == nil {
		return []string{rel}, true, false, len(trackedLines) > 0, nil
	} else if !os.IsNotExist(statErr) {
		return nil, false, false, false, wrapError(op, CodeReplicateFailed, rel, statErr, "inspect replicated source %s", rel)
	}
	return nil, false, false, len(trackedLines) > 0, nil
}

// materializationRoot resolves the path an item materializes at from the item's
// shape on disk, never from how its pattern happens to be spelled. A pattern
// naming a directory materializes that whole directory whether it is written
// `d`, `d/`, or `d/**`; only a pattern that names no directory falls back to the
// matched member. Reading the suffix instead made `d` link one member file and
// report the same outcome as the complete `d/` materialization.
//
// DHF-REQ: keel/requirement-160 (keel/ac-671)
func (m *Manager) materializationRoot(pattern, first string) string {
	root := patternRoot(pattern)
	if root == "" {
		return first
	}
	info, err := os.Lstat(filepath.Join(m.repoRoot, filepath.FromSlash(root)))
	if err == nil && info.IsDir() {
		return root
	}
	return first
}

func literalPattern(pattern string) string {
	return strings.Trim(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(pattern)), "./"), "/")
}

// copyPath reproduces one candidate. A symlink is reproduced as a symlink
// carrying the same target string — never dereferenced — the way `cp -a` and
// `rsync -a` do it: dereferencing turned a directory symlink into a read of a
// directory file descriptor and aborted bring-up outright.
//
// DHF-REQ: keel/requirement-160 (keel/ac-672)
func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return copySymlink(src, dst)
	case info.IsDir():
		return errors.New("directory copy requires ignored file candidates")
	}
	return copyFile(src, dst, info.Mode())
}

// copySymlink recreates a link rather than its target. The destination is
// unlinked first because a symlink cannot be truncated into place the way a
// regular file can.
func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, dst)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	return errors.Join(copyErr, closeErr)
}

func (m *Manager) logReplicateResult(result ReplicateResult) {
	if m.logger == nil {
		return
	}
	m.logger.Info("worktree replicate item",
		"pattern", result.Pattern,
		"path", result.Path,
		"mode", string(result.Mode),
		"outcome", string(result.Outcome),
	)
}

func (m *Manager) logReplicateHazard(item ReplicateItem) {
	mode := item.Mode
	if mode == "" {
		mode = ReplicateCopy
	}
	m.logReplicateResult(ReplicateResult{
		Pattern: item.Pattern,
		Mode:    mode,
		Outcome: ReplicateOutcomeRefusedHazard,
	})
}
