package vscode

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// runEventSources is the closed run-event source value set. It carries two
// axes, which is the cost design_decision-14 accepted rather than designed
// around: "vscode" and "external" name which producer normalized the events,
// and "editor" names the surface that initiated the run. A bridge run that
// declares no initiating surface keeps stamping "vscode", so a stream written
// by an older producer stays importable.
//
// DHF-REQ: keel/requirement-36
var runEventSources = map[string]struct{}{
	"vscode":   {},
	"external": {},
	"editor":   {},
}

// IsKnownRunEventSource reports whether source is one of the closed run-event
// source values. It is the single producer-side answer to that question, so a
// validator elsewhere in the module cannot drift from the stamper's value set.
//
// DHF-REQ: keel/requirement-36
func IsKnownRunEventSource(source string) bool {
	_, ok := runEventSources[source]
	return ok
}

var artifactKinds = map[string]struct{}{
	"log":        {},
	"trace":      {},
	"screenshot": {},
	"video":      {},
	"coverage":   {},
	"report":     {},
	"other":      {},
}

// EventStamper applies producer-owned invariant fields and validates the
// value-level constraints the stdlib drift test intentionally does not cover:
// version const, non-empty enum values, non-negative durations, date-time
// presence through the injected clock, and artifact URI safety.
//
// DHF-REQ: keel/requirement-23, keel/requirement-34
type EventStamper struct {
	Now       func() time.Time
	RunID     string
	Source    string
	Workspace string
	Logf      func(string)
}

// Stamp returns event with version/time/run/source/workspace applied. Invalid
// producer events are demoted to an output event and logged when Logf is set.
func (s EventStamper) Stamp(event RunEvent) RunEvent {
	if !IsKnownRunEvent(event.Event) {
		return s.invalid(event, fmt.Sprintf("invalid run event %q", event.Event))
	}
	if event.DurationMS < 0 {
		return s.invalid(event, "duration_ms must be non-negative")
	}
	if event.Artifact != nil {
		if _, ok := artifactKinds[event.Artifact.Kind]; !ok {
			return s.invalid(event, fmt.Sprintf("invalid artifact kind %q", event.Artifact.Kind))
		}
	}

	event.Version = 1
	if s.Now != nil {
		event.Time = s.Now().UTC()
	} else {
		event.Time = time.Now().UTC()
	}
	if s.RunID != "" {
		event.RunID = s.RunID
	}
	if s.Source != "" {
		event.Source = s.Source
	}
	if s.Workspace != "" {
		event.Workspace = s.Workspace
	}
	if _, ok := runEventSources[event.Source]; event.Source != "" && !ok {
		return s.invalid(event, fmt.Sprintf("invalid run-event source %q", event.Source))
	}
	return event
}

func (s EventStamper) invalid(event RunEvent, message string) RunEvent {
	if s.Logf != nil {
		s.Logf(message)
	}
	out := RunEvent{Event: "output", Message: message}
	out.Version = 1
	if s.Now != nil {
		out.Time = s.Now().UTC()
	} else {
		out.Time = time.Now().UTC()
	}
	out.RunID = s.RunID
	out.Source = recoverySource(s.Source)
	out.Workspace = s.Workspace
	if event.TestID != "" {
		out.TestID = event.TestID
	}
	return out
}

func recoverySource(source string) string {
	if source == "" {
		return ""
	}
	if _, ok := runEventSources[source]; ok {
		return source
	}
	return "vscode"
}

func sanitizeArtifactURI(workspace string, artifact *RunArtifact) (RunArtifact, bool, string) {
	if artifact == nil {
		return RunArtifact{}, true, ""
	}
	out := *artifact
	parsed, err := url.Parse(out.URI)
	if err != nil {
		return out, false, "artifact.uri is not parseable"
	}
	if parsed.Scheme != "file" {
		return out, false, "artifact.uri must use file scheme"
	}
	path := parsed.Path
	if path == "" {
		return out, false, "artifact.uri file path is empty"
	}
	cleanWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return out, false, "workspace path is not absolute"
	}
	cleanPath, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return out, false, "artifact.uri path is not absolute"
	}
	rel, err := filepath.Rel(cleanWorkspace, cleanPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return out, false, "artifact.uri points outside workspace"
	}
	return out, true, ""
}
