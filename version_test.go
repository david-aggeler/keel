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

// DHF-TEST: keel/requirement-110 (keel/ac-688)
func TestVersionRendersStampedBuildNumberAsFourthComponent(t *testing.T) {
	old := BuildNumber
	t.Cleanup(func() { BuildNumber = old })

	BuildNumber = "480"
	if got, want := Version(), Semver()+".480"; got != want {
		t.Fatalf("Version() with BuildNumber stamped = %q, want %q", got, want)
	}
	if strings.Contains(Version(), "+") {
		t.Fatalf("Version() = %q, want no + metadata suffix", Version())
	}
}

// DHF-TEST: keel/requirement-110 (keel/ac-689, keel/ac-391)
func TestVersionUnstampedFallsBackToBareSemver(t *testing.T) {
	old := BuildNumber
	t.Cleanup(func() { BuildNumber = old })

	BuildNumber = ""
	if got, want := Version(), Semver(); got != want {
		t.Fatalf("Version() unstamped = %q, want bare %q", got, want)
	}
	if got := Version(); got == "dev" || got == "demo" {
		t.Fatalf("Version() = %q, want no placeholder", got)
	}
}

// DHF-TEST: keel/requirement-110 (keel/ac-689)
func TestVersionIgnoresNonNumericBuildNumberStamp(t *testing.T) {
	old := BuildNumber
	t.Cleanup(func() { BuildNumber = old })

	for _, bad := range []string{"abc123", "1.2", " 480", "480 ", "-1", "+4"} {
		BuildNumber = bad
		if got, want := Version(), Semver(); got != want {
			t.Fatalf("Version() with BuildNumber=%q = %q, want bare %q", bad, got, want)
		}
	}
}
