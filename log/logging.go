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
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Console selects the process console rendering for [New].
type Console string

const (
	// ConsoleSparseAI emits sparse JSON events used by agent-oriented runs.
	ConsoleSparseAI Console = "sparse-ai"
	// ConsolePlain emits the human-readable console formatter.
	ConsolePlain Console = "plain"
	// ConsoleJSON emits verbose JSON records to the console writer.
	ConsoleJSON Console = "json"
	// ConsoleNone disables the console sink.
	ConsoleNone Console = "none"
)

// Config holds the parameters for constructing a production logger. The zero
// value is usable: Service is blank, the level defaults to Info, and output
// goes to os.Stdout with the sparse-AI console and no file sinks. All fields are
// optional.
//
// DHF-REQ: keel/requirement-30
type Config struct {
	// Service is the value stamped into the "service" field of every record.
	Service string
	// Level is the minimum severity emitted to the primary sink. Nil → Info.
	Level slog.Leveler
	// Console selects the console rendering. Empty → ConsoleSparseAI.
	Console Console
	// Writer is the console sink destination. Nil → os.Stdout. Set it to a
	// bytes.Buffer (or any io.Writer) to capture console output in tests.
	Writer io.Writer
	// TextDir, when non-empty, opens a daily human-readable .log file sink.
	TextDir string
	// JSONLDir, when non-empty, opens a daily JSON Lines .jsonl file sink.
	JSONLDir string
	// PerRun, when true, makes JSONLDir use a per-invocation JSON Lines file
	// whose path and line counter back RunLogPath and RunLogLine. When false,
	// JSONLDir uses the daily rolling JSONL file.
	PerRun bool
	// SourceInFiles keeps automatic caller source enabled for text file sinks.
	SourceInFiles bool
	// ForceColor forces ANSI color on the console sink even when the writer is
	// not a terminal. Ignored when NO_COLOR is set or DisableColor is true.
	ForceColor bool
	// DisableColor suppresses ANSI color on the console sink unconditionally.
	DisableColor bool

	// ConsoleOmitKeys suppresses selected attrs from the human console sink.
	// Machine JSON logging is intentionally unaffected.
	ConsoleOmitKeys []string

	// HumanFileHandler is a pre-opened human rolling-file handler. It is kept
	// for internal tests and advanced composition; prefer TextDir for normal
	// construction.
	HumanFileHandler slog.Handler

	// JSONFileHandler is a pre-opened JSON Lines rolling-file handler. It is kept
	// for internal tests and advanced composition; prefer JSONLDir for normal
	// construction.
	JSONFileHandler slog.Handler

	// Handlers are additional slog handlers fanned out after the built-in sinks.
	// Optional packages such as log/otel use this hook so the core log package
	// keeps its dependency surface unchanged.
	Handlers []slog.Handler
}

// Logger is keel/log's public logger. It wraps slog while owning any file sinks
// opened by [New].
//
// DHF-REQ: keel/requirement-16
type Logger struct {
	base          *slog.Logger
	closers       []io.Closer
	runLogPath    string
	runLog        *lineCountingWriteCloser
	sourceInFiles bool
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

// DHF-REQ: keel/requirement-5, keel/requirement-20
// replaceForOpenBrain renames "time" -> "ts" (RFC3339Nano), keeps slog-native
// uppercase levels, drops "source", and redacts rendered string values before
// they reach either the JSON or console sink.
func replaceForOpenBrain(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Key = "ts"
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.Format(time.RFC3339Nano))
		}
		return a
	}
	if a.Key == slog.LevelKey {
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

// DHF-REQ: keel/requirement-20
func replaceForOpenBrainFile(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		return a
	}
	return replaceForOpenBrain(groups, a)
}

func replaceForConsole(groups []string, a slog.Attr) slog.Attr {
	a = replaceForOpenBrain(groups, a)
	if isContextKey(a.Key) {
		return slog.Attr{}
	}
	return a
}

func isContextKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		key = key[i+1:]
	}
	return key == "module" || key == slog.SourceKey
}

func groupedKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	parts := make([]string, 0, len(groups)+1)
	parts = append(parts, groups...)
	parts = append(parts, key)
	return strings.Join(parts, ".")
}

func isSensitiveAttrKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "token") ||
		strings.Contains(k, "password") ||
		strings.Contains(k, "secret") ||
		k == "pat" ||
		strings.HasSuffix(k, "_pat")
}

// New creates a production logger from the four-sink Config model.
//
// DHF-REQ: keel/requirement-16, keel/requirement-22, keel/requirement-29, openbrain/requirement-602
func New(cfg Config) (*Logger, error) {
	level := cfg.Level
	if level == nil {
		level = slog.LevelInfo
	}
	w := cfg.Writer
	if w == nil {
		w = os.Stdout
	}

	handlers := make([]slog.Handler, 0, 3+len(cfg.Handlers))
	console := cfg.Console
	if console == "" {
		console = ConsoleSparseAI
	}
	switch console {
	case ConsoleNone:
	case ConsoleJSON:
		handlers = append(handlers, slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: replaceForConsole,
		}))
	case ConsoleSparseAI:
		handlers = append(handlers, newSparseAIHandler(w, level))
	default:
		handlers = append(handlers, newConsoleHandler(w, level, colorEnabled(w, cfg.ForceColor, cfg.DisableColor), cfg.ConsoleOmitKeys))
	}

	closers := make([]io.Closer, 0, 2)
	if cfg.HumanFileHandler != nil {
		handlers = append(handlers, cfg.HumanFileHandler)
		if c, ok := cfg.HumanFileHandler.(io.Closer); ok {
			closers = append(closers, c)
		}
	} else if cfg.TextDir != "" {
		h, err := newHumanFileHandler(cfg.TextDir, cfg.Service, cfg.SourceInFiles)
		if err != nil {
			return nil, fmt.Errorf("keel/log: open text sink: %w", err)
		}
		handlers = append(handlers, h)
		if c, ok := h.(io.Closer); ok {
			closers = append(closers, c)
		}
	}
	var runLogPath string
	var runLog *lineCountingWriteCloser
	if cfg.JSONFileHandler != nil {
		handlers = append(handlers, cfg.JSONFileHandler)
		if c, ok := cfg.JSONFileHandler.(io.Closer); ok {
			closers = append(closers, c)
		}
	} else if cfg.JSONLDir != "" {
		h, path, counter, err := newJSONFileHandler(cfg.JSONLDir, cfg.Service, cfg.PerRun, cfg.SourceInFiles)
		if err != nil {
			closeAll(closers)
			return nil, fmt.Errorf("keel/log: open jsonl sink: %w", err)
		}
		handlers = append(handlers, h)
		if c, ok := h.(io.Closer); ok {
			closers = append(closers, c)
		}
		runLogPath = path
		runLog = counter
	}
	for _, h := range cfg.Handlers {
		if h == nil {
			continue
		}
		handlers = append(handlers, h)
		if c, ok := h.(io.Closer); ok {
			closers = append(closers, c)
		}
	}
	if len(handlers) == 0 {
		handlers = append(handlers, slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{
			Level:       level,
			ReplaceAttr: replaceForOpenBrain,
		}))
	}

	h := handlers[0]
	if len(handlers) > 1 {
		h = multiHandler{handlers: handlers}
	}
	return &Logger{base: slog.New(h).With("service", cfg.Service), closers: closers, runLogPath: runLogPath, runLog: runLog, sourceInFiles: cfg.SourceInFiles}, nil
}

func closeAll(closers []io.Closer) {
	for i := len(closers) - 1; i >= 0; i-- {
		_ = closers[i].Close()
	}
}

func (l *Logger) slog() *slog.Logger {
	if l == nil || l.base == nil {
		return slog.Default()
	}
	return l.base
}

// Slog returns the wrapped slog logger for APIs that have not migrated yet.
func (l *Logger) Slog() *slog.Logger { return l.slog() }

