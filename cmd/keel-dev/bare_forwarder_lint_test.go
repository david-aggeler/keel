package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bareForwarderFixture writes a go.mod plus one package directory holding src,
// and returns the tree root. Every case below differs only in that source, so
// the policy is exercised through runLint's public shape and nothing else.
func bareForwarderFixture(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
	pkg := filepath.Join(dir, "sample")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, pkg, "sample.go", src)
	return dir
}

// TestLintRejectsBareForwarder proves the ac-497 policy fails the gate on an
// unexported function whose entire body forwards to a same-package exported
// function with no non-test caller, and names it with its file:line.
//
// DHF-TEST: keel/requirement-33
func TestLintRejectsBareForwarder(t *testing.T) {
	dir := bareForwarderFixture(t, `package sample

// Resolve is the exported resolver.
func Resolve(s string) string { return s }

func resolve(s string) string {
	return Resolve(s)
}
`)
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil {
		t.Fatal("bare forwarder should fail lint, got nil")
	}
	got := err.Error()
	for _, want := range []string{"no-bare-forwarder", filepath.Join("sample", "sample.go") + ":6", "resolve", "Resolve"} {
		if !strings.Contains(got, want) {
			t.Fatalf("bare forwarder violation missing %q:\n%s", want, got)
		}
	}
}

// TestLintAllowsNonForwarders proves the policy stays off the constructs the
// unit deliberately leaves alone: a wrapper that adapts its argument, one that
// adds a statement, one with a production caller, a method, a forwarder to
// another package, and a call whose arguments are reordered.
//
// DHF-TEST: keel/requirement-33
func TestLintAllowsNonForwarders(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"adapts the argument", `package sample

// Resolve is the exported resolver.
func Resolve(s string) string { return s }

func resolve(s string) string {
	return Resolve(s + "!")
}
`},
		{"adds a statement", `package sample

// Resolve is the exported resolver.
func Resolve(s string) string { return s }

func resolve(s string) string {
	_ = s
	return Resolve(s)
}
`},
		{"has a production caller", `package sample

// Resolve is the exported resolver.
func Resolve(s string) string { return s }

func resolve(s string) string {
	return Resolve(s)
}

// Use exercises the wrapper from production code.
func Use() string { return resolve("x") }
`},
		{"is a method", `package sample

// Resolve is the exported resolver.
func Resolve(s string) string { return s }

type box struct{}

func (b box) resolve(s string) string {
	return Resolve(s)
}
`},
		{"forwards to another package", `package sample

import "strings"

func resolve(s string) string {
	return strings.TrimSpace(s)
}
`},
		{"reorders the arguments", `package sample

// Join is the exported joiner.
func Join(a, b string) string { return a + b }

func join(a, b string) string {
	return Join(b, a)
}
`},
		{"forwards to an unexported function", `package sample

func target(s string) string { return s }

func resolve(s string) string {
	return target(s)
}
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := bareForwarderFixture(t, tc.src)
			if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-bare-forwarder") {
				t.Fatalf("%s should not trip the bare-forwarder policy, got:\n%v", tc.name, err)
			}
		})
	}
}

// TestLintBareForwarderSeesVariadicAndMultiFilePackages proves the policy
// reads the whole package (the caller may live in a sibling file) and matches a
// variadic forward.
//
// DHF-TEST: keel/requirement-33
func TestLintBareForwarderSeesVariadicAndMultiFilePackages(t *testing.T) {
	dir := bareForwarderFixture(t, `package sample

// Emit is the exported emitter.
func Emit(parts ...string) string { return parts[0] }

func emit(parts ...string) string {
	return Emit(parts...)
}
`)
	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil || !strings.Contains(err.Error(), "no-bare-forwarder") {
		t.Fatalf("variadic bare forwarder should fail lint, got %v", err)
	}

	// A caller in a sibling non-test file of the same package clears it.
	writeFile(t, filepath.Join(dir, "sample"), "use.go", `package sample

// Use exercises the wrapper from a sibling production file.
func Use() string { return emit("x") }
`)
	if err := runLint(dir, lintFixtureFiles(t, dir)); err != nil && strings.Contains(err.Error(), "no-bare-forwarder") {
		t.Fatalf("a sibling-file production caller should clear the policy, got:\n%v", err)
	}
}
