package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// noticeFixture renders a NOTICE body shaped like the committed one: a titled
// banner, the rights sentence, and an optional trailing "Generated:" line.
func noticeFixture(product, generated string) string {
	body := "" +
		"=============================================================================\n" +
		product + " — third-party software notice\n" +
		"=============================================================================\n" +
		"\n" +
		"The product itself is licensed under the terms of the LICENSE file at the\n" +
		"root of this repository. Nothing in this NOTICE file grants any additional\n" +
		"rights in " + product + " beyond those granted by LICENSE.\n"
	if generated != "" {
		body += "\nGenerated: " + generated + "\n"
	}
	return body
}

// sbomFixture writes a tracked-artifact tree and returns the artifact set as the
// gate would enumerate it from git.
func sbomFixture(t *testing.T, files map[string]string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	var tracked []string
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		tracked = append(tracked, name)
	}
	return dir, tracked
}

// A NOTICE regenerated in a unit worktree is titled after the checkout
// directory, which is what keel/issue-157 measured. The guard must red on the
// mutated artifact and name the offending line — the committed copy reads
// "keel" by hand today, so a check only ever run against the live tree would
// pass vacuously forever.
//
// DHF-TEST: keel/requirement-123 (keel/ac-495)
func TestSBOMConvergenceRejectsNOTICENamedAfterTheCheckoutDirectory(t *testing.T) {
	dir, tracked := sbomFixture(t, map[string]string{
		"NOTICE": noticeFixture("cr-190", ""),
	})

	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("want both product-name occurrences reported, got %d:\n%s", len(violations), strings.Join(violations, "\n"))
	}
	joined := strings.Join(violations, "\n")
	for _, want := range []string{"sbom-notice-product-name", "NOTICE:2", "NOTICE:7", "cr-190", "keel/ac-495"} {
		if !strings.Contains(joined, want) {
			t.Errorf("violation text missing %q:\n%s", want, joined)
		}
	}
}

// DHF-TEST: keel/requirement-123 (keel/ac-495)
func TestSBOMConvergenceAcceptsNOTICENamingTheProduct(t *testing.T) {
	dir, tracked := sbomFixture(t, map[string]string{
		"NOTICE": noticeFixture("keel", ""),
	})

	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("a NOTICE naming keel must pass, got:\n%s", strings.Join(violations, "\n"))
	}
}

// A NOTICE whose banner the generator reshapes carries no occurrence this guard
// matches, so it would pass while asserting nothing. That silent pass is the
// failure mode the criterion exists to prevent, so it is itself a violation.
//
// DHF-TEST: keel/requirement-123 (keel/ac-495)
func TestSBOMConvergenceRejectsNOTICEWithNoProductNameLine(t *testing.T) {
	dir, tracked := sbomFixture(t, map[string]string{
		"NOTICE": "third-party software attributions\n\nnothing this guard can key on.\n",
	})

	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	joined := strings.Join(violations, "\n")
	if !strings.Contains(joined, "sbom-notice-product-name") || !strings.Contains(joined, "no product-name line") {
		t.Fatalf("a NOTICE with no product-name line must not pass silently, got:\n%s", joined)
	}
}

// DHF-TEST: keel/requirement-123 (keel/ac-496)
func TestSBOMConvergenceRejectsGenerationInstantInTrackedArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		file    string
		body    string
		wantHit string
	}{
		{
			name:    "notice generation date",
			file:    "NOTICE",
			body:    noticeFixture("keel", "2026-08-15"),
			wantHit: "2026-08-15",
		},
		{
			name:    "raw scanner descriptor timestamp",
			file:    "docs/auto-generated/sbom/raw/cve-filesystem.json",
			body:    "{\n  \"descriptor\": {\n    \"name\": \"grype\",\n    \"timestamp\": \"2026-08-15T21:53:58.160395529+02:00\"\n  }\n}\n",
			wantHit: "2026-08-15T21:53:58",
		},
		{
			name:    "rendered table footer",
			file:    "docs/auto-generated/sbom/linked-components.md",
			body:    "# Linked components\n\nGenerated 2026-08-15 by the sbom skill.\n",
			wantHit: "2026-08-15",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{tc.file: tc.body}
			if tc.file != "NOTICE" {
				files["NOTICE"] = noticeFixture("keel", "")
			}
			dir, tracked := sbomFixture(t, files)

			violations, err := scanSBOMConvergence(dir, tracked)
			if err != nil {
				t.Fatalf("scan returned error: %v", err)
			}
			joined := strings.Join(violations, "\n")
			for _, want := range []string{"sbom-no-generation-instant", tc.file, tc.wantHit, "keel/ac-496"} {
				if !strings.Contains(joined, want) {
					t.Errorf("violation text missing %q:\n%s", want, joined)
				}
			}
		})
	}
}

