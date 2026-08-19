package vscode

import "testing"

func int64Ptr(v int64) *int64 { return &v }

// TestRenderDescriptionFixedClassOrderAndSeparator holds keel/ac-554: an item
// carrying producer prose, a typed last run, typed desired-state facts and a
// warning-severity finding renders those four classes in the order the
// renderer declares, joined by the one separator it declares — and reordering
// the fields of the received item changes nothing, because the renderer reads
// fields by name rather than by arrival.
//
// DHF-TEST: keel/requirement-139
func TestRenderDescriptionFixedClassOrderAndSeparator(t *testing.T) {
	item := TestItem{
		ID:          "keel::lane::a",
		Description: "the lane prose",
		LastRun:     &LastRunFacts{DurationMS: int64Ptr(9800)},
		DesiredStateRow: &DesiredStateRowFacts{
			Current: "empty",
			Action:  "reconcile",
			Active:  true,
		},
		Findings: []Finding{{Rule: "lane-order", Severity: "warning", Message: "order drifted"}},
	}

	got := RenderDescription(item, DefaultDisplayConfig())
	want := "the lane prose; · last 9.8s; current=empty; action=reconcile; active=true; lane-order warning: order drifted"
	if got != want {
		t.Fatalf("RenderDescription()\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderDescriptionToggleSuppressesExactlyItsClass holds keel/ac-555:
// clearing exactly one display toggle removes that class from the rendered
// string and leaves every other class byte-identical to the all-enabled
// render.
//
// DHF-TEST: keel/requirement-139
func TestRenderDescriptionToggleSuppressesExactlyItsClass(t *testing.T) {
	item := TestItem{
		ID:              "keel::lane::a",
		Description:     "the lane prose",
		LastRun:         &LastRunFacts{DurationMS: int64Ptr(9800)},
		DesiredStateRow: &DesiredStateRowFacts{Current: "empty", Action: "reconcile", Active: true},
		Findings:        []Finding{{Rule: "lane-order", Severity: "warning", Message: "order drifted"}},
	}

	for _, tc := range []struct {
		name    string
		display DisplayConfig
		want    string
	}{
		{
			name:    "description off",
			display: DisplayConfig{LastRun: true, DesiredState: true, Findings: true},
			want:    "· last 9.8s; current=empty; action=reconcile; active=true; lane-order warning: order drifted",
		},
		{
			name:    "last run off",
			display: DisplayConfig{Description: true, DesiredState: true, Findings: true},
			want:    "the lane prose; current=empty; action=reconcile; active=true; lane-order warning: order drifted",
		},
		{
			name:    "desired state off",
			display: DisplayConfig{Description: true, LastRun: true, Findings: true},
			want:    "the lane prose; · last 9.8s; lane-order warning: order drifted",
		},
		{
			name:    "findings off",
			display: DisplayConfig{Description: true, LastRun: true, DesiredState: true},
			want:    "the lane prose; · last 9.8s; current=empty; action=reconcile; active=true",
		},
		{
			name:    "every class off",
			display: DisplayConfig{},
			want:    "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderDescription(item, tc.display); got != tc.want {
				t.Fatalf("RenderDescription()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestRenderDescriptionIsProducerIndependent holds keel/ac-553: two discovery
// items carrying byte-identical typed facts and prose render a byte-identical
// description, whatever else distinguishes the producers that sent them.
//
// DHF-TEST: keel/requirement-139
func TestRenderDescriptionIsProducerIndependent(t *testing.T) {
	facts := func(id string, limitations []string) TestItem {
		return TestItem{
			ID:              id,
			Label:           "shared label",
			Limitations:     limitations,
			Description:     "the same prose",
			LastRun:         &LastRunFacts{DurationMS: int64Ptr(1500)},
			DesiredStateRow: &DesiredStateRowFacts{Current: "running", Action: "reuse", Active: true},
			Findings:        []Finding{{Rule: "r", Severity: "error", Message: "m"}},
		}
	}

	fromKeelDev := RenderDescription(facts("keel::lane::a", []string{"the same prose", "· last 1.5s"}), DefaultDisplayConfig())
	fromOpenbrainDev := RenderDescription(facts("openbrain::lane::a", nil), DefaultDisplayConfig())
	if fromKeelDev != fromOpenbrainDev {
		t.Fatalf("producers rendered differently\n keel-dev: %q\n openbrain-dev: %q", fromKeelDev, fromOpenbrainDev)
	}
	if fromKeelDev == "" {
		t.Fatal("RenderDescription() rendered nothing for an item carrying every fact class")
	}
}

// TestFormatLastRunOmitsAnAbsentMeasurement holds the absence half of
// keel/ac-554: a missing measurement contributes no segment at all rather than
// a zero-valued one, so the separator never leads or doubles.
//
// DHF-TEST: keel/requirement-139
func TestFormatLastRunOmitsAnAbsentMeasurement(t *testing.T) {
	for _, tc := range []struct {
		name string
		last *LastRunFacts
		want string
	}{
		{name: "no last run at all", last: nil, want: ""},
		{name: "run without a measured duration", last: &LastRunFacts{}, want: ""},
		{name: "negative duration", last: &LastRunFacts{DurationMS: int64Ptr(-1)}, want: ""},
		{name: "measured zero", last: &LastRunFacts{DurationMS: int64Ptr(0)}, want: "· last 0.0s"},
		{name: "sub-minute", last: &LastRunFacts{DurationMS: int64Ptr(9800)}, want: "· last 9.8s"},
		{name: "at the ninety-second boundary", last: &LastRunFacts{DurationMS: int64Ptr(90000)}, want: "· last 90.0s"},
		{name: "over the boundary", last: &LastRunFacts{DurationMS: int64Ptr(192000)}, want: "· last 3m 12s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatLastRun(tc.last); got != tc.want {
				t.Fatalf("FormatLastRun()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestRenderDescriptionOfAnItemWithoutFactsIsEmpty holds the consumer-side
// fallback precondition: an item carrying no typed fact renders nothing, so a
// consumer can tell "the renderer had nothing to say" from "the renderer said
// something short" without inspecting the item itself.
//
// DHF-TEST: keel/requirement-139
func TestRenderDescriptionOfAnItemWithoutFactsIsEmpty(t *testing.T) {
	item := TestItem{ID: "openbrain::lane::a", Limitations: []string{"current=empty", "action=reconcile"}}
	if got := RenderDescription(item, DefaultDisplayConfig()); got != "" {
		t.Fatalf("RenderDescription() of an item carrying no facts = %q, want empty", got)
	}
	if HasRenderableFacts(item) {
		t.Fatal("HasRenderableFacts() reported facts on an item carrying only limitations")
	}
}