// RunLogPath returns the JSONL run-log path selected by Config.PerRun, or the
// JSONL file path for the current logger when JSONLDir is configured.
//
// DHF-REQ: keel/requirement-19
func (l *Logger) RunLogPath() string {
	if l == nil {
		return ""
	}
	return l.runLogPath
}

// RunLogLine returns the 1-based line number of the last JSONL record written
// by this logger. It returns zero when no JSONL sink is configured or no record
// has been written yet.
//
// DHF-REQ: keel/requirement-19
func (l *Logger) RunLogLine() int {
	if l == nil || l.runLog == nil {
		return 0
	}
	return l.runLog.Line()
}

// Debug emits a DEBUG record.
func (l *Logger) Debug(msg string, args ...any) { l.slog().Debug(msg, l.argsWithAutoSource(args)...) }

// Info emits an INFO record.
func (l *Logger) Info(msg string, args ...any) { l.slog().Info(msg, l.argsWithAutoSource(args)...) }

// Warn emits a WARN record.
func (l *Logger) Warn(msg string, args ...any) { l.slog().Warn(msg, l.argsWithAutoSource(args)...) }

// Error emits an ERROR record.
func (l *Logger) Error(msg string, args ...any) { l.slog().Error(msg, l.argsWithAutoSource(args)...) }

// DebugContext emits a DEBUG record with ctx.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.slog().DebugContext(ctx, msg, l.argsWithAutoSource(args)...)
}

// InfoContext emits an INFO record with ctx.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.slog().InfoContext(ctx, msg, l.argsWithAutoSource(args)...)
}

// WarnContext emits a WARN record with ctx.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.slog().WarnContext(ctx, msg, l.argsWithAutoSource(args)...)
}

// ErrorContext emits an ERROR record with ctx.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.slog().ErrorContext(ctx, msg, l.argsWithAutoSource(args)...)
}

// With returns a logger carrying the supplied attrs.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{base: l.slog().With(args...), closers: l.closers, runLogPath: l.runLogPath, runLog: l.runLog, sourceInFiles: l.sourceInFiles}
}

// WithGroup returns a logger that groups subsequent attrs under name.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{base: l.slog().WithGroup(name), closers: l.closers, runLogPath: l.runLogPath, runLog: l.runLog, sourceInFiles: l.sourceInFiles}
}

// Event emits an INFO record with event_type set to verb.
func (l *Logger) Event(verb, msg string, fields ...any) {
	args := append([]any{"event_type", verb}, fields...)
	l.slog().Info(msg, l.argsWithAutoSource(args)...)
}

// DHF-REQ: keel/requirement-20
func (l *Logger) argsWithAutoSource(args []any) []any {
	if l == nil || !l.sourceInFiles {
		return args
	}
	_, file, line, ok := runtime.Caller(2)
	if !ok || file == "" {
		return args
	}
	out := make([]any, 0, len(args)+2)
	out = append(out, args...)
	out = append(out, slog.SourceKey, filepath.Base(file)+":"+fmt.Sprintf("%d", line))
	return out
}

// Header emits a ruled human-mode banner.
func (l *Logger) Header(title string, version string) { Header(l.slog(), title, version) }

// Section emits a ruled human-mode section header.
func (l *Logger) Section(name string) { Section(l.slog(), name) }

// Field emits one aligned label/value row.
func (l *Logger) Field(label string, value any) { Field(l.slog(), label, value) }

// Close releases file sinks opened by New.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	var err error
	for _, c := range l.closers {
		err = errors.Join(err, c.Close())
	}
	l.closers = nil
	return err
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
	r = redactRecord(r)
	var err error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			err = errors.Join(err, handler.Handle(ctx, r))
		}
	}
	return err
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	attrs = redactAttrs(attrs)
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

// DHF-REQ: keel/requirement-16, keel/requirement-22
func redactRecord(r slog.Record) slog.Record {
	r = r.Clone()
	r.Message = RedactString(r.Message)
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, redactAttr(a))
		return true
	})
	r = slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.AddAttrs(attrs...)
	return r
}

