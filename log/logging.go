// Package logging provides structured JSON logging with the G1 field schema
// committed in the homelab interface spec: ts (RFC3339Nano), level (lowercase),
// msg, service, plus contextual fields. This is the shared module imported by
// all four services (mcp-server, vault-indexer, webdav-server, web-ui).
package log

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config holds the parameters for constructing a production logger.
type Config struct {
	Service      string
	Level        slog.Leveler
	Writer       io.Writer
	FileWriter   io.Writer
	FileLevel    slog.Leveler
	HumanLogDir  string
	ForceColor   bool
	DisableColor bool

	// ConsoleOmitKeys suppresses selected attrs from the human console sink.
	// Machine JSON logging is intentionally unaffected.
	ConsoleOmitKeys []string

	// HumanFileHandler is a pre-opened human rolling-file handler (see
	// NewHumanFileHandler) to compose alongside the console sink. When set, the
	// daily file is NOT opened by NewConsole — the caller owns the handler for
	// the invocation lifetime and closes it once. Takes precedence over
	// HumanLogDir, which opens (and leaks) a fresh file per NewConsole call and
	// is only safe for one-shot loggers.
	HumanFileHandler slog.Handler

	// JSONFileHandler is a pre-opened JSON Lines rolling-file handler (see
	// NewJSONFileHandler) to compose alongside the primary sink. The caller owns
	// the handler for the invocation lifetime and closes it once.
	JSONFileHandler slog.Handler
}

// ctxKey is the context key for storing a *slog.Logger.
type ctxKey struct{}

// WithLogger stores a logger in the context.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger stored in ctx, falling back to slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// DHF-REQ: keel/requirement-5
// replaceForOpenBrain renames "time" -> "ts" (RFC3339Nano), lowercases
// "level", drops "source", and redacts rendered string values before they
// reach either the JSON or console sink.
func replaceForOpenBrain(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Key = "ts"
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.Format(time.RFC3339Nano))
		}
		return a
	}
	if a.Key == slog.LevelKey {
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(strings.ToLower(lvl.String()))
		}
		return a
	}
	if a.Key == slog.SourceKey {
		// Drop source unless we decide to add it for debug.
		return slog.Attr{}
	}
	if a.Value.Kind() == slog.KindString && isSensitiveAttrKey(a.Key) {
		a.Value = slog.StringValue("[REDACTED]")
		return a
	}
	if a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue(redactString(a.Value.String()))
	}
	return a
}

func isSensitiveAttrKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "token") ||
		strings.Contains(k, "password") ||
		strings.Contains(k, "secret") ||
		k == "pat" ||
		strings.HasSuffix(k, "_pat")
}

// New creates a production logger writing JSON to stdout with the G1 field
// schema: ts, level (lowercase), msg, service.
// DHF-REQ: openbrain/requirement-602
func New(cfg Config) *slog.Logger {
	level := cfg.Level
	if level == nil {
		level = slog.LevelInfo
	}
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	var h slog.Handler = slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceForOpenBrain,
	})
	if cfg.FileWriter != nil {
		fileLevel := cfg.FileLevel
		if fileLevel == nil {
			fileLevel = slog.LevelDebug
		}
		h = multiHandler{handlers: []slog.Handler{
			h,
			slog.NewJSONHandler(cfg.FileWriter, &slog.HandlerOptions{
				Level:       fileLevel,
				ReplaceAttr: replaceForOpenBrain,
			}),
		}}
	}
	switch {
	case cfg.HumanFileHandler != nil && cfg.JSONFileHandler != nil:
		h = multiHandler{handlers: []slog.Handler{h, cfg.HumanFileHandler, cfg.JSONFileHandler}}
	case cfg.HumanFileHandler != nil:
		h = multiHandler{handlers: []slog.Handler{h, cfg.HumanFileHandler}}
	case cfg.JSONFileHandler != nil:
		h = multiHandler{handlers: []slog.Handler{h, cfg.JSONFileHandler}}
	}
	return slog.New(h).With("service", cfg.Service)
}

