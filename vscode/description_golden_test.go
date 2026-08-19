package vscode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenFixturePath is the committed fixture both renderer copies are pinned
// to. It lives at the repository root rather than under either implementation
// so that neither side owns it.
const goldenFixturePath = "../testdata/description-golden.json"

type descriptionGoldenFixture struct {
	Version    int                       `json:"version"`
	Separator  string                    `json:"separator"`
	ClassOrder []DisplayClass            `json:"class_order"`
	Cases      []descriptionGoldenCase   `json:"cases"`
	Defaults   descriptionGoldenDefaults `json:"defaults"`
}

type descriptionGoldenCase struct {
	Name     string        `json:"name"`
	Item     TestItem      `json:"item"`
	Display  DisplayConfig `json:"display"`
	Expected string        `json:"expected"`
	HasFacts bool          `json:"has_facts"`
}

type descriptionGoldenDefaults struct {
	Display DisplayConfig `json:"display"`
}

func readDescriptionGoldenFixture(t *testing.T) descriptionGoldenFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(goldenFixturePath))
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fixture descriptionGoldenFixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	return fixture
}

// TestDescriptionGoldenFixturePinsTheGoRenderer holds the Go half of
// keel/ac-561: every committed case renders to its expected string through the
// canonical renderer, so a change to the Go renderer alone reds this gate. The
// VSIX suite asserts the same file, which is what makes the second copy pinned
// rather than accidental.
//
// DHF-TEST: keel/requirement-139
func TestDescriptionGoldenFixturePinsTheGoRenderer(t *testing.T) {
	fixture := readDescriptionGoldenFixture(t)
	if fixture.Version != 1 {
		t.Fatalf("golden fixture version = %d, want 1", fixture.Version)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("golden fixture carries no cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := RenderDescription(tc.Item, tc.Display); got != tc.Expected {
				t.Fatalf("RenderDescription()\n got: %q\nwant: %q", got, tc.Expected)
			}
			if got := HasRenderableFacts(tc.Item); got != tc.HasFacts {
				t.Fatalf("HasRenderableFacts() = %t, want %t", got, tc.HasFacts)
			}
		})
	}
}

// TestDescriptionGoldenFixtureDeclaresTheSameContractAsTheRenderer keeps the
// fixture's own declarations — the separator, the class order, the defaults —
// equal to the package's. Without this, an implementation and a fixture could
// drift together and still agree with each other.
//
// DHF-TEST: keel/requirement-139
func TestDescriptionGoldenFixtureDeclaresTheSameContractAsTheRenderer(t *testing.T) {
	fixture := readDescriptionGoldenFixture(t)
	if fixture.Separator != DescriptionSeparator {
		t.Fatalf("fixture separator = %q, want %q", fixture.Separator, DescriptionSeparator)
	}
	if len(fixture.ClassOrder) != len(DisplayClassOrder) {
		t.Fatalf("fixture class order = %v, want %v", fixture.ClassOrder, DisplayClassOrder)
	}
	for i, class := range DisplayClassOrder {
		if fixture.ClassOrder[i] != class {
			t.Fatalf("fixture class order = %v, want %v", fixture.ClassOrder, DisplayClassOrder)
		}
	}
	if fixture.Defaults.Display != DefaultDisplayConfig() {
		t.Fatalf("fixture default display = %+v, want %+v", fixture.Defaults.Display, DefaultDisplayConfig())
	}
}

// TestDescriptionGoldenFixtureCoversEveryToggleShape keeps the fixture honest
// about its own coverage: every class on, every class off, and each class
// alone. A fixture that stops covering a class stops pinning it.
//
// DHF-TEST: keel/requirement-139
func TestDescriptionGoldenFixtureCoversEveryToggleShape(t *testing.T) {
	fixture := readDescriptionGoldenFixture(t)
	seen := make(map[DisplayConfig]bool, len(fixture.Cases))
	for _, tc := range fixture.Cases {
		seen[tc.Display] = true
	}
	required := []DisplayConfig{DefaultDisplayConfig(), {}}
	for _, class := range DisplayClassOrder {
		alone := DisplayConfig{}
		switch class {
		case DisplayClassDescription:
			alone.Description = true
		case DisplayClassLastRun:
			alone.LastRun = true
		case DisplayClassDesiredState:
			alone.DesiredState = true
		case DisplayClassFindings:
			alone.Findings = true
		}
		required = append(required, alone)
	}
	for _, want := range required {
		if !seen[want] {
			t.Fatalf("golden fixture covers no case with display %+v", want)
		}
	}
}
