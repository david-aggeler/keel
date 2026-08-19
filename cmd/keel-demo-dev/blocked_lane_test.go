package main

import (
	"testing"

	"github.com/david-aggeler/keel/vscode"
)

// TestBlockedLaneSurfacesAsAPersistentConditionAndNeverAsAFailedRun holds
// keel/ac-558: a lane whose declared prerequisite cannot be satisfied carries
// the blocking reason at discovery, where no run took place, and no `failed`
// event is ever emitted for it. A failed result is the wrong lifetime — it
// asserts an outcome for a test that never executed, and per platform fact F10
// in keel/interface_spec-7 it is restored after a window reload with no run
// behind it.
//
// DHF-TEST: keel/requirement-140
func TestBlockedLaneSurfacesAsAPersistentConditionAndNeverAsAFailedRun(t *testing.T) {
	root := t.TempDir()

	if _, err := dispatchDemoBridge(t, root, "test-bridge", "run", "--id", idDetectLanes); err != nil {
		t.Fatalf("detect-lanes dispatch: %v", err)
	}
	if _, err := dispatchDemoBridge(t, root, "test-bridge", "run", "--id", idBlockBadLane); err != nil {
		t.Fatalf("block maintenance dispatch: %v", err)
	}

	discoveryOut, err := dispatchDemoBridge(t, root, "test-bridge", "discover", "--format", "json")
	if err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, discoveryOut, &discovery)

	blocked := assertItem(t, discovery.Items, idLaneGoFail, "lane", true)
	if len(blocked.Conditions) != 1 {
		t.Fatalf("blocked lane conditions = %+v, want the blocking reason as one persistent condition (keel/ac-558)", blocked.Conditions)
	}
	if blocked.Conditions[0].Kind != "prerequisite_unsatisfied" {
		t.Fatalf("blocked lane condition kind = %q, want %q (keel/ac-558)", blocked.Conditions[0].Kind, "prerequisite_unsatisfied")
	}
	if blocked.Conditions[0].Message == "" {
		t.Fatalf("blocked lane condition carries no message (keel/ac-558)")
	}

	// The unblocked sibling proves the condition is reported because the lane
	// is blocked, not because every lane carries one.
	if ready := assertItem(t, discovery.Items, idLaneGoPass, "lane", true); len(ready.Conditions) != 0 {
		t.Fatalf("unblocked lane conditions = %+v, want none (keel/ac-558)", ready.Conditions)
	}

	// Running the blocked lane must not claim an outcome either. The run cannot
	// execute at all, which is the run-scoped surface `errored` names
	// (keel/ac-568), never `failed`.
	runOut, err := dispatchDemoBridge(t, root, "test-bridge", "run", "--id", idLaneGoFail)
	if err == nil {
		t.Fatalf("blocked lane dispatch succeeded, want RunError")
	}
	for _, event := range decodeRunEvents(t, runOut) {
		if event.Event == "failed" {
			t.Fatalf("blocked lane emitted %+v, want no failed run event for a lane that never executed (keel/ac-558)", event)
		}
	}
	assertRunEvent(t, decodeRunEvents(t, runOut), "errored", idLaneGoFail, "lane blocked")
}