// The check accumulates: one run reports every offending artifact, so an
// operator fixing a regeneration gets the whole list instead of one file per
// gate run.
//
// DHF-TEST: keel/requirement-123 (keel/ac-495, keel/ac-496)
func TestSBOMConvergenceAccumulatesAcrossArtifacts(t *testing.T) {
	dir, tracked := sbomFixture(t, map[string]string{
		"NOTICE": noticeFixture("cr-201", "2026-08-16"),
		"docs/auto-generated/sbom/raw/cve-filesystem.json": "{\"descriptor\":{\"timestamp\":\"2026-08-16T09:00:00Z\"}}\n",
		"docs/auto-generated/sbom/licenses/SPDX-MIT.txt":   "MIT License\n\nCopyright (c) 2009 The Go Authors.\n",
	})

	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	joined := strings.Join(violations, "\n")
	for _, want := range []string{"NOTICE", "cve-filesystem.json"} {
		if !strings.Contains(joined, want) {
			t.Errorf("accumulated report missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "SPDX-MIT.txt") {
		t.Errorf("a bare copyright year is not a generation instant:\n%s", joined)
	}
}

// Files outside the SBOM generation pass are none of this policy's business: it
// asserts a property of one artifact set, not a repo-wide date ban.
//
// DHF-TEST: keel/requirement-123 (keel/ac-496)
func TestSBOMConvergenceIgnoresArtifactsOutsideTheGenerationPass(t *testing.T) {
	dir, tracked := sbomFixture(t, map[string]string{
		"NOTICE":                 noticeFixture("keel", ""),
		"docs/release.md":        "Released 2026-08-15.\n",
		"CHANGELOG.md":           "## 2026-08-15\n",
		"docs/auto-generated/ok": "no instant here\n",
	})

	violations, err := scanSBOMConvergence(dir, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("dates outside the SBOM artifact set must not red the gate, got:\n%s", strings.Join(violations, "\n"))
	}
}

// The gate is the delivery surface both criteria are stated over: "when the keel
// gate runs". This drives `keel-dev ci` end to end against a fixture whose
// committed NOTICE carries both defects, and asserts the run reds and names
// them.
//
// DHF-TEST: keel/requirement-123 (keel/ac-495, keel/ac-496)
func TestCIFailsOnSBOMConvergenceViolations(t *testing.T) {
	stubTools(t, false, false)
	root := moduleFixture(t)
	t.Chdir(root)

	writeFile(t, root, "NOTICE", noticeFixture("cr-201", "2026-08-16"))
	writeFile(t, root, ".stub-git-ls-files", strings.Join([]string{
		"go.mod",
		"VERSION",
		"p.go",
		"NOTICE",
		"vsix/package.json",
		"vsix/SUPPORTED_VSCODE.md",
	}, "\n")+"\n")

	stdout, stderr := captureProcessStreams(t, func() {
		if code := run([]string{"--no-header", "ci"}); code == 0 {
			t.Fatal("ci with a divergent NOTICE exit = 0, want non-zero")
		}
	})
	out := stdout + stderr
	for _, want := range []string{"sbom-notice-product-name", "cr-201", "sbom-no-generation-instant", "2026-08-16"} {
		if !strings.Contains(out, want) {
			t.Errorf("ci output missing %q:\nstdout=%s\nstderr=%s", want, stdout, stderr)
		}
	}
}

// The committed tree is the artifact this unit exists to keep convergent, so
// the guard is also run against it: the real NOTICE and the real tracked SBOM
// set must satisfy both criteria, not only the fixtures.
//
// DHF-TEST: keel/requirement-123 (keel/ac-495, keel/ac-496)
func TestSBOMConvergenceHoldsForTheCommittedTree(t *testing.T) {
	requireTool(t, "git")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := findModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	ls := exec.Command("git", "ls-files")
	ls.Dir = root
	out, err := ls.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var tracked []string
	for _, file := range strings.Split(string(out), "\n") {
		if file = strings.TrimSpace(file); file != "" {
			tracked = append(tracked, file)
		}
	}

	violations, err := scanSBOMConvergence(root, tracked)
	if err != nil {
		t.Fatalf("scan returned error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("committed SBOM artifact set is divergent:\n%s", strings.Join(violations, "\n"))
	}
}
