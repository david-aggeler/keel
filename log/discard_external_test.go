package log_test

import (
	"context"
	"log/slog"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-122
func TestDiscardIsExportedAndDropsEveryRecord(t *testing.T) {
	logger := logging.Discard()
	if logger == nil {
		t.Fatal("Discard() returned nil, want a usable *slog.Logger")
	}

	ctx := context.Background()
	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	for _, level := range levels {
		if logger.Handler().Enabled(ctx, level) {
			t.Errorf("Discard() logger is enabled at %s; no record may reach a writer", level)
		}
	}

	logger.Debug("debug record", "level", "debug")
	logger.Info("info record", "level", "info")
	logger.Warn("warn record", "level", "warn")
	logger.Error("error record", "level", "error")
}
