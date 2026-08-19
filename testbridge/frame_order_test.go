package testbridge_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// TestDiscoverOrdersTheTopLevelFrameWhateverTheProducerEmits holds
// keel/ac-563. Registration order is normative for a producer's own children,
// but the four top-level groups are keel's frame: the producer below emits them
// D, C, B and the bridge appends A last, so nothing but the frame rule can put
// the document's roots in A, B, C, D order.
//
// DHF-TEST: keel/requirement-137
func TestDiscoverOrdersTheTopLevelFrameWhateverTheProducerEmits(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{ID: "demo::frameworks", Label: "D - Frameworks", Kind: "group", Profiles: []string{}},
		{ID: "demo::lanes", Label: "C - Lanes", Kind: "group", Profiles: []string{}},
		desiredStateGroupItem(),
	}

	var out bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &out})
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &out, &doc)

	var roots []string
	for _, item := range doc.Items {
		if item.ParentID == "" {
			roots = append(roots, item.ID)
		}
	}
	want := []string{
		testbridge.MaintenanceGroupID,
		fixtureAnchorID(),
		"demo::lanes",
		"demo::frameworks",
	}
	if len(roots) < len(want) {
		t.Fatalf("document roots = %v, want the four-group frame first", roots)
	}
	for i, id := range want {
		if roots[i] != id {
			t.Fatalf("root %d = %q, want %q; roots = %v", i, roots[i], id, roots)
		}
	}
}
