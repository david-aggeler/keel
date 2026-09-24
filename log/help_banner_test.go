package log_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-57
func TestWriteHeaderBannerRendersPlainEqualsRuleTitleRule(t *testing.T) {
	var out bytes.Buffer
	if err := logging.WriteHeaderBanner(&out, "tool v1.2.3"); err != nil {
		t.Fatalf("WriteHeaderBanner: %v", err)
	}
	rule := strings.Repeat("=", logging.BannerWidth)
	if want := rule + "\ntool v1.2.3\n" + rule + "\n"; out.String() != want {
		t.Fatalf("WriteHeaderBanner = %q, want %q", out.String(), want)
	}
}

// DHF-TEST: keel/requirement-57
func TestWriteSectionBannerRendersPlainDashRuleThenName(t *testing.T) {
	var out bytes.Buffer
	if err := logging.WriteSectionBanner(&out, "tool grp leaf"); err != nil {
		t.Fatalf("WriteSectionBanner: %v", err)
	}
	if want := strings.Repeat("-", logging.BannerWidth) + "\ntool grp leaf\n"; out.String() != want {
		t.Fatalf("WriteSectionBanner = %q, want %q", out.String(), want)
	}
}

// DHF-TEST: keel/requirement-57
func TestBannerWidthIsSharedWithConsoleBanners(t *testing.T) {
	if logging.BannerWidth != 70 {
		t.Fatalf("BannerWidth = %d, want 70", logging.BannerWidth)
	}
	var out bytes.Buffer
	logger := mustNewLogger(t, logging.Config{
		Console:          logging.ConsolePlain,
		Service:          "keel-dev",
		ConsoleVerbosity: slog.LevelDebug,
		Writer:           &out,
	})
	logger.Header("tool", "v1.2.3")
	logger.Section("tool grp")
	got := out.String()
	for _, rule := range []string{strings.Repeat("=", logging.BannerWidth), strings.Repeat("-", logging.BannerWidth)} {
		if !strings.Contains(got, rule+"\n") {
			t.Fatalf("console banner output missing %d-wide rule %q:\n%s", logging.BannerWidth, rule[:1], got)
		}
	}
}
