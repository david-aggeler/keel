package testbridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/log/logtest"
	"github.com/david-aggeler/keel/testbridge"
	"github.com/david-aggeler/keel/vscode"
)

// DHF-TEST: keel/requirement-58
func TestCommandSpecOwnsCanonicalBridgeWire(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	spec := testbridge.CommandSpec(fake)
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &bytes.Buffer{},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		RunID:    func() string { return "run-fixed" },
	})

	protocol := protocolFromContext(t, ctx)
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, protocol, &discovery)
	if discovery.Workspace != "consumer-node" || discovery.ModulePath != "example.dev/tool" {
		t.Fatalf("discovery = %+v, want provider document through package envelope", discovery)
	}
	if _, ok := testItemByID(discovery.Items, "demo::lane::fast"); !ok {
		t.Fatalf("discovery items = %+v, want provider item retained", discovery.Items)
	}
	if _, ok := testItemByID(discovery.Items, testbridge.MaintenanceClearStateID); !ok {
		t.Fatalf("discovery items = %+v, want package-owned maintenance vocabulary", discovery.Items)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, protocol, &desired)
	if got := fake.calls; got != "discover,desiredState:demo::lane::fast" {
		t.Fatalf("provider calls = %q, want discover,desiredState:demo::lane::fast", got)
	}
	if len(desired.Groups) != 1 || len(desired.Groups[0].Rows) != 1 || desired.Groups[0].Rows[0].Action != "reconcile_during_run" || !desired.Groups[0].Rows[0].Owned {
		t.Fatalf("desired state groups = %+v, want owned reconcile_during_run row", desired.Groups)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	if !fake.sawRunLock {
		t.Fatal("runner did not observe package-owned run.lock serialization")
	}
	events := decodeEvents(t, protocol.String())
	if len(events) != 3 || events[0].Event != "run_started" || events[1].Event != "passed" || events[2].Event != "run_finished" || events[2].RunID == "" {
		t.Fatalf("events = %+v, want stamped run_started, runner event, terminal run_finished", events)
	}
	if events[2].RunID != "run-fixed" {
		t.Fatalf("run id = %q, want runtime override", events[2].RunID)
	}

	protocol.Reset()
	if err := spec.Dispatch(ctx, []string{"test-bridge", "config", "init"}); err != nil {
		t.Fatalf("config init dispatch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "test-bridge.json")); err != nil {
		t.Fatalf("config init did not write test-bridge.json: %v", err)
	}
}

// DHF-TEST: keel/requirement-78
func TestBridgeDispatchAndTerminalEventsReachLogSinksWithoutChangingProtocol(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Rows: []testbridge.DesiredStateRow{
			probedCountingRow(map[string]int{}, "demo::desired-state::db", "db", "seeded", true, "db ready"),
			probedCountingRow(map[string]int{}, "demo::desired-state::cache", "cache", "warm", false, "warm cache"),
		},
	}}
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "testbridge-test",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		Log:      logger.Slog(),
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
		RunID:    func() string { return "run-log" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	discoverProtocol := protocol.String()
	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json", "--id", "demo::desired-state::cache"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	protocol.Reset()
	err = testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::desired-state::db",
		"--id", "demo::desired-state::cache",
	})
	var runErr testbridge.RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 {
		t.Fatalf("run dispatch err = %#v, want non-zero RunError", err)
	}
	runProtocol := protocol.String()
	records := capture.AllJSON()

	capture.Reset()
	protocol.Reset()
	plainCtx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return time.Unix(10, 0).UTC() },
		RunID:    func() string { return "run-log" },
	})
	if err := testbridge.CommandSpec(fake).Dispatch(plainCtx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("plain discover dispatch: %v", err)
	}
	if got := protocol.String(); got != discoverProtocol {
		t.Fatalf("discover protocol changed with logging:\nwith logs: %q\nplain:     %q", discoverProtocol, got)
	}

	if !hasBridgeDispatchRecord(records, "discover", nil) {
		t.Fatalf("records = %+v, want discover dispatch record with empty ids", records)
	}
	if !hasBridgeDispatchRecord(records, "desired-state", []string{"demo::desired-state::cache"}) {
		t.Fatalf("records = %+v, want desired-state dispatch record with requested id", records)
	}
	if !hasBridgeDispatchRecord(records, "run", []string{"demo::desired-state::db", "demo::desired-state::cache"}) {
		t.Fatalf("records = %+v, want run dispatch record with requested ids", records)
	}
	if !hasBridgeDispatchArgsRecord(records, "run", []string{"--id", "demo::desired-state::db", "--id", "demo::desired-state::cache"}) {
		t.Fatalf("records = %+v, want run dispatch record with normalized args", records)
	}
	if !hasBridgeTerminalRecord(records, "demo::desired-state::db", "passed", 1) ||
		!hasBridgeTerminalRecord(records, "demo::desired-state::cache", "failed", 1) {
		t.Fatalf("records = %+v, want terminal pass/fail records carrying exit code", records)
	}

	capture.Reset()
	protocol.Reset()
	err = testbridge.CommandSpec(fake).Dispatch(plainCtx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::desired-state::db",
		"--id", "demo::desired-state::cache",
	})
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 {
		t.Fatalf("plain run dispatch err = %#v, want non-zero RunError", err)
	}
	if got := protocol.String(); got != runProtocol {
		t.Fatalf("run protocol changed with logging:\nwith logs: %q\nplain:     %q", runProtocol, got)
	}
}

// DHF-TEST: keel/requirement-78
func TestBridgeDispatchLogsDryRunAndValidationFailures(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "testbridge-test",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      logger.Slog(),
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--dry-run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("dry-run dispatch: %v", err)
	}
	err = testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id"})
	var usage cli.UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("run invalid args err = %v, want usage error", err)
	}
	records := capture.AllJSON()
	if !hasBridgeDispatchArgsRecord(records, "run", []string{"--id", "demo::lane::fast"}) {
		t.Fatalf("records = %+v, want non-dry-run args dispatch", records)
	}
	if !hasBridgeDispatchArgsRecord(records, "run", []string{"--dry-run", "--id", "demo::lane::fast"}) {
		t.Fatalf("records = %+v, want dry-run args dispatch", records)
	}
	if !hasBridgeDispatchErrorRecord(records, "run", []string{"--id"}, "--id requires a test id") {
		t.Fatalf("records = %+v, want invalid known-verb dispatch evidence", records)
	}
}

// DHF-TEST: keel/requirement-78
func TestBridgeTerminalLogIncludesRunLevelErrors(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.runErr = errors.New("runner failed before test id")
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "testbridge-test",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer logger.Close()
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      logger.Slog(),
		RunID:    func() string { return "run-level-error" },
	})

	err = testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	var runErr testbridge.RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 {
		t.Fatalf("run dispatch err = %#v, want non-zero RunError", err)
	}
	records := capture.AllJSON()
	if !hasBridgeTerminalMessageRecord(records, "", "errored", 1, "runner failed before test id") {
		t.Fatalf("records = %+v, want run-level terminal error record", records)
	}
}

func hasBridgeDispatchRecord(records []map[string]any, verb string, ids []string) bool {
	for _, record := range records {
		if record["msg"] != "testbridge dispatch" || record["verb"] != verb {
			continue
		}
		if equalJSONStrings(record["ids"], ids) {
			return true
		}
	}
	return false
}

func hasBridgeDispatchArgsRecord(records []map[string]any, verb string, args []string) bool {
	for _, record := range records {
		if record["msg"] != "testbridge dispatch" || record["verb"] != verb {
			continue
		}
		if equalJSONStrings(record["args"], args) {
			return true
		}
	}
	return false
}

func hasBridgeDispatchErrorRecord(records []map[string]any, verb string, args []string, errorText string) bool {
	for _, record := range records {
		if record["msg"] != "testbridge dispatch" || record["verb"] != verb {
			continue
		}
		if equalJSONStrings(record["args"], args) && strings.Contains(stringFromJSON(record["error"]), errorText) {
			return true
		}
	}
	return false
}

func hasBridgeTerminalRecord(records []map[string]any, testID, verdict string, exitCode int) bool {
	for _, record := range records {
		if record["msg"] != "testbridge terminal event" ||
			record["test_id"] != testID ||
			record["verdict"] != verdict ||
			intFromJSONNumber(record["exit_code"]) != exitCode {
			continue
		}
		return true
	}
	return false
}

func hasBridgeTerminalMessageRecord(records []map[string]any, testID, verdict string, exitCode int, message string) bool {
	for _, record := range records {
		if record["msg"] != "testbridge terminal event" ||
			stringFromJSON(record["test_id"]) != testID ||
			record["verdict"] != verdict ||
			intFromJSONNumber(record["exit_code"]) != exitCode ||
			!strings.Contains(stringFromJSON(record["message"]), message) {
			continue
		}
		return true
	}
	return false
}

