package keel

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-110 (keel/ac-390)
func TestVersionFileContainsBareSemver(t *testing.T) {
	body, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	got := strings.TrimSpace(string(body))
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(got) {
		t.Fatalf("VERSION = %q, want bare MAJOR.MINOR.PATCH semver", got)
	}
}

// DHF-TEST: keel/requirement-110 (keel/ac-391)
func TestVersionReportsSemverWithOptionalBuildMetadata(t *testing.T) {
	old := BuildMetadata
	t.Cleanup(func() { BuildMetadata = old })

	BuildMetadata = ""
	if got := Version(); !strings.HasPrefix(got, Semver()) || got == "dev" || got == "demo" {
		t.Fatalf("Version() = %q, want VERSION semver prefix and no placeholder", got)
	}

	BuildMetadata = "abc123"
	if got, want := Version(), Semver()+"+abc123"; got != want {
		t.Fatalf("Version() with build metadata = %q, want %q", got, want)
	}
}
