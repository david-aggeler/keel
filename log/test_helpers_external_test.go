package log_test

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type recordCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (rc *recordCapture) Write(p []byte) (int, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.buf.Write(p)
}

func (rc *recordCapture) LastJSON() map[string]any {
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

func (rc *recordCapture) AllJSON() []map[string]any {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	raw := strings.TrimSpace(rc.buf.String())
	out := make([]map[string]any, 0)
	if raw == "" {
		return out
	}
	for _, line := range strings.Split(raw, "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func (rc *recordCapture) LastRaw() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(rc.buf.String()), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func (rc *recordCapture) Reset() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.buf.Reset()
}

func textLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".log")
}

func jsonLogPath(dir string, service string) string {
	return filepath.Join(dir, safeLogService(service)+"-"+time.Now().Format("2006-01-02")+".jsonl")
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
