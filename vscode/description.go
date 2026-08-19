package vscode

import (
	"fmt"
	"strings"
)

// DescriptionSeparator joins the segments of a rendered item description. It is
// declared once, here, because a second copy of it is a second contract: the
// VSIX composer mirrors this value and the golden fixture pins the pair.
//
// DHF-REQ: keel/requirement-139
const DescriptionSeparator = "; "

// DisplayClass names one fact class the renderer may contribute a segment for.
// The declared order of the slice below is the rendered order; no producer can
// influence it.
//
// DHF-REQ: keel/requirement-139
type DisplayClass string

const (
	DisplayClassDescription  DisplayClass = "description"
	DisplayClassLastRun      DisplayClass = "lastRun"
	DisplayClassDesiredState DisplayClass = "desiredState"
	DisplayClassFindings     DisplayClass = "findings"
)

// DisplayClassOrder is the fixed sequence the classes render in. It is the
// single statement of that order on the Go side.
//
// DHF-REQ: keel/requirement-139
var DisplayClassOrder = []DisplayClass{
	DisplayClassDescription,
	DisplayClassLastRun,
	DisplayClassDesiredState,
	DisplayClassFindings,
}

// DisplayConfig carries one toggle per fact class. It is the decoded form of
// the `display` block of .vscode/test-bridge.json. A zero DisplayConfig
// suppresses every class; an absent block in the config file decodes to
// DefaultDisplayConfig, which enables every class so that upgrading a
// workspace hides nothing that was visible beforehand.
//
// DHF-REQ: keel/requirement-139
type DisplayConfig struct {
	Description  bool `json:"description"`
	LastRun      bool `json:"lastRun"`
	DesiredState bool `json:"desiredState"`
	Findings     bool `json:"findings"`
}

// DefaultDisplayConfig enables every fact class.
//
// DHF-REQ: keel/requirement-139
func DefaultDisplayConfig() DisplayConfig {
	return DisplayConfig{Description: true, LastRun: true, DesiredState: true, Findings: true}
}

// Enabled reports whether the named class renders under this configuration.
//
// DHF-REQ: keel/requirement-139
func (c DisplayConfig) Enabled(class DisplayClass) bool {
	switch class {
	case DisplayClassDescription:
		return c.Description
	case DisplayClassLastRun:
		return c.LastRun
	case DisplayClassDesiredState:
		return c.DesiredState
	case DisplayClassFindings:
		return c.Findings
	default:
		return false
	}
}

// RenderDescription is the canonical composition of an item's secondary text.
// It is the only place the class order, the separator, and each class's own
// format live on the Go side; the VSIX composer mirrors it and the committed
// golden fixture reds a gate when the two drift apart.
//
// It reads the item's fields by name, so the order the producer wrote them in
// the JSON object cannot reach the output.
//
// DHF-REQ: keel/requirement-139
func RenderDescription(item TestItem, display DisplayConfig) string {
	var segments []string
	for _, class := range DisplayClassOrder {
		if !display.Enabled(class) {
			continue
		}
		segments = append(segments, classSegments(item, class)...)
	}
	return strings.Join(segments, DescriptionSeparator)
}

// HasRenderableFacts reports whether the item carries any fact the renderer
// composes from, independent of the display configuration. A consumer uses it
// to tell an item that rendered nothing because it carries nothing from one
// that rendered nothing because every class is switched off — the first may
// fall back to the legacy prose channel, the second must not.
//
// DHF-REQ: keel/requirement-139
func HasRenderableFacts(item TestItem) bool {
	for _, class := range DisplayClassOrder {
		if len(classSegments(item, class)) > 0 {
			return true
		}
	}
	return false
}

// classSegments renders the zero or more segments one fact class contributes.
func classSegments(item TestItem, class DisplayClass) []string {
	switch class {
	case DisplayClassDescription:
		return nonEmpty(item.Description)
	case DisplayClassLastRun:
		return nonEmpty(FormatLastRun(item.LastRun))
	case DisplayClassDesiredState:
		return desiredStateSegments(item)
	case DisplayClassFindings:
		segments := make([]string, 0, len(item.Findings))
		for _, finding := range item.Findings {
			segments = append(segments, nonEmpty(FormatFinding(finding))...)
		}
		return segments
	default:
		return nil
	}
}

// FormatLastRun renders the measured duration of an item's newest run. An
// absent run, or a run whose duration was never measured, renders nothing at
// all — the separator must never lead on a zero standing in for "not measured".
// A measured zero is a measurement and does render.
//
// DHF-REQ: keel/requirement-139
func FormatLastRun(last *LastRunFacts) string {
	if last == nil || last.DurationMS == nil || *last.DurationMS < 0 {
		return ""
	}
	totalSeconds := float64(*last.DurationMS) / 1000
	if totalSeconds > 90 {
		seconds := int(totalSeconds + 0.5)
		return fmt.Sprintf("· last %dm %02ds", seconds/60, seconds%60)
	}
	return fmt.Sprintf("· last %.1fs", totalSeconds)
}

// FormatFinding renders one typed validation finding.
//
// DHF-REQ: keel/requirement-139
func FormatFinding(finding Finding) string {
	if finding.Rule == "" && finding.Severity == "" && finding.Message == "" {
		return ""
	}
	return finding.Rule + " " + finding.Severity + ": " + finding.Message
}

// desiredStateSegments renders the typed desired-state facts an item carries.
// Group and row facts are one class: an item is at most one of the two, so the
// single toggle governs whichever it is.
func desiredStateSegments(item TestItem) []string {
	var segments []string
	if group := item.DesiredStateGroup; group != nil && group.MutuallyExclusive {
		segments = append(segments, "mutually_exclusive=true")
	}
	if row := item.DesiredStateRow; row != nil {
		if row.Current != "" {
			segments = append(segments, "current="+row.Current)
		}
		if row.Action != "" {
			segments = append(segments, "action="+row.Action)
		}
		segments = append(segments, fmt.Sprintf("active=%t", row.Active))
	}
	return segments
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
