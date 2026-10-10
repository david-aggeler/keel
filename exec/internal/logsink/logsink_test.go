package logsink

import (
	"io"
	"log/slog"
	"testing"
)

type valueLogger struct{}

// DHF-TEST: keel/requirement-122 (keel/ac-795)
func TestAbsentTreatsNilAndTypedNilAsAbsent(t *testing.T) {
	var nilSlog *slog.Logger
	var nilIface io.Writer
	var nilMap map[string]int
	for _, tc := range []struct {
		name   string
		logger any
		want   bool
	}{
		{"nil interface", nil, true},
		{"typed-nil pointer", nilSlog, true},
		{"nil interface value", nilIface, true},
		{"nil map", nilMap, true},
		{"live pointer", slog.New(slog.DiscardHandler), false},
		{"non-pointer value", valueLogger{}, false},
	} {
		if got := Absent(tc.logger); got != tc.want {
			t.Errorf("Absent(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
