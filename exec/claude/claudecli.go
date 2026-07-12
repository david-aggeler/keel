package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
)

type processLogger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
}

// Request describes one headless claude -p invocation.
type Request struct {
	// Prompt is the user prompt (may be a slash invocation like "/hello-world").
	Prompt string
	// Dir is the working directory for the claude process; its .claude/
	// tree (skills, agents, settings) is what the session discovers.
	// Empty means the current process working directory.
	Dir string
	// Model optionally overrides the session model (--model).
	Model string
	// MaxTurns bounds the agentic loop (--max-turns). 0 → 10.
	MaxTurns int
	// Timeout bounds the whole invocation. 0 → 3 minutes.
	Timeout time.Duration
	// SkipPermissions passes --dangerously-skip-permissions and suppresses
	// AllowedTools. It is intended for runner-owned non-interactive sessions.
	SkipPermissions bool
	// AllowedTools entries are passed via --allowedTools (one flag per entry).
	AllowedTools []string
	// Bin is the claude binary to execute. Empty → "claude" (resolved via PATH).
	// Tests point this at a stub.
	Bin string
	// Logger receives the shared process lifecycle and curated claude progress
	// records. Nil uses slog.Default through the process facility.
	Logger processLogger
}

// Usage mirrors the usage block of the claude -p result event. See
// [Usage.TotalInput] for the combined prompt volume across the token classes.
type Usage struct {
	// InputTokens is the count of fresh (uncached) prompt input tokens.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the count of generated output tokens.
	OutputTokens int `json:"output_tokens"`
	// CacheCreationInputTokens is input tokens written to the prompt cache.
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens is input tokens served from the prompt cache.
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

// TotalInput returns fresh + cache-created + cache-read input tokens — the
// full prompt volume the model saw, regardless of cache accounting.
func (u Usage) TotalInput() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// Result is the parsed final event of a claude -p run.
type Result struct {
	Text       string          // final result text
	IsError    bool            // result event is_error flag
	NumTurns   int             // agentic turns consumed
	DurationMS int64           // wall-clock duration reported by the CLI
	CostUSD    float64         // total_cost_usd
	Usage      Usage           // token accounting
	Raw        json.RawMessage // the verbatim result event for anything not surfaced
}

// resultEvent is the wire shape of the final JSON object emitted by
// `claude -p --output-format stream-json --verbose`.
type resultEvent struct {
	Type       string  `json:"type"`
	IsError    bool    `json:"is_error"`
	Result     string  `json:"result"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int64   `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      Usage   `json:"usage"`
}

// Run executes one claude -p call and returns the parsed result.
//
// A non-zero exit with parseable JSON on stdout still returns the Result
// (with an error when is_error is false — e.g. --max-turns exhaustion), so
// callers can inspect partial metrics. A non-zero exit with empty stdout
// returns only an error carrying stderr.
//
// DHF-REQ: keel/requirement-2, openbrain/requirement-615
func Run(ctx context.Context, req Request) (*Result, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("keel/exec/claude: empty prompt")
	}
	bin := req.Bin
	if bin == "" {
		bin = "claude"
	}
	maxTurns := req.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-p", req.Prompt, "--output-format", "stream-json", "--verbose", "--max-turns", strconv.Itoa(maxTurns)}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		for _, t := range req.AllowedTools {
			args = append(args, "--allowedTools", t)
		}
	}

	var stderr bytes.Buffer
	stdout := &claudeStreamWriter{logger: req.Logger}
	logger := req.Logger
	if logger == nil {
		logger = slog.Default()
	}
	proc, startErr := procexec.ProcessStart(ctx, procexec.Request{
		Program: bin,
		Args:    args,
		Dir:     req.Dir,
		Env:     os.Environ(),
		Stdout:  stdout,
		Stderr:  &stderr,
		Logger:  logger,
	})
	var runErr error
	if startErr != nil {
		runErr = startErr
	} else {
		_, runErr = proc.Wait()
	}

	if err := stdout.Err(); err != nil {
		return nil, fmt.Errorf("keel/exec/claude: read stdout: %w — stderr: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}

	out := bytes.TrimSpace(stdout.ResultRaw())
	if len(out) == 0 {
		if runErr != nil {
			return nil, fmt.Errorf("keel/exec/claude: %s: %w — stderr: %s", bin, runErr, bytes.TrimSpace(stderr.Bytes()))
		}
		return nil, fmt.Errorf("keel/exec/claude: empty stdout from %s", bin)
	}

	var ev resultEvent
	if err := json.Unmarshal(out, &ev); err != nil {
		return nil, fmt.Errorf("keel/exec/claude: parse result JSON: %w — first 200 bytes: %q", err, truncateBytes(out, 200))
	}

	res := &Result{
		Text:       ev.Result,
		IsError:    ev.IsError,
		NumTurns:   ev.NumTurns,
		DurationMS: ev.DurationMS,
		CostUSD:    ev.CostUSD,
		Usage:      ev.Usage,
		Raw:        json.RawMessage(append([]byte(nil), out...)),
	}
	if runErr != nil && !ev.IsError {
		return res, fmt.Errorf("keel/exec/claude: %s exited non-zero with non-error result: %w", bin, runErr)
	}
	return res, nil
}

// Version returns claude's reported version by running `<bin> --version`. An
// empty bin resolves "claude" on PATH. Output is trimmed; a spawn failure or
// non-zero exit returns the error.
//
// DHF-REQ: keel/requirement-56
func Version(ctx context.Context, bin string) (string, error) {
	if bin == "" {
		bin = "claude"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: bin,
		Args:    []string{"--version"},
		Stdout:  &out,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		return "", fmt.Errorf("keel/exec/claude: %s --version: %w", bin, err)
	}
	if _, err := proc.Wait(); err != nil {
		return "", fmt.Errorf("keel/exec/claude: %s --version: %w", bin, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

type claudeStreamWriter struct {
	logger    processLogger
	buf       []byte
	resultRaw []byte
	err       error
}

func (w *claudeStreamWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.buf = append(w.buf, p...)
	const maxLine = 4 * 1024 * 1024
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) > maxLine {
				w.err = fmt.Errorf("line too long")
				return 0, w.err
			}
			return len(p), nil
		}
		line := w.buf[:i]
		w.consumeLine(line)
		w.buf = w.buf[i+1:]
	}
}

func (w *claudeStreamWriter) Err() error {
	if w.err != nil {
		return w.err
	}
	if len(w.buf) > 0 {
		w.consumeLine(w.buf)
		w.buf = nil
	}
	return nil
}

func (w *claudeStreamWriter) ResultRaw() []byte {
	return append([]byte(nil), w.resultRaw...)
}

func (w *claudeStreamWriter) consumeLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	if stringValue(ev["type"]) == "result" {
		w.resultRaw = append(w.resultRaw[:0], line...)
		return
	}
	if detail := claudeProgressDetail(ev); detail != "" {
		log := w.logger
		if log == nil {
			log = slog.Default()
		}
		log.Info("claude progress",
			"event_type", stringValue(ev["type"]),
			"detail", detail,
		)
	}
}

// DHF-REQ: keel/requirement-2
func claudeProgressDetail(ev map[string]any) string {
	for _, src := range []map[string]any{progressObj(ev["message"]), ev} {
		if src == nil {
			continue
		}
		for _, key := range []string{"text", "summary", "result"} {
			if s := stringValue(src[key]); s != "" {
				return trimProgressDetail(s)
			}
		}
		if s := firstClaudeContentText(src["content"]); s != "" {
			return trimProgressDetail(s)
		}
	}
	return ""
}

func progressObj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func firstClaudeContentText(v any) string {
	items, _ := v.([]any)
	for _, item := range items {
		obj, _ := item.(map[string]any)
		if obj == nil {
			continue
		}
		if stringValue(obj["type"]) == "text" {
			if text := stringValue(obj["text"]); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func trimProgressDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 160 {
		return s
	}
	return s[:160] + "..."
}
