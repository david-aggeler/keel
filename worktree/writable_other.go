//go:build !unix

package worktree

// directoryWritable reports whether the calling process may create and unlink
// entries in dir. On platforms without POSIX access semantics the question has
// no cheap, side-effect-free answer, so the inspection reports nothing rather
// than guessing; a removal that then fails surfaces the platform's own error.
func directoryWritable(string) bool { return true }