func redactAttrs(attrs []slog.Attr) []slog.Attr {
	if len(attrs) == 0 {
		return attrs
	}
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, redactAttr(attr))
	}
	return redacted
}

func redactAttr(a slog.Attr) slog.Attr {
	a.Value = redactValue(a.Key, a.Value.Resolve())
	return a
}

func redactValue(key string, v slog.Value) slog.Value {
	if v.Kind() == slog.KindString && isSensitiveAttrKey(key) {
		return slog.StringValue("[REDACTED]")
	}
	if v.Kind() == slog.KindString {
		return slog.StringValue(RedactString(v.String()))
	}
	if v.Kind() == slog.KindGroup {
		attrs := v.Group()
		for i := range attrs {
			attrs[i] = redactAttr(attrs[i])
		}
		return slog.GroupValue(attrs...)
	}
	return v
}

type sparseAIHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newSparseAIHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return &sparseAIHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *sparseAIHandler) Enabled(_ context.Context, level slog.Level) bool {
	min := slog.LevelInfo
	if h.level != nil {
		min = h.level.Level()
	}
	return level >= min
}

// DHF-REQ: keel/requirement-17, keel/requirement-20
func (h *sparseAIHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	event := "log"
	fields := make(map[string]any)
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || attr.Key == "module" {
			continue
		}
		if attr.Key == "banner" && event == "log" {
			if attr.Value.Kind() == slog.KindString && strings.TrimSpace(attr.Value.String()) != "" {
				event = attr.Value.String()
			}
			continue
		}
		if attr.Key == "event_type" {
			if attr.Value.Kind() == slog.KindString && strings.TrimSpace(attr.Value.String()) != "" {
				event = attr.Value.String()
			}
			continue
		}
		fields[attr.Key] = sparseFieldValue(attr.Value)
	}

	payload := struct {
		Level   string         `json:"level"`
		Event   string         `json:"event"`
		Message string         `json:"message"`
		Fields  map[string]any `json:"fields"`
	}{
		Level:   r.Level.String(),
		Event:   event,
		Message: RedactString(r.Message),
		Fields:  fields,
	}

	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(b.Bytes())
	return err
}

func (h *sparseAIHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = slicesClone(h.attrs)
	for _, attr := range attrs {
		attr = replaceForOpenBrain(h.groups, attr)
		if attr.Equal(slog.Attr{}) || attr.Key == "" {
			continue
		}
		next.attrs = append(next.attrs, attr)
	}
	return next
}

func (h *sparseAIHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.groups = append(slicesClone(h.groups), name)
	return next
}

func (h *sparseAIHandler) clone() *sparseAIHandler {
	return &sparseAIHandler{
		mu:     h.mu,
		w:      h.w,
		level:  h.level,
		attrs:  h.attrs,
		groups: h.groups,
	}
}

func sparseFieldValue(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return RedactString(v.String())
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindGroup:
		attrs := v.Group()
		out := make(map[string]any, len(attrs))
		for _, attr := range attrs {
			attr = replaceForOpenBrain(nil, attr)
			if attr.Equal(slog.Attr{}) || attr.Key == "" {
				continue
			}
			out[attr.Key] = sparseFieldValue(attr.Value)
		}
		return out
	default:
		return RedactString(fmt.Sprint(v.Any()))
	}
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