func equalJSONStrings(raw any, want []string) bool {
	values, ok := raw.([]any)
	if !ok {
		return len(want) == 0 && raw == nil
	}
	if len(values) != len(want) {
		return false
	}
	for i := range want {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}

func stringFromJSON(raw any) string {
	value, _ := raw.(string)
	return value
}

func intFromJSONNumber(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

// DHF-TEST: keel/requirement-74
func TestDiscoverDerivesDesiredStateGroupsFromProvider(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}, {
		ID:       "demo::desired-state::legacy-child",
		ParentID: "demo::desired-state",
		Label:    "legacy consumer-authored B child",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Provisioning",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::action::seed-small", "db-small", "fixture-data", "small", "empty", false, "seed small", false, true),
			probedRow("", "python", "tool", "available", "available", true, "ok", true, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)

	group, ok := testItemByID(doc.Items, "demo::desired-state::group::provisioning")
	if !ok {
		t.Fatalf("derived desired-state group missing: %+v", doc.Items)
	}
	if group.ParentID != "demo::desired-state" || group.Label != "Provisioning" || group.SortText != "b.020" || strings.Join(group.Limitations, " ") != "mutually_exclusive=true" {
		t.Fatalf("derived group = %+v, want provider label/order/exclusivity under B", group)
	}
	runnable, ok := testItemByID(doc.Items, "demo::action::seed-small")
	if !ok || runnable.ParentID != group.ID || !runnable.Runnable || !equalStrings(runnable.Profiles, []string{"run"}) {
		t.Fatalf("run_id row = %+v ok=%v, want runnable run-profile child", runnable, ok)
	}
	informational, ok := testItemByID(doc.Items, "demo::desired-state::group::provisioning::row::python-tool-available")
	if !ok || informational.ParentID != group.ID || informational.Runnable || len(informational.Profiles) != 0 {
		t.Fatalf("informational row = %+v ok=%v, want non-runnable child", informational, ok)
	}
	if got := fake.calls; got != "discover,desiredState:" {
		t.Fatalf("provider calls = %q, want discover plus unselected desired-state query", got)
	}
	if legacy, ok := testItemByID(doc.Items, "demo::desired-state::legacy-child"); ok {
		t.Fatalf("consumer-authored B child was not replaced by bridge derivation: %+v", legacy)
	}
}

// DHF-TEST: keel/requirement-88
func TestExclusiveDesiredStateGroupsDeriveUnknownAndSingleActiveMember(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}}
	calls := map[string]int{}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Data Set",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedCountingRow(calls, "demo::desired-state::dataset::small", "app-db-small", "small", false, "small not active"),
			probedCountingRow(calls, "demo::desired-state::dataset::full", "app-db-full", "full", false, "full not active"),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &discovery)
	groupID := "demo::desired-state::group::data-set"
	unknownID := groupID + "::unknown"
	unknownItem, ok := testItemByID(discovery.Items, unknownID)
	if !ok {
		t.Fatalf("discovery missing bridge-synthesized Unknown State item: %+v", discovery.Items)
	}
	if unknownItem.ParentID != groupID || !unknownItem.Runnable || !equalStrings(unknownItem.Profiles, []string{"run"}) {
		t.Fatalf("Unknown State discovery item = %+v, want runnable bridge-owned reset child under exclusive group", unknownItem)
	}
	if got := limitationValue(unknownItem.Limitations, "action"); got != "reuse" {
		t.Fatalf("Unknown State action limitation = %q, want reuse", got)
	}

	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch with no concrete satisfied: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desired)
	group := desiredStateGroupByLabel(t, desired.Groups, "Data Set")
	unknown := desiredStateRowByResource(t, group.Rows, "Unknown State")
	if !strings.HasSuffix(unknown.RunID, "::desired-state::group::data-set::unknown") || unknown.Kind != "unknown" || unknown.Desired != "unknown" || unknown.Current != "unknown" || unknown.Action != "reuse" || !unknown.Active {
		t.Fatalf("Unknown row = %+v, want active bridge-owned reset reuse row", unknown)
	}
	for _, resource := range []string{"app-db-small", "app-db-full"} {
		row := desiredStateRowByResource(t, group.Rows, resource)
		if row.Active {
			t.Fatalf("concrete row %s active with Unknown active: %+v", resource, group.Rows)
		}
	}

	fake.desiredGroups[0].Rows[1] = probedCountingRow(calls, "demo::desired-state::dataset::full", "app-db-full", "full", true, "full active")
	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch with one concrete satisfied: %v", err)
	}
	decodeJSON(t, &protocol, &desired)
	group = desiredStateGroupByLabel(t, desired.Groups, "Data Set")
	if row := desiredStateRowByResource(t, group.Rows, "app-db-full"); !row.Active {
		t.Fatalf("satisfied concrete row inactive: %+v", group.Rows)
	}
	if unknown := desiredStateRowByResource(t, group.Rows, "Unknown State"); unknown.Active {
		t.Fatalf("Unknown row active while concrete satisfied: %+v", group.Rows)
	}
}

// DHF-TEST: keel/requirement-88
func TestExclusiveUnknownRunIsBridgeOwnedAndDoesNotInvokeConsumer(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Data Set",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::dataset::small", "app-db-small", "fixture-data", "small", "small", true, "small active", false, true),
			probedRow("demo::desired-state::dataset::full", "app-db-full", "fixture-data", "full", "small", false, "full inactive", false, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-unknown" },
	})

	unknownID := "demo::desired-state::group::data-set::unknown"
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", unknownID}); err != nil {
		t.Fatalf("run Unknown dispatch: %v\n%s", err, protocol.String())
	}
	if fake.runCalls != 0 {
		t.Fatalf("consumer Run calls = %d, want 0 for bridge-owned Unknown reset", fake.runCalls)
	}
	events := decodeEvents(t, protocol.String())
	if !eventsContain(events, "passed", unknownID, "selected Unknown State") {
		t.Fatalf("events = %+v, want passed Unknown reset event", events)
	}
}

// DHF-TEST: keel/requirement-88
func TestExclusiveDesiredStateSingleSelectionClearsSiblingResults(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Data Set",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::dataset::small", "app-db-small", "fixture-data", "small", "small", true, "small active", false, true),
			probedRow("demo::desired-state::dataset::full", "app-db-full", "fixture-data", "full", "full", true, "full active", false, true),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-exclusive" },
	})

	fullID := "demo::desired-state::dataset::full"
	unknownID := "demo::desired-state::group::data-set::unknown"
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", fullID}); err != nil {
		t.Fatalf("run concrete dispatch: %v\n%s", err, protocol.String())
	}
	events := decodeEvents(t, protocol.String())
	smallID := "demo::desired-state::dataset::small"
	if !eventsContain(events, "passed", fullID, "full active") ||
		!eventMessageContainsAll(events, "cleared", smallID, smallID, fullID) ||
		!eventMessageContainsAll(events, "cleared", unknownID, unknownID, fullID) {
		t.Fatalf("concrete selection events = %+v, want selected pass and sibling clear events", events)
	}
	if eventsContain(events, "skipped", smallID, "deactivated by exclusive desired-state selection") {
		t.Fatalf("sibling deactivation must emit a cleared event, not a terminal skipped result: %+v", events)
	}

	protocol.Reset()
	fake.runCalls = 0
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", unknownID}); err != nil {
		t.Fatalf("run Unknown dispatch: %v\n%s", err, protocol.String())
	}
	if fake.runCalls != 0 {
		t.Fatalf("consumer Run calls = %d, want 0 for bridge-owned Unknown reset", fake.runCalls)
	}
	events = decodeEvents(t, protocol.String())
	if !eventsContain(events, "passed", unknownID, "selected Unknown State") ||
		!eventMessageContainsAll(events, "cleared", smallID, smallID, unknownID) ||
		!eventMessageContainsAll(events, "cleared", fullID, fullID, unknownID) {
		t.Fatalf("Unknown selection events = %+v, want Unknown pass and concrete sibling clear events", events)
	}
}

// DHF-TEST: keel/requirement-88
func TestExclusiveDesiredStateReconcileSelectionClearsSiblingResults(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Runnable: false,
		Profiles: []string{},
	}}
	fake.desiredStateEmptyForSelectedIDs = true
	marker := filepath.Join(root, ".devtools", "selected-full")
	fake.mutateDuringRun = marker
	selectedFull := func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Data Set",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			mutableDesiredStateRow("demo::desired-state::dataset::small", "app-db-small", "small", func() bool { return !selectedFull() }),
			mutableDesiredStateRow("demo::desired-state::dataset::full", "app-db-full", "full", selectedFull),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-exclusive-reconcile" },
	})

	fullID := "demo::desired-state::dataset::full"
	unknownID := "demo::desired-state::group::data-set::unknown"
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", fullID}); err != nil {
		t.Fatalf("run reconcile concrete dispatch: %v\n%s", err, protocol.String())
	}
	if got, want := fake.runIDs, []string{fullID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("consumer Run ids = %v, want reconcile path for %v", got, want)
	}
	events := decodeEvents(t, protocol.String())
	smallID := "demo::desired-state::dataset::small"
	if !eventsContain(events, "passed", fullID, "") ||
		!eventMessageContainsAll(events, "cleared", smallID, smallID, fullID) ||
		!eventMessageContainsAll(events, "cleared", unknownID, unknownID, fullID) {
		t.Fatalf("reconcile selection events = %+v, want selected pass and sibling clear events", events)
	}

	protocol.Reset()
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("post-run desired-state dispatch: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desired)
	group := desiredStateGroupByLabel(t, desired.Groups, "Data Set")
	full := desiredStateRowByResource(t, group.Rows, "app-db-full")
	if !full.Active || full.Action != "reuse" || full.Status != "satisfied" {
		t.Fatalf("post-run full row = %+v, want selected active satisfied reuse row", full)
	}
	for _, resource := range []string{"app-db-small", "Unknown State"} {
		row := desiredStateRowByResource(t, group.Rows, resource)
		if row.Active {
			t.Fatalf("post-run row %q active with selected full active: %+v", resource, group.Rows)
		}
	}
}

// DHF-TEST: keel/requirement-87
func TestDiscoverInjectsBridgeOwnedMaintenanceVocabulary(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}

	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)
	if got, want := doc.Capabilities.ClearResultsTestIDs, []string{testbridge.MaintenanceClearResultsID}; !equalStrings(got, want) {
		t.Fatalf("clear_results_test_ids = %v, want %v", got, want)
	}
	if got, want := doc.Capabilities.ClearStateTestIDs, []string{testbridge.MaintenanceClearStateID}; !equalStrings(got, want) {
		t.Fatalf("clear_state_test_ids = %v, want %v", got, want)
	}
	for id, want := range map[string]struct {
		label string
		sort  string
	}{
		testbridge.MaintenanceDetectLanesID:  {label: "a.1 detect lanes", sort: "a.001"},
		testbridge.MaintenanceUnlockID:       {label: "a.2 unlock test bridge", sort: "a.002"},
		testbridge.MaintenanceClearResultsID: {label: "a.3 clear test results", sort: "a.003"},
		testbridge.MaintenanceClearStateID:   {label: "a.4 clear local test state", sort: "a.004"},
	} {
		item, ok := testItemByID(doc.Items, id)
		if !ok {
			t.Fatalf("missing bridge-owned maintenance item %q", id)
		}
		if item.ParentID != testbridge.MaintenanceGroupID || item.Kind != "maintenance" || item.Label != want.label || item.SortText != want.sort || !item.Runnable {
			t.Fatalf("maintenance item %q = %+v, want canonical parent label=%q sort=%q runnable", id, item, want.label, want.sort)
		}
	}
}

