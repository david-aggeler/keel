package vscode

import (
	"math"
	"path/filepath"
	"strings"
)

// TSItemID assembles a VS Code test item id from its framework, kind,
// namespace, module-relative path, and title slug parts. It is the single
// source of truth for the id shape shared by test discovery and the result
// projectors, so a discovered item and a projected result address the same
// tree node byte-for-byte.
//
// DHF-REQ: keel/requirement-23
func TSItemID(framework, kind, namespace, rel, slug string) string {
	parts := []string{framework, kind}
	if namespace != "" {
		parts = append(parts, namespace)
	}
	parts = append(parts, rel)
	if slug != "" {
		parts = append(parts, slug)
	}
	return strings.Join(parts, "::")
}

// StableTitleSlug lowercases a test title and collapses every run of
// non-alphanumeric characters into a single dash, yielding a deterministic id
// segment. Empty results fall back to "unnamed".
//
// DHF-REQ: keel/requirement-23
func StableTitleSlug(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unnamed"
	}
	return out
}

// StatusEventName maps a framework-reported status string to the neutral
// run-event verb emitted on the wire.
//
// DHF-REQ: keel/requirement-23
func StatusEventName(status string) string {
	switch status {
	case "passed", "pass", "expected":
		return "passed"
	case "failed", "fail", "unexpected":
		return "failed"
	case "skipped", "skip", "pending":
		return "skipped"
	default:
		return "failed"
	}
}

// MergedStatusEvent folds two run-event verbs into the most severe of the two,
// used to roll leaf results up into suite/file/project aggregates.
//
// DHF-REQ: keel/requirement-23
func MergedStatusEvent(current, next string) string {
	if current == "" {
		return next
	}
	if current == "failed" || next == "failed" {
		return "failed"
	}
	if current == "passed" || next == "passed" {
		return "passed"
	}
	return "skipped"
}

// FloatDurationMillis rounds a fractional-millisecond duration up to whole
// milliseconds, clamping non-positive values to zero.
//
// DHF-REQ: keel/requirement-23
func FloatDurationMillis(duration float64) int64 {
	if duration <= 0 {
		return 0
	}
	return int64(math.Ceil(duration))
}

// IsTerminalRunEvent reports whether a run-event verb is a terminal leaf
// outcome (as opposed to a lifecycle marker like test_started or output).
//
// DHF-REQ: keel/requirement-23
func IsTerminalRunEvent(event string) bool {
	switch event {
	case "passed", "failed", "skipped", "cancelled", "errored":
		return true
	default:
		return false
	}
}

// StringInSlice reports whether target is present in values.
func StringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// NormalizeReportPath resolves a framework-reported test file path to a clean,
// forward-slashed, module-relative path, leaving already-relative paths intact.
//
// DHF-REQ: keel/requirement-23
func NormalizeReportPath(repo, path string) string {
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		rel, err := filepath.Rel(repo, clean)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(clean)
}