// DHF-REQ: openbrain/requirement-151, openbrain/requirement-152, keel/requirement-20
func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	boundAttrs := slicesClone(h.attrs)
	recordAttrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})

	var b strings.Builder
	if h.writeBanner(&b, r, boundAttrs, recordAttrs) {
		h.mu.Lock()
		defer h.mu.Unlock()
		_, err := io.WriteString(h.w, b.String())
		return err
	}
	writeConsoleTimestamp(&b, r.Time, h.color)
	b.WriteByte(' ')
	writeConsoleLevel(&b, r.Level, h.color)
	b.WriteString("  ")
	message, skipKeys := h.consoleMessage(r.Message, boundAttrs, recordAttrs)
	b.WriteString(RedactString(message))
	for _, attr := range boundAttrs {
		attr = replaceForOpenBrain(h.groups, attr)
		key := attr.Key
		if attr.Equal(slog.Attr{}) || attr.Key == "" || isContextKey(key) || h.omits(key) || skipKeys[key] {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatConsoleValue(attr.Value))
	}
	for _, attr := range recordAttrs {
		attr = replaceForOpenBrain(h.groups, attr)
		key := groupedKey(h.groups, attr.Key)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || isContextKey(attr.Key) || h.omits(key) || skipKeys[key] {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatConsoleValue(attr.Value))
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *consoleHandler) writeBanner(b *strings.Builder, r slog.Record, boundAttrs []slog.Attr, recordAttrs []slog.Attr) bool {
	banner := bannerAttr(h.groups, boundAttrs, recordAttrs)
	switch banner {
	case "header":
		title := bannerText("title", h.groups, boundAttrs, recordAttrs)
		version := bannerText("version", h.groups, boundAttrs, recordAttrs)
		if version != "" {
			title = strings.TrimSpace(title + " " + version)
		}
		writeConsoleBannerLine(b, r.Time, r.Level, h.color, strings.Repeat("=", ruleWidth))
		writeConsoleBannerLine(b, r.Time, r.Level, h.color, title)
		writeConsoleBannerLine(b, r.Time, r.Level, h.color, strings.Repeat("=", ruleWidth))
		return true
	case "section":
		name := bannerText("name", h.groups, boundAttrs, recordAttrs)
		writeConsoleBannerLine(b, r.Time, r.Level, h.color, strings.Repeat("-", ruleWidth))
		writeConsoleBannerLine(b, r.Time, r.Level, h.color, name)
		return true
	default:
		return false
	}
}

func writeConsoleBannerLine(b *strings.Builder, t time.Time, level slog.Level, color bool, msg string) {
	writeConsoleTimestamp(b, t, color)
	b.WriteByte(' ')
	writeConsoleLevel(b, level, color)
	b.WriteString("  ")
	b.WriteString(RedactString(msg))
	b.WriteByte('\n')
}

func bannerAttr(groups []string, boundAttrs []slog.Attr, recordAttrs []slog.Attr) string {
	for _, attrs := range [][]slog.Attr{boundAttrs, recordAttrs} {
		for _, attr := range attrs {
			attr = replaceForOpenBrain(groups, attr)
			if attr.Key == "banner" && attr.Value.Kind() == slog.KindString {
				return attr.Value.String()
			}
		}
	}
	return ""
}