// DHF-TEST: keel/requirement-87
func TestRunBridgeOwnedClearStateInvokesConsumerCallback(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, ".devtools", "consumer-state")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newFakeBridge(root)
	fake.clearStatePath = statePath
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-clear-state" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", testbridge.MaintenanceClearStateID}); err != nil {
		t.Fatalf("clear-state run dispatch: %v\n%s", err, protocol.String())
	}

	if fake.clearStateCalls != 1 {
		t.Fatalf("clear-state calls = %d, want 1", fake.clearStateCalls)
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("clear-state was delegated to generic runner ids=%v, want callback only", fake.runIDs)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear-state should remove consumer state, stat err=%v", err)
	}
	events := decodeEvents(t, protocol.String())
	if !eventsContain(events, "passed", testbridge.MaintenanceClearStateID, "") || events[len(events)-1].ExitCode == nil || *events[len(events)-1].ExitCode != 0 {
		t.Fatalf("clear-state events = %+v, want passed and run_finished exit 0", events)
	}
}

// DHF-TEST: keel/requirement-75
func TestDiscoverDesiredStateRowUsesProbeDerivedCurrentAndAction(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Data Set",
		Order:             10,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::app-db-full", "app-db-full", "fixture-data", "full", "full", true, "already full", false, true),
		},
	}}

	var discoverOut bytes.Buffer
	discoverCtx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &discoverOut})
	if err := testbridge.CommandSpec(fake).Dispatch(discoverCtx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var discovery vscode.DiscoveryDocument
	decodeJSON(t, &discoverOut, &discovery)

	var desiredOut bytes.Buffer
	desiredCtx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &desiredOut})
	if err := testbridge.CommandSpec(fake).Dispatch(desiredCtx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	decodeJSON(t, &desiredOut, &desiredState)

	discoveryRow, ok := testItemByID(discovery.Items, "demo::desired-state::app-db-full")
	if !ok {
		t.Fatalf("discovery row missing from items: %+v", discovery.Items)
	}
	documentRow := desiredState.Groups[0].Rows[0]
	gotCurrent := limitationValue(discoveryRow.Limitations, "current")
	gotAction := limitationValue(discoveryRow.Limitations, "action")
	if gotCurrent != documentRow.Current || gotAction != documentRow.Action {
		t.Fatalf("discovery row limitations = %v, desired-state row = %+v; want matching current/action", discoveryRow.Limitations, documentRow)
	}
	if gotCurrent != "full" || gotAction != "reuse" {
		t.Fatalf("discovery current/action = %q/%q, want probe-derived full/reuse", gotCurrent, gotAction)
	}
}

// DHF-TEST: keel/requirement-75
func TestDiscoverAnonymousDesiredStateRowIDIgnoresProbeDerivedAction(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	current := "empty"
	satisfied := false
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Data Set",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{{
			Resource: "app-db-full",
			Kind:     "fixture-data",
			Desired:  "full",
			Active:   true,
			Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
				return testbridge.DesiredStateProbeResult{Current: current, Satisfied: satisfied}
			},
		}},
	}}
	discover := func() vscode.TestItem {
		t.Helper()
		var out bytes.Buffer
		ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &out})
		if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
			t.Fatalf("discover dispatch: %v", err)
		}
		var doc vscode.DiscoveryDocument
		decodeJSON(t, &out, &doc)
		for _, item := range doc.Items {
			if item.Label == "app-db-full: full" {
				return item
			}
		}
		t.Fatalf("anonymous desired-state row missing from items: %+v", doc.Items)
		return vscode.TestItem{}
	}

	before := discover()
	current = "full"
	satisfied = true
	after := discover()
	if before.ID != after.ID {
		t.Fatalf("anonymous row ID changed with probe-derived action: before=%q after=%q", before.ID, after.ID)
	}
	if got := limitationValue(before.Limitations, "action"); got != "reconcile_during_run" {
		t.Fatalf("first discovery action = %q, want reconcile_during_run", got)
	}
	if got := limitationValue(after.Limitations, "action"); got != "reuse" {
		t.Fatalf("second discovery action = %q, want reuse", got)
	}
}

// DHF-TEST: keel/requirement-83
func TestDiscoverServesRunnableNonExclusiveDesiredStateGroups(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Test Preconditions",
		Order:             10,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::db", "db", "service", "seeded", "empty", false, "seed db", false, false),
			probedRow("", "python", "tool", "available", "available", true, "ok", true, false),
		},
	}, {
		Label:             "Exclusive Choices",
		Order:             20,
		MutuallyExclusive: true,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::small", "db", "fixture-data", "small", "empty", false, "seed small", false, true),
		},
	}, {
		Label:             "Informational Checks",
		Order:             30,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedRow("", "go", "tool", "installed", "installed", true, "ok", true, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover dispatch: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)

	runnableGroup, ok := testItemByID(doc.Items, "demo::desired-state::group::test-preconditions")
	if !ok || !runnableGroup.Runnable || !equalStrings(runnableGroup.Profiles, []string{"run"}) {
		t.Fatalf("non-exclusive runnable group = %+v ok=%v, want runnable run profile", runnableGroup, ok)
	}
	exclusiveGroup, ok := testItemByID(doc.Items, "demo::desired-state::group::exclusive-choices")
	if !ok || exclusiveGroup.Runnable || len(exclusiveGroup.Profiles) != 0 {
		t.Fatalf("exclusive group = %+v ok=%v, want non-runnable empty profiles", exclusiveGroup, ok)
	}
	emptyGroup, ok := testItemByID(doc.Items, "demo::desired-state::group::informational-checks")
	if !ok || emptyGroup.Runnable || len(emptyGroup.Profiles) != 0 {
		t.Fatalf("zero-runnable group = %+v ok=%v, want non-runnable empty profiles", emptyGroup, ok)
	}
	runnableRow, ok := testItemByID(doc.Items, "demo::desired-state::db")
	if !ok || !runnableRow.Runnable || !equalStrings(runnableRow.Profiles, []string{"run"}) {
		t.Fatalf("runnable row = %+v ok=%v, want existing row run state retained", runnableRow, ok)
	}
	informationalRow, ok := testItemByID(doc.Items, "demo::desired-state::group::test-preconditions::row::python-tool-available")
	if !ok || informationalRow.Runnable || len(informationalRow.Profiles) != 0 {
		t.Fatalf("informational row = %+v ok=%v, want existing row informational state retained", informationalRow, ok)
	}
}

// DHF-TEST: keel/requirement-74
func TestRunDryRunResolvesDerivedDesiredStateRunIDsReadOnly(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows:  []testbridge.DesiredStateRow{probedRow("demo::action::seed-small", "db-small", "fixture-data", "small", "empty", false, "seed small", false, false)},
	}}
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--dry-run", "--id", "demo::action::seed-small"}); err != nil {
		t.Fatalf("dry-run desired-state run_id dispatch: %v", err)
	}
	if len(fake.runIDs) != 0 || fake.sawRunLock {
		t.Fatalf("dry-run executed runner path: runIDs=%v sawRunLock=%v", fake.runIDs, fake.sawRunLock)
	}
	if got := fake.calls; got != "discover,desiredState:" {
		t.Fatalf("provider calls = %q, want discover plus unselected desired-state query", got)
	}
}

// DHF-TEST: keel/requirement-74
func TestDiscoverRejectsDuplicateDerivedDesiredStateIDs(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows:  []testbridge.DesiredStateRow{probedRow("", "db-small", "fixture-data", "small", "empty", false, "seed small", false, false)},
	}, {
		Label: "Provisioning",
		Order: 20,
		Rows:  []testbridge.DesiredStateRow{probedRow("", "db-large", "fixture-data", "large", "empty", false, "seed large", false, false)},
	}}
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"})
	if err == nil || !strings.Contains(err.Error(), "duplicate discovery item id") {
		t.Fatalf("duplicate derived ID err = %v, want duplicate discovery item id", err)
	}
}

// DHF-TEST: keel/requirement-74
func TestDiscoverDegradesDesiredStateProviderFailure(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredErr = errors.New("desired provider exploded")
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover should degrade desired-state failure, got: %v", err)
	}
	var doc vscode.DiscoveryDocument
	decodeJSON(t, &protocol, &doc)

	diagnostic, ok := testItemByID(doc.Items, "demo::desired-state::diagnostic::desired-state")
	if !ok || diagnostic.ParentID != "demo::desired-state" || diagnostic.Runnable || !strings.Contains(strings.Join(diagnostic.Limitations, " "), "desired provider exploded") {
		t.Fatalf("diagnostic item = %+v ok=%v, want one non-runnable B child with provider error", diagnostic, ok)
	}
	for _, item := range doc.Items {
		if item.ParentID == "demo::desired-state" && item.ID != diagnostic.ID {
			t.Fatalf("provider failure emitted extra B child: %+v", item)
		}
	}
}

// DHF-TEST: keel/requirement-75
func TestDesiredStateRowsExposeDeclaredStructurePlusProbeOnly(t *testing.T) {
	rowType := reflect.TypeOf(testbridge.DesiredStateRow{})
	for _, forbidden := range []string{"Current", "Status", "Action"} {
		if _, ok := rowType.FieldByName(forbidden); ok {
			t.Fatalf("DesiredStateRow exposes consumer-written state field %s", forbidden)
		}
	}
	if _, ok := rowType.FieldByName("Probe"); !ok {
		t.Fatal("DesiredStateRow has no probe field")
	}
}

