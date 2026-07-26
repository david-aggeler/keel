//go:build unix

package worktree

import "syscall"

// directoryWritable reports whether the calling process may create and unlink
// entries in dir. Unlinking a file is a permission on its PARENT directory, not
// on the file, which is why the check is directory-scoped: residue owned by
// another user is removable when its directory is writable, and residue the
// caller owns is not when the directory is not.
func directoryWritable(dir string) bool {
	return syscall.Access(dir, writeOK|executeOK) == nil
}

const (
	writeOK   = 0x2
	executeOK = 0x1
)
