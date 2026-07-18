package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-11, keel/requirement-40
func TestHandleVSIXGateRejectsArgsAndReportsMissingToolchain(t *testing.T) {
	if err := handleVSIXGate(context.Background(), []string{"extra"}); err == nil || !strings.Contains(err.Error(), "takes no arguments") {
		t.Fatalf("handleVSIXGate extra args err = %v, want usage error", err)
	}

	t.Setenv("PATH", t.TempDir())
	ctx := withRunStateProtocol(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, t.TempDir(), io.Discard)
	err := handleVSIXGate(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), `required tool "node" not found`) {
		t.Fatalf("handleVSIXGate missing toolchain err = %v, want node missing", err)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-79
func TestEvaluateVSIXCoverageSummaryValidatesFixtureAndTotals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "coverage-summary.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "excluded test fixture",
			body: `{"total":{"statements":{"pct":99}},"src/test/fixture.ts":{"statements":{"pct":100}}}`,
			want: "excluded test fixture",
		},
		{
			name: "missing total",
			body: `{"total":{"statements":{}}}`,
			want: "has no total statement coverage",
		},
		{
			name: "below floor",
			body: `{"total":{"statements":{"pct":10}}}`,
			want: "below the",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			err := evaluateVSIXCoverageSummary(logger, path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("evaluateVSIXCoverageSummary err = %v, want containing %q", err, tc.want)
			}
		})
	}

	if err := os.WriteFile(path, []byte(`{"total":{"statements":{"pct":80}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := evaluateVSIXCoverageSummary(logger, path); err != nil {
		t.Fatalf("evaluateVSIXCoverageSummary valid summary: %v", err)
	}

	if err := evaluateVSIXCoverageSummary(logger, filepath.Join(root, "missing.json")); err == nil || !strings.Contains(err.Error(), "coverage summary") {
		t.Fatalf("missing summary err = %v, want read failure", err)
	}
	if err := os.WriteFile(path, []byte("{bad json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := evaluateVSIXCoverageSummary(logger, path); err == nil || !strings.Contains(err.Error(), "parse vsix coverage summary") {
		t.Fatalf("malformed summary err = %v, want parse failure", err)
	}
}