// DHF-TEST: keel/requirement-75
func TestDesiredStateDerivationExecutesProbeAndMapsStateFields(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	probeCalls := 0
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{{
			RunID:    "demo::desired-state::db",
			Resource: "db",
			Kind:     "fixture-data",
			Desired:  "seeded",
			Owned:    true,
			Probe: func(_ context.Context, req testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
				probeCalls++
				if req.Root != root || req.RunID != "demo::desired-state::db" {
					t.Fatalf("probe request = %+v, want root/run id", req)
				}
				return testbridge.DesiredStateProbeResult{Current: "empty", Satisfied: false, Message: "seed db"}
			},
		}},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	var desiredState vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desiredState)
	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", probeCalls)
	}
	row := desiredState.Groups[0].Rows[0]
	if row.Current != "empty" || row.Status != "reconcilable" || row.Action != "reconcile_during_run" || row.Message != "seed db" {
		t.Fatalf("derived row = %+v, want probe-derived Current/Status/Action/Message", row)
	}
}

// DHF-TEST: keel/requirement-75
func TestRunExecutesDesiredStateProbeInsideTestbridge(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	probeCalls := 0
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{{
			RunID:    "demo::desired-state::db",
			Resource: "db",
			Kind:     "fixture-data",
			Desired:  "seeded",
			Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
				probeCalls++
				return testbridge.DesiredStateProbeResult{Current: "seeded", Satisfied: true, Message: "db ready"}
			},
		}},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-probe" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::desired-state::db"}); err != nil {
		t.Fatalf("desired-state run dispatch: %v\n%s", err, protocol.String())
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("consumer runner received desired-state row ids: %v", fake.runIDs)
	}
	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want row-run execution", probeCalls)
	}
	events := decodeEvents(t, protocol.String())
	if !eventsContain(events, "passed", "demo::desired-state::db", "db ready") {
		t.Fatalf("events = %+v, want package-owned passed event for desired-state row", events)
	}
}

// DHF-TEST: keel/requirement-75
func TestRunExecutesMultipleDesiredStateRows(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	calls := map[string]int{}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label: "Provisioning",
		Order: 10,
		Rows: []testbridge.DesiredStateRow{
			probedCountingRow(calls, "demo::desired-state::db", "db", "seeded", true, "db ready"),
			probedCountingRow(calls, "demo::desired-state::cache", "cache", "warm", false, "warm cache"),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-multi" }})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::desired-state::db",
		"--id", "demo::desired-state::cache",
	})
	if err == nil {
		t.Fatal("multi desired-state run returned nil error, want non-zero from failed cache row")
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("consumer runner received desired-state row ids: %v", fake.runIDs)
	}
	events := decodeEvents(t, protocol.String())
	if !eventsContain(events, "passed", "demo::desired-state::db", "db ready") || !eventsContain(events, "failed", "demo::desired-state::cache", "warm cache") {
		t.Fatalf("events = %+v, want terminal event per desired-state row", events)
	}
	if got := calls["demo::desired-state::db"]; got != 1 {
		t.Fatalf("db probe calls = %d, want row-run execution", got)
	}
	if got := calls["demo::desired-state::cache"]; got != 1 {
		t.Fatalf("cache probe calls = %d, want row-run execution", got)
	}
}

// DHF-TEST: keel/requirement-84
func TestRunExpandsRunnableDesiredStateGroupToRows(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	calls := map[string]int{}
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Test Preconditions",
		Order:             10,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedCountingRow(calls, "demo::desired-state::db", "db", "seeded", true, "db ready"),
			probedCountingRow(calls, "demo::desired-state::cache", "cache", "warm", true, "cache ready"),
			probedRow("", "python", "tool", "available", "available", true, "ok", true, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-group" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::desired-state::group::test-preconditions"}); err != nil {
		t.Fatalf("group run dispatch: %v\n%s", err, protocol.String())
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("consumer runner received group run ids: %v", fake.runIDs)
	}
	if calls["demo::desired-state::db"] != 2 || calls["demo::desired-state::cache"] != 2 {
		t.Fatalf("probe calls = %+v, want plan-derivation probe plus row-run probe per runnable member row", calls)
	}
	events := decodeEvents(t, protocol.String())
	wantRequested := []vscode.RunRequest{
		{ID: "demo::desired-state::db", Label: "db: seeded"},
		{ID: "demo::desired-state::cache", Label: "cache: warm"},
	}
	if len(events) == 0 || !reflect.DeepEqual(events[0].Requested, wantRequested) {
		t.Fatalf("run_started requested = %+v, want expanded member rows %+v", events[0].Requested, wantRequested)
	}
	if !eventsContain(events, "passed", "demo::desired-state::db", "db ready") ||
		!eventsContain(events, "passed", "demo::desired-state::cache", "cache ready") {
		t.Fatalf("events = %+v, want per-row desired-state events", events)
	}
}

// DHF-TEST: keel/requirement-84
func TestDesiredStateExpandsRunnableGroupSelectionBeforeProviderFilter(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.filterDesiredStateByIDs = true
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Test Preconditions",
		Order:             10,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedRow("demo::desired-state::db", "db", "service", "seeded", "empty", false, "seed db", false, false),
			probedRow("demo::desired-state::cache", "cache", "service", "warm", "cold", false, "warm cache", false, false),
			probedRow("", "python", "tool", "available", "available", true, "ok", true, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "desired-state",
		"--format", "json",
		"--id", "demo::desired-state::group::test-preconditions",
	}); err != nil {
		t.Fatalf("desired-state group dispatch: %v", err)
	}
	var desired vscode.DesiredStateDocument
	decodeJSON(t, &protocol, &desired)
	if got := fake.calls; got != "discover,desiredState:,desiredState:demo::desired-state::db,demo::desired-state::cache" {
		t.Fatalf("provider calls = %q, want discovery query then expanded row-id desired-state query", got)
	}
	if len(desired.Groups) != 1 || len(desired.Groups[0].Rows) != 2 {
		t.Fatalf("desired-state groups = %+v, want only runnable member rows", desired.Groups)
	}
	if desired.Groups[0].Rows[0].RunID != "demo::desired-state::db" || desired.Groups[0].Rows[1].RunID != "demo::desired-state::cache" {
		t.Fatalf("desired-state rows = %+v, want expanded runnable member rows", desired.Groups[0].Rows)
	}
}

// DHF-TEST: keel/requirement-84
func TestRunGroupSelectionDoesNotDuplicateExplicitMemberRows(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	calls := map[string]int{}
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Test Preconditions",
		Order:             10,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedCountingRow(calls, "demo::desired-state::db", "db", "seeded", true, "db ready"),
			probedCountingRow(calls, "demo::desired-state::cache", "cache", "warm", true, "cache ready"),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-mixed" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::desired-state::group::test-preconditions",
		"--id", "demo::desired-state::db",
	}); err != nil {
		t.Fatalf("mixed group run dispatch: %v\n%s", err, protocol.String())
	}
	if calls["demo::desired-state::db"] != 2 || calls["demo::desired-state::cache"] != 2 {
		t.Fatalf("probe calls = %+v, want plan-derivation probe plus one deduplicated row-run probe per member row", calls)
	}
	events := decodeEvents(t, protocol.String())
	wantRequested := []vscode.RunRequest{
		{ID: "demo::desired-state::db", Label: "db: seeded"},
		{ID: "demo::desired-state::cache", Label: "cache: warm"},
	}
	if len(events) == 0 || !reflect.DeepEqual(events[0].Requested, wantRequested) {
		t.Fatalf("run_started requested = %+v, want deduplicated member rows %+v", events[0].Requested, wantRequested)
	}
}

// DHF-TEST: keel/requirement-84
func TestRunDesiredStateGroupWithNoRunnableRowsFailsLoudly(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::desired-state",
		Label:    "B - Desired State",
		Kind:     "group",
		Profiles: []string{},
	}}
	fake.desiredGroups = []testbridge.DesiredStateGroup{{
		Label:             "Informational Checks",
		Order:             10,
		MutuallyExclusive: false,
		Rows: []testbridge.DesiredStateRow{
			probedRow("", "python", "tool", "available", "available", true, "ok", true, false),
		},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-empty-group" }})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::desired-state::group::informational-checks"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "has no runnable rows") {
		t.Fatalf("zero-runnable group err = %v, want loud usage error", err)
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("consumer runner received zero-runnable group ids: %v", fake.runIDs)
	}
	if events := decodeEvents(t, protocol.String()); len(events) != 0 {
		t.Fatalf("events = %+v, want no success events before run resolution", events)
	}
}

// DHF-TEST: keel/requirement-86
func TestRunExpandsNonDesiredStateGroupToRunnableDescendantLeaves(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{
			ID:       "demo::lanes",
			Label:    "C - Lanes",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:       "demo::lane::fast",
			ParentID: "demo::lanes",
			Label:    "Fast",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "demo::lane::slow",
			ParentID: "demo::lanes",
			Label:    "Slow",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "demo::lane::manual",
			ParentID: "demo::lanes",
			Label:    "Manual",
			Kind:     "lane",
			Runnable: false,
			Profiles: []string{},
		},
	}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-lanes" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lanes"}); err != nil {
		t.Fatalf("non-desired group run dispatch: %v\n%s", err, protocol.String())
	}
	if !equalStrings(fake.runIDs, []string{"demo::lane::fast", "demo::lane::slow"}) {
		t.Fatalf("consumer runner ids = %v, want runnable descendant leaves only", fake.runIDs)
	}
	events := decodeEvents(t, protocol.String())
	wantRequested := []vscode.RunRequest{
		{ID: "demo::lane::fast", Label: "Fast"},
		{ID: "demo::lane::slow", Label: "Slow"},
	}
	if len(events) == 0 || !reflect.DeepEqual(events[0].Requested, wantRequested) {
		t.Fatalf("run_started requested = %+v, want expanded leaf requests %+v", events[0].Requested, wantRequested)
	}
}

