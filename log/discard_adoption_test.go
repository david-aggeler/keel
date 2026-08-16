package log_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// discardConstructionNeedles spells the inline discard-logger constructions
// that [log.Discard] exists to retire. The fragments are concatenated at run
// time so this file does not match its own scan.
//
// DHF-TEST: keel/requirement-122
func discardConstructionNeedles() []string {
	newLogger := "slog." + "New("
	return []string{
		newLogger + "slog.NewTextHandler(io." + "Discard",
		newLogger + "slog.NewJSONHandler(io." + "Discard",
		newLogger + "slog." + "DiscardHandler",
	}
}

// TestDiscardIsTheOnlyInlineDiscardConstructionSite walks the module and fails
// on any file outside log/discard.go that builds a discard logger by hand. The
// scan is self-controlling: discard.go itself must match, so a needle that
// stopped matching anything would fail the test rather than pass it silently.
//
// DHF-TEST: keel/requirement-122
func TestDiscardIsTheOnlyInlineDiscardConstructionSite(t *testing.T) {
	moduleRoot := ".."
	canonical := filepath.Join("log", "discard.go")

	var matched []string
	err := filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if name != "." && name != ".." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range discardConstructionNeedles() {
			if strings.Contains(string(body), needle) {
				rel, relErr := filepath.Rel(moduleRoot, path)
				if relErr != nil {
					rel = path
				}
				matched = append(matched, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module tree: %v", err)
	}

	if len(matched) != 1 || matched[0] != canonical {
		t.Fatalf("inline discard-logger construction found in %v; want exactly [%s] — call log.Discard() instead", matched, canonical)
	}
}
