package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/vscode"
)

// stubTrackedGoFiles puts a git stub in bin whose `ls-files` reports exactly
// files, so a stubbed-go coverage test gets a deterministic package selection
// without a real repository. It uses only the shell builtin printf, so it works
// on a PATH that holds nothing but bin.
func stubTrackedGoFiles(t *testing.T, bin, callsFile string, files ...string) {
	t.Helper()
	stub(t, bin, callsFile, "git", `
case "$*" in
  "ls-files") printf '%s\n' `+shellQuoteAll(files)+` ;;
esac
exit 0`)
}

// ignoredPackageFixture builds a real git checkout with one tracked, fully
// covered package and two gitignored packages under scratchpad/: a probe with
// untested statements (the keel/issue-246 shape, which would pull the total
// below the floor) and a test-only package whose test fails (the keel/ac-666
// shape). A test stage that enumerates the working tree fails on either one; a
// stage scoped to tracked packages does not see them.
func ignoredPackageFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/fixture\n\ngo 1.25\n")
	writeFile(t, root, ".gitignore", "scratchpad/\n")
	for _, dir := range []string{"p", "scratchpad/probe", "scratchpad/testonly"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, root, "p/p.go", "package p\n\nfunc One() int { return 1 }\n")
	writeFile(t, root, "p/p_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"One\")\n\t}\n}\n")
	var probe strings.Builder
	probe.WriteString("package main\n\nfunc main() {\n\tx := 0\n")
	for i := 0; i < 50; i++ {
		probe.WriteString("\tx++\n")
	}
	probe.WriteString("\t_ = x\n}\n")
	writeFile(t, root, "scratchpad/probe/main.go", probe.String())
	writeFile(t, root, "scratchpad/testonly/probe_test.go", "package probe_test\n\nimport \"testing\"\n\nfunc TestGitignoredProbe(t *testing.T) { t.Fatal(\"gitignored package reached the test stage\") }\n")
	mustRun(t, root, "git", "init", "-q")
	mustRun(t, root, "git", "add", ".")
	return root
}

// DHF-TEST: keel/requirement-85 (keel/ac-666), keel/requirement-11 (keel/ac-37)
func TestTestStageIgnoresGitignoredPackages(t *testing.T) {
	root := ignoredPackageFixture(t)
	if err := runTestWithCoverage(context.Background(), logging.Discard(), root); err != nil {
		t.Fatalf("ci test stage must ignore gitignored packages, got: %v", err)
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-666), keel/requirement-39
func TestVSCodeCoverageLaneIgnoresGitignoredPackages(t *testing.T) {
	root := ignoredPackageFixture(t)
	var events []vscode.RunEvent
	err := runVSCodeTestCoverage(context.Background(), logging.Discard(), root, "run-scope", 1<<20, func(event vscode.RunEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("VS Code coverage lane must ignore gitignored packages, got: %v", err)
	}
	for _, event := range events {
		if strings.Contains(event.TestID, "scratchpad") {
			t.Fatalf("gitignored package reached the coverage lane: %+v", event)
		}
	}
}

// TestTrackedGoPackagesMatchesToolchainEnumerationOnTheRealTree guards the
// opposite failure: a selector that silently drops tracked packages shrinks
// the coverage denominator and raises the total. On the real checkout the
// tracked set must equal `go list ./...` minus gitignored directories.
//
// DHF-TEST: keel/requirement-85 (keel/ac-666), keel/requirement-11 (keel/ac-37)
func TestTrackedGoPackagesMatchesToolchainEnumerationOnTheRealTree(t *testing.T) {
	root := repoRootForTest(t)
	got, err := trackedGoPackages(context.Background(), logging.Discard(), root)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./...: %v", err)
	}
	var want []string
	for _, dir := range strings.Fields(string(out)) {
		ignored := exec.Command("git", "check-ignore", "-q", dir)
		ignored.Dir = root
		if ignored.Run() == nil {
			continue
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		if rel == "." {
			want = append(want, ".")
			continue
		}
		want = append(want, "./"+filepath.ToSlash(rel))
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked packages differ from go list ./... minus ignored dirs\n got: %v\nwant: %v", got, want)
	}
}