// DHF-TEST: keel/requirement-86
func TestRunCoversGroupResolvesToSameRequestsAsOwningLane(t *testing.T) {
	items := []vscode.TestItem{
		{
			ID:       "keel::lane::ci",
			Label:    "ci",
			Kind:     "group",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "keel::lane::ci::lint",
			ParentID: "keel::lane::ci",
			Label:    "lint",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "keel::lane::ci::test-coverage",
			ParentID: "keel::lane::ci",
			Label:    "test-coverage",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "keel::lane::ci::covers",
			ParentID: "keel::lane::ci",
			Label:    "covers",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:          "keel::lane::ci::covers::lint",
			ParentID:    "keel::lane::ci::covers",
			Label:       "lint",
			Kind:        "lane",
			Runnable:    false,
			Profiles:    []string{},
			CanonicalID: "keel::lane::lint",
		},
	}

	laneRunIDs, laneRequested := runTestBridgeID(t, items, "keel::lane::ci")
	coversRunIDs, coversRequested := runTestBridgeID(t, items, "keel::lane::ci::covers")

	if !equalStrings(coversRunIDs, laneRunIDs) {
		t.Fatalf("covers consumer runner ids = %v, want same as owning lane %v", coversRunIDs, laneRunIDs)
	}
	if !reflect.DeepEqual(coversRequested, laneRequested) {
		t.Fatalf("covers requested = %+v, want same as owning lane %+v", coversRequested, laneRequested)
	}
	if !equalStrings(coversRunIDs, []string{"keel::lane::ci::lint", "keel::lane::ci::test-coverage"}) {
		t.Fatalf("ci covers runner ids = %v, want ci lane leaf runs", coversRunIDs)
	}
}

// DHF-TEST: keel/requirement-86
func TestRunCoversGroupSelectionDoesNotDuplicateExplicitOwningLane(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{
			ID:       "keel::lane::lint",
			Label:    "lint",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "keel::lane::lint::covers",
			ParentID: "keel::lane::lint",
			Label:    "covers",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:          "keel::lane::lint::covers::go-root",
			ParentID:    "keel::lane::lint::covers",
			Label:       "Go",
			Kind:        "root",
			Runnable:    false,
			Profiles:    []string{},
			CanonicalID: "go::root",
		},
	}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-mixed-covers" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "keel::lane::lint::covers",
		"--id", "keel::lane::lint",
	}); err != nil {
		t.Fatalf("mixed covers run dispatch: %v\n%s", err, protocol.String())
	}
	if !equalStrings(fake.runIDs, []string{"keel::lane::lint"}) {
		t.Fatalf("consumer runner ids = %v, want deduplicated owning lane", fake.runIDs)
	}
	events := decodeEvents(t, protocol.String())
	wantRequested := []vscode.RunRequest{{ID: "keel::lane::lint", Label: "lint"}}
	if len(events) == 0 || !reflect.DeepEqual(events[0].Requested, wantRequested) {
		t.Fatalf("run_started requested = %+v, want deduplicated owning lane request %+v", events[0].Requested, wantRequested)
	}
}

// DHF-TEST: keel/requirement-86
func TestRunNonDesiredStateGroupWithNoRunnableDescendantsFailsLoudly(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{
			ID:       "demo::empty-group",
			Label:    "Empty Group",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:       "demo::empty-group::info",
			ParentID: "demo::empty-group",
			Label:    "Info",
			Kind:     "note",
			Runnable: false,
			Profiles: []string{},
		},
	}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-empty-tree" }})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::empty-group"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), `group "demo::empty-group" has no runnable descendants`) {
		t.Fatalf("zero-runnable non-desired group err = %v, want loud usage error", err)
	}
	if len(fake.runIDs) != 0 {
		t.Fatalf("consumer runner received zero-runnable group ids: %v", fake.runIDs)
	}
	if events := decodeEvents(t, protocol.String()); len(events) != 0 {
		t.Fatalf("events = %+v, want no success events before run resolution", events)
	}
}

// DHF-TEST: keel/requirement-86
func TestRunNonDesiredStateGroupSelectionDoesNotDuplicateExplicitLeaves(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{
			ID:       "demo::lanes",
			Label:    "C - Lanes",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:       "demo::lane::fast",
			ParentID: "demo::lanes",
			Label:    "Fast",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "demo::lane::slow",
			ParentID: "demo::lanes",
			Label:    "Slow",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
	}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-mixed-lanes" }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::lanes",
		"--id", "demo::lane::fast",
	}); err != nil {
		t.Fatalf("mixed non-desired group run dispatch: %v\n%s", err, protocol.String())
	}
	if !equalStrings(fake.runIDs, []string{"demo::lane::fast", "demo::lane::slow"}) {
		t.Fatalf("consumer runner ids = %v, want deduplicated runnable leaves", fake.runIDs)
	}
	events := decodeEvents(t, protocol.String())
	wantRequested := []vscode.RunRequest{
		{ID: "demo::lane::fast", Label: "Fast"},
		{ID: "demo::lane::slow", Label: "Slow"},
	}
	if len(events) == 0 || !reflect.DeepEqual(events[0].Requested, wantRequested) {
		t.Fatalf("run_started requested = %+v, want deduplicated leaf requests %+v", events[0].Requested, wantRequested)
	}
}

func runTestBridgeID(t *testing.T, items []vscode.TestItem, id string) ([]string, []vscode.RunRequest) {
	t.Helper()
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = items
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-" + strings.ReplaceAll(id, ":", "-") }})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", id}); err != nil {
		t.Fatalf("run %q dispatch: %v\n%s", id, err, protocol.String())
	}
	events := decodeEvents(t, protocol.String())
	if len(events) == 0 {
		t.Fatalf("run %q emitted no events", id)
	}
	return fake.runIDs, events[0].Requested
}

// DHF-TEST: keel/requirement-86
func TestRunNonDesiredStateGroupCancellationStopsRemainingChildren(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{
		{
			ID:       "demo::lanes",
			Label:    "C - Lanes",
			Kind:     "group",
			Runnable: false,
			Profiles: []string{},
		},
		{
			ID:       "demo::lane::fast",
			ParentID: "demo::lanes",
			Label:    "Fast",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
		{
			ID:       "demo::lane::slow",
			ParentID: "demo::lanes",
			Label:    "Slow",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	fake.cancelAfterFirstRun = cancel
	var protocol bytes.Buffer
	ctx = testbridge.WithRuntime(ctx, testbridge.Runtime{Root: root, Protocol: &protocol, RunID: func() string { return "run-cancel-lanes" }})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lanes"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled group run err = %v, want context.Canceled", err)
	}
	if got, want := fake.runCalls, 1; got != want {
		t.Fatalf("runner calls = %d, want %d", got, want)
	}
	if !equalStrings(fake.runIDs, []string{"demo::lane::fast"}) {
		t.Fatalf("consumer runner ids = %v, want only first child before cancellation", fake.runIDs)
	}
	if events := decodeEvents(t, protocol.String()); !eventsContain(events, "passed", "demo::lane::fast", "") || eventsContain(events, "passed", "demo::lane::slow", "") {
		t.Fatalf("events = %+v, want first child only before cancellation", events)
	}
}

// DHF-TEST: keel/requirement-84
func TestRunDoesNotTreatLimitationStringAloneAsDesiredStateGroup(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:          "demo::custom::info",
		Label:       "custom informational item",
		Kind:        "group",
		Runnable:    false,
		Profiles:    []string{},
		Limitations: []string{"mutually_exclusive=false"},
	}}
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--dry-run", "--id", "demo::custom::info"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), `resolves to non-runnable id "demo::custom::info"`) {
		t.Fatalf("limitation-string collision err = %v, want ordinary non-runnable-id error", err)
	}
}

// DHF-TEST: keel/requirement-60
func TestArgvContractForDesiredStateAndRun(t *testing.T) {
	spec := testbridge.CommandSpec(newFakeBridge(t.TempDir()))
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: t.TempDir(), Protocol: io.Discard})

	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "discover", "--format", "json"}); err != nil {
		t.Fatalf("discover --format json: %v", err)
	}
	if err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--format", "json"}); err != nil {
		t.Fatalf("desired-state --format json: %v", err)
	}
	err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--format", "json", "--id", "demo::lane::fast"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "unknown flag \"--format\"") {
		t.Fatalf("run --format err = %v, want usage error rejecting --format", err)
	}
}

// DHF-TEST: keel/requirement-60
func TestDesiredStateIsReadOnly(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	fake := newFakeBridge(root)
	fake.mutateDuringRun = marker
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "desired-state", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("desired-state dispatch: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("desired-state mutated workspace marker, stat err=%v", err)
	}

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("run did not perform runner-owned reconciliation: %v", err)
	}
}

// DHF-TEST: keel/requirement-72
func TestRunDryRunResolvesDiscoveryServedIDsReadOnly(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "marker")
	fake := newFakeBridge(root)
	fake.mutateDuringRun = marker
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--dry-run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("dry-run dispatch: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run mutated workspace marker, stat err=%v", err)
	}
	if fake.sawRunLock || len(fake.runIDs) != 0 {
		t.Fatalf("dry-run executed run path: sawRunLock=%v runIDs=%v", fake.sawRunLock, fake.runIDs)
	}

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--dry-run", "--id", "demo::missing"})
	var usage cli.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "unknown test id") {
		t.Fatalf("unknown dry-run err = %v, want structured unknown-id usage error", err)
	}
}

// DHF-TEST: keel/requirement-72
func TestRunResolvesAliasToCanonicalBeforeRunner(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:          "demo::alias::fast",
		ParentID:    "demo::lane::fast",
		Label:       "fast alias",
		Kind:        "test",
		Runnable:    true,
		Profiles:    []string{"run"},
		CanonicalID: "demo::lane::fast",
	}}
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::alias::fast"}); err != nil {
		t.Fatalf("alias run dispatch: %v", err)
	}
	if !equalStrings(fake.runIDs, []string{"demo::lane::fast"}) {
		t.Fatalf("runner ids = %v, want canonical id only", fake.runIDs)
	}
}

// DHF-TEST: keel/requirement-58
func TestRunStartedRequestedLabelsUseDiscoveryLabelsOrRawIDs(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.extraItems = []vscode.TestItem{{
		ID:       "demo::lane::friendly",
		Label:    "Friendly Fast",
		Kind:     "lane",
		Runnable: true,
		Profiles: []string{"run"},
	}}
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		RunID:    func() string { return "run-labels" },
	})

	err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{
		"test-bridge", "tests", "run",
		"--id", "demo::lane::friendly",
		"--id", "other::lane::raw",
	})
	if err != nil {
		t.Fatalf("run dispatch: %v", err)
	}
	events := decodeEvents(t, protocol.String())
	if len(events) == 0 || events[0].Event != "run_started" {
		t.Fatalf("events = %+v, want first event run_started", events)
	}
	want := []vscode.RunRequest{
		{ID: "demo::lane::friendly", Label: "Friendly Fast"},
		{ID: "other::lane::raw", Label: "other::lane::raw"},
	}
	if len(events[0].Requested) != len(want) {
		t.Fatalf("requested = %+v, want %+v", events[0].Requested, want)
	}
	for i := range want {
		if events[0].Requested[i] != want[i] {
			t.Fatalf("requested[%d] = %+v, want %+v", i, events[0].Requested[i], want[i])
		}
	}
}

