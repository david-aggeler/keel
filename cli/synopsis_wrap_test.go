package cli

import (
	"slices"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-101 (keel/ac-724)
func TestWrapSynopsisPacksWholePartsIntoWidthWithIndentedContinuations(t *testing.T) {
	parts := []string{"tool", "[--mode human|ai|json]", "[-v|--verbose]", "[-q|--quiet]", "[--color auto|always|never]", "<command> [args]"}
	oneLine := strings.Join(parts, " ")
	indent := strings.Repeat(" ", len("tool")+1)

	// Every width fits the widest part plus the continuation indent (5+27), the
	// precondition keel/ac-724 states; each is narrower than the one line.
	for _, width := range []int{32, 40, 60} {
		lines := wrapSynopsis(parts, width)
		if len(lines) < 2 {
			t.Fatalf("wrapSynopsis(width %d) = %q, want more than one line for a %d-column synopsis", width, lines, len(oneLine))
		}
		if !strings.HasPrefix(lines[0], "tool ") {
			t.Fatalf("wrapSynopsis(width %d) first line = %q, want the program name first", width, lines[0])
		}
		var rejoined []string
		for i, line := range lines {
			if len(line) > width {
				t.Fatalf("wrapSynopsis(width %d) line %d = %q is %d columns, want at most %d", width, i, line, len(line), width)
			}
			if i > 0 {
				if !strings.HasPrefix(line, indent) || strings.HasPrefix(line, indent+" ") {
					t.Fatalf("wrapSynopsis(width %d) continuation line %d = %q, want indent of exactly %d columns (past the program name)", width, i, line, len(indent))
				}
			}
			rejoined = append(rejoined, strings.TrimSpace(line))
		}
		// Each part stays whole: the parts, in order, are a partition of the
		// lines' contents at part boundaries.
		next := 0
		for i, line := range rejoined {
			for rest := line; rest != ""; {
				if next >= len(parts) || !strings.HasPrefix(rest, parts[next]) {
					t.Fatalf("wrapSynopsis(width %d) line %d = %q does not continue with whole part %d %q; lines %q", width, i, line, next, parts[min(next, len(parts)-1)], lines)
				}
				rest = strings.TrimPrefix(strings.TrimPrefix(rest, parts[next]), " ")
				next++
			}
		}
		if next != len(parts) {
			t.Fatalf("wrapSynopsis(width %d) rendered %d of %d parts: %q", width, next, len(parts), lines)
		}
	}

	for _, width := range []int{0, -1} {
		if got := wrapSynopsis(parts, width); !slices.Equal(got, []string{oneLine}) {
			t.Fatalf("wrapSynopsis(width %d) = %q, want one line %q", width, got, oneLine)
		}
	}
	if got := wrapSynopsis(nil, 40); got != nil {
		t.Fatalf("wrapSynopsis(nil) = %q, want nil", got)
	}
}