// NewConsole creates a production logger writing human-readable text to stdout
// with short HH:MM:SS fixed-width severity prefixes and the same redaction path
// as New.
//
// DHF-REQ: keel/requirement-5, openbrain/requirement-151, openbrain/requirement-152
func NewConsole(cfg Config) *slog.Logger {
	level := cfg.Level
	if level == nil {
		level = slog.LevelInfo
	}
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}
	h := newConsoleHandler(w, level, colorEnabled(w, cfg.ForceColor, cfg.DisableColor), cfg.ConsoleOmitKeys)
	switch {
	case cfg.HumanFileHandler != nil && cfg.JSONFileHandler != nil:
		h = multiHandler{handlers: []slog.Handler{h, cfg.HumanFileHandler, cfg.JSONFileHandler}}
	case cfg.HumanFileHandler != nil:
		// Caller owns a file handler for the invocation lifetime (no per-call
		// open). This is the leak-free path for loggers built per log record.
		h = multiHandler{handlers: []slog.Handler{h, cfg.HumanFileHandler}}
	case cfg.JSONFileHandler != nil:
		h = multiHandler{handlers: []slog.Handler{h, cfg.JSONFileHandler}}
	case cfg.HumanLogDir != "":
		if fileHandler, err := newHumanFileHandler(cfg.HumanLogDir, cfg.Service); err == nil {
			h = multiHandler{handlers: []slog.Handler{h, fileHandler}}
		}
	}
	return slog.New(h).With("service", cfg.Service)
}

type multiHandler struct {
	handlers []slog.Handler
}

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var err error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			err = errors.Join(err, handler.Handle(ctx, r))
		}
	}
	return err
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := multiHandler{handlers: make([]slog.Handler, 0, len(h.handlers))}
	for _, handler := range h.handlers {
		next.handlers = append(next.handlers, handler.WithAttrs(attrs))
	}
	return next
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	next := multiHandler{handlers: make([]slog.Handler, 0, len(h.handlers))}
	for _, handler := range h.handlers {
		next.handlers = append(next.handlers, handler.WithGroup(name))
	}
	return next
}

type consoleHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	color  bool
	omit   map[string]struct{}
	attrs  []slog.Attr
	groups []string
}

func newConsoleHandler(w io.Writer, level slog.Leveler, color bool, omitKeys []string) slog.Handler {
	return &consoleHandler{mu: &sync.Mutex{}, w: w, level: level, color: color, omit: omitKeySet(omitKeys)}
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	min := slog.LevelInfo
	if h.level != nil {
		min = h.level.Level()
	}
	return level >= min
}

// DHF-REQ: openbrain/requirement-151, openbrain/requirement-152
func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	var b strings.Builder
	writeConsoleTimestamp(&b, r.Time, h.color)
	b.WriteByte(' ')
	writeConsoleLevel(&b, r.Level, h.color)
	b.WriteString("  ")
	message, skipKeys := h.consoleMessage(r.Message, attrs)
	b.WriteString(RedactString(message))
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || h.omits(attr.Key) || skipKeys[attr.Key] {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Key)
		b.WriteByte('=')
		b.WriteString(formatConsoleValue(attr.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = slicesClone(h.attrs)
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || next.omits(attr.Key) {
			continue
		}
		next.attrs = append(next.attrs, attr)
	}
	return next
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.groups = append(slicesClone(h.groups), name)
	return next
}

func (h *consoleHandler) clone() *consoleHandler {
	return &consoleHandler{
		mu:     h.mu,
		w:      h.w,
		level:  h.level,
		color:  h.color,
		omit:   h.omit,
		attrs:  h.attrs,
		groups: h.groups,
	}
}

func (h *consoleHandler) omits(key string) bool {
	if h == nil || h.omit == nil {
		return false
	}
	_, ok := h.omit[key]
	return ok
}

func (h *consoleHandler) consoleMessage(msg string, attrs []slog.Attr) (string, map[string]bool) {
	skip := map[string]bool{}
	if msg != "codex progress" {
		return msg, skip
	}
	detail := ""
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		switch attr.Key {
		case "detail":
			if attr.Value.Kind() == slog.KindString {
				detail = attr.Value.String()
			} else {
				detail = formatConsoleValue(attr.Value)
			}
			skip["detail"] = true
		case "event_type":
			skip["event_type"] = true
		}
	}
	if strings.TrimSpace(detail) == "" {
		return "codex progress", skip
	}
	return "codex detail: " + detail, skip
}