// DHF-TEST: keel/requirement-58
func TestPackageSourceDoesNotHardcodeKeelDomainIdentifiers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "keel::") {
			t.Fatalf("%s contains hardcoded keel-domain identifier literal", file)
		}
	}
}

func TestConfigHelpersInitUpgradeAndRefuseNewer(t *testing.T) {
	root := t.TempDir()
	template := newFakeBridge(root).ConfigTemplate()

	init, err := testbridge.InitConfig(root, template)
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	if !init.Changed || init.FromVersion != 0 || init.ToVersion != vscode.CurrentConfigVersion {
		t.Fatalf("init result = %+v, want changed 0 -> current", init)
	}
	again, err := testbridge.InitConfig(root, template)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if again.Changed {
		t.Fatalf("second init changed existing config: %+v", again)
	}

	path := filepath.Join(root, ".vscode", "test-bridge.json")
	old := `{"version":2,"command":"bin/custom","args":["go","run","./cmd/custom","vscode","tests"],"displayName":"Custom","env":{"A":"B"}}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	upgraded, err := testbridge.UpgradeConfig(root, template)
	if err != nil {
		t.Fatalf("upgrade config: %v", err)
	}
	if !upgraded.Changed || upgraded.FromVersion != 2 || upgraded.ToVersion != vscode.CurrentConfigVersion {
		t.Fatalf("upgrade result = %+v, want changed 2 -> current", upgraded)
	}
	var cfg vscode.TestBridgeConfig
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Command != "bin/custom" || cfg.Env["A"] != "B" {
		t.Fatalf("upgrade did not preserve consumer values: %+v", cfg)
	}
	if want := []string{"go", "run", "./cmd/custom"}; !equalStrings(cfg.Args, want) {
		t.Fatalf("upgrade args = %#v, want launcher-only %#v", cfg.Args, want)
	}

	if err := os.WriteFile(path, []byte(`{"version":999,"command":"bin/future","args":["wrapper"],"displayName":"Future"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := testbridge.UpgradeConfig(root, template); err == nil || !strings.Contains(err.Error(), "newer than this binary") {
		t.Fatalf("newer upgrade err = %v, want refusal", err)
	}
}

func TestRunErrorsAndLockConflictsUsePackagePaths(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.runErr = errors.New("runner failed")
	spec := testbridge.CommandSpec(fake)
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-error" }})

	err := spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	var runErr testbridge.RunError
	if !errors.As(err, &runErr) || runErr.ExitCode != 1 || !strings.Contains(runErr.Error(), "runner failed") || runErr.Unwrap() == nil {
		t.Fatalf("run error = %#v, want RunError wrapping runner failure", err)
	}

	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testbridge.RunLockPath(root), []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["x"],"token":"other"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = spec.Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	if err == nil || !strings.Contains(err.Error(), "keel/testbridge: run lock already exists") {
		t.Fatalf("lock conflict err = %v, want package-prefixed lock refusal", err)
	}
}

func TestRunLockExemptionLeavesExistingLockForConsumerMaintenance(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	fake.exemptRun = true
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-exempt" }})
	if err := os.MkdirAll(filepath.Dir(testbridge.RunLockPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := []byte(`{"pid":1,"created_at":"2026-07-13T00:00:00Z","ids":["demo::maintenance::unlock"],"token":"foreign"}` + "\n")
	if err := os.WriteFile(testbridge.RunLockPath(root), lock, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::maintenance::unlock"}); err != nil {
		t.Fatalf("lock-exempt run dispatch: %v", err)
	}
	got, err := os.ReadFile(testbridge.RunLockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, lock) {
		t.Fatalf("lock-exempt run modified foreign lock:\n%s", got)
	}
}

// DHF-TEST: keel/requirement-67
func TestRunLockReleaseSymmetryDoesNotWarnWhenNoLockWasAcquiredOrLockIsAbsent(t *testing.T) {
	root := t.TempDir()
	var logs bytes.Buffer
	fake := newFakeBridge(root)
	fake.exemptRun = true
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
		RunID:    func() string { return "run-exempt" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::maintenance::unlock"}); err != nil {
		t.Fatalf("lock-exempt run dispatch: %v", err)
	}
	if strings.Contains(logs.String(), "release testbridge run lock") || strings.Contains(logs.String(), "no such file or directory") {
		t.Fatalf("lock-exempt logs = %q, want no release-lock warning", logs.String())
	}

	root = t.TempDir()
	logs.Reset()
	fake = newFakeBridge(root)
	fake.removeRunLockDuringRun = true
	ctx = testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: io.Discard,
		Log:      slog.New(slog.NewTextHandler(&logs, nil)),
		RunID:    func() string { return "run-missing-lock" },
	})

	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("run with absent lock at release dispatch: %v", err)
	}
	if strings.Contains(logs.String(), "release testbridge run lock") || strings.Contains(logs.String(), "no such file or directory") {
		t.Fatalf("absent-lock release logs = %q, want missing lock treated as no-op", logs.String())
	}

	root = t.TempDir()
	fake = newFakeBridge(root)
	ctx = testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard, RunID: func() string { return "run-locked" }})
	if err := testbridge.CommandSpec(fake).Dispatch(ctx, []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"}); err != nil {
		t.Fatalf("locked run dispatch: %v", err)
	}
	if _, err := os.Stat(testbridge.RunLockPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock after acquired run stat err = %v, want removed lock", err)
	}
}

