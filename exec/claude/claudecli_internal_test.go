package claude

// Internal tests for the stream-writer helpers, closing coverage gaps under
// keel/change_request-4 (keel/ac-37).

import (
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

func TestTruncateBytes(t *testing.T) {
	if got := truncateBytes([]byte("short"), 10); got != "short" {
		t.Errorf("no-truncation case: %q", got)
	}
	if got := truncateBytes([]byte("abcdefgh"), 3); got != "abc…" {
		t.Errorf("truncation case: %q", got)
	}
}

func TestClaudeStreamWriterErrFlushesTrailingLine(t *testing.T) {
	logger, cap := logging.NewForTesting("t")
	w := &claudeStreamWriter{logger: logger}

	// Unterminated trailing event must be consumed by Err().
	if _, err := w.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"tail"}]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, rec := range cap.AllJSON() {
		if d, _ := rec["detail"].(string); d == "tail" {
			saw = true
		}
	}
	if !saw {
		t.Error("trailing unterminated event not consumed by Err")
	}

	// A sticky write error is returned by both Write and Err.
	w2 := &claudeStreamWriter{err: errSticky}
	if _, err := w2.Write([]byte("x")); err != errSticky {
		t.Error("sticky error not returned from Write")
	}
	if err := w2.Err(); err != errSticky {
		t.Error("sticky error not returned from Err")
	}
}

var errSticky = &stickyErr{}

type stickyErr struct{}

func (*stickyErr) Error() string { return "sticky" }

func TestClaudeStreamWriterLineTooLong(t *testing.T) {
	w := &claudeStreamWriter{logger: logging.Discard()}
	huge := make([]byte, 5*1024*1024) // > maxLine, no newline
	for i := range huge {
		huge[i] = 'a'
	}
	if _, err := w.Write(huge); err == nil || !strings.Contains(err.Error(), "line too long") {
		t.Errorf("want line-too-long error, got %v", err)
	}
}

func TestConsumeLineEdges(t *testing.T) {
	logger, cap := logging.NewForTesting("t")
	w := &claudeStreamWriter{logger: logger}

	w.consumeLine([]byte("   "))          // blank: ignored
	w.consumeLine([]byte("not-json"))     // unparseable: ignored
	w.consumeLine([]byte(`{"type":"x"}`)) // no detail: no record

	w.consumeLine([]byte(`{"type":"result","result":"done"}`))
	if string(w.ResultRaw()) != `{"type":"result","result":"done"}` {
		t.Errorf("result line not captured: %q", w.ResultRaw())
	}

	// nil logger path falls back to slog.Default without panicking.
	w2 := &claudeStreamWriter{}
	w2.consumeLine([]byte(`{"type":"assistant","text":"hi"}`))

	if n := len(cap.AllJSON()); n != 0 {
		t.Errorf("edge lines should produce no progress records, got %d", n)
	}
}

func TestClaudeProgressDetailSources(t *testing.T) {
	cases := []struct {
		name string
		ev   map[string]any
		want string
	}{
		{"top-level text", map[string]any{"text": "a"}, "a"},
		{"top-level summary", map[string]any{"summary": "b"}, "b"},
		{"top-level result", map[string]any{"result": "c"}, "c"},
		{"message text", map[string]any{"message": map[string]any{"text": "d"}}, "d"},
		{"message content", map[string]any{"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use"},
			map[string]any{"type": "text", "text": "e"},
		}}}, "e"},
		{"content non-map items", map[string]any{"content": []any{"raw", map[string]any{"type": "text", "text": ""}}}, ""},
		{"nothing", map[string]any{"type": "system"}, ""},
		{"long detail trimmed", map[string]any{"text": strings.Repeat("x", 200)}, strings.Repeat("x", 160) + "..."},
	}
	for _, c := range cases {
		if got := claudeProgressDetail(c.ev); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