func omitKeySet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func slicesClone[S ~[]E, E any](in S) S {
	if in == nil {
		return nil
	}
	out := make(S, len(in))
	copy(out, in)
	return out
}

func consoleLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERRO"
	case level >= slog.LevelWarn:
		return "WARN"
	case level <= slog.LevelDebug:
		return "DEBU"
	default:
		return "INFO"
	}
}

func writeConsoleTimestamp(b *strings.Builder, t time.Time, color bool) {
	if color {
		b.WriteString("\x1b[90m")
	}
	b.WriteString(t.Format("15:04:05"))
	if color {
		b.WriteString("\x1b[0m")
	}
}

func writeConsoleLevel(b *strings.Builder, level slog.Level, color bool) {
	tag := consoleLevel(level)
	if color {
		b.WriteString(levelColor(level))
	}
	b.WriteString(tag)
	if color {
		b.WriteString("\x1b[0m")
	}
}

func levelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "\x1b[31m"
	case level >= slog.LevelWarn:
		return "\x1b[33m"
	case level <= slog.LevelDebug:
		return "\x1b[37m"
	default:
		return "\x1b[32m"
	}
}

func colorEnabled(w io.Writer, force bool, disable bool) bool {
	if disable || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if force {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func formatConsoleValue(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return RedactString(v.String())
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindGroup:
		attrs := v.Group()
		parts := make([]string, 0, len(attrs))
		for _, attr := range attrs {
			if attr.Key == "" {
				continue
			}
			parts = append(parts, attr.Key+"="+formatConsoleValue(attr.Value))
		}
		return "{" + strings.Join(parts, " ") + "}"
	default:
		return RedactString(fmt.Sprint(v.Any()))
	}
}

type humanFileHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	attrs  []slog.Attr
	groups []string
}

// NewJSONFileHandler opens today's JSON Lines daily log file under dir and
// returns a handler (DEBUG and above) that appends to it. The returned handler
// also satisfies io.Closer; callers own it for the invocation lifetime.
//
// DHF-REQ: openbrain/change_request-441
func NewJSONFileHandler(dir string, service string) (slog.Handler, error) {
	return newJSONFileHandler(dir, service)
}

func newJSONFileHandler(dir string, service string) (slog.Handler, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(JSONLogPath(dir, service), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &jsonFileHandler{
		Handler: slog.NewJSONHandler(f, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceForOpenBrain,
		}),
		close: f.Close,
	}, nil
}

type jsonFileHandler struct {
	slog.Handler
	close func() error
}

func (h *jsonFileHandler) Close() error {
	if h.close == nil {
		return nil
	}
	return h.close()
}

// NewHumanFileHandler opens today's Serilog-style daily human log file under dir
// and returns a handler (DEBUG and above) that appends to it. The returned
// handler also satisfies io.Closer: the caller owns it for the invocation
// lifetime and must Close it once when done, rather than opening a fresh file
// per log record. Pass it to NewConsole via Config.HumanFileHandler.
//
// DHF-REQ: openbrain/requirement-152
func NewHumanFileHandler(dir string, service string) (slog.Handler, error) {
	return newHumanFileHandler(dir, service)
}