func TestValidationRejectsInvalidProtocolDocuments(t *testing.T) {
	now := time.Unix(1, 0).UTC()
	cases := []struct {
		name string
		doc  any
		want string
	}{
		{name: "unsupported", doc: struct{}{}, want: "unsupported protocol document"},
		{name: "discovery version", doc: vscode.DiscoveryDocument{Version: 2, Workspace: "w", ModulePath: "m", GeneratedAt: now}, want: "discovery version"},
		{name: "discovery item id", doc: vscode.DiscoveryDocument{Version: 1, Workspace: "w", ModulePath: "m", GeneratedAt: now, Items: []vscode.TestItem{{ID: "bad", Label: "bad", Kind: "lane"}}}, want: "does not match schema pattern"},
		{name: "desiredState status", doc: vscode.DesiredStateDocument{Version: 3, Devtool: vscode.DevtoolMetadata{Name: "d", Version: "v"}, Workspace: "w", GeneratedAt: now, Groups: []vscode.DesiredStateGroup{{Label: "Test Preconditions", Rows: []vscode.DesiredState{{Resource: "db", Kind: "service", Desired: "up", Current: "down", Status: "bogus", Action: "reuse"}}}}}, want: "invalid status"},
		{name: "run event", doc: vscode.RunEvent{Version: 1, Event: "bogus", Time: now}, want: "invalid event"},
		{name: "run lock", doc: vscode.RunLockFile{PID: 0, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}, Token: "t"}, want: "run-lock missing pid"},
		{name: "config", doc: vscode.TestBridgeConfig{Version: 999, Command: "bin/demo", DisplayName: "Demo"}, want: "config version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := testbridge.ValidateDocument(tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateDocument err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-60
func TestDesiredStateGroupsRequireExactlyOneActiveRowInExclusiveGroups(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	valid := vscode.DesiredStateDocument{
		Version:     3,
		Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: now,
		Groups: []vscode.DesiredStateGroup{{
			Label:             "Data set",
			Order:             10,
			MutuallyExclusive: true,
			Rows: []vscode.DesiredState{
				{Resource: "db-empty", Kind: "fixture-data", Desired: "empty", Current: "empty", Status: "satisfied", Action: "reuse", Message: "already empty", Reusable: true, Active: true},
				{Resource: "db-small", Kind: "fixture-data", Desired: "small", Current: "empty", Status: "reconcilable", Action: "reconcile_during_run", Message: "seed small", Owned: true},
			},
		}},
		TeardownPolicy: "owned-after-run",
	}
	if err := testbridge.ValidateDocument(valid); err != nil {
		t.Fatalf("exclusive group with one active row should validate: %v", err)
	}

	noneActive := valid
	noneActive.Groups = cloneDesiredStateGroups(valid.Groups)
	noneActive.Groups[0].Rows[0].Active = false
	if err := testbridge.ValidateDocument(noneActive); err == nil || !strings.Contains(err.Error(), "exactly one active row") {
		t.Fatalf("zero-active exclusive group err = %v, want exactly one active row", err)
	}

	multipleActive := valid
	multipleActive.Groups = cloneDesiredStateGroups(valid.Groups)
	multipleActive.Groups[0].Rows[1].Active = true
	if err := testbridge.ValidateDocument(multipleActive); err == nil || !strings.Contains(err.Error(), "exactly one active row") {
		t.Fatalf("multi-active exclusive group err = %v, want exactly one active row", err)
	}
}

// Runnable rows carry a devtool-served run_id; run ids must be unique across
// the whole document so activation is unambiguous (formal_review-80).
//
// DHF-TEST: keel/requirement-60
func TestDesiredStateRowRunIDsAreUniqueAcrossDocument(t *testing.T) {
	now := time.Unix(3, 0).UTC()
	valid := vscode.DesiredStateDocument{
		Version:     3,
		Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: now,
		Groups: []vscode.DesiredStateGroup{{
			Label: "Test Preconditions",
			Order: 10,
			Rows: []vscode.DesiredState{
				{Resource: "python", Kind: "tool", Desired: "available", Current: "available", Status: "satisfied", Action: "reuse", Message: "ok", Reusable: true},
				{RunID: "demo::action::provision-python-env", Resource: "python-env", Kind: "dependency", Desired: "provisioned", Current: "missing", Status: "reconcilable", Action: "reconcile", Message: "provision", Owned: true},
			},
		}},
	}
	if err := testbridge.ValidateDocument(valid); err != nil {
		t.Fatalf("desiredState with a served run_id should validate: %v", err)
	}

	dup := valid
	dup.Groups = cloneDesiredStateGroups(valid.Groups)
	dup.Groups[0].Rows[0].RunID = "demo::action::provision-python-env"
	if err := testbridge.ValidateDocument(dup); err == nil || !strings.Contains(err.Error(), "run ids must be unique") {
		t.Fatalf("duplicate run_id err = %v, want run ids must be unique", err)
	}
}

// DHF-TEST: keel/requirement-60
func TestDesiredStateDocumentV3EnvelopeAndGroupsOnly(t *testing.T) {
	now := time.Unix(4, 0).UTC()
	valid := vscode.DesiredStateDocument{
		Version:     3,
		Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
		Workspace:   "w",
		GeneratedAt: now,
		Groups: []vscode.DesiredStateGroup{{
			Label: "Test Preconditions",
			Order: 10,
			Rows: []vscode.DesiredState{{
				RunID:    "demo::desired-state::db",
				Resource: "db",
				Kind:     "service",
				Desired:  "up",
				Current:  "down",
				Status:   "reconcilable",
				Action:   "reconcile_during_run",
				Message:  "start database during run",
				Owned:    true,
			}},
		}},
		TeardownPolicy: "owned resources are cleaned after the run",
	}
	if err := testbridge.ValidateDocument(valid); err != nil {
		t.Fatalf("v3 envelope plus groups should validate: %v", err)
	}

	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]bool{
		"version":         true,
		"devtool":         true,
		"workspace":       true,
		"generated_at":    true,
		"groups":          true,
		"teardown_policy": true,
	}
	for key := range keys {
		if !wantKeys[key] {
			t.Fatalf("DesiredStateDocument encoded removed field %q in %s", key, raw)
		}
	}
	for key := range wantKeys {
		if _, ok := keys[key]; !ok {
			t.Fatalf("DesiredStateDocument encoded keys %v, missing %q", keys, key)
		}
	}

	legacyJSON := []byte(`{
		"version": 3,
		"devtool": {"name": "d", "version": "v", "commit": "", "built_at": ""},
		"workspace": "w",
		"generated_at": "1970-01-01T00:00:04Z",
		"items": [],
		"groups": [{
			"label": "Test Preconditions",
			"order": 10,
			"mutually_exclusive": false,
			"rows": [{
				"resource": "db",
				"kind": "service",
				"desired": "up",
				"current": "down",
				"status": "reconcilable",
				"action": "reconcile_during_run",
				"message": "start database during run",
				"reusable": false,
				"owned": true
			}]
		}]
	}`)
	var decoded vscode.DesiredStateDocument
	if err := json.Unmarshal(legacyJSON, &decoded); err == nil || !strings.Contains(err.Error(), "removed field") {
		t.Fatalf("legacy desired-state decode err = %v, want removed field rejection", err)
	}
}

func TestCommandSpecErrorsAndRuntimeDefaults(t *testing.T) {
	root := t.TempDir()
	fake := newFakeBridge(root)
	spec := testbridge.CommandSpec(fake)

	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "discover"}); err != nil {
		t.Fatalf("discover with default runtime: %v", err)
	}
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "run"}); err == nil || !strings.Contains(err.Error(), "--id is required") {
		t.Fatalf("run without id err = %v, want --id required", err)
	}
	for _, args := range [][]string{
		{"test-bridge", "tests", "discover", "--format", "yaml"},
		{"test-bridge", "tests", "desired-state", "--id"},
		{"test-bridge", "tests", "desired-state", "extra"},
		{"test-bridge", "config", "init", "extra"},
		{"test-bridge", "config", "upgrade", "extra"},
	} {
		if err := spec.Dispatch(context.Background(), args); err == nil {
			t.Fatalf("Dispatch(%v) returned nil, want usage error", args)
		}
	}

	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := spec.Dispatch(testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: root, Protocol: io.Discard}), []string{"test-bridge", "config", "upgrade"}); err != nil {
		t.Fatalf("config upgrade dispatch: %v", err)
	}

	fake.discoverErr = errors.New("discover failed")
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "discover"}); err == nil || !strings.Contains(err.Error(), "discover failed") {
		t.Fatalf("discover provider err = %v, want provider failure", err)
	}
	fake.discoverErr = nil
	fake.desiredErr = errors.New("desired failed")
	if err := spec.Dispatch(context.Background(), []string{"test-bridge", "tests", "desired-state"}); err == nil || !strings.Contains(err.Error(), "desired failed") {
		t.Fatalf("desired provider err = %v, want provider failure", err)
	}

	fileRoot := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileFake := newFakeBridge(fileRoot)
	err := testbridge.CommandSpec(fileFake).Dispatch(testbridge.WithRuntime(context.Background(), testbridge.Runtime{Root: fileRoot, Protocol: io.Discard}), []string{"test-bridge", "tests", "run", "--id", "demo::lane::fast"})
	if err == nil {
		t.Fatal("run with file root returned nil, want run writer mkdir failure")
	}
}

func TestValidationCoversClosedEnumsAndRequiredFields(t *testing.T) {
	now := time.Unix(2, 0).UTC()
	validDiscovery := func() vscode.DiscoveryDocument {
		return vscode.DiscoveryDocument{
			Version:     1,
			Workspace:   "w",
			ModulePath:  "m",
			GeneratedAt: now,
			Capabilities: vscode.DiscoveryCapabilities{
				ClearResults:              true,
				RefreshInvalidatesResults: true,
				NeutralParentRollups:      true,
			},
			Items: []vscode.TestItem{{ID: "demo::lane::fast", Label: "fast", Kind: "lane", Runnable: true, Profiles: []string{"run"}}},
		}
	}
	validDesiredStateDocument := func() vscode.DesiredStateDocument {
		return vscode.DesiredStateDocument{
			Version:     3,
			Devtool:     vscode.DevtoolMetadata{Name: "d", Version: "v"},
			Workspace:   "w",
			GeneratedAt: now,
			Groups: []vscode.DesiredStateGroup{{
				Label: "Test Preconditions",
				Rows: []vscode.DesiredState{{
					Resource: "db",
					Kind:     "service",
					Desired:  "up",
					Current:  "down",
					Status:   "reconcilable",
					Action:   "reconcile_during_run",
					Message:  "ok",
				}},
			}},
		}
	}
	assertValid := func(name string, doc any) {
		t.Helper()
		if err := testbridge.ValidateDocument(doc); err != nil {
			t.Fatalf("%s should validate: %v", name, err)
		}
	}
	assertInvalid := func(name string, doc any, want string) {
		t.Helper()
		err := testbridge.ValidateDocument(doc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s err = %v, want containing %q", name, err, want)
		}
	}

	assertValid("discovery", validDiscovery())
	discovery := validDiscovery()
	discovery.Workspace = ""
	assertInvalid("discovery required", discovery, "missing workspace")
	discovery = validDiscovery()
	discovery.Items[0].Kind = "folder"
	assertInvalid("discovery kind", discovery, "invalid kind")
	discovery = validDiscovery()
	discovery.Items[0].Profiles = []string{"profile"}
	assertInvalid("discovery profile", discovery, "invalid profile")

	assertValid("desired-state document", validDesiredStateDocument())
	desiredState := validDesiredStateDocument()
	desiredState.Groups[0].Rows[0].Resource = ""
	assertInvalid("desired required", desiredState, "missing required fields")
	desiredState = validDesiredStateDocument()
	desiredState.Groups[0].Rows[0].Kind = "workspace"
	assertInvalid("desired kind", desiredState, "invalid kind")
	desiredState = validDesiredStateDocument()
	desiredState.Groups[0].Rows[0].Action = "setup_required"
	assertInvalid("desired action", desiredState, "invalid action")

	assertValid("run event", vscode.RunEvent{Version: 1, Event: "passed", Time: now, Source: "vscode"})
	assertInvalid("run source", vscode.RunEvent{Version: 1, Event: "passed", Time: now, Source: "consumer"}, "invalid source")
	assertInvalid("run duration", vscode.RunEvent{Version: 1, Event: "passed", Time: now, DurationMS: -1}, "negative")
	assertInvalid("run artifact", vscode.RunEvent{Version: 1, Event: "artifact", Time: now, Artifact: &vscode.RunArtifact{Name: "a", URI: "file:///tmp/a", Kind: "zip"}}, "artifact")

	assertValid("run lock", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}, Token: "t"})
	assertInvalid("run lock time", vscode.RunLockFile{PID: 1, CreatedAt: "bad", IDs: []string{"x"}, Token: "t"}, "created_at")
	assertInvalid("run lock id", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{""}, Token: "t"}, "empty id")
	assertInvalid("run lock token", vscode.RunLockFile{PID: 1, CreatedAt: now.Format(time.RFC3339Nano), IDs: []string{"x"}}, "token")
	assertInvalid("config missing", vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion}, "missing command")
	legacyArgs := []string{"vs" + "code", "tests"}
	assertInvalid("config protocol args", vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo", Args: legacyArgs, DisplayName: "Demo"}, "launcher-only")
}

func cloneDesiredStateGroups(groups []vscode.DesiredStateGroup) []vscode.DesiredStateGroup {
	cloned := append([]vscode.DesiredStateGroup(nil), groups...)
	for i := range cloned {
		cloned[i].Rows = append([]vscode.DesiredState(nil), cloned[i].Rows...)
	}
	return cloned
}

func desiredStateGroupByLabel(t *testing.T, groups []vscode.DesiredStateGroup, label string) vscode.DesiredStateGroup {
	t.Helper()
	for _, group := range groups {
		if group.Label == label {
			return group
		}
	}
	t.Fatalf("missing desired-state group %q in %+v", label, groups)
	return vscode.DesiredStateGroup{}
}

