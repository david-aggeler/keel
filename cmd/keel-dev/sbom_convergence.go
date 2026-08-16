package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// sbomArtifactDir is the tracked tree the SBOM generation pass writes.
const sbomArtifactDir = "docs/auto-generated/sbom/"

// sbomRootArtifacts are the artifacts of that same pass which live outside it.
// NOTICE is rendered from docs/auto-generated/sbom/licenses/ by the same skill
// in the same run, so it belongs to the artifact set even though it sits at the
// repository root (keel/requirement-123).
var sbomRootArtifacts = []string{"NOTICE"}

// sbomProductName is the name the committed NOTICE must state: the last element
// of keel's module path, so the expected value follows the module rather than a
// second hand-maintained literal.
var sbomProductName = path.Base(modulePath)

// noticeProductNamePatterns locate the product name in the committed NOTICE.
// The generator interpolates it into a banner and into the rights sentence, and
// derives it from the basename of the checkout it ran in — so a regeneration in
// a unit worktree titles a public redistribution notice after an in-flight
// change request (keel/issue-157).
var noticeProductNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(\S+) — third-party software notice\s*$`),
	regexp.MustCompile(`rights in (\S+) beyond those granted by LICENSE`),
}

// generationInstantPattern matches a wall-clock generation stamp: an ISO-8601
// instant, or the bare calendar date the NOTICE footer carries. It is keyed on
// the shape rather than on a known field name so the next tool that stamps one
// into a tracked artifact is caught too, not only grype's descriptor.timestamp.
var generationInstantPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)?`)

// runSBOMConvergence is the gate step: it enumerates the git-tracked tree and
// asserts the two convergence properties of the SBOM artifact set. It runs on
// the tracked set rather than the working tree because the property is about
// what keel commits — a locally regenerated, gitignored scanner dump is not the
// subject.
//
// DHF-REQ: keel/requirement-123 (keel/ac-495, keel/ac-496)
func runSBOMConvergence(ctx context.Context, logger *slog.Logger, dir string) error {
	tracked, err := listTrackedFiles(ctx, logger, dir)
	if err != nil {
		return err
	}
	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("sbom-convergence: %d violation(s):\n%s", len(violations), strings.Join(violations, "\n"))
	}
	return nil
}

// scanSBOMConvergence reports every way the tracked SBOM artifact set records a
// property of the moment or the directory that generated it. It accumulates:
// one run names every offending artifact, so an operator repairing a
// regeneration gets the whole list instead of one file per gate run.
//
// Two policies, both stated over the committed artifact and neither over the
// generator, which is a chezmoi-managed user-level skill outside keel's change
// control (keel/change_request-184):
//
//   - sbom-notice-product-name: every product-name occurrence in the committed
//     NOTICE reads the product name, not the basename of the checkout that
//     produced it (keel/ac-495). A NOTICE carrying no recognisable occurrence is
//     itself a violation — a guard that silently matches nothing asserts
//     nothing.
//
//   - sbom-no-generation-instant: no artifact of the pass carries a generation
//     timestamp or date (keel/ac-496).
//
// DHF-REQ: keel/requirement-123 (keel/ac-495, keel/ac-496)
func scanSBOMConvergence(root string, tracked []string) ([]string, error) {
	var violations []string
	for _, file := range tracked {
		if !sbomArtifact(file) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, fmt.Errorf("sbom-convergence: read %s: %w", file, err)
		}
		lines := strings.Split(string(body), "\n")
		if isSBOMNotice(file) {
			violations = append(violations, noticeProductNameViolations(file, lines)...)
		}
		violations = append(violations, generationInstantViolations(file, lines)...)
	}
	sort.Strings(violations)
	return violations, nil
}

// noticeProductNameViolations reports product-name occurrences that name
// something other than the product, and reports the absence of any occurrence
// at all.
func noticeProductNameViolations(file string, lines []string) []string {
	var violations []string
	found := 0
	for i, line := range lines {
		for _, pattern := range noticeProductNamePatterns {
			match := pattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			found++
			if match[1] == sbomProductName {
				continue
			}
			violations = append(violations, fmt.Sprintf(
				"  sbom-notice-product-name: %s:%d names %q, want %q — the generator derives it from the basename of the checkout it ran in, so a worktree regeneration titles a public redistribution notice after an in-flight unit (keel/ac-495)",
				file, i+1, match[1], sbomProductName))
		}
	}
	if found == 0 {
		violations = append(violations, fmt.Sprintf(
			"  sbom-notice-product-name: %s carries no product-name line this policy recognises — the guard would pass while asserting nothing; restate the banner or update the patterns (keel/ac-495)",
			file))
	}
	return violations
}

// generationInstantViolations reports the first wall-clock stamp in an artifact
// and how many more follow it. One entry per file keeps a stamped 50 KB scanner
// dump from burying the other artifacts' findings.
func generationInstantViolations(file string, lines []string) []string {
	for i, line := range lines {
		match := generationInstantPattern.FindString(line)
		if match == "" {
			continue
		}
		more := ""
		if rest := countGenerationInstants(lines[i:]) - 1; rest > 0 {
			more = fmt.Sprintf(" (and %d more in this file)", rest)
		}
		return []string{fmt.Sprintf(
			"  sbom-no-generation-instant: %s:%d carries %q%s — regenerating on another date must change no committed byte; strip the field before committing or stop tracking the artifact (keel/ac-496)",
			file, i+1, match, more)}
	}
	return nil
}

func countGenerationInstants(lines []string) int {
	n := 0
	for _, line := range lines {
		n += len(generationInstantPattern.FindAllString(line, -1))
	}
	return n
}

// sbomArtifact reports whether a tracked path belongs to the SBOM generation
// pass's committed output.
func sbomArtifact(file string) bool {
	file = filepath.ToSlash(file)
	if strings.HasPrefix(file, sbomArtifactDir) {
		return true
	}
	return isSBOMNotice(file)
}

func isSBOMNotice(file string) bool {
	file = filepath.ToSlash(file)
	for _, artifact := range sbomRootArtifacts {
		if file == artifact {
			return true
		}
	}
	return false
}
