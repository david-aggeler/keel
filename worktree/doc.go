// Package worktree is keel's git worktree lifecycle facility: one neutral
// mechanism for bringing worktrees up and tearing them down, with no product,
// tool, or agent vocabulary anywhere in its surface. Naming, API, and defaults
// reference git concepts only.
//
// # Symmetric up and down
//
// [Manager.Up] creates, attaches, or reuses a worktree on a named branch off a
// base ref, and refuses — modifying nothing — a checkout it does not own: a
// path that is not a directory, is not a worktree registered with this
// repository, or is registered on a different branch, and a branch that is
// already checked out in some other registered worktree.
//
// [Manager.Down] removes a worktree. Its precondition is inspectable
// working-tree state, never merge status: uncommitted tracked changes,
// untracked non-ignored files, commits no remote ref and no local default
// branch keeps, locked or
// prunable registrations, and content the process cannot unlink each block
// removal and are reported individually with the offending path or commit id.
// A link this package's own replication materialized is the one exclusion: it
// points at the primary checkout, so it holds nothing removal would destroy,
// and tear-down clears it rather than counting it as work.
// That makes tear-down callable before, after, or entirely without a merge —
// an abandoned branch's checkout is reclaimable, and the branch itself always
// survives the removal.
//
// # Reports
//
// [Manager.State] and [Manager.Compare] are read-only: they run no mutating git
// subcommand, so any process may call them any number of times. [Manager.State]
// reports registration health, the blocking-item detail behind a tear-down
// refusal, and ahead/behind counts. [Manager.Compare] lifts the git facts a
// merge-readiness policy needs — on the base branch, base ref unresolvable,
// no commits ahead, working tree dirty — and accumulates one reason per
// applicable condition without rendering a verdict. Whether those facts add up
// to "ready" is the caller's conjunction, not this package's.
//
// # Branches
//
// Branch removal is never a side effect of tear-down. [Manager.DeleteBranch]
// and [Manager.ForceDeleteBranch] are separate conveniences carrying git's own
// safe-delete semantics: an unmerged branch is refused unless the force call is
// used deliberately. This is the one place the package consults merge state.
//
// # Errors and observability
//
// Failures are [*Error] values carrying an [ErrorCode] a caller can branch on
// (or map back onto a shell exit code). Every git invocation runs through
// keel/exec, so each one leaves a START/END pair in the caller's log sinks.
//
// DHF-REQ: keel/requirement-113
package worktree
