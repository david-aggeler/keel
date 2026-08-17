package worktree

// BlockerKind classifies one reason a checkout cannot be reclaimed. The kinds
// are separate because the caller's remedy differs per kind: work wants a
// commit, a stash, a push, or a deliberate force; a bad registration wants a
// prune or an unlock; content that cannot be unlinked wants privilege, which no
// force in this package can supply.
type BlockerKind string

const (
	// BlockerUncommittedChange is a tracked file with changes that exist only
	// in this checkout. Remedy: commit or stash.
	BlockerUncommittedChange BlockerKind = "uncommitted_change"
	// BlockerUntrackedFile is a file git does not track and does not ignore.
	// Remedy: add and commit it, or delete it.
	BlockerUntrackedFile BlockerKind = "untracked_file"
	// BlockerUnpushedCommit is a commit reachable from this checkout but from
	// neither a remote ref nor the local default branch. Remedy: merge it into
	// the default branch, or push it.
	BlockerUnpushedCommit BlockerKind = "unpushed_commit"
	// BlockerLockedRegistration is a worktree git has marked locked. Remedy:
	// unlock it.
	BlockerLockedRegistration BlockerKind = "locked_registration"
	// BlockerStaleRegistration is a directory whose registration this repository
	// has lost or marked prunable. Remedy: prune the registrations.
	BlockerStaleRegistration BlockerKind = "stale_registration"
	// BlockerUndeletableContent is a path the calling process cannot unlink —
	// foreign ownership, or a parent directory it cannot write. The files are
	// usually worthless residue; the remedy is escalated privilege or an
	// out-of-process delete, never a force.
	BlockerUndeletableContent BlockerKind = "undeletable_content"
	// BlockerCurrentDirectory is the calling process standing inside the
	// checkout it asked to remove. Remedy: move out of it first.
	BlockerCurrentDirectory BlockerKind = "current_directory"
	// BlockerInspectionFailed is a check that could not be evaluated. It blocks:
	// an uninspectable checkout is treated as holding work, never as clean.
	BlockerInspectionFailed BlockerKind = "inspection_failed"
)

// Blocker is one item standing between a checkout and its removal, named
// precisely enough for a caller to act on without re-deriving anything.
type Blocker struct {
	// Kind classifies the item.
	Kind BlockerKind
	// Path is the offending path, worktree-relative where git reports it that
	// way. Empty for kinds that are not path-scoped.
	Path string
	// Commit is the offending commit id, for [BlockerUnpushedCommit]. Empty
	// otherwise.
	Commit string
	// Detail carries the raw observation — a porcelain status code, a git
	// message, the inspection error.
	Detail string
	// Remediation names what would clear this item.
	Remediation string
}

// StaleReport accumulates every blocking item found in one inspection. It
// accumulates rather than stopping at the first, because a caller fixing one
// item should not have to re-run to discover the next.
type StaleReport struct {
	// Blockers are the items found, in inspection order.
	Blockers []Blocker
}

// Empty reports whether the inspection found nothing blocking.
func (r StaleReport) Empty() bool { return len(r.Blockers) == 0 }

// OfKind returns the blockers of one kind, in inspection order.
func (r StaleReport) OfKind(kind BlockerKind) []Blocker {
	var out []Blocker
	for _, b := range r.Blockers {
		if b.Kind == kind {
			out = append(out, b)
		}
	}
	return out
}

// Has reports whether the inspection found at least one blocker of a kind.
func (r StaleReport) Has(kind BlockerKind) bool { return len(r.OfKind(kind)) > 0 }

// HoldsWork reports whether any blocker represents content that would be lost
// by removing the checkout, as distinct from a registration or filesystem
// problem. This is the distinction that decides whether a force is even the
// right conversation.
func (r StaleReport) HoldsWork() bool {
	return r.Has(BlockerUncommittedChange) || r.Has(BlockerUntrackedFile) || r.Has(BlockerUnpushedCommit)
}

func (r *StaleReport) add(b Blocker) { r.Blockers = append(r.Blockers, b) }