func bannerText(key string, groups []string, boundAttrs []slog.Attr, recordAttrs []slog.Attr) string {
	for _, attrs := range [][]slog.Attr{boundAttrs, recordAttrs} {
		for _, attr := range attrs {
			attr = replaceForOpenBrain(groups, attr)
			if attr.Key == key && attr.Value.Kind() == slog.KindString {
				return attr.Value.String()
			}
		}
	}
	return ""
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = slicesClone(h.attrs)
	for _, attr := range attrs {
		attr = replaceForOpenBrain(nil, attr)
		attr.Key = groupedKey(h.groups, attr.Key)
		key := attr.Key
		if attr.Equal(slog.Attr{}) || attr.Key == "" || isContextKey(attr.Key) || next.omits(key) {
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

func (h *consoleHandler) consoleMessage(msg string, boundAttrs []slog.Attr, recordAttrs []slog.Attr) (string, map[string]bool) {
	skip := map[string]bool{}
	progressName, ok := strings.CutSuffix(msg, " progress")
	if !ok || strings.TrimSpace(progressName) == "" {
		return msg, skip
	}
	detail := ""
	for _, attr := range boundAttrs {
		attr = replaceForOpenBrain(nil, attr)
		key := attr.Key
		switch attr.Key {
		case "detail":
			if attr.Value.Kind() == slog.KindString {
				detail = attr.Value.String()
			} else {
				detail = formatConsoleValue(attr.Value)
			}
			skip[key] = true
		case "event_type":
			skip[key] = true
		}
	}
	for _, attr := range recordAttrs {
		attr = replaceForOpenBrain(h.groups, attr)
		key := groupedKey(h.groups, attr.Key)
		switch attr.Key {
		case "detail":
			if attr.Value.Kind() == slog.KindString {
				detail = attr.Value.String()
			} else {
				detail = formatConsoleValue(attr.Value)
			}
			skip[key] = true
		case "event_type":
			skip[key] = true
		}
	}
	if strings.TrimSpace(detail) == "" {
		return msg, skip
	}
	return progressName + " detail: " + detail, skip
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

// DHF-REQ: keel/requirement-20
func consoleLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN"
	case level <= slog.LevelDebug:
		return "DEBUG"
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
	source bool
}

// NewJSONFileHandler opens today's JSON Lines daily log file under dir and
// returns a handler (DEBUG and above) that appends to it. The returned handler
// also satisfies io.Closer; callers own it for the invocation lifetime.
//
// DHF-REQ: openbrain/change_request-441, keel/requirement-20
func NewJSONFileHandler(dir string, service string) (slog.Handler, error) {
	h, _, _, err := newJSONFileHandler(dir, service, false, false)
	return h, err
}

func newJSONFileHandler(dir string, service string, perRun bool, source bool) (slog.Handler, string, *lineCountingWriteCloser, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, "", nil, err
	}
	path := JSONLogPath(dir, service)
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if perRun {
		path = PerRunJSONLogPath(dir)
		flag = os.O_CREATE | os.O_WRONLY | os.O_EXCL
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return nil, "", nil, err
	}
	w := &lineCountingWriteCloser{WriteCloser: f}
	return &jsonFileHandler{
		Handler: slog.NewJSONHandler(w, &slog.HandlerOptions{
			AddSource:   source,
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceForOpenBrainFile,
		}),
		close: w.Close,
	}, path, w, nil
}

type lineCountingWriteCloser struct {
	io.WriteCloser
	mu    sync.Mutex
	lines int
}

// DHF-REQ: keel/requirement-19
func (w *lineCountingWriteCloser) Write(p []byte) (int, error) {
	n, err := w.WriteCloser.Write(p)
	w.mu.Lock()
	w.lines += bytes.Count(p[:n], []byte{'\n'})
	w.mu.Unlock()
	return n, err
}

func (w *lineCountingWriteCloser) Line() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lines
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
// per log record. Prefer Config.TextDir with New for production construction.
//
// DHF-REQ: openbrain/requirement-152, keel/requirement-20
func NewHumanFileHandler(dir string, service string) (slog.Handler, error) {
	return newHumanFileHandler(dir, service, false)
}

func newHumanFileHandler(dir string, service string, source bool) (slog.Handler, error) {
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
	return &humanFileHandler{mu: &sync.Mutex{}, w: f, source: source}, nil
}

func (h *humanFileHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

// DHF-REQ: openbrain/requirement-152, keel/requirement-20
func (h *humanFileHandler) Handle(_ context.Context, r slog.Record) error {
	boundAttrs := slicesClone(h.attrs)
	recordAttrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})
	source := sourceFromAttrs(nil, boundAttrs)
	if source == "" {
		source = sourceFromAttrs(h.groups, recordAttrs)
	}
	if source == "" && h.source {
		source = sourceFromPC(r.PC)
	}
	if source == "" {
		source = serviceFromAttrs(boundAttrs)
		if source == "" {
			source = serviceFromAttrs(recordAttrs)
		}
	}

	var b strings.Builder
	b.WriteString(r.Time.Format("2006-01-02 15:04:05.000"))
	b.WriteByte('\t')
	b.WriteString(consoleLevel(r.Level))
	b.WriteByte('\t')
	b.WriteString(fmt.Sprintf("%-26s", source))
	b.WriteByte('\t')
	b.WriteString(RedactString(r.Message))
	for _, attr := range boundAttrs {
		attr = replaceForOpenBrain(nil, attr)
		key := attr.Key
		if attr.Equal(slog.Attr{}) || attr.Key == "" || attr.Key == "service" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(formatConsoleValue(attr.Value))
	}
	for _, attr := range recordAttrs {
		attr = replaceForOpenBrain(h.groups, attr)
		key := groupedKey(h.groups, attr.Key)
		if attr.Equal(slog.Attr{}) || attr.Key == "" || attr.Key == "service" {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(key)
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
		attr = replaceForOpenBrain(nil, attr)
		attr.Key = groupedKey(h.groups, attr.Key)
		next.attrs = append(next.attrs, attr)
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
// DHF-REQ: openbrain/requirement-152, keel/requirement-20
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
		source: h.source,
	}
}

// DHF-REQ: keel/requirement-20
func sourceFromAttrs(groups []string, attrs []slog.Attr) string {
	for _, attr := range attrs {
		attr.Value = attr.Value.Resolve()
		if isContextKey(attr.Key) && attr.Value.Kind() == slog.KindString {
			return attr.Value.String()
		}
		if attr.Value.Kind() == slog.KindGroup {
			if source := sourceFromAttrs(append(slicesClone(groups), attr.Key), attr.Value.Group()); source != "" {
				return source
			}
		}
	}
	return ""
}

func serviceFromAttrs(attrs []slog.Attr) string {
	for _, attr := range attrs {
		attr = replaceForOpenBrain(nil, attr)
		if attr.Key == "service" && attr.Value.Kind() == slog.KindString {
			return attr.Value.String()
		}
	}
	return ""
}

func sourceFromPC(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frames := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frames.Next()
	if frame.File == "" {
		return ""
	}
	return filepath.Base(frame.File) + ":" + fmt.Sprintf("%d", frame.Line)
}

// DHF-REQ: openbrain/requirement-152, keel/requirement-20
// HumanLogPath returns today's Serilog-style daily human log path for service.
func HumanLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".log")
}

