package vscode

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GoSelection is the parsed shape of a Go test item id selected in VS Code.
type GoSelection struct {
	Kind      string
	Pkg       string
	File      string
	TestName  string
	TestNames []string
}

// ParseGoItemID decodes a VS Code Go test item id into a GoSelection.
//
// DHF-REQ: keel/requirement-23
func ParseGoItemID(id string) (GoSelection, bool) {
	switch {
	case id == "go::root":
		return GoSelection{Kind: "root", Pkg: "..."}, true
	case strings.HasPrefix(id, "go::pkg::"):
		return GoSelection{Kind: "package", Pkg: strings.TrimPrefix(id, "go::pkg::")}, true
	case strings.HasPrefix(id, "go::file::"):
		rel := strings.TrimPrefix(id, "go::file::")
		if rel == "" {
			return GoSelection{}, false
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		return GoSelection{Kind: "file", Pkg: pkg, File: rel}, true
	case strings.HasPrefix(id, "go::test::"):
		rest := strings.TrimPrefix(id, "go::test::")
		pkg, testName, ok := strings.Cut(rest, "::")
		if !ok || pkg == "" || testName == "" {
			return GoSelection{}, false
		}
		return GoSelection{Kind: "test", Pkg: pkg, TestName: testName}, true
	default:
		return GoSelection{}, false
	}
}

// GoEventPackageRel resolves a go test -json package path to a module-relative
// path, or "" when it is outside the module.
//
// DHF-REQ: keel/requirement-23
func GoEventPackageRel(pkg, modulePath string) string {
	switch {
	case pkg == "":
		return ""
	case modulePath != "" && pkg == modulePath:
		return "."
	case modulePath != "" && strings.HasPrefix(pkg, modulePath+"/"):
		return strings.TrimPrefix(pkg, modulePath+"/")
	default:
		return ""
	}
}

// GoPackageArg turns a module-relative package path into a `go` command
// package argument.
//
// DHF-REQ: keel/requirement-23
func GoPackageArg(pkg string) string {
	if pkg == "." {
		return "."
	}
	if pkg == "..." {
		return "./..."
	}
	return "./" + filepath.ToSlash(pkg)
}

// GoTestNamePattern builds a `-run` regexp matching exactly the given test
// names.
//
// DHF-REQ: keel/requirement-23
func GoTestNamePattern(names []string) string {
	escaped := make([]string, 0, len(names))
	for _, name := range names {
		escaped = append(escaped, regexp.QuoteMeta(name))
	}
	return "^(" + strings.Join(escaped, "|") + ")$"
}

// MergeGoAggregateResult folds a per-test run event into a running aggregate,
// keeping the most severe outcome and summing durations.
//
// DHF-REQ: keel/requirement-23
func MergeGoAggregateResult(current, next RunEvent) RunEvent {
	if current.Event == "" {
		return next
	}
	current.DurationMS += next.DurationMS
	if next.Event == "failed" || next.Event == "errored" {
		current.Event = next.Event
		current.Message = next.Message
	} else if next.Event == "skipped" && current.Event == "passed" {
		current.Event = "skipped"
		current.Message = next.Message
	}
	return current
}

// GoElapsedMillis converts a go test -json Elapsed value (seconds) to
// milliseconds, falling back to wall-clock elapsed when Elapsed is absent.
//
// DHF-REQ: keel/requirement-23
func GoElapsedMillis(elapsed float64, start time.Time) int64 {
	if elapsed > 0 {
		return int64(elapsed * 1000)
	}
	return time.Since(start).Milliseconds()
}

// ParseVitestItemID extracts the file path from a VS Code vitest test item id.
//
// DHF-REQ: keel/requirement-23
func ParseVitestItemID(id string) (string, bool) {
	if id == "vitest::root" {
		return "", true
	}
	for _, prefix := range []string{"vitest::file::", "vitest::suite::", "vitest::test::"} {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		rest := strings.TrimPrefix(id, prefix)
		file, _, _ := strings.Cut(rest, "::")
		if file == "" {
			return "", false
		}
		return file, true
	}
	return "", false
}

// PlaywrightSelection is the parsed shape of a Playwright test item id selected
// in VS Code.
type PlaywrightSelection struct {
	Project string
	File    string
}

// ParsePlaywrightItemID decodes a VS Code Playwright test item id into a
// PlaywrightSelection.
//
// DHF-REQ: keel/requirement-23
func ParsePlaywrightItemID(id string) (PlaywrightSelection, bool) {
	switch {
	case strings.HasPrefix(id, "playwright::project::"):
		project := strings.TrimPrefix(id, "playwright::project::")
		return PlaywrightSelection{Project: project}, project != ""
	case strings.HasPrefix(id, "playwright::file::"):
		rest := strings.TrimPrefix(id, "playwright::file::")
		project, file, ok := strings.Cut(rest, "::")
		return PlaywrightSelection{Project: project, File: file}, ok && project != "" && file != ""
	case strings.HasPrefix(id, "playwright::test::"):
		rest := strings.TrimPrefix(id, "playwright::test::")
		project, remaining, ok := strings.Cut(rest, "::")
		if !ok {
			return PlaywrightSelection{}, false
		}
		file, _, ok := strings.Cut(remaining, "::")
		return PlaywrightSelection{Project: project, File: file}, ok && project != "" && file != ""
	default:
		return PlaywrightSelection{}, false
	}
}

// ParsePlaywrightListDurationMS parses a Playwright list-reporter duration
// token (e.g. "1.2s", "340ms", "2m") into milliseconds.
//
// DHF-REQ: keel/requirement-23
func ParsePlaywrightListDurationMS(raw string) (int64, bool) {
	value := strings.TrimSpace(raw)
	multiplier := float64(1)
	switch {
	case strings.HasSuffix(value, "ms"):
		value = strings.TrimSpace(strings.TrimSuffix(value, "ms"))
	case strings.HasSuffix(value, "s"):
		value = strings.TrimSpace(strings.TrimSuffix(value, "s"))
		multiplier = 1000
	case strings.HasSuffix(value, "m"):
		value = strings.TrimSpace(strings.TrimSuffix(value, "m"))
		multiplier = 60 * 1000
	default:
		return 0, false
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || amount < 0 {
		return 0, false
	}
	return int64(math.Round(amount * multiplier)), true
}
