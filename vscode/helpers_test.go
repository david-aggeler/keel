package vscode

import (
	"testing"
	"time"
)

// DHF-TEST: keel/requirement-23
func TestParseGoItemID(t *testing.T) {
	cases := []struct {
		id   string
		want GoSelection
		ok   bool
	}{
		{"go::root", GoSelection{Kind: "root", Pkg: "..."}, true},
		{"go::pkg::internal/sample", GoSelection{Kind: "package", Pkg: "internal/sample"}, true},
		{"go::file::internal/sample/thing_test.go", GoSelection{Kind: "file", Pkg: "internal/sample", File: "internal/sample/thing_test.go"}, true},
		{"go::test::internal/sample::TestFoo", GoSelection{Kind: "test", Pkg: "internal/sample", TestName: "TestFoo"}, true},
		{"go::file::", GoSelection{}, false},
		{"go::test::::TestFoo", GoSelection{}, false},
		{"nonsense", GoSelection{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseGoItemID(tc.id)
		if ok != tc.ok {
			t.Fatalf("ParseGoItemID(%q) ok = %v, want %v", tc.id, ok, tc.ok)
		}
		if got.Kind != tc.want.Kind || got.Pkg != tc.want.Pkg || got.File != tc.want.File || got.TestName != tc.want.TestName {
			t.Fatalf("ParseGoItemID(%q) = %#v, want %#v", tc.id, got, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-91
func TestParseVSIXItemID(t *testing.T) {
	cases := []struct {
		id   string
		want VSIXSelection
		ok   bool
	}{
		{"vsix::root", VSIXSelection{Kind: "root"}, true},
		{"vsix::file::src/test/suite/extension.test.ts", VSIXSelection{Kind: "file", File: "src/test/suite/extension.test.ts"}, true},
		{"vsix::file::", VSIXSelection{}, false},
		{"vsix::file::/src/test/suite/extension.test.ts", VSIXSelection{}, false},
		{"vsix::file::../src/test/suite/extension.test.ts", VSIXSelection{}, false},
		{"vsix::file::src/test/suite/../extension.test.ts", VSIXSelection{}, false},
		{"vsix::file::src/test/suite/extension.ts", VSIXSelection{}, false},
		{"vsix::test::src/test/suite/extension.test.ts::runs", VSIXSelection{}, false},
		{"nonsense", VSIXSelection{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseVSIXItemID(tc.id)
		if ok != tc.ok {
			t.Fatalf("ParseVSIXItemID(%q) ok = %v, want %v", tc.id, ok, tc.ok)
		}
		if got.Kind != tc.want.Kind || got.File != tc.want.File {
			t.Fatalf("ParseVSIXItemID(%q) = %#v, want %#v", tc.id, got, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-23
func TestGoEventPackageRel(t *testing.T) {
	const mod = "github.com/david-aggeler/keel"
	cases := []struct{ pkg, want string }{
		{"", ""},
		{mod, "."},
		{mod + "/internal/sample", "internal/sample"},
		{"other.com/pkg", ""},
	}
	for _, tc := range cases {
		if got := GoEventPackageRel(tc.pkg, mod); got != tc.want {
			t.Fatalf("GoEventPackageRel(%q) = %q, want %q", tc.pkg, got, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-23
func TestGoPackageArg(t *testing.T) {
	cases := []struct{ pkg, want string }{
		{".", "."},
		{"...", "./..."},
		{"internal/sample", "./internal/sample"},
	}
	for _, tc := range cases {
		if got := GoPackageArg(tc.pkg); got != tc.want {
			t.Fatalf("GoPackageArg(%q) = %q, want %q", tc.pkg, got, tc.want)
		}
	}
}

// DHF-TEST: keel/requirement-23
func TestGoTestNamePattern(t *testing.T) {
	if got := GoTestNamePattern([]string{"TestA", "TestB.Sub"}); got != `^(TestA|TestB\.Sub)$` {
		t.Fatalf("GoTestNamePattern = %q", got)
	}
}

// DHF-TEST: keel/requirement-23
func TestMergeGoAggregateResult(t *testing.T) {
	base := MergeGoAggregateResult(RunEvent{}, RunEvent{Event: "passed", DurationMS: 10})
	if base.Event != "passed" || base.DurationMS != 10 {
		t.Fatalf("first merge = %#v", base)
	}
	withSkip := MergeGoAggregateResult(base, RunEvent{Event: "skipped", DurationMS: 5, Message: "skip"})
	if withSkip.Event != "skipped" || withSkip.DurationMS != 15 {
		t.Fatalf("skip merge = %#v", withSkip)
	}
	withFail := MergeGoAggregateResult(withSkip, RunEvent{Event: "failed", DurationMS: 2, Message: "boom"})
	if withFail.Event != "failed" || withFail.Message != "boom" || withFail.DurationMS != 17 {
		t.Fatalf("fail merge = %#v", withFail)
	}
}

// DHF-TEST: keel/requirement-23
func TestGoElapsedMillis(t *testing.T) {
	if got := GoElapsedMillis(1.5, time.Now()); got != 1500 {
		t.Fatalf("GoElapsedMillis(1.5) = %d, want 1500", got)
	}
	start := time.Now().Add(-20 * time.Millisecond)
	if got := GoElapsedMillis(0, start); got < 0 {
		t.Fatalf("GoElapsedMillis(0) = %d, want >= 0", got)
	}
}

// DHF-TEST: keel/requirement-23
func TestParseVitestItemID(t *testing.T) {
	file, ok := ParseVitestItemID("vitest::test::web/src/lib/thing.test.ts::fails")
	if !ok || file != "web/src/lib/thing.test.ts" {
		t.Fatalf("parse vitest id = %q, %v", file, ok)
	}
	file, ok = ParseVitestItemID("vitest::root")
	if !ok || file != "" {
		t.Fatalf("parse vitest root = %q, %v", file, ok)
	}
	if _, ok := ParseVitestItemID("vitest::file::"); ok {
		t.Fatalf("empty vitest file id should not parse")
	}
	if _, ok := ParseVitestItemID("nonsense"); ok {
		t.Fatalf("nonsense vitest id should not parse")
	}
}

// DHF-TEST: keel/requirement-23
func TestParsePlaywrightItemID(t *testing.T) {
	selection, ok := ParsePlaywrightItemID("playwright::test::e2e-mock::web/tests/e2e/login.spec.ts::logs-in")
	if !ok || selection.Project != "e2e-mock" || selection.File != "web/tests/e2e/login.spec.ts" {
		t.Fatalf("selection = %#v, %v", selection, ok)
	}
	if _, ok := ParsePlaywrightItemID("playwright::project::"); ok {
		t.Fatalf("empty project id should not parse")
	}
	if _, ok := ParsePlaywrightItemID("nonsense"); ok {
		t.Fatalf("nonsense playwright id should not parse")
	}
}

// DHF-TEST: keel/requirement-23
func TestParsePlaywrightListDurationMS(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
		ok   bool
	}{
		{"340ms", 340, true},
		{"1.2s", 1200, true},
		{"2m", 120000, true},
		{"", 0, false},
		{"-1s", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParsePlaywrightListDurationMS(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("ParsePlaywrightListDurationMS(%q) = %d, %v; want %d, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