// DHF-REQ: openbrain/change_request-441, keel/requirement-20
// JSONLogPath returns today's JSON Lines daily log path for service.
func JSONLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".jsonl")
}

// PerRunJSONLogPath returns a new per-invocation JSON Lines log path under dir.
//
// DHF-REQ: keel/requirement-19, keel/requirement-20
func PerRunJSONLogPath(dir string) string {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	return filepath.Join(dir, stamp+"-"+fmt.Sprintf("%d", os.Getpid())+".jsonl")
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

// FieldRow is one aligned label/value row rendered by [Fields].
type FieldRow struct {
	// Label is the left-hand column text; the widest Label in a batch sets the
	// alignment width for all rows.
	Label string
	// Value is the right-hand value, rendered with fmt's default %v verb.
	Value any
}

// DHF-REQ: openbrain/requirement-151, keel/requirement-24
// Header logs a structured banner through the provided logger.
func Header(logger *slog.Logger, title string, version string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(title, "banner", "header", "title", title, "version", version)
}

// DHF-REQ: openbrain/requirement-151, keel/requirement-24
// Section logs a structured section banner through the provided logger.
func Section(logger *slog.Logger, name string) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(name, "banner", "section", "name", name)
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

// Write appends raw handler output to the capture buffer. It is the
// [io.Writer] the JSON handler emits into; it is safe for concurrent use.
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

// newForTesting creates a logger that writes to a RecordCapture buffer,
// using the same G1 JSON handler chain as production. The log level is
// set to LevelDebug so all levels are captured. Tests MUST NOT call
// slog.SetDefault with the returned logger.
func newForTesting(service string) (*slog.Logger, *RecordCapture) {
	rc := &RecordCapture{}
	h := slog.NewJSONHandler(rc, &slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceForOpenBrain,
	})
	return slog.New(h).With("service", service), rc
}

// newConsoleForTesting creates a console logger that writes to a RecordCapture
// buffer using the same human console handler chain as production.
//
// DHF-REQ: keel/requirement-5, openbrain/requirement-151, openbrain/requirement-152
func newConsoleForTesting(service string) (*slog.Logger, *RecordCapture) {
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