func desiredStateRowByResource(t *testing.T, rows []vscode.DesiredState, resource string) vscode.DesiredState {
	t.Helper()
	for _, row := range rows {
		if row.Resource == resource {
			return row
		}
	}
	t.Fatalf("missing desired-state row %q in %+v", resource, rows)
	return vscode.DesiredState{}
}

type fakeBridge struct {
	root                            string
	calls                           string
	extraItems                      []vscode.TestItem
	runIDs                          []string
	runCalls                        int
	cancelAfterFirstRun             context.CancelFunc
	mutateDuringRun                 string
	sawRunLock                      bool
	exemptRun                       bool
	removeRunLockDuringRun          bool
	clearStatePath                  string
	clearStateCalls                 int
	runErr                          error
	discoverErr                     error
	desiredErr                      error
	desiredGroups                   []testbridge.DesiredStateGroup
	filterDesiredStateByIDs         bool
	desiredStateEmptyForSelectedIDs bool
}

func newFakeBridge(root string) *fakeBridge {
	return &fakeBridge{root: root}
}

func (f *fakeBridge) Metadata() vscode.DevtoolMetadata {
	return vscode.DevtoolMetadata{Name: "demo-dev", Version: "v0.0.0", Commit: "abc123", BuiltAt: "test"}
}

func (f *fakeBridge) Workspace() testbridge.Workspace {
	return testbridge.Workspace{Root: f.root, Node: "consumer-node", ModulePath: "example.dev/tool"}
}

func (f *fakeBridge) ConfigTemplate() vscode.TestBridgeConfig {
	return vscode.TestBridgeConfig{Version: vscode.CurrentConfigVersion, Command: "bin/demo-dev", Args: []string{}, DisplayName: "Demo"}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func limitationValue(limitations []string, key string) string {
	prefix := key + "="
	for _, limitation := range limitations {
		if strings.HasPrefix(limitation, prefix) {
			return strings.TrimPrefix(limitation, prefix)
		}
	}
	return ""
}

func (f *fakeBridge) Discover(_ context.Context) (vscode.DiscoveryDocument, error) {
	if f.discoverErr != nil {
		return vscode.DiscoveryDocument{}, f.discoverErr
	}
	f.appendCall("discover")
	return vscode.DiscoveryDocument{
		Version:     1,
		Workspace:   "consumer-node",
		ModulePath:  "example.dev/tool",
		GeneratedAt: time.Unix(1, 0).UTC(),
		Capabilities: vscode.DiscoveryCapabilities{
			ClearResults:              true,
			RefreshInvalidatesResults: true,
			NeutralParentRollups:      true,
		},
		Items: append([]vscode.TestItem{{
			ID:       "demo::lane::fast",
			Label:    "fast",
			Kind:     "lane",
			Runnable: true,
			Profiles: []string{"run"},
			LaneID:   "demo::lane::fast",
		}}, f.extraItems...),
	}, nil
}

func (f *fakeBridge) DesiredState(_ context.Context, ids []string) (testbridge.DesiredStateDeclaration, error) {
	if f.desiredErr != nil {
		return testbridge.DesiredStateDeclaration{}, f.desiredErr
	}
	f.appendCall("desiredState:" + strings.Join(ids, ","))
	if f.desiredStateEmptyForSelectedIDs && len(ids) > 0 {
		return testbridge.DesiredStateDeclaration{}, nil
	}
	groups := f.desiredGroups
	if groups == nil {
		groups = []testbridge.DesiredStateGroup{{
			Label: "Test Preconditions",
			Rows:  []testbridge.DesiredStateRow{probedRow("", "db", "service", "seeded", "empty", false, "seed test database during run", false, false)},
		}}
	}
	if f.filterDesiredStateByIDs && len(ids) > 0 {
		groups = filterDesiredStateGroupsByRunID(groups, ids)
	}
	return testbridge.DesiredStateDeclaration{
		Groups: groups,
	}, nil
}

func filterDesiredStateGroupsByRunID(groups []testbridge.DesiredStateGroup, ids []string) []testbridge.DesiredStateGroup {
	keep := map[string]struct{}{}
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	filtered := make([]testbridge.DesiredStateGroup, 0, len(groups))
	for _, group := range groups {
		rows := make([]testbridge.DesiredStateRow, 0, len(group.Rows))
		for _, row := range group.Rows {
			if _, ok := keep[row.RunID]; ok {
				rows = append(rows, row)
			}
		}
		if len(rows) == 0 {
			continue
		}
		group.Rows = rows
		filtered = append(filtered, group)
	}
	return filtered
}

func probedRow(runID, resource, kind, desired, current string, satisfied bool, message string, reusable, active bool) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     kind,
		Desired:  desired,
		Reusable: reusable,
		Owned:    !reusable,
		Active:   active,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			return testbridge.DesiredStateProbeResult{Current: current, Satisfied: satisfied, Message: message}
		},
	}
}

func mutableDesiredStateRow(runID, resource, desired string, satisfied func() bool) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     "fixture-data",
		Desired:  desired,
		Owned:    true,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			if satisfied() {
				return testbridge.DesiredStateProbeResult{Current: desired, Satisfied: true, Message: resource + " active"}
			}
			return testbridge.DesiredStateProbeResult{Current: "small", Satisfied: false, Message: resource + " reconcilable"}
		},
	}
}

func probedCountingRow(calls map[string]int, runID, resource, desired string, satisfied bool, message string) testbridge.DesiredStateRow {
	return testbridge.DesiredStateRow{
		RunID:    runID,
		Resource: resource,
		Kind:     "fixture-data",
		Desired:  desired,
		Owned:    true,
		Probe: func(context.Context, testbridge.DesiredStateProbeRequest) testbridge.DesiredStateProbeResult {
			calls[runID]++
			current := "missing"
			if satisfied {
				current = desired
			}
			return testbridge.DesiredStateProbeResult{Current: current, Satisfied: satisfied, Message: message}
		},
	}
}

func testItemByID(items []vscode.TestItem, id string) (vscode.TestItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return vscode.TestItem{}, false
}

func (f *fakeBridge) Run(_ context.Context, req testbridge.RunRequest, emit vscode.RunEventWriter) (int, error) {
	f.runCalls++
	f.runIDs = append(f.runIDs, req.IDs...)
	if f.cancelAfterFirstRun != nil && f.runCalls == 1 {
		f.cancelAfterFirstRun()
	}
	if _, err := os.Stat(filepath.Join(f.root, ".devtools", "vscode-runs", "run.lock")); err == nil {
		f.sawRunLock = true
	}
	if f.removeRunLockDuringRun {
		if err := os.Remove(filepath.Join(f.root, ".devtools", "vscode-runs", "run.lock")); err != nil {
			return 1, err
		}
	}
	if f.mutateDuringRun != "" {
		if err := os.WriteFile(f.mutateDuringRun, []byte("run\n"), 0o644); err != nil {
			return 1, err
		}
	}
	for _, id := range req.IDs {
		emit(vscode.RunEvent{Event: "passed", TestID: id})
	}
	if f.runErr != nil {
		return 1, f.runErr
	}
	return 0, nil
}

func (f *fakeBridge) LockExemptRun([]string) bool {
	return f.exemptRun
}

func (f *fakeBridge) ClearState(_ context.Context, _ testbridge.RunRequest, _ vscode.RunEventWriter) (int, error) {
	f.clearStateCalls++
	if f.clearStatePath == "" {
		return 0, nil
	}
	if err := os.Remove(f.clearStatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 1, err
	}
	return 0, nil
}

func (f *fakeBridge) appendCall(call string) {
	if f.calls != "" {
		f.calls += ","
	}
	f.calls += call
}

func eventsContain(events []vscode.RunEvent, event, testID, message string) bool {
	for _, got := range events {
		if got.Event == event && got.TestID == testID && strings.Contains(got.Message, message) {
			return true
		}
	}
	return false
}

func eventMessageContainsAll(events []vscode.RunEvent, event, testID string, substrings ...string) bool {
	for _, got := range events {
		if got.Event != event || got.TestID != testID {
			continue
		}
		for _, substring := range substrings {
			if !strings.Contains(got.Message, substring) {
				return false
			}
		}
		return true
	}
	return false
}

func protocolFromContext(t *testing.T, ctx context.Context) *bytes.Buffer {
	t.Helper()
	runtime, ok := testbridge.RuntimeFrom(ctx)
	if !ok {
		t.Fatal("runtime missing")
	}
	buf, ok := runtime.Protocol.(*bytes.Buffer)
	if !ok {
		t.Fatalf("protocol writer = %T, want *bytes.Buffer", runtime.Protocol)
	}
	return buf
}

func decodeJSON(t *testing.T, buf *bytes.Buffer, out any) {
	t.Helper()
	if err := json.Unmarshal(buf.Bytes(), out); err != nil {
		t.Fatalf("decode JSON %T: %v\n%s", out, err, buf.String())
	}
}

func decodeEvents(t *testing.T, raw string) []vscode.RunEvent {
	t.Helper()
	var events []vscode.RunEvent
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

// EncodeDocument is the package-owned protocol JSON sink consumer devtools route
// their protocol output through instead of hand-rolling a json.Encoder each.
//
// DHF-TEST: keel/requirement-63
func TestEncodeDocumentOwnsCanonicalProtocolJSON(t *testing.T) {
	doc := map[string]any{"module_path": "keel", "note": "a<b>c & d"}
	var buf bytes.Buffer
	if err := testbridge.EncodeDocument(&buf, doc); err != nil {
		t.Fatalf("EncodeDocument: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("EncodeDocument output must end with a newline: %q", out)
	}
	// HTML escaping is disabled so protocol payloads stay byte-faithful.
	if !strings.Contains(out, "a<b>c & d") {
		t.Fatalf("EncodeDocument must not HTML-escape payloads: %q", out)
	}
	var round map[string]any
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("re-decode encoded document: %v\n%s", err, out)
	}
	if round["module_path"] != "keel" {
		t.Fatalf("round-tripped document = %+v, want module_path=keel", round)
	}
	// A nil writer is tolerated (discards), matching the package sink default.
	if err := testbridge.EncodeDocument(nil, doc); err != nil {
		t.Fatalf("EncodeDocument(nil): %v", err)
	}
}
