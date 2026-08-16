package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// treeWalkFixture writes a go.mod plus one package directory holding a single
// test file, and returns the tree root. Cases differ only in that source, so
// the policy is exercised through runLint's public shape and nothing else.
func treeWalkFixture(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	pkg := filepath.Join(dir, "sample")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, pkg, name, src)
	return dir
}

// TestLintRejectsTestOwnedWalkEscapingItsPackage proves the ac-501 policy fails
// the gate on the shape keel/change_request-198 shipped: a test that selects
// files by walking upward out of its own package, which is how gitignored
// worktrees/ and scratchpad/ content entered a gate-reached evaluated set.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintRejectsTestOwnedWalkEscapingItsPackage(t *testing.T) {
	dir := treeWalkFixture(t, "scan_test.go", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	moduleRoot := ".."
	_ = filepath.WalkDir(moduleRoot, func(path string, entry os.DirEntry, err error) error {
		return nil
	})
}
`)
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil {
		t.Fatal("a test-owned walk rooted outside its package should fail lint, got nil")
	}
	got := err.Error()
	for _, want := range []string{"no-test-owned-tree-walk", filepath.Join("sample", "scan_test.go") + ":11", "filepath.WalkDir", `".."`} {
		if !strings.Contains(got, want) {
			t.Fatalf("tree-walk violation missing %q:\n%s", want, got)
		}
	}
}

// TestLintRejectsTheChangeRequest198Walker is the policy's positive control. Its
// fixture is the scan body of log/discard_adoption_test.go as it stood at
// 37f78c2 — the walker keel/change_request-198 shipped and keel/issue-171
// measured redding the gate on main. A policy that does not fail on these exact
// bytes has not closed the defect it was written for, whatever its fixtures say.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintRejectsTheChangeRequest198Walker(t *testing.T) {
	dir := treeWalkFixture(t, "discard_adoption_test.go", `package log_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module tree: %v", err)
	}
	_ = canonical
	_ = matched
}
`)
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil || !strings.Contains(err.Error(), "no-test-owned-tree-walk") {
		t.Fatalf("the change_request-198 walker must trip the policy, got %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join("sample", "discard_adoption_test.go")+":15") {
		t.Fatalf("the violation should name the walk call site:\n%v", err)
	}
}

// TestLintRejectsEscapingWalkRootShapes proves the resolver sees past the
// spellings a hand-rolled structural assertion actually uses: a bare literal, a
// filepath.Join of literals, a climb of more than one level, and an absolute
// root. It also covers the other selection calls, not only WalkDir.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintRejectsEscapingWalkRootShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"bare literal root", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_ = filepath.WalkDir("..", func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"filepath.Join of literals", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_ = filepath.WalkDir(filepath.Join("..", "log"), func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"climbs two levels", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"absolute root", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_ = filepath.WalkDir("/projects/keel", func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"glob escaping the package", `package sample

import (
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_, _ = filepath.Glob(filepath.Join("..", "*.go"))
}
`},
		{"ReadDir escaping the package", `package sample

import (
	"os"
	"testing"
)

func TestScan(t *testing.T) {
	_, _ = os.ReadDir("..")
}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := treeWalkFixture(t, "scan_test.go", tc.src)
			err := runLint(dir, lintFixtureFiles(t, dir))
			if err == nil || !strings.Contains(err.Error(), "no-test-owned-tree-walk") {
				t.Fatalf("%s should trip the tree-walk policy, got %v", tc.name, err)
			}
		})
	}
}

// TestLintAllowsCalibratedTreeWalks proves the policy stays off the walkers
// keel/issue-171 measured as unaffected. The shapes are reproduced from the
// tracked tree: vscode/traceability_test.go walks its own package dir,
// log/api_surface_test.go globs a runtime.Caller-derived package dir, and
// cmd/keel-dev's lintFixtureFiles walks a fixture root the test created. A
// tightening that reds any of these is too loose, not too strict.
//
// The live complement to this test is the gate itself: runLint's real input is
// every tracked _test.go in the module, so a green lint stage on the primary
// checkout is the calibration set passing in situ.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintAllowsCalibratedTreeWalks(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"walks its own package dir", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_ = filepath.WalkDir(".", func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"reads its own package dir", `package sample

import (
	"os"
	"testing"
)

func TestScan(t *testing.T) {
	_, _ = os.ReadDir(".")
}
`},
		{"globs a caller-derived package dir", `package sample

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestScan(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	_, _ = filepath.Glob(filepath.Join(filepath.Dir(thisFile), "*.go"))
}
`},
		{"walks a fixture root it created", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error { return nil })
}
`},
		{"globs a descendant of its own package", `package sample

import (
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	_, _ = filepath.Glob(filepath.Join("testdata", "*.json"))
}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := treeWalkFixture(t, "scan_test.go", tc.src)
			if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-test-owned-tree-walk") {
				t.Fatalf("%s should not trip the tree-walk policy, got:\n%v", tc.name, err)
			}
		})
	}
}

// TestLintTreeWalkPolicyIgnoresNonTestFiles proves the policy's subject is the
// test-owned walker. Production code selecting files is bound by the gate's own
// selector contract, not by this policy, and a walk in a non-test file is left
// to it.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintTreeWalkPolicyIgnoresNonTestFiles(t *testing.T) {
	dir := treeWalkFixture(t, "scan.go", `package sample

import (
	"os"
	"path/filepath"
)

// Scan walks upward from the package directory.
func Scan() error {
	return filepath.WalkDir("..", func(p string, e os.DirEntry, err error) error { return nil })
}
`)
	if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-test-owned-tree-walk") {
		t.Fatalf("a non-test walker is out of this policy's scope, got:\n%v", err)
	}
}

// TestLintTreeWalkDropsAmbiguousRoots proves an identifier bound to two
// different literals is not reported: the resolver would have to guess which
// binding reaches the call, and a policy that guesses reds honest tests.
//
// DHF-TEST: keel/requirement-85 (keel/ac-501)
func TestLintTreeWalkDropsAmbiguousRoots(t *testing.T) {
	dir := treeWalkFixture(t, "scan_test.go", `package sample

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan(t *testing.T) {
	root := "."
	if testing.Short() {
		root = ".."
	}
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error { return nil })
}
`)
	if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-test-owned-tree-walk") {
		t.Fatalf("an ambiguously-bound root should not be reported, got:\n%v", err)
	}
}
