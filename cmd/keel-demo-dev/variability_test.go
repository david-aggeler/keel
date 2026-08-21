package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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

// Running the slow lane holds each of its three members in flight for the
// demo's fake work time, so lane-level partial progress — some members settled
// while others still run — is observable (keel/ac-581).
//
// DHF-TEST: keel/requirement-62
// DHF-TEST: keel/requirement-99
// DHF-TEST: keel/ac-597
func TestDemoSlowLaneHoldsEachOfItsThreeMembersInFlight(t *testing.T) {
	const delay = 150 * time.Millisecond
	stubDemoSlowRunDelay(t, delay)
	root := t.TempDir()

	doc := demoDiscoveryAfterDetect(t, root)
	assertDemoItem(t, doc.Items, idLaneSlow)
	var members []vscode.TestItem
	for _, item := range doc.Items {
		if item.Kind == "test" && item.LaneID == idLaneSlow {
			members = append(members, item)
		}
	}
	if len(members) != 3 {
		t.Fatalf("slow lane holds %d members, want 3: %+v", len(members), members)
	}

	out, err := dispatchDemoBridgeWithRunID(t, root, "run-slow-lane", "test-bridge", "run", "--id", idLaneSlow)
	if err != nil {
		t.Fatalf("slow lane run dispatch: %v\n%s", err, out)
	}
	events := decodeRunEvents(t, out)
	assertRunEvent(t, events, "test_started", idLaneSlow, "")
	assertRunEvent(t, events, "passed", idLaneSlow, "")
	if got := countTerminalEvents(events, idLaneSlow); got != 1 {
		t.Fatalf("slow lane reported %d terminal events for %q, want exactly 1: %+v", got, idLaneSlow, events)
	}
	laneTerminalIndex := terminalEventIndex(events, idLaneSlow)
	gaps := demoSettleGaps(t, events)
	if _, ok := gaps[idLaneSlow]; !ok {
		t.Fatalf("slow lane id %q reported no result: %v", idLaneSlow, gaps)
	}
	for _, member := range members {
		gap, ok := gaps[member.ID]
		if !ok {
			t.Fatalf("slow lane member %q reported no result: %v", member.ID, gaps)
		}
		if gap < delay {
			t.Fatalf("slow lane member %q settled %s after it started, want at least %s", member.ID, gap, delay)
		}
		assertRunEvent(t, events, "passed", member.ID, "")
		if memberTerminalIndex := terminalEventIndex(events, member.ID); laneTerminalIndex <= memberTerminalIndex {
			t.Fatalf("slow lane terminal index = %d, want after member %q terminal index %d", laneTerminalIndex, member.ID, memberTerminalIndex)
		}
	}
	if want := len(members) + 1; len(gaps) != want {
		t.Fatalf("slow lane run reported %d results, want lane plus one per member (%d): %v", len(gaps), want, gaps)
	}
}

func countTerminalEvents(events []vscode.RunEvent, testID string) int {
	var count int
	for _, event := range events {
		if event.TestID != testID {
			continue
		}
		switch event.Event {
		case "passed", "failed", "errored", "skipped":
			count++
		}
	}
	return count
}

func terminalEventIndex(events []vscode.RunEvent, testID string) int {
	for i, event := range events {
		if event.TestID != testID {
			continue
		}
		switch event.Event {
		case "passed", "failed", "errored", "skipped":
			return i
		}
	}
	return -1
}

func assertDemoItem(t *testing.T, items []vscode.TestItem, id string) vscode.TestItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("discovery serves no item %q", id)
	return vscode.TestItem{}
}

// The demo serves a lane whose declaration fails validation on purpose, so the
// diagnostic rendering is demo content rather than an artefact of a broken
// workspace, and the lanes that validate stay discoverable and runnable
// (keel/ac-584, keel/requirement-51).
//
// DHF-TEST: keel/requirement-62, keel/requirement-51
func TestDemoServesLaneValidationDiagnosticAsDemoContent(t *testing.T) {
	doc := demoDiscoveryAfterDetect(t, t.TempDir())

	var diagnostics []vscode.TestItem
	for _, item := range demoItemsWithParent(doc.Items, idLanes) {
		if item.Kind == "lane" {
			continue
		}
		diagnostics = append(diagnostics, item)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("C - Lanes holds %d diagnostics, want exactly the deliberate one: %+v", len(diagnostics), diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Runnable {
		t.Fatalf("lane diagnostic %q is runnable; a diagnostic reports, it does not run", diagnostic.ID)
	}
	stated := diagnostic.Label + " " + diagnostic.Description
	if !strings.Contains(stated, demoBrokenLaneName) {
		t.Fatalf("lane diagnostic does not name the lane that failed validation: %q", stated)
	}
	if !strings.Contains(stated, demoLaneMembersRule) {
		t.Fatalf("lane diagnostic does not name the rule the lane failed: %q", stated)
	}

	for _, id := range []string{idLaneGoPass, idLaneGoFail, idLaneFakeSmoke, idLaneSlow} {
		lane := assertDemoItem(t, doc.Items, id)
		if lane.Kind != "lane" || !lane.Runnable {
			t.Fatalf("lane %q kind = %q runnable = %v after a sibling failed validation, want a runnable lane", id, lane.Kind, lane.Runnable)
		}
	}
}

// Every runnable lane and test item the demo serves carries a description of its
// own, and none crowds the label it follows: forty characters is the bound
// (keel/ac-583). The bound governs the authored description only, never the
// secondary text a consumer composes from several fact classes.
//
// DHF-TEST: keel/requirement-62, keel/requirement-138
func TestDemoRunnableItemDescriptionsAreShortAndPresent(t *testing.T) {
	doc := demoDiscoveryAfterDetect(t, t.TempDir())

	bounded := 0
	for _, item := range doc.Items {
		if !item.Runnable || (item.Kind != "lane" && item.Kind != "test") {
			continue
		}
		bounded++
		if item.Description == "" {
			t.Errorf("%s item %q carries no description", item.Kind, item.ID)
			continue
		}
		if len(item.Description) > demoDescriptionLimit {
			t.Errorf("%s item %q description is %d characters, want at most %d: %q", item.Kind, item.ID, len(item.Description), demoDescriptionLimit, item.Description)
		}
	}
	if bounded == 0 {
		t.Fatal("discovery serves no runnable lane or test items to bound")
	}
}
