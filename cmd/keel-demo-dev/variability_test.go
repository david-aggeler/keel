package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/david-aggeler/keel/testbridge"
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

// dispatchDemoBridgeWithRunID dispatches in process under a caller-named run id
// and the real clock, so each run persists its own stream and the elapsed time
// between the stream's start and finish is a measurement rather than a fixture.
func dispatchDemoBridgeWithRunID(t *testing.T, root, runID string, args ...string) (string, error) {
	t.Helper()
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return runID },
	})
	err := testbridge.CommandSpec(demoBridge{}).Dispatch(ctx, args)
	return protocol.String(), err
}

// demoRunnableDesiredStateRows returns every runnable desired-state row below
// rootID. A row is identified by its typed desired_state_row facts, never by id
// shape: the enclosing group items carry no such facts and are not rows — a
// group's run is its rows' runs, so no stream is ever attributable to a group
// alone (keel/requirement-53).
func demoRunnableDesiredStateRows(items []vscode.TestItem, rootID string) []vscode.TestItem {
	within := map[string]bool{rootID: true}
	for changed := true; changed; {
		changed = false
		for _, item := range items {
			if within[item.ParentID] && !within[item.ID] {
				within[item.ID] = true
				changed = true
			}
		}
	}
	var out []vscode.TestItem
	for _, item := range items {
		if item.ID != rootID && within[item.ID] && item.Runnable && item.DesiredStateRow != nil {
			out = append(out, item)
		}
	}
	return out
}

// Every desired-state row that has been run reports its measured duration on
// the wire, so the B group renders a duration exactly as the C lanes do
// (keel/ac-579).
//
// DHF-TEST: keel/requirement-62, keel/requirement-138
func TestDemoDesiredStateRowsCarryMeasuredLastRunDuration(t *testing.T) {
	// The criterion is that a run is measured, not how long it took: the demo's
	// slow row is shortened so the gate does not wait for it (keel/ac-580).
	stubDemoSlowRunDelay(t, time.Millisecond)
	root := t.TempDir()

	rows := demoRunnableDesiredStateRows(demoDiscoveryAfterDetect(t, root).Items, idDesired)
	if len(rows) == 0 {
		t.Fatal("no runnable desired-state rows discovered")
	}
	for i, row := range rows {
		// A row whose probe reports the resource unsatisfied settles failed and
		// the dispatch returns a run error; the row still ran, which is all this
		// criterion asks of it.
		if _, err := dispatchDemoBridgeWithRunID(t, root, fmt.Sprintf("run-desired-%d", i), "test-bridge", "run", "--id", row.ID); err != nil {
			t.Logf("desired-state row %s run reported %v", row.ID, err)
		}
	}

	served := map[string]vscode.TestItem{}
	for _, item := range demoDiscoveryAfterDetect(t, root).Items {
		served[item.ID] = item
	}
	for _, row := range rows {
		item, ok := served[row.ID]
		if !ok {
			t.Fatalf("desired-state row %q disappeared from discovery after its run", row.ID)
		}
		if item.LastRun == nil || item.LastRun.DurationMS == nil {
			t.Fatalf("desired-state row %q carries no last_run.duration_ms after its run: %+v", row.ID, item.LastRun)
		}
		if *item.LastRun.DurationMS < 0 {
			t.Fatalf("desired-state row %q last_run.duration_ms = %d, want a non-negative measurement", row.ID, *item.LastRun.DurationMS)
		}
	}
}

// demoSettleGaps measures, per test id, the elapsed time between the item's
// test_started event and the event that settles it.
func demoSettleGaps(t *testing.T, events []vscode.RunEvent) map[string]time.Duration {
	t.Helper()
	started := map[string]time.Time{}
	gaps := map[string]time.Duration{}
	for _, event := range events {
		if event.TestID == "" {
			continue
		}
		switch event.Event {
		case "test_started":
			started[event.TestID] = event.Time
		case "passed", "failed", "errored", "skipped":
			at, ok := started[event.TestID]
			if !ok {
				t.Fatalf("item %q settled %q with no test_started before it", event.TestID, event.Event)
			}
			gaps[event.TestID] = event.Time.Sub(at)
		}
	}
	return gaps
}

// The shipped slow demo row and slow demo lane take at least the ten seconds
// keel/ac-580 and keel/ac-581 ask for. The delay itself is authored content, so
// the number is asserted here; that the run path applies it is asserted by the
// run tests below, which shorten it so the gate never waits for it.
//
// DHF-TEST: keel/requirement-62
func TestDemoSlowRunDelayIsAtLeastTenSeconds(t *testing.T) {
	if demoSlowRunDelayDefault < 10*time.Second {
		t.Fatalf("demo slow run delay = %s, want at least 10s", demoSlowRunDelayDefault)
	}
	if demoSlowRunDelay != demoSlowRunDelayDefault {
		t.Fatalf("demo slow run delay in force = %s, want the shipped default %s", demoSlowRunDelay, demoSlowRunDelayDefault)
	}
}

// stubDemoSlowRunDelay shortens the demo's fake work time for one test. The
// gate never runs the demo's slow row or slow lane at their shipped length
// (keel/ac-580).
func stubDemoSlowRunDelay(t *testing.T, delay time.Duration) {
	t.Helper()
	restore := demoSlowRunDelay
	demoSlowRunDelay = delay
	t.Cleanup(func() { demoSlowRunDelay = restore })
}

// Running the Test Preconditions group keeps exactly one row in flight for the
// demo's fake work time while every other row settles at once, so an operator
// can watch a transitional row instead of only its settled result
// (keel/ac-580).
//
// DHF-TEST: keel/requirement-62
func TestDemoPreconditionsGroupRunHoldsExactlyOneRowInFlight(t *testing.T) {
	const delay = 150 * time.Millisecond
	stubDemoSlowRunDelay(t, delay)
	root := t.TempDir()
	if _, err := dispatchDemoBridge(t, root, "test-bridge", "run", "--id", idDetectLanes); err != nil {
		t.Fatalf("detect-lanes dispatch: %v", err)
	}

	out, err := dispatchDemoBridgeWithRunID(t, root, "run-preconditions", "test-bridge", "run", "--id", idPreconditionsGroup)
	if err != nil {
		t.Fatalf("preconditions group run dispatch: %v\n%s", err, out)
	}
	events := decodeRunEvents(t, out)
	gaps := demoSettleGaps(t, events)
	// Selecting the group runs every row in it: the group expands to its rows
	// and each settles under its own id.
	for _, row := range demoItemsWithParent(demoDiscoveryAfterDetect(t, root).Items, idPreconditionsGroup) {
		if !row.Runnable {
			continue
		}
		if _, ok := gaps[row.ID]; !ok {
			t.Fatalf("precondition row %q did not run when its group was selected: %v", row.ID, gaps)
		}
		assertRunEvent(t, events, "passed", row.ID, "")
	}
	var slow []string
	for id, gap := range gaps {
		if gap >= delay {
			slow = append(slow, id)
			continue
		}
		if gap >= time.Second {
			t.Fatalf("precondition row %q settled after %s, want under one second", id, gap)
		}
	}
	if len(slow) != 1 {
		t.Fatalf("preconditions group held %d rows in flight for %s, want exactly 1: %v", len(slow), delay, slow)
	}
}
