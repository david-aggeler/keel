package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// discardCanonicalSite is the one file permitted to build a discard logger by
// hand: [log.Discard]'s own body.
var discardCanonicalSite = filepath.Join("log", "discard.go")

// discardConstructionNeedles spells the inline discard-logger constructions
// that log.Discard exists to retire. The fragments are concatenated at run time
// so this file does not match its own scan.
func discardConstructionNeedles() []string {
	newLogger := "slog." + "New("
	return []string{
		newLogger + "slog.NewTextHandler(io." + "Discard",
		newLogger + "slog.NewJSONHandler(io." + "Discard",
		newLogger + "slog." + "DiscardHandler",
	}
}

// scanInlineDiscardConstruction reports every .go file outside log/discard.go
// that builds a discard logger by hand (keel/ac-493). It reads file bodies
// rather than walking the tree: its input is the gate's tracked-files selector
// output, so gitignored scratch and in-flight worktrees are already out of the
// evaluated set (keel/requirement-85, keel/ac-501). Test files are scanned too
// — the duplication the policy retires is not a production-only habit.
//
// The scan keeps the self-control property of the test it replaces: when
// log/discard.go is in the evaluated set it must itself match, so a needle that
// silently stopped matching anything fails the gate rather than passing it.
//
// DHF-REQ: keel/requirement-122 (keel/ac-493), keel/requirement-85 (keel/ac-501)
func scanInlineDiscardConstruction(root string, files []string) ([]string, error) {
	var violations []string
	canonicalPresent, canonicalMatched := false, false
	for _, file := range files {
		if filepath.Ext(file) != ".go" {
			continue
		}
		rel := filepath.FromSlash(file)
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		text := string(body)
		if rel == discardCanonicalSite {
			canonicalPresent = true
		}
		for _, needle := range discardConstructionNeedles() {
			line := firstLineContaining(text, needle)
			if line == 0 {
				continue
			}
			if rel == discardCanonicalSite {
				canonicalMatched = true
				break
			}
			violations = append(violations, fmt.Sprintf(
				"  no-inline-discard-construction: %s:%d builds a discard logger inline — call log.Discard() instead (keel/requirement-122, keel/ac-493)",
				rel, line))
			break
		}
	}
	// Self-control: the canonical site must match, or the needles have drifted
	// away from the construction they are supposed to name and every "clean"
	// result above is vacuous.
	if canonicalPresent && !canonicalMatched {
		violations = append(violations, fmt.Sprintf(
			"  no-inline-discard-construction: %s matches none of the discard-construction needles — the needles have drifted and the scan proves nothing (keel/ac-493)",
			discardCanonicalSite))
	}
	return violations, nil
}
