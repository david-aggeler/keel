package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-80
func TestBuiltBinaryGitignorePatternsDoNotShadowCommandSourceDirs(t *testing.T) {
	requireTool(t, "git")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	moduleRoot, err := findModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	gitignore, err := os.ReadFile(filepath.Join(moduleRoot, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(string(gitignore), "\n")
	for _, want := range []string{"/keel-dev", "/keel-demo"} {
		if !containsGitignoreLine(lines, want) {
			t.Fatalf(".gitignore missing root-anchored built-binary pattern %q", want)
		}
	}
	for _, bare := range []string{"keel-dev", "keel-demo"} {
		if containsGitignoreLine(lines, bare) {
			t.Fatalf(".gitignore still contains bare built-binary pattern %q", bare)
		}
	}

	repo := t.TempDir()
	mustRun(t, repo, "git", "init")
	writeFile(t, repo, ".gitignore", string(gitignore))
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "keel-dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, filepath.Join("cmd", "keel-dev", "doc.go"), "package main\n")

	add := exec.Command("git", "add", filepath.Join("cmd", "keel-dev", "doc.go"))
	add.Dir = repo
	out, err := add.CombinedOutput()
	if err != nil {
		t.Fatalf("git add cmd/keel-dev/doc.go failed without -f: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "ignored by one of your .gitignore files") {
		t.Fatalf("git add emitted ignored-path warning:\n%s", out)
	}

	writeFile(t, repo, "keel-dev", "binary\n")
	writeFile(t, repo, "keel-demo", "binary\n")
	ignored := exec.Command("git", "check-ignore", "keel-dev", "keel-demo")
	ignored.Dir = repo
	if out, err := ignored.CombinedOutput(); err != nil {
		t.Fatalf("root built artifacts must remain ignored: %v\n%s", err, out)
	}
}

func containsGitignoreLine(lines []string, want string) bool {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == want {
			return true
		}
	}
	return false
}
