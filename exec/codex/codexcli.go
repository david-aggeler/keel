package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	procexec "github.com/david-aggeler/keel/exec"
	logging "github.com/david-aggeler/keel/log"
)

type processLogger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
}

// Event is one decoded JSONL line from the codex event stream.
type Event struct {
	// Type is the "type" field of the JSONL event line ("" if absent).
	Type string
	// Raw is the verbatim line as emitted (no trailing newline).
	Raw json.RawMessage
}

// Result collects the full event stream of a codex exec run.
type Result struct {
	// Events is every decoded event, in arrival order.
	Events []Event
	// ThreadID is the codex thread id from the thread.started event, when
	// present. It is the join key to codex's rollout transcript.
	ThreadID string
	// Final is the last line read regardless of JSON validity; on a truncated
	// or killed run it may not be codex's terminal `result` event.
	Final *Event
	// ExitCode is the process exit code.
	ExitCode int
	// Raw is the verbatim stdout (each line + "\n"). Blank lines are preserved
	// here but excluded from Events.
	Raw []byte
}

// Request describes one headless `codex exec --json` invocation.
type Request struct {
	// Prompt is the user prompt, passed as the final positional arg.
	Prompt string
	// Dir is the working directory for the codex process. Empty means the
	// current process working directory.
	Dir string
	// Bin is the codex binary to execute. Empty → "codex" (resolved via PATH).
	// Tests point this at a stub.
	Bin string
	// Model optionally overrides the session model (--model).
	Model string
	// Timeout bounds the whole invocation. 0 → 10 minutes.
	Timeout time.Duration
	// ExtraArgs are caller passthrough flags (sandbox/approvals), placed
	// before the prompt positional arg.
	ExtraArgs []string
	// Env are additional "KEY=VALUE" environment assignments layered on top of
	// the parent process environment for the codex child (and thus its own
	// children, e.g. the merge-gate build). Later entries win over earlier ones
	// and over the inherited value for the same key. Nil leaves the child with
	// the unmodified parent environment. openbrain-client uses this to export a
	// provisioned TMPDIR/GOTMPDIR so the CGO link does not OOM on a small /tmp
	// (openbrain/requirement-181).
	Env []string
	// OnEvent, if non-nil, is called once per decoded event in arrival order,
	// before Run returns.
	OnEvent func(Event)
	// Logger receives the shared process lifecycle and raw output records. Nil
	// uses slog.Default through the process facility.
	Logger processLogger
}

// Run executes one `codex exec --json` call, streaming events as they arrive.
//
// A non-zero exit with at least one parsed event still returns the Result so
// callers can inspect ExitCode and partial events. A non-zero exit with zero
// parsed events returns only an error carrying stderr. A spawn failure returns
// the error.
//
// DHF-REQ: keel/requirement-7, openbrain/requirement-181, keel/requirement-2
func Run(ctx context.Context, req Request) (*Result, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("keel/exec/codex: empty prompt")
	}
	bin := req.Bin
	if bin == "" {
		bin = "codex"
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// codex's streaming-JSON event log is selected by `exec --json`.
	args := []string{"exec", "--json"}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, req.ExtraArgs...)
	args = append(args, req.Prompt)

	// stdin from the null device so codex can never hang waiting for input.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("keel/exec/codex: open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()

	res := &Result{}
	stdout := &eventStreamWriter{res: res, onEvent: req.OnEvent, logger: req.Logger}

	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: bin,
		Args:    args,
		Dir:     req.Dir,
		// Layer caller-supplied assignments on top of the inherited environment;
		// exec uses the last value for a duplicate key, so appending lets Env
		// override an inherited TMPDIR (openbrain/requirement-181).
		Env:    append(os.Environ(), req.Env...),
		Stdin:  devNull,
		Stdout: stdout,
		Logger: req.Logger,
		Configure: func(cmd *exec.Cmd) {
			// Put the child in its own process group so a cancel/timeout kill reaches
			// codex's whole subprocess tree, not just the direct child.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error {
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return nil
			}
			cmd.WaitDelay = 5 * time.Second
		},
	})
	if err != nil {
		return nil, fmt.Errorf("keel/exec/codex: start %s: %w", bin, err)
	}

	processResult, waitErr := proc.Wait()
	res.ExitCode = processResult.ExitCode
	if n := len(res.Events); n > 0 {
		res.Final = &res.Events[n-1]
	}
	scanErr := stdout.Err()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return res, fmt.Errorf("keel/exec/codex: %s: %w — stderr: %s",
			bin, ctxErr, strings.TrimSpace(processResult.Stderr))
	}
	if scanErr != nil {
		return res, fmt.Errorf("keel/exec/codex: read stdout: %w — stderr: %s",
			scanErr, strings.TrimSpace(processResult.Stderr))
	}
	if res.ExitCode != 0 && len(res.Events) == 0 {
		return nil, fmt.Errorf("keel/exec/codex: %s exited %d with no events — stderr: %s",
			bin, res.ExitCode, strings.TrimSpace(processResult.Stderr))
	}
	if waitErr != nil && len(res.Events) == 0 {
		return nil, fmt.Errorf("keel/exec/codex: %s wait: %w — stderr: %s",
			bin, waitErr, strings.TrimSpace(processResult.Stderr))
	}

	return res, nil
}

type eventStreamWriter struct {
	res     *Result
	onEvent func(Event)
	logger  processLogger
	buf     []byte
	err     error
}

func (w *eventStreamWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.buf = append(w.buf, p...)
	const maxLine = 4 * 1024 * 1024
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) > maxLine {
				w.err = bufio.ErrTooLong
				return 0, w.err
			}
			return len(p), nil
		}
		line := w.buf[:i]
		w.consumeLine(line)
		w.buf = w.buf[i+1:]
	}
}

