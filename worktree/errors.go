package worktree

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode classifies a failure. The numeric values are the exit-code
// taxonomy keel's worktree shell scripts already established, so a command
// wrapper can map an [*Error] straight onto a process exit status.
type ErrorCode int

const (
	// CodeGit is an underlying git invocation that failed for a reason this
	// package does not model more precisely.
	CodeGit ErrorCode = 1
	// CodeNotInRepository is a path or working directory that is not inside a
	// git repository.
	CodeNotInRepository ErrorCode = 2
	// CodeInvalidArgument is a caller-supplied value rejected before any git
	// command ran — an unsafe work-item name, an option-like base ref.
	CodeInvalidArgument ErrorCode = 64
	// CodeConflict is a refusal because the target path is occupied, a
	// registration points at a different path, or an existing branch/worktree
	// registration conflicts with the requested work item.
	CodeConflict ErrorCode = 65
	// CodeBlocked is a tear-down refusal: the checkout still holds work, is
	// registered in a state git will not remove cleanly, or contains content
	// the process cannot unlink. The error carries the [StaleReport].
	CodeBlocked ErrorCode = 66
	// CodeBranchMissing is an operation that needs an existing branch and did
	// not find one.
	CodeBranchMissing ErrorCode = 67
)

// String renders the code as its lower-case taxonomy name.
func (c ErrorCode) String() string {
	switch c {
	case CodeGit:
		return "git"
	case CodeNotInRepository:
		return "not_in_repository"
	case CodeInvalidArgument:
		return "invalid_argument"
	case CodeConflict:
		return "conflict"
	case CodeBlocked:
		return "blocked"
	case CodeBranchMissing:
		return "branch_missing"
	default:
		return "unknown"
	}
}

// ExitCodeDoc describes one public worktree exit-code taxonomy row.
type ExitCodeDoc struct {
	// Code is the numeric process status returned for this failure class.
	Code ErrorCode
	// Meaning is the generated-help description of the failure class.
	Meaning string
}

// ExitCodeTaxonomy returns the public worktree exit-code taxonomy in display
// order. The Code fields are the ErrorCode constants the verbs return.
//
// DHF-REQ: keel/requirement-114
func ExitCodeTaxonomy() []ExitCodeDoc {
	return []ExitCodeDoc{
		{Code: CodeGit, Meaning: "underlying git failure or unclassified worktree failure"},
		{Code: CodeNotInRepository, Meaning: "current directory is not inside a git repository"},
		{Code: CodeInvalidArgument, Meaning: "invalid worktree argument or option"},
		{Code: CodeConflict, Meaning: "path, registration, or branch/worktree conflict"},
		{Code: CodeBlocked, Meaning: "checkout cannot be removed because work or stale state blocks removal"},
		{Code: CodeBranchMissing, Meaning: "requested branch is required but does not exist"},
	}
}

// Error is every failure this package returns. Callers branch on Code (or map
// it onto an exit status); Report is populated only for [CodeBlocked].
type Error struct {
	// Op is the operation that failed, e.g. "up" or "down".
	Op string
	// Code classifies the failure.
	Code ErrorCode
	// Path is the worktree path the failure concerns, when there is one.
	Path string
	// Message states the failure in the caller's terms.
	Message string
	// Report lists the blocking items behind a [CodeBlocked] refusal. It is nil
	// for every other code.
	Report *StaleReport
	// Err is the wrapped cause, if any.
	Err error
}

// Error renders the failure with keel/worktree's package-path prefix.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("keel/worktree: ")
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the wrapped cause so errors.Is and errors.As see through it.
func (e *Error) Unwrap() error { return e.Err }

// ExitCode returns the numeric taxonomy value of the failure's code.
func (e *Error) ExitCode() int { return int(e.Code) }

// CodeOf reports the [ErrorCode] carried by err, or zero when err is nil or is
// not an [*Error].
func CodeOf(err error) ErrorCode {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

func newError(op string, code ErrorCode, path, format string, args ...any) *Error {
	return &Error{Op: op, Code: code, Path: path, Message: fmt.Sprintf(format, args...)}
}

func wrapError(op string, code ErrorCode, path string, err error, format string, args ...any) *Error {
	return &Error{Op: op, Code: code, Path: path, Message: fmt.Sprintf(format, args...), Err: err}
}
