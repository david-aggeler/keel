package cli

import (
	"bytes"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

func compositeHelpTestRoot(version string) *CommandSpec {
	return &CommandSpec{
		Name: "root-node",
		Config: Config{
			Program:      "tool",
			Version:      version,
			RootSummary:  "tool does things.",
			Usage:        "tool <command>",
			HelpUsage:    "tool help [command]",
			CommandUsage: "tool <command> --help",
		},
		Subcommands: []*CommandSpec{
			{
				Name:  "grp",
				Short: "Group of commands.",
				Subcommands: []*CommandSpec{
					{Name: "leaf", Short: "Leaf command.", Flags: []FlagSpec{{Name: "id", Value: "id", Short: "Target id."}}},
				},
			},
			{Name: "status", Short: "Show status."},
		},
	}
}

func renderAllHelpLines(t *testing.T, root *CommandSpec) (string, []string) {
	t.Helper()
	var out bytes.Buffer
	root.RenderAllHelp(&out)
	got := out.String()
	return got, strings.Split(strings.TrimSuffix(got, "\n"), "\n")
}

func lineIndex(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	return -1
}

func countLine(lines []string, want string) int {
	n := 0
	for _, line := range lines {
		if line == want {
			n++
		}
	}
	return n
}

// DHF-TEST: keel/requirement-57, keel/requirement-111 (keel/ac-722)
func TestRenderAllHelpOpensWithHeaderEditionAndCarriesIdentityOnce(t *testing.T) {
	got, lines := renderAllHelpLines(t, compositeHelpTestRoot("1.2.3"))
	rule := strings.Repeat("=", logging.BannerWidth)
	if len(lines) < 3 || lines[0] != rule || lines[1] != "tool v1.2.3" || lines[2] != rule {
		t.Fatalf("first three lines = %q, want Header help edition around %q\n%s", lines[:min(3, len(lines))], "tool v1.2.3", got)
	}
	if n := countLine(lines, "tool v1.2.3"); n != 1 {
		t.Fatalf("identity line count = %d, want 1\n%s", n, got)
	}
	if strings.Count(got, "v1.2.3") != 1 {
		t.Fatalf("version string occurs %d times, want 1\n%s", strings.Count(got, "v1.2.3"), got)
	}
}

// DHF-TEST: keel/requirement-57 (keel/ac-722)
func TestRenderAllHelpHeaderShowsProgramAloneWhenVersionEmpty(t *testing.T) {
	got, lines := renderAllHelpLines(t, compositeHelpTestRoot(""))
	rule := strings.Repeat("=", logging.BannerWidth)
	if len(lines) < 3 || lines[0] != rule || lines[1] != "tool" || lines[2] != rule {
		t.Fatalf("first three lines = %q, want Header help edition around %q\n%s", lines[:min(3, len(lines))], "tool", got)
	}
	if strings.Count(got, rule) != 2 {
		t.Fatalf("Header rule occurs %d times, want 2\n%s", strings.Count(got, rule), got)
	}
}

// DHF-TEST: keel/requirement-57 (keel/ac-723)
func TestRenderAllHelpOpensEachLaterSectionWithSectionEdition(t *testing.T) {
	got, lines := renderAllHelpLines(t, compositeHelpTestRoot("1.2.3"))
	dash := strings.Repeat("-", logging.BannerWidth)

	sections := []struct {
		banner  string
		summary string
	}{
		{"tool grp", "  Group of commands."},
		{"tool grp leaf", "  Leaf command."},
		{"tool status", "  Show status."},
		{"tool help mode", "  " + helpOnlyTopics()[0].Summary},
	}
	prev := -1
	for _, s := range sections {
		i := lineIndex(lines, s.banner)
		if i < 3 {
			t.Fatalf("banner line %q missing\n%s", s.banner, got)
		}
		if i <= prev {
			t.Fatalf("banner %q at line %d is not after previous section (line %d)\n%s", s.banner, i, prev, got)
		}
		prev = i
		if countLine(lines, s.banner) != 1 {
			t.Fatalf("banner line %q occurs %d times, want 1\n%s", s.banner, countLine(lines, s.banner), got)
		}
		if lines[i-1] != dash {
			t.Fatalf("line before %q = %q, want %d-wide '-' rule\n%s", s.banner, lines[i-1], logging.BannerWidth, got)
		}
		if lines[i-2] != "" || lines[i-3] == "" {
			t.Fatalf("section %q is not preceded by exactly one blank line: %q\n%s", s.banner, lines[i-3:i-1], got)
		}
		if i+1 >= len(lines) || lines[i+1] != s.summary {
			t.Fatalf("line after banner %q = %q, want summary %q\n%s", s.banner, lines[min(i+1, len(lines)-1)], s.summary, got)
		}
	}
	if n := countLine(lines, dash); n != len(sections) {
		t.Fatalf("section rule count = %d, want %d (one per section after the root)\n%s", n, len(sections), got)
	}
	for _, title := range []string{"grp commands:", "grp leaf:", "status:", "mode:"} {
		if lineIndex(lines, title) >= 0 {
			t.Fatalf("standalone title line %q occurs in the composite dump\n%s", title, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("composite dump contains two consecutive blank lines\n%s", got)
	}
	if !strings.Contains(got, "\nUsage:\n  tool grp leaf") {
		t.Fatalf("section body is not flush-left under the banner\n%s", got)
	}
}

// DHF-TEST: keel/requirement-111, keel/requirement-149
func TestRenderAllHelpLeavesStandalonePagesUnchanged(t *testing.T) {
	root := compositeHelpTestRoot("1.2.3")
	var before bytes.Buffer
	if err := root.RenderTopicHelp(&before, []string{"grp", "leaf"}); err != nil {
		t.Fatalf("RenderTopicHelp: %v", err)
	}
	var all bytes.Buffer
	root.RenderAllHelp(&all)
	var after bytes.Buffer
	if err := root.RenderTopicHelp(&after, []string{"grp", "leaf"}); err != nil {
		t.Fatalf("RenderTopicHelp: %v", err)
	}
	if before.String() != after.String() {
		t.Fatalf("standalone page changed after RenderAllHelp:\nbefore:\n%s\nafter:\n%s", before.String(), after.String())
	}
	if want := strings.Join([]string{"tool v1.2.3", "", "grp leaf:", "  Leaf command.", ""}, "\n"); !strings.HasPrefix(after.String(), want) {
		t.Fatalf("standalone page lost its identity line or title:\n%s", after.String())
	}
}
