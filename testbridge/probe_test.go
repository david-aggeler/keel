package testbridge_test

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// countingDesiredStateRow declares a row whose probe counts its own invocations
// and reports whatever current() observes at invocation time.
func countingDesiredStateRow(runID, resource string, count *atomic.Int64, current func() string) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     "tool",
		Desired:  "present",
		Reusable: true,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			count.Add(1)
			value := current()
			return testbridge.DesiredStateProbeResult{
				Current:   value,
				Satisfied: value == "present",
				Message:   resource + " is " + value,
			}
		},
	}
}

// blockingDesiredStateRow declares a row whose probe blocks until release is
// closed — the probe that ignores its context, which is exactly the probe the
// deadline must survive.
func blockingDesiredStateRow(runID, resource string, release <-chan struct{}) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     "service",
		Desired:  "ready",
		Owned:    true,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			<-release
			return testbridge.DesiredStateProbeResult{Current: "ready", Satisfied: true, Message: resource + " ready"}
		},
	}
}

func probeBridgeRuntime(root string, protocol *bytes.Buffer, deadline time.Duration) context.Context {
	return testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:          root,
		Protocol:      protocol,
		ProbeDeadline: deadline,
	})
}

func desiredStateDocumentRow(t *testing.T, doc vscode.DesiredStateDocument, resource string) vscode.DesiredState {
	t.Helper()
	for _, group := range doc.Groups {
		for _, row := range group.Rows {
			if row.Resource == resource {
				return row
			}
		}
	}
	t.Fatalf("desired-state document has no row for %q: %+v", resource, doc.Groups)
	return vscode.DesiredState{}
}

// DHF-TEST: keel/requirement-129
func TestDiscoverExecutesEachDesiredStateProbeOnce(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	var dbCount, toolCount atomic.Int64
	present := func() string { return "present" }
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Test Preconditions",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			countingDesiredStateRow("demo::desired-state::db", "db", &dbCount, present),
			countingDesiredStateRow("demo::desired-state::tool", "tool", &toolCount, present),
		},
	}}
	var protocol bytes.Buffer
	ctx := probeBridgeRuntime(root, &protocol, 0)

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}

	if got := dbCount.Load(); got != 1 {
		t.Fatalf("db probe invocations = %d, want exactly 1 per discover", got)
	}
	if got := toolCount.Load(); got != 1 {
		t.Fatalf("tool probe invocations = %d, want exactly 1 per discover", got)
	}
}

// DHF-TEST: keel/requirement-129
func TestGroupSelectedDesiredStateExecutesEachProbeOnce(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	var dbCount, toolCount atomic.Int64
	present := func() string { return "present" }
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Test Preconditions",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			countingDesiredStateRow("demo::desired-state::db", "db", &dbCount, present),
			countingDesiredStateRow("demo::desired-state::tool", "tool", &toolCount, present),
		},
	}}
	var protocol bytes.Buffer
	ctx := probeBridgeRuntime(root, &protocol, 0)

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "desired-state", "--format", "json",
		"--id", "demo::desired-state::group::test-preconditions",
	}); err != nil {
		t.Fatalf("group-selected desired-state dispatch: %v", err)
	}

	if got := dbCount.Load(); got != 1 {
		t.Fatalf("db probe invocations = %d, want exactly 1 despite the discovery re-entry", got)
	}
	if got := toolCount.Load(); got != 1 {
		t.Fatalf("tool probe invocations = %d, want exactly 1 despite the discovery re-entry", got)
	}
}

// DHF-TEST: keel/requirement-129
func TestDesiredStateProbeResultsNeverOutliveTheirPass(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	var count atomic.Int64
	current := "missing"
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Test Preconditions",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			countingDesiredStateRow("demo::desired-state::db", "db", &count, func() string { return current }),
		},
	}}

	discover := func() vscode.DiscoveryDocument {
		t.Helper()
		var protocol bytes.Buffer
		ctx := probeBridgeRuntime(root, &protocol, 0)
		if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
			t.Fatalf("discover dispatch: %v", err)
		}
		var doc vscode.DiscoveryDocument
		decodeJSON(t, &protocol, &doc)
		return doc
	}

	first := discover()
	current = "present"
	second := discover()

	if got := count.Load(); got != 2 {
		t.Fatalf("probe invocations across two passes = %d, want 2 — a memo must not outlive its pass", got)
	}
	firstRow, ok := testItemByID(first.Items, "demo::desired-state::db")
	if !ok || firstRow.DesiredStateRow == nil || firstRow.DesiredStateRow.Current != "missing" {
		t.Fatalf("first pass row = %+v ok=%v, want current missing", firstRow.DesiredStateRow, ok)
	}
	secondRow, ok := testItemByID(second.Items, "demo::desired-state::db")
	if !ok || secondRow.DesiredStateRow == nil || secondRow.DesiredStateRow.Current != "present" {
		t.Fatalf("second pass row = %+v ok=%v, want the changed environment value present", secondRow.DesiredStateRow, ok)
	}
}