func newHumanFileHandler(dir string, service string) (slog.Handler, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := pruneHumanLogs(dir, service, 10); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(HumanLogPath(dir, service), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if err := pruneHumanLogs(dir, service, 10); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &humanFileHandler{mu: &sync.Mutex{}, w: f}, nil
}

func (h *humanFileHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

// DHF-REQ: openbrain/requirement-152
func (h *humanFileHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	source := sourceFromAttrs(attrs)

	var b strings.Builder
	b.WriteString(r.Time.Format("2006-01-02 15:04:05.000"))
	b.WriteByte('\t')
	b.WriteString(consoleLevel(r.Level))
	b.WriteByte('\t')
	b.WriteString(fmt.Sprintf("%-26s", source))
	b.WriteByte('\t')
	b.WriteString(RedactString(r.Message))
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || attr.Key == "service" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(attr.Key)
		b.WriteByte('=')
		b.WriteString(formatConsoleValue(attr.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *humanFileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = slicesClone(h.attrs)
	for _, attr := range attrs {
		next.attrs = append(next.attrs, replaceForOpenBrain(h.groups, attr))
	}
	return next
}

func (h *humanFileHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.groups = append(slicesClone(h.groups), name)
	return next
}

// Close closes the underlying log file. Clones produced by WithAttrs/WithGroup
// share the same file, so a single Close on any of them releases the descriptor
// for all. Safe to call once at the end of an invocation; handlers must not be
// used afterward.
//
// DHF-REQ: openbrain/requirement-152
func (h *humanFileHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (h *humanFileHandler) clone() *humanFileHandler {
	return &humanFileHandler{
		mu:     h.mu,
		w:      h.w,
		attrs:  h.attrs,
		groups: h.groups,
	}
}

func sourceFromAttrs(attrs []slog.Attr) string {
	for _, attr := range attrs {
		attr = replaceForOpenBrain(nil, attr)
		if attr.Key == "service" && attr.Value.Kind() == slog.KindString {
			return attr.Value.String()
		}
	}
	return ""
}

// DHF-REQ: openbrain/requirement-152
// HumanLogPath returns today's Serilog-style daily human log path for service.
func HumanLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".log")
}

// DHF-REQ: openbrain/change_request-441
// JSONLogPath returns today's JSON Lines daily log path for service.
func JSONLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".jsonl")
}

func pruneHumanLogs(dir string, service string, keep int) error {
	matches, err := filepath.Glob(filepath.Join(dir, safeLogService(service)+"-*.log"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for len(matches) > keep {
		if err := os.Remove(matches[0]); err != nil && !os.IsNotExist(err) {
			return err
		}
		matches = matches[1:]
	}
	return nil
}

func safeLogService(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return "openbrain"
	}
	var b strings.Builder
	for _, r := range service {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

const ruleWidth = 70

// FieldRow is one aligned label/value row rendered by Fields.
type FieldRow struct {
	Label string
	Value any
}

// DHF-REQ: openbrain/requirement-151
// Header logs a ruled human-mode banner through the provided logger.
func Header(logger *slog.Logger, title string, version string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(strings.Repeat("=", ruleWidth))
	if version != "" {
		title += " " + version
	}
	logger.Info(title)
	logger.Info(strings.Repeat("=", ruleWidth))
}

// DHF-REQ: openbrain/requirement-151
// Section logs a ruled human-mode section header through the provided logger.
func Section(logger *slog.Logger, name string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(strings.Repeat("-", ruleWidth) + " " + name)
}

// DHF-REQ: openbrain/requirement-151
// Field logs one label/value row. Use Fields when multiple rows should align.
func Field(logger *slog.Logger, label string, value any) {
	Fields(logger, []FieldRow{{Label: label, Value: value}})
}

// DHF-REQ: openbrain/requirement-151
// Fields logs aligned label/value rows through the provided logger.
func Fields(logger *slog.Logger, rows []FieldRow) {
	if logger == nil {
		logger = slog.Default()
	}
	width := 0
	for _, row := range rows {
		if len(row.Label) > width {
			width = len(row.Label)
		}
	}
	for _, row := range rows {
		logger.Info(fmt.Sprintf("%-*s : %v", width, row.Label, row.Value))
	}
}

// RecordCapture captures JSON log output for testing.
type RecordCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// write is called by the handler; it's the io.Writer target.
func (rc *RecordCapture) Write(p []byte) (int, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.buf.Write(p)
}

// LastJSON returns the last complete JSON line as a map, or nil.
func (rc *RecordCapture) LastJSON() map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(rc.buf.String()), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		return nil
	}
	return m
}

// AllJSON returns every captured log line as a slice of parsed maps, in
// emission order. Lines that fail JSON parsing are skipped. Returns a non-nil
// empty slice when the buffer is empty.
func (rc *RecordCapture) AllJSON() []map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	raw := strings.TrimSpace(rc.buf.String())
	out := make([]map[string]any, 0)
	if raw == "" {
		return out
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// LastRaw returns the last complete JSON line as a raw string.
func (rc *RecordCapture) LastRaw() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(rc.buf.String()), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// Reset clears the capture buffer.
func (rc *RecordCapture) Reset() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.buf.Reset()
}

// NewForTesting creates a logger that writes to a RecordCapture buffer,
// using the same G1 JSON handler chain as production. The log level is
// set to LevelDebug so all levels are captured. Tests MUST NOT call
// slog.SetDefault with the returned logger.
func NewForTesting(service string) (*slog.Logger, *RecordCapture) {
	rc := &RecordCapture{}
	h := slog.NewJSONHandler(rc, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceForOpenBrain,
	})
	return slog.New(h).With("service", service), rc
}

// NewConsoleForTesting creates a console logger that writes to a RecordCapture
// buffer using the same human console handler chain as production.
//
// DHF-REQ: keel/requirement-5, openbrain/requirement-151, openbrain/requirement-152
func NewConsoleForTesting(service string) (*slog.Logger, *RecordCapture) {
	rc := &RecordCapture{}
	h := newConsoleHandler(rc, slog.LevelDebug, false, nil)
	return slog.New(h).With("service", service), rc
}

// --- Error redaction ---

// dsnRegex matches postgres://user:password@host patterns (classic user:pass@host form).
var dsnRegex = regexp.MustCompile(`://([^:@]+):([^@]+)@`)

// tokenUserinfoRegex matches token-only userinfo: ://<token>@host where the
// token contains no colon (finding #11: bare PAT in userinfo without a username).
// Example: https://ghp_abc123@github.com/org/repo.git
var tokenUserinfoRegex = regexp.MustCompile(`://([^:@/]+)@`)

// credParamRegex matches password-bearing key=value credential forms outside
// the userinfo position: query params (?password=…, &sslpassword=…) and libpq
// keyword DSNs (password=… host=…). Deliberately unanchored at the start so
// suffix keys (user_password=) redact too — over-redaction is the safe
// direction. (issue-0111)
var credParamRegex = regexp.MustCompile(`(password|sslpassword)=[^\s&]+`)

// tokenParamRegex matches token-bearing query parameters (finding #11: PAT in
// ?token= or ?access_token= query forms).
// Example: https://gitea/repo.git?token=ghp_abc123
var tokenParamRegex = regexp.MustCompile(`(?i)((?:^|[?&])(?:token|access_token)=)[^\s&]+`)

// bearerRegex matches Bearer tokens.
var bearerRegex = regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-_.]+`)

// redactString strips DSN passwords and bearer tokens from a rendered string.
// The single shared implementation behind both RedactErr and RedactString. (KD7.)
func redactString(s string) string {
	// user:pass@host form — replace with ***:***@.
	s = dsnRegex.ReplaceAllString(s, "://***:***@")
	// token-only userinfo (no colon) — must run AFTER dsnRegex which handles user:pass.
	// After dsnRegex, remaining ://xxx@ patterns have no colon in xxx → PAT.
	s = tokenUserinfoRegex.ReplaceAllString(s, "://***@")
	s = credParamRegex.ReplaceAllString(s, "$1=***")
	// token= / access_token= query params.
	s = tokenParamRegex.ReplaceAllString(s, "${1}***")
	s = bearerRegex.ReplaceAllString(s, "Bearer [REDACTED]")
	return s
}

// RedactString redacts secrets from an already-rendered string (metadata
// values, root_cause). String→string sibling of RedactErr. (KD7.)
func RedactString(s string) string { return redactString(s) }

// RedactErr walks the error string and strips DSN passwords and bearer tokens.
// Returns nil for nil input. Delegates regex work to redactString; flatten-no-wrap
// contract is unchanged — errors.Is/As do NOT see through a redacted error.
func RedactErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactString(err.Error()))
}

// ParseLevel parses a log level string ("debug", "info", "warn", "error").
// Returns slog.LevelInfo on empty or unrecognized input.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Ensure RedactErr returns a plain error without wrapping (so errors.Is
// does not leak through).
var _ error = RedactErr(errors.New("test"))