func (w *eventStreamWriter) Err() error {
	if w.err != nil {
		return w.err
	}
	if len(w.buf) > 0 {
		w.consumeLine(w.buf)
		w.buf = nil
	}
	return nil
}

func (w *eventStreamWriter) consumeLine(line []byte) {
	w.res.Raw = append(w.res.Raw, line...)
	w.res.Raw = append(w.res.Raw, '\n')
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}

	ev := Event{Type: decodeEventType(line), Raw: append([]byte(nil), line...)}
	if ev.Type == "thread.started" && w.res.ThreadID == "" {
		w.res.ThreadID = decodeThreadID(line)
	}
	if detail := codexProgressDetail(line); detail != "" {
		log := w.logger
		if log == nil {
			log = slog.Default()
		}
		log.Info("codex progress",
			"event_type", ev.Type,
			"detail", detail,
		)
	}
	if w.onEvent != nil {
		w.onEvent(ev)
	}
	w.res.Events = append(w.res.Events, ev)
}

// DHF-REQ: keel/requirement-2
func codexProgressDetail(line []byte) string {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return ""
	}
	for _, src := range []map[string]any{progressObj(ev["item"]), progressObj(ev["payload"]), progressObj(ev["msg"]), ev} {
		if src == nil {
			continue
		}
		for _, key := range []string{"text", "summary", "result"} {
			if s := stringValue(src[key]); s != "" {
				return trimProgressDetail(s)
			}
		}
	}
	return ""
}

func progressObj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func trimProgressDetail(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

// itemEnvelopeTypes are the codex `exec --json` thread/item wrappers whose
// top-level "type" names the wrapper and nests the semantic event under "item"
// (item.type). This is the documented stdout stream format (codex CLI manual,
// `codex exec --json`): thread.started / turn.started / item.started /
// item.completed / item.updated / turn.completed, with the item carrying
// type ∈ {agent_message, reasoning, command_execution, file_change,
// mcp_tool_call, web_search, …} and its text under item.text.
var itemEnvelopeTypes = map[string]bool{
	"item.started":   true,
	"item.completed": true,
	"item.updated":   true,
}

// payloadEnvelopeTypes are codex's persistence/older-protocol discriminators:
// the top-level "type" names the wrapper and the semantic type is nested under
// "payload" (rollout files, codex >= 0.140) or "msg" (older builds). These do
// NOT appear on the exec --json stdout stream codexcli reads, but decoding them
// keeps codexcli robust to codex version/format drift.
var payloadEnvelopeTypes = map[string]bool{
	"event_msg":     true,
	"response_item": true,
	"turn_context":  true,
	"session_meta":  true,
}

// decodeEventType extracts the semantic event type from one JSONL line so
// Event.Type is the value every consumer expects (agent_message, reasoning,
// command_execution, …) regardless of codex's stream framing. The real
// `codex exec --json` stdout wraps each event as
// {"type":"item.completed","item":{"type":"agent_message",…}} — so for an
// item.* wrapper it returns item.type. A semantic top-level type (the flat
// scripted-stub shape, or thread.*/turn.*/error) is returned as-is. Other
// wrappers (event_msg/payload, msg) are unwrapped as a robustness fallback.
//
// DHF-REQ: openbrain/requirement-32
func decodeEventType(line []byte) string {
	var hdr struct {
		Type string `json:"type"`
		Item struct {
			Type string `json:"type"`
		} `json:"item"`
		Payload struct {
			Type string `json:"type"`
		} `json:"payload"`
		Msg struct {
			Type string `json:"type"`
		} `json:"msg"`
	}
	_ = json.Unmarshal(line, &hdr)
	// codex exec --json stdout: thread/item events — the semantic type is under
	// "item".
	if itemEnvelopeTypes[hdr.Type] && hdr.Item.Type != "" {
		return hdr.Item.Type
	}
	// A semantic top-level type: the flat stub shape, or thread.*/turn.*/error.
	if hdr.Type != "" && !payloadEnvelopeTypes[hdr.Type] && !itemEnvelopeTypes[hdr.Type] {
		return hdr.Type
	}
	// Persistence/older envelopes nest under payload then msg.
	if hdr.Payload.Type != "" {
		return hdr.Payload.Type
	}
	if hdr.Msg.Type != "" {
		return hdr.Msg.Type
	}
	return hdr.Type
}

// decodeThreadID extracts the stdout stream's thread_id from thread.started.
//
// DHF-REQ: openbrain/requirement-591
func decodeThreadID(line []byte) string {
	var hdr struct {
		ThreadID string `json:"thread_id"`
	}
	_ = json.Unmarshal(line, &hdr)
	return hdr.ThreadID
}

// Version returns codex's reported version by running `<bin> --version`. An
// empty bin resolves "codex" on PATH. Output is trimmed; a spawn failure or
// non-zero exit returns the error. openbrain-client logs this at startup so the
// codex build driving a tail is recorded — no feedback loop without logs.
func Version(ctx context.Context, bin string) (string, error) {
	if bin == "" {
		bin = "codex"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	proc, err := procexec.ProcessStart(ctx, procexec.Request{
		Program: bin,
		Args:    []string{"--version"},
		Stdout:  &out,
		Logger:  logging.Discard(),
	})
	if err != nil {
		return "", fmt.Errorf("keel/exec/codex: %s --version: %w", bin, err)
	}
	if _, err := proc.Wait(); err != nil {
		return "", fmt.Errorf("keel/exec/codex: %s --version: %w", bin, err)
	}
	return strings.TrimSpace(out.String()), nil
}