// DHF-TEST: keel/requirement-129
func TestDesiredStateProbeDeadlineNamesTimeoutAndResource(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			blockingDesiredStateRow("demo::desired-state::slow", "slow-resource", release),
		},
	}}
	var protocol bytes.Buffer
	ctx := probeBridgeRuntime(root, &protocol, 25*time.Millisecond)

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)
	item, ok := testItemByID(doc.Items, "demo::desired-state::slow")
	if !ok || item.DesiredStateRow == nil {
		t.Fatalf("discovery items = %+v, want a rendered row for the timed-out probe", doc.Items)
	}
	if !strings.Contains(item.DesiredStateRow.Current, "timeout") {
		t.Fatalf("timed-out row current = %q, want it to name the timeout", item.DesiredStateRow.Current)
	}

	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desired)
	row := desiredStateDocumentRow(t, desired, "slow-resource")
	if row.Status == "satisfied" {
		t.Fatalf("timed-out row status = %q, want an unsatisfied row", row.Status)
	}
	if !strings.Contains(row.Message, "slow-resource") || !strings.Contains(row.Message, "timed out") {
		t.Fatalf("timed-out row message = %q, want it to name both the timeout and the resource", row.Message)
	}
	if row.Message == context.DeadlineExceeded.Error() {
		t.Fatalf("timed-out row message = %q, want more than the bare context error", row.Message)
	}
}

// Deriving active from the probe introduces a case a declared flag never had:
// the probe may return no verdict at all. The rule is fail-closed — an
// underivable fact never reads as satisfied, and the rendered row names why.
//
// DHF-TEST: keel/requirement-75, keel/ac-504
func TestAbandonedDesiredStateProbeIsNeverRenderedActive(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	blocked := blockingDesiredStateRow("demo::desired-state::slow", "slow-resource", release)
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Provisioning",
		Order:             10,
		MutuallyExclusive: false,
		Rows:              []testbridge.DesiredStateRow{blocked},
	}}
	var protocol bytes.Buffer
	ctx := probeBridgeRuntime(root, &protocol, 25*time.Millisecond)

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)
	item, ok := testItemByID(doc.Items, "demo::desired-state::slow")
	if !ok || item.DesiredStateRow == nil {
		t.Fatalf("discovery items = %+v, want a rendered row for the timed-out probe", doc.Items)
	}
	if item.DesiredStateRow.Active {
		t.Fatalf("timed-out discovery row active = true, want fail-closed false: %+v", item.DesiredStateRow)
	}

	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desired)
	row := desiredStateDocumentRow(t, desired, "slow-resource")
	if row.Active {
		t.Fatalf("timed-out desired-state row active = true, want fail-closed false: %+v", row)
	}
	if row.Status != "blocked" {
		t.Fatalf("timed-out row status = %q, want blocked", row.Status)
	}
	if !strings.Contains(row.Message, "slow-resource") || !strings.Contains(row.Message, "timed out") {
		t.Fatalf("timed-out row message = %q, want it to name the probe failure and the resource", row.Message)
	}
}

// DHF-TEST: keel/requirement-129
func TestDesiredStateProbeTimeoutLeavesRemainingRowsDerived(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{desiredStateGroupItem()}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var promptCount, otherCount atomic.Int64
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			countingDesiredStateRow("demo::desired-state::prompt", "prompt-resource", &promptCount, func() string { return "present" }),
			blockingDesiredStateRow("demo::desired-state::slow", "slow-resource", release),
			countingDesiredStateRow("demo::desired-state::other", "other-resource", &otherCount, func() string { return "absent" }),
		},
	}}
	var protocol bytes.Buffer
	ctx := probeBridgeRuntime(root, &protocol, 25*time.Millisecond)

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v — one slow row must not fail the pass", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)
	for _, id := range []string{"demo::desired-state::prompt", "demo::desired-state::slow", "demo::desired-state::other"} {
		if _, ok := testItemByID(doc.Items, id); !ok {
			t.Fatalf("discovery items = %+v, want row %q rendered alongside the timed-out row", doc.Items, id)
		}
	}
	prompt, _ := testItemByID(doc.Items, "demo::desired-state::prompt")
	if prompt.DesiredStateRow == nil || prompt.DesiredStateRow.Current != "present" {
		t.Fatalf("prompt row = %+v, want its own probed verdict", prompt.DesiredStateRow)
	}
	other, _ := testItemByID(doc.Items, "demo::desired-state::other")
	if other.DesiredStateRow == nil || other.DesiredStateRow.Current != "absent" {
		t.Fatalf("other row = %+v, want its own probed verdict", other.DesiredStateRow)
	}
	if promptCount.Load() != 1 || otherCount.Load() != 1 {
		t.Fatalf("prompt=%d other=%d probe invocations, want 1 each", promptCount.Load(), otherCount.Load())
	}
}
