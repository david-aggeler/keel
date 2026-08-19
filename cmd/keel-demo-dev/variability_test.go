package main

import (
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// demoDiscoveryAfterDetect returns the demo's discovery document once the lanes
// file exists, which is the state every consumer-visible item is served from.
// It dispatches in process so a test observes the served document without
// paying for a build.
func demoDiscoveryAfterDetect(t *testing.T, root string) vscode.DiscoveryDocument {
	t.Helper()
	if _, err := dispatchDemoBridge(t, root, "test-bridge", "run", "--id", idDetectLanes); err != nil {
		t.Fatalf("detect-lanes dispatch: %v", err)
	}
	out, err := dispatchDemoBridge(t, root, "test-bridge", "discover", "--format", "json")
	if err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, out, &doc)
	return doc
}

func demoItemsWithParent(items []vscode.TestItem, parentID string) []vscode.TestItem {
	var out []vscode.TestItem
	for _, item := range items {
		if item.ParentID == parentID {
			out = append(out, item)
		}
	}
	return out
}

// The Fake infrastructure family holds three test items rather than one, and
// each carries its own description, so the tree shows how a framework family
// with several described members renders (keel/ac-582).
//
// DHF-TEST: keel/requirement-62
func TestDemoFakeFamilyServesThreeDescribedTestItems(t *testing.T) {
	doc := demoDiscoveryAfterDetect(t, t.TempDir())

	members := demoItemsWithParent(doc.Items, idFakeFamily)
	if len(members) != 3 {
		t.Fatalf("Fake infrastructure family holds %d items, want 3: %+v", len(members), members)
	}
	byDescription := map[string]string{}
	for _, item := range members {
		if item.Kind != "test" {
			t.Fatalf("fake family member %q kind = %q, want test", item.ID, item.Kind)
		}
		if item.Description == "" {
			t.Fatalf("fake family member %q carries no description", item.ID)
		}
		if sibling, ok := byDescription[item.Description]; ok {
			t.Fatalf("fake family members %q and %q share description %q", sibling, item.ID, item.Description)
		}
		byDescription[item.Description] = item.ID
	}
}
