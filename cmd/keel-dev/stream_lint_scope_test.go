package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStreamLintsRejectPlantedViolationsInEveryBinary plants an os.Stdout
// reference outside the allowlist and a raw fmt print in each first-party binary outside
// cmd/keel-dev, and requires both stream policies to report each plant. The
// plant is the positive control: a scan whose scope stopped at cmd/keel-dev
// would pass the same tree silently. cmd/keel-future stands for a binary that
// does not exist yet — the scope is every directory under cmd/, not a list.
//
// DHF-TEST: keel/requirement-164 (keel/ac-698)
func TestStreamLintsRejectPlantedViolationsInEveryBinary(t *testing.T) {
	for _, binary := range []string{"keel-demo", "keel-demo-dev", "keel-future"} {
		t.Run(binary, func(t *testing.T) {
			dir := t.TempDir()
			pkg := filepath.Join(dir, "cmd", binary)
			if err := os.MkdirAll(pkg, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")
			writeFile(t, pkg, "leak.go",
				"package main\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n)\n\nfunc leakStream() io.Writer { return os.Stdout }\n\nfunc leakPrint() { fmt.Println(\"payload\") }\n")

			err := runLint(dir, lintFixtureFiles(t, dir))
			if err == nil {
				t.Fatalf("lint passed a tree with planted stream violations in cmd/%s", binary)
			}
			leak := filepath.Join("cmd", binary, "leak.go")
			for _, want := range []string{
				"no-raw-stdout-stream: " + leak + ":9 references os.Stdout in leakStream",
				"no-raw-fmt-output: " + leak + ":11 calls fmt.Println",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("lint report lacks %q:\n%v", want, err)
				}
			}
		})
	}
}

// TestStreamLintAllowlistIsScopedToItsFile proves an allowlist entry covers one
// (file, function) pair and nothing else: keel-dev's run is admitted in
// cmd/keel-dev/main.go, and the same function name in another binary's main.go
// is still a violation. Keying the allowlist on the base name alone would let
// every binary's main.go inherit keel-dev's exceptions.
//
// DHF-TEST: keel/requirement-164 (keel/ac-698)
func TestStreamLintAllowlistIsScopedToItsFile(t *testing.T) {
	dir := t.TempDir()
	for _, binary := range []string{"keel-dev", "keel-future"} {
		pkg := filepath.Join(dir, "cmd", binary)
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, pkg, "main.go", "package main\n\nimport \"os\"\n\nfunc run() { _ = os.Stderr }\n")
	}
	writeFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.25\n")

	err := runLint(dir, lintFixtureFiles(t, dir))
	if err == nil {
		t.Fatal("lint admitted run in cmd/keel-future/main.go on keel-dev's allowlist entry")
	}
	future := filepath.Join("cmd", "keel-future", "main.go")
	if !strings.Contains(err.Error(), "no-raw-stdout-stream: "+future+":5 references os.Stderr in run") {
		t.Fatalf("lint report lacks the keel-future violation:\n%v", err)
	}
	if strings.Contains(err.Error(), filepath.Join("cmd", "keel-dev", "main.go")) {
		t.Fatalf("lint reported keel-dev's allowlisted run:\n%v", err)
	}
}
