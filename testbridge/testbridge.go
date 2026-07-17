// Package testbridge provides the reusable devtool side of keel's VS Code test
// bridge contract. A consumer devtool supplies content providers; this package
// owns the canonical argv tree, protocol JSON emission, config helpers, run
// event streaming, and run.lock serialization.
//
// DHF-REQ: keel/requirement-58
package testbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/david-aggeler/keel/cli"
	"github.com/david-aggeler/keel/vscode"
)

type runtimeKey struct{}
type desiredStateReportKey struct{}

// Runtime carries the process-local sinks and workspace root used while a
// canonical test-bridge command is executing.
type Runtime struct {
	Root     string
	Protocol io.Writer
	Log      *slog.Logger
	Now      func() time.Time
	RunID    func() string
}

const externalRunStreamRetentionLimit = 32

// WithRuntime stores Runtime in ctx for a CommandSpec handler.
func WithRuntime(ctx context.Context, rt Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// RuntimeFrom returns the Runtime stored in ctx.
func RuntimeFrom(ctx context.Context) (Runtime, bool) {
	rt, ok := ctx.Value(runtimeKey{}).(Runtime)
	return rt, ok
}

func withDesiredStateReport(ctx context.Context) context.Context {
	return context.WithValue(ctx, desiredStateReportKey{}, true)
}

// DesiredStateReportRequested reports whether the current DesiredState call is
// serving the read-only desired-state document rather than planning a run.
func DesiredStateReportRequested(ctx context.Context) bool {
	requested, _ := ctx.Value(desiredStateReportKey{}).(bool)
	return requested
}

// Workspace identifies the consumer workspace in protocol envelopes.
type Workspace struct {
	Root       string
	Node       string
	ModulePath string
}

// DiscoveryProvider supplies the test tree content. The package wraps the
// result in the canonical argv and JSON sink discipline.
type DiscoveryProvider interface {
	Discover(context.Context) (vscode.DiscoveryDocument, error)
}

// DesiredStateProvider supplies declared desired-state rows for a selection.
// The package executes row probes to derive the protocol state fields.
type DesiredStateProvider interface {
	DesiredState(context.Context, []string) (DesiredStateDeclaration, error)
}

// DesiredStateDeclaration is the consumer-declared structure for desired state.
// Current, Status, Action, and Message are derived by executing row probes.
//
// DHF-REQ: keel/requirement-77
type DesiredStateDeclaration struct {
	Groups         []DesiredStateGroup
	TeardownPolicy string
}

// DesiredStateGroup is a consumer-declared desired-state row cluster.
type DesiredStateGroup struct {
	Label             string
	Order             int
	MutuallyExclusive bool
	Rows              []DesiredStateRow
}

// DesiredStateRow is the consumer registration contract for one desired-state
// row. It deliberately carries no Current, Status, or Action field.
type DesiredStateRow struct {
	RunID    string
	Resource string
	Kind     string
	Desired  string
	Detail   string
	Reusable bool
	Owned    bool
	Active   bool
	Probe    DesiredStateProbe
}

// DesiredStateProbe derives the live state for one desired-state row.
type DesiredStateProbe func(context.Context, DesiredStateProbeRequest) DesiredStateProbeResult

// DesiredStateProbeRequest describes the row probe invocation.
type DesiredStateProbeRequest struct {
	RunID string
	Root  string
}

// DesiredStateProbeResult is the state observed by a desired-state row probe.
type DesiredStateProbeResult struct {
	Current   string
	Satisfied bool
	Message   string
}

const (
	exclusiveUnknownResource = "Unknown State"
	exclusiveUnknownKind     = "unknown"
	exclusiveUnknownValue    = "unknown"
)

// Runner executes a selected run and emits run events through the package-owned
// writer.
type Runner interface {
	Run(context.Context, RunRequest, vscode.RunEventWriter) (int, error)
}

// ConfigProvider supplies the consumer's test-bridge config template.
type ConfigProvider interface {
	ConfigTemplate() vscode.TestBridgeConfig
}

// WorkspaceProvider supplies workspace metadata used by envelopes and run-event
// attribution.
type WorkspaceProvider interface {
	Workspace() Workspace
}

// MetadataProvider supplies devtool identity for desired-state documents.
type MetadataProvider interface {
	Metadata() vscode.DevtoolMetadata
}

// TestTreeProvider is an explicit content-provider name for consumers that want
// their package boundary to mirror the contract language.
type TestTreeProvider interface {
	DiscoveryProvider
}

// MaintenanceItemProvider describes maintenance items a consumer may fold into
// its discovery tree before returning a DiscoveryDocument.
type MaintenanceItemProvider interface {
	MaintenanceItems(context.Context) ([]vscode.TestItem, error)
}

// Bridge-owned maintenance vocabulary. Consumers may reference these ids from
// run handlers, but they do not author the discovery rows or capability arrays.
const (
	MaintenanceGroupID        = "keel" + "::maintenance"
	MaintenanceDetectLanesID  = MaintenanceGroupID + "::detect-lanes"
	MaintenanceUnlockID       = MaintenanceGroupID + "::unlock"
	MaintenanceClearResultsID = MaintenanceGroupID + "::clear-results"
	MaintenanceClearStateID   = MaintenanceGroupID + "::clear-state"
)

// ClearStateProvider supplies the only consumer-owned action behind the
// bridge-owned Group-A vocabulary: clearing local devtool state.
type ClearStateProvider interface {
	ClearState(context.Context, RunRequest, vscode.RunEventWriter) (int, error)
}

// LaneProvider describes runnable lanes a consumer may fold into its discovery
// tree before returning a DiscoveryDocument.
type LaneProvider interface {
	Lanes(context.Context) ([]vscode.TestItem, error)
}

// Bridge is the provider set required by the canonical command tree.
type Bridge interface {
	DiscoveryProvider
	DesiredStateProvider
	Runner
	ConfigProvider
	WorkspaceProvider
	MetadataProvider
}

type lockExemptRunner interface {
	LockExemptRun([]string) bool
}

// RunRequest is the package-owned runner invocation contract.
type RunRequest struct {
	IDs   []string
	RunID string
	Root  string
}

// CommandSpec returns a dispatch root for the canonical protocol token:
// test-bridge tests discover|desired-state|run and config init|upgrade.
//
// DHF-REQ: keel/requirement-58, keel/requirement-60
func CommandSpec(bridge Bridge) *cli.CommandSpec {
	return &cli.CommandSpec{
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "test-bridge",
				Short: "Serve VS Code test-bridge protocol commands.",
				Subcommands: []*cli.CommandSpec{
					{
						Name:  "config",
						Short: "Initialize or upgrade test bridge config.",
						Subcommands: []*cli.CommandSpec{
							{Name: "init", Use: "test-bridge config init", Short: "Write .vscode/test-bridge.json if absent.", Handler: handleConfigInit(bridge)},
							{Name: "upgrade", Use: "test-bridge config upgrade", Short: "Upgrade .vscode/test-bridge.json to the current schema.", Handler: handleConfigUpgrade(bridge)},
						},
					},
					{
						Name:  "tests",
						Short: "Discover tests, report desired state, and run selections.",
						Subcommands: []*cli.CommandSpec{
							{Name: "discover", Use: "test-bridge tests discover [--format json]", Short: "Emit the test discovery document.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}}, Handler: handleDiscover(bridge)},
							{Name: "desired-state", Use: "test-bridge tests desired-state [--format json] [--id test-id]", Short: "Emit the read-only desired-state document.", Flags: []cli.FlagSpec{{Name: "format", Value: "json", Short: "Output format."}, {Name: "id", Value: "test-id", Short: "Selected test id."}}, Handler: handleDesiredState(bridge)},
							{Name: "run", Use: "test-bridge tests run [--dry-run] --id test-id", Short: "Run selected tests.", Flags: []cli.FlagSpec{{Name: "id", Value: "test-id", Short: "Selected test id."}, {Name: "dry-run", Short: "Resolve selected test ids without executing them."}}, Handler: handleRun(bridge)},
						},
					},
				},
			},
		},
	}
}

func handleDiscover(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		ids, err := parseIDs(args, true, true)
		if err != nil {
			logBridgeDispatch(rt, "discover", bridgeDispatchLog{Args: args, Err: err})
			return err
		}
		logBridgeDispatch(rt, "discover", bridgeDispatchLog{Args: args, IDs: ids})
		doc, err := discoverWithDerivedDesiredState(ctx, bridge)
		if err != nil {
			return err
		}
		return writeDocument(rt, doc)
	}
}

func discoverWithDerivedDesiredState(ctx context.Context, bridge Bridge) (vscode.DiscoveryDocument, error) {
	doc, err := bridge.Discover(ctx)
	if err != nil {
		return vscode.DiscoveryDocument{}, err
	}
	doc, err = deriveMaintenanceDiscovery(doc)
	if err != nil {
		return vscode.DiscoveryDocument{}, err
	}
	return deriveDesiredStateDiscovery(ctx, bridge, doc)
}

// DHF-REQ: keel/requirement-87
func deriveMaintenanceDiscovery(doc vscode.DiscoveryDocument) (vscode.DiscoveryDocument, error) {
	doc.Capabilities.ClearResultsTestIDs = []string{MaintenanceClearResultsID}
	doc.Capabilities.ClearStateTestIDs = []string{MaintenanceClearStateID}
	doc.Items = appendMissingDiscoveryItems(doc.Items, bridgeMaintenanceItems()...)
	return doc, nil
}

func bridgeMaintenanceItems() []vscode.TestItem {
	return []vscode.TestItem{
		{
			ID:       MaintenanceGroupID,
			Label:    "A - Test Bridge Maintenance",
			SortText: "a",
			Kind:     "group",
			Profiles: []string{},
		},
		bridgeMaintenanceItem(MaintenanceDetectLanesID, "a.1 detect lanes", "a.001"),
		bridgeMaintenanceItem(MaintenanceUnlockID, "a.2 unlock test bridge", "a.002"),
		bridgeMaintenanceItem(MaintenanceClearResultsID, "a.3 clear test results", "a.003"),
		bridgeMaintenanceItem(MaintenanceClearStateID, "a.4 clear local test state", "a.004"),
	}
}

func bridgeMaintenanceItem(id, label, sortText string) vscode.TestItem {
	return vscode.TestItem{
		ID:          id,
		ParentID:    MaintenanceGroupID,
		Label:       label,
		SortText:    sortText,
		Kind:        "maintenance",
		Framework:   "keel",
		Runner:      "testbridge",
		RunnerLabel: "testbridge",
		Runnable:    true,
		Profiles:    []string{"run"},
	}
}

func appendMissingDiscoveryItems(items []vscode.TestItem, candidates ...vscode.TestItem) []vscode.TestItem {
	seen := make(map[string]struct{}, len(items)+len(candidates))
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range candidates {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		items = append(items, item)
		seen[item.ID] = struct{}{}
	}
	return items
}

// DHF-REQ: keel/requirement-74, keel/requirement-83
func deriveDesiredStateDiscovery(ctx context.Context, bridge Bridge, doc vscode.DiscoveryDocument) (vscode.DiscoveryDocument, error) {
	parent, ok := desiredStateParent(doc.Items)
	if !ok {
		return doc, nil
	}
	doc.Items = withoutDesiredStateChildren(doc.Items, parent.ID)
	desiredState, err := bridge.DesiredState(ctx, nil)
	if err != nil {
		doc.Items = append(doc.Items, desiredStateDiagnosticItem(parent.ID, err))
		if err := validateUniqueDiscoveryItemIDs(doc.Items); err != nil {
			return vscode.DiscoveryDocument{}, err
		}
		return doc, nil
	}
	rt := runtimeOrDefault(ctx, bridge)
	items, err := desiredStateDeclarationDiscoveryItems(ctx, runtimeRoot(rt, bridge), parent.ID, desiredState.Groups)
	if err != nil {
		return vscode.DiscoveryDocument{}, err
	}
	doc.Items = append(doc.Items, items...)
	if err := validateUniqueDiscoveryItemIDs(doc.Items); err != nil {
		return vscode.DiscoveryDocument{}, err
	}
	return doc, nil
}

func validateUniqueDiscoveryItemIDs(items []vscode.TestItem) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			return fmt.Errorf("keel/testbridge: duplicate discovery item id %q", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func withoutDesiredStateChildren(items []vscode.TestItem, parentID string) []vscode.TestItem {
	remove := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for _, item := range items {
			if item.ParentID == parentID || remove[item.ParentID] {
				if !remove[item.ID] {
					remove[item.ID] = true
					changed = true
				}
			}
		}
	}
	filtered := items[:0]
	for _, item := range items {
		if remove[item.ID] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func desiredStateParent(items []vscode.TestItem) (vscode.TestItem, bool) {
	for _, item := range items {
		if item.Label == "B - Desired State" && item.Kind == "group" {
			return item, true
		}
	}
	return vscode.TestItem{}, false
}

func desiredStateDiagnosticItem(parentID string, err error) vscode.TestItem {
	return vscode.TestItem{
		ID:          parentID + "::diagnostic::desired-state",
		ParentID:    parentID,
		Label:       "desired-state unavailable",
		Kind:        "group",
		Runnable:    false,
		Profiles:    []string{},
		Limitations: []string{err.Error()},
	}
}

// DHF-REQ: keel/requirement-75, keel/requirement-83, keel/requirement-88
func desiredStateDeclarationDiscoveryItems(ctx context.Context, root, parentID string, groups []DesiredStateGroup) ([]vscode.TestItem, error) {
	groups = append([]DesiredStateGroup(nil), groups...)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Order < groups[j].Order })
	items := make([]vscode.TestItem, 0)
	for _, group := range groups {
		groupID := parentID + "::group::" + stableIDSegment(group.Label)
		derivedRows, err := deriveDesiredStateGroupRows(ctx, root, parentID, group)
		if err != nil {
			return nil, err
		}
		runnable := !group.MutuallyExclusive && desiredStateGroupHasRunnableRows(group)
		profiles := []string{}
		if runnable {
			profiles = []string{"run"}
		}
		groupItem := vscode.TestItem{
			ID:          groupID,
			ParentID:    parentID,
			Label:       group.Label,
			SortText:    fmt.Sprintf("b.%03d", group.Order),
			Kind:        "group",
			Runnable:    runnable,
			Profiles:    profiles,
			Limitations: []string{fmt.Sprintf("mutually_exclusive=%t", group.MutuallyExclusive)},
		}
		items = append(items, groupItem)
		for rowIndex, row := range derivedRows {
			items = append(items, desiredStateDeclarationDiscoveryItem(groupID, groupItem.SortText, rowIndex+1, row.Declaration, row.State))
		}
	}
	return items, nil
}

func desiredStateGroupHasRunnableRows(group DesiredStateGroup) bool {
	for _, row := range group.Rows {
		if row.RunID != "" {
			return true
		}
	}
	return false
}

func desiredStateDeclarationDiscoveryItem(parentID, parentSort string, rowIndex int, row DesiredStateRow, derived vscode.DesiredState) vscode.TestItem {
	action := derived.Action
	id := row.RunID
	if id == "" {
		id = parentID + "::row::" + stableIDSegment(strings.Join([]string{row.Resource, row.Kind, row.Desired, row.Detail}, "-"))
	}
	profiles := []string{}
	if row.RunID != "" {
		profiles = []string{"run"}
	}
	return vscode.TestItem{
		ID:          id,
		ParentID:    parentID,
		Label:       fmt.Sprintf("%s: %s", row.Resource, row.Desired),
		SortText:    fmt.Sprintf("%s.%03d", parentSort, rowIndex),
		Kind:        "group",
		Runnable:    row.RunID != "",
		Profiles:    profiles,
		Limitations: []string{"current=" + derived.Current, "action=" + action, fmt.Sprintf("active=%t", derived.Active)},
	}
}

func stableIDSegment(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "diagnostic"
	}
	return out
}

// DHF-REQ: keel/requirement-60
func handleDesiredState(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		ids, err := parseIDs(args, true, true)
		if err != nil {
			logBridgeDispatch(rt, "desired-state", bridgeDispatchLog{Args: args, Err: err})
			return err
		}
		logBridgeDispatch(rt, "desired-state", bridgeDispatchLog{Args: args, IDs: ids})
		doc, err := deriveDesiredStateDeclaration(ctx, bridge, ids)
		if err != nil {
			return err
		}
		return writeDocument(rt, doc)
	}
}

// DHF-REQ: keel/requirement-75, keel/requirement-88
// DHF-REQ: keel/requirement-84
func deriveDesiredStateDeclaration(ctx context.Context, bridge Bridge, ids []string) (vscode.DesiredStateDocument, error) {
	var err error
	ids, err = resolveDesiredStateSelectionIDs(ctx, bridge, ids)
	if err != nil {
		return vscode.DesiredStateDocument{}, err
	}
	reportCtx := withDesiredStateReport(ctx)
	declared, err := bridge.DesiredState(reportCtx, ids)
	if err != nil {
		return vscode.DesiredStateDocument{}, err
	}
	rt := runtimeOrDefault(ctx, bridge)
	root := runtimeRoot(rt, bridge)
	groups := make([]vscode.DesiredStateGroup, 0, len(declared.Groups))
	for _, group := range declared.Groups {
		derivedRows, err := deriveDesiredStateGroupRows(reportCtx, root, desiredStateRootID(bridge), group)
		if err != nil {
			return vscode.DesiredStateDocument{}, err
		}
		rows := make([]vscode.DesiredState, 0, len(derivedRows))
		for _, row := range derivedRows {
			rows = append(rows, row.State)
		}
		groups = append(groups, vscode.DesiredStateGroup{
			Label:             group.Label,
			Order:             group.Order,
			MutuallyExclusive: group.MutuallyExclusive,
			Rows:              rows,
		})
	}
	return vscode.DesiredStateDocument{
		Version:        3,
		Devtool:        bridge.Metadata(),
		Workspace:      workspaceNode(bridge.Workspace(), root),
		GeneratedAt:    runtimeNow(rt),
		Groups:         groups,
		TeardownPolicy: declared.TeardownPolicy,
	}, nil
}

func resolveDesiredStateSelectionIDs(ctx context.Context, bridge Bridge, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if !hasDesiredStateGroupSelection(ids) {
		return ids, nil
	}
	doc, err := discoverWithDerivedDesiredState(ctx, bridge)
	if err != nil {
		return nil, err
	}
	items := make(map[string]vscode.TestItem, len(doc.Items))
	for _, item := range doc.Items {
		items[item.ID] = item
	}
	resolved := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	appendID := func(id string) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		resolved = append(resolved, id)
	}
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			appendID(id)
			continue
		}
		targetID := item.ID
		if item.CanonicalID != "" {
			targetID = item.CanonicalID
		}
		target, ok := items[targetID]
		if !ok {
			appendID(targetID)
			continue
		}
		if !desiredStateGroupItem(target) {
			appendID(targetID)
			continue
		}
		for _, child := range runnableDesiredStateGroupChildren(doc.Items, targetID) {
			appendID(child.ID)
		}
	}
	return resolved, nil
}

func hasDesiredStateGroupSelection(ids []string) bool {
	for _, id := range ids {
		if strings.Contains(id, "::group::") {
			return true
		}
	}
	return false
}

type derivedDesiredStateRow struct {
	Declaration DesiredStateRow
	State       vscode.DesiredState
}

// DHF-REQ: keel/requirement-88
func deriveDesiredStateGroupRows(ctx context.Context, root, parentID string, group DesiredStateGroup) ([]derivedDesiredStateRow, error) {
	rows := make([]derivedDesiredStateRow, 0, len(group.Rows)+1)
	satisfied := make([]int, 0, len(group.Rows))
	for _, row := range group.Rows {
		derived, err := deriveDesiredStateRow(ctx, root, row)
		if err != nil {
			return nil, err
		}
		if group.MutuallyExclusive && derived.Status == "satisfied" {
			satisfied = append(satisfied, len(rows))
		}
		rows = append(rows, derivedDesiredStateRow{Declaration: row, State: derived})
	}
	if !group.MutuallyExclusive {
		return rows, nil
	}
	for i := range rows {
		rows[i].State.Active = false
	}
	unknownActive := len(satisfied) == 0
	if len(satisfied) == 1 {
		rows[satisfied[0]].State.Active = true
	} else if len(satisfied) > 1 {
		for _, index := range satisfied {
			rows[index].State.Active = true
		}
	}
	unknown := exclusiveUnknownState(parentID, group.Label, unknownActive)
	rows = append(rows, derivedDesiredStateRow{
		Declaration: DesiredStateRow{
			RunID:    unknown.RunID,
			Resource: unknown.Resource,
			Kind:     unknown.Kind,
			Desired:  unknown.Desired,
			Reusable: unknown.Reusable,
			Owned:    unknown.Owned,
			Active:   unknown.Active,
		},
		State: unknown,
	})
	return rows, nil
}

func exclusiveUnknownState(parentID, groupLabel string, active bool) vscode.DesiredState {
	message := "no concrete alternative is active"
	if !active {
		message = "a concrete alternative is active"
	}
	return vscode.DesiredState{
		RunID:    exclusiveUnknownRunID(parentID, groupLabel),
		Resource: exclusiveUnknownResource,
		Kind:     exclusiveUnknownKind,
		Desired:  exclusiveUnknownValue,
		Current:  exclusiveUnknownValue,
		Status:   "satisfied",
		Action:   "reuse",
		Message:  message,
		Reusable: true,
		Owned:    false,
		Active:   active,
	}
}

func exclusiveUnknownRunID(parentID, groupLabel string) string {
	return parentID + "::group::" + stableIDSegment(groupLabel) + "::unknown"
}

func desiredStateRootID(bridge Bridge) string {
	node := bridge.Workspace().Node
	if node == "" {
		node = "keel"
	}
	return node + "::desired-state"
}

func isExclusiveUnknownRunID(id string) bool {
	return strings.Contains(id, "::desired-state::group::") && strings.HasSuffix(id, "::unknown")
}

func deriveDesiredStateRow(ctx context.Context, root string, row DesiredStateRow) (vscode.DesiredState, error) {
	if row.Probe == nil {
		return vscode.DesiredState{}, fmt.Errorf("keel/testbridge: desired-state row %q has no probe", row.Resource)
	}
	result := row.Probe(ctx, DesiredStateProbeRequest{RunID: row.RunID, Root: root})
	current := result.Current
	if current == "" {
		current = "unknown"
	}
	status := "reconcilable"
	action := "reconcile_during_run"
	if result.Satisfied {
		status = "satisfied"
		action = "reuse"
	}
	message := result.Message
	if message == "" {
		message = fmt.Sprintf("%s is %s", row.Resource, status)
	}
	return vscode.DesiredState{
		RunID:    row.RunID,
		Resource: row.Resource,
		Kind:     row.Kind,
		Desired:  row.Desired,
		Current:  current,
		Status:   status,
		Action:   action,
		Message:  message,
		Detail:   row.Detail,
		Reusable: row.Reusable,
		Owned:    row.Owned,
		Active:   row.Active,
	}, nil
}

// DHF-REQ: keel/requirement-58
// DHF-REQ: keel/requirement-86
func handleRun(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		ids, dryRun, err := parseRunArgs(args)
		if err != nil {
			logBridgeDispatch(rt, "run", bridgeDispatchLog{Args: args, Err: err})
			return err
		}
		logBridgeDispatch(rt, "run", bridgeDispatchLog{Args: args, IDs: ids, DryRun: boolPtr(dryRun)})
		requests, err := resolveRunRequests(ctx, bridge, ids, dryRun)
		if err != nil {
			logBridgeDispatch(rt, "run", bridgeDispatchLog{Args: args, IDs: ids, DryRun: boolPtr(dryRun), Err: err})
			return err
		}
		if dryRun {
			return nil
		}
		ids = runResolutionIDs(requests)
		runID := newRunID(rt)
		writer, closeWriter, err := newRunWriter(rt, bridge.Workspace(), runID)
		if err != nil {
			return err
		}
		defer closeWriter()
		exitCode := 1
		writer(vscode.RunEvent{Event: "run_started", Live: boolPtr(true), Requested: runResolutionRequests(requests)})
		if !bridgeLockExempt(bridge, ids) {
			releaseLock, err := acquireRunLock(runtimeRoot(rt, bridge), ids, runID)
			if err != nil {
				writer(vscode.RunEvent{Event: "errored", Message: err.Error()})
				writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
				return err
			}
			defer func() {
				if err := releaseLock(); err != nil && rt.Log != nil {
					rt.Log.Warn("release testbridge run lock", "error", err.Error())
				}
			}()
		}

		root := runtimeRoot(rt, bridge)
		var remaining []runResolution
		exitCode, remaining, runErr := runDesiredStateSelections(ctx, bridge, requests, writer)
		if runErr == nil && len(remaining) > 0 {
			exitCode, runErr = runRemainingSelections(ctx, bridge, remaining, runID, root, writer)
		}
		if runErr != nil {
			writer(vscode.RunEvent{Event: "errored", Message: runErr.Error()})
		}
		writer(vscode.RunEvent{Event: "run_finished", ExitCode: &exitCode})
		if runErr != nil {
			return RunError{ExitCode: exitCode, Err: runErr}
		}
		if exitCode != 0 {
			return RunError{ExitCode: exitCode, Err: fmt.Errorf("testbridge run exited %d", exitCode)}
		}
		return nil
	}
}

func bridgeLockExempt(bridge Bridge, ids []string) bool {
	if len(ids) == 1 && ids[0] == MaintenanceUnlockID {
		return true
	}
	locker, ok := bridge.(lockExemptRunner)
	return ok && locker.LockExemptRun(ids)
}

// DHF-REQ: keel/requirement-75, keel/requirement-88
func runDesiredStateSelections(ctx context.Context, bridge Bridge, requests []runResolution, writer vscode.RunEventWriter) (int, []runResolution, error) {
	ids := runResolutionIDs(requests)
	declared, err := bridge.DesiredState(ctx, ids)
	if err != nil {
		return 0, append([]runResolution{}, requests...), nil
	}
	rt := runtimeOrDefault(ctx, bridge)
	rows := desiredStateDeclarationsByRunID(declared)
	remaining := make([]runResolution, 0, len(requests))
	exitCode := 0
	for _, request := range requests {
		id := request.Request.ID
		if isExclusiveUnknownRunID(id) {
			writer(vscode.RunEvent{Event: "test_started", TestID: id})
			writer(vscode.RunEvent{Event: "passed", TestID: id, Message: "selected Unknown State without running consumer reconcile"})
			emitExclusiveDesiredStateSiblingClears(request.ExclusiveSiblingIDs, id, writer)
			continue
		}
		row, ok := rows[id]
		if !ok {
			remaining = append(remaining, request)
			continue
		}
		derived, err := deriveDesiredStateRow(ctx, runtimeRoot(rt, bridge), row)
		if err != nil {
			return 1, remaining, err
		}
		writer(vscode.RunEvent{Event: "test_started", TestID: id})
		if derived.Status == "satisfied" {
			writer(vscode.RunEvent{Event: "passed", TestID: id, Message: derived.Message})
			emitExclusiveDesiredStateSiblingClears(request.ExclusiveSiblingIDs, id, writer)
			continue
		}
		writer(vscode.RunEvent{Event: "failed", TestID: id, Message: derived.Message})
		exitCode = 1
	}
	if exitCode != 0 {
		return exitCode, remaining, fmt.Errorf("desired-state row failed")
	}
	return exitCode, remaining, nil
}

// emitExclusiveDesiredStateSiblingClears emits a bridge-owned "cleared" event
// per exclusive sibling so the VSIX drops the sibling's stale result to
// no-result (verbatim apply), rather than a "skipped" terminal state that
// merely swaps the icon. This satisfies requirement-88's at-most-one-result
// invariant: after activating a concrete member or Unknown, every other member
// of the exclusive group shows no result.
//
// DHF-REQ: keel/requirement-88
func emitExclusiveDesiredStateSiblingClears(ids []string, selectedID string, writer vscode.RunEventWriter) {
	for _, id := range ids {
		writer(vscode.RunEvent{
			Event:   "cleared",
			TestID:  id,
			Message: fmt.Sprintf("%s deactivated by exclusive desired-state selection of %s", id, selectedID),
		})
	}
}

// DHF-REQ: keel/requirement-86, keel/requirement-88
func runRemainingSelections(ctx context.Context, bridge Bridge, requests []runResolution, runID, root string, writer vscode.RunEventWriter) (int, error) {
	runBatch := func(batch []runResolution) (int, error) {
		if len(batch) == 0 {
			return 0, nil
		}
		if err := ctx.Err(); err != nil {
			return 1, err
		}
		ids := runResolutionIDs(batch)
		exitCode, err := bridge.Run(ctx, RunRequest{IDs: ids, RunID: runID, Root: root}, writer)
		if err != nil || exitCode != 0 {
			return exitCode, err
		}
		for _, request := range batch {
			emitExclusiveDesiredStateSiblingClears(request.ExclusiveSiblingIDs, request.Request.ID, writer)
		}
		return exitCode, nil
	}

	batch := make([]runResolution, 0, len(requests))
	for _, request := range requests {
		if bridgeHandlesMaintenanceRun(request.Request.ID) {
			if exitCode, err := runBatch(batch); err != nil || exitCode != 0 {
				return exitCode, err
			}
			batch = batch[:0]
			if exitCode, err := runBridgeMaintenance(ctx, bridge, root, runID, request.Request.ID, writer); err != nil || exitCode != 0 {
				return exitCode, err
			}
			continue
		}
		if !request.ExpandedGroupChild {
			batch = append(batch, request)
			continue
		}
		if exitCode, err := runBatch(batch); err != nil || exitCode != 0 {
			return exitCode, err
		}
		batch = batch[:0]
		if exitCode, err := runBatch([]runResolution{request}); err != nil || exitCode != 0 {
			return exitCode, err
		}
	}
	return runBatch(batch)
}

func bridgeHandlesMaintenanceRun(id string) bool {
	switch id {
	case MaintenanceUnlockID, MaintenanceClearResultsID, MaintenanceClearStateID:
		return true
	default:
		return false
	}
}

// DHF-REQ: keel/requirement-87
func runBridgeMaintenance(ctx context.Context, bridge Bridge, root, runID, id string, writer vscode.RunEventWriter) (int, error) {
	writer(vscode.RunEvent{Event: "test_started", TestID: id})
	switch id {
	case MaintenanceUnlockID:
		if err := os.Remove(RunLockPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 1, err
		}
	case MaintenanceClearResultsID:
	case MaintenanceClearStateID:
		clearer, ok := bridge.(ClearStateProvider)
		if !ok {
			return 1, fmt.Errorf("keel/testbridge: bridge does not implement clear-state callback")
		}
		exitCode, err := clearer.ClearState(ctx, RunRequest{IDs: []string{id}, RunID: runID, Root: root}, writer)
		if err != nil || exitCode != 0 {
			return exitCode, err
		}
	default:
		return 2, cli.NewUsageError("unknown bridge maintenance id %q", id)
	}
	writer(vscode.RunEvent{Event: "passed", TestID: id})
	return 0, nil
}

func desiredStateDeclarationsByRunID(desiredState DesiredStateDeclaration) map[string]DesiredStateRow {
	rows := map[string]DesiredStateRow{}
	for _, group := range desiredState.Groups {
		for _, row := range group.Rows {
			if row.RunID != "" {
				rows[row.RunID] = row
			}
		}
	}
	return rows
}

func parseRunArgs(args []string) ([]string, bool, error) {
	ids := make([]string, 0)
	dryRun := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--id":
			if i+1 >= len(args) {
				return nil, false, cli.NewUsageError("--id requires a test id")
			}
			i++
			ids = append(ids, args[i])
		case "--format":
			return nil, false, cli.NewUsageError("unknown flag \"--format\"")
		default:
			return nil, false, cli.NewUsageError("unknown test-bridge tests argument %q", args[i])
		}
	}
	if len(ids) == 0 {
		return nil, false, cli.NewUsageError("--id is required")
	}
	return ids, dryRun, nil
}

// DHF-REQ: keel/requirement-58, keel/requirement-72
// DHF-REQ: keel/requirement-84, keel/requirement-86
func resolveRunRequests(ctx context.Context, bridge Bridge, ids []string, strict bool) ([]runResolution, error) {
	doc, err := discoverWithDerivedDesiredState(ctx, bridge)
	if err != nil {
		return nil, err
	}
	items := make(map[string]vscode.TestItem, len(doc.Items))
	for _, item := range doc.Items {
		items[item.ID] = item
	}
	resolved := make([]runResolution, 0, len(ids))
	resolvedIDs := map[string]struct{}{}
	appendResolved := func(request runResolution) {
		if _, ok := resolvedIDs[request.Request.ID]; ok {
			return
		}
		resolvedIDs[request.Request.ID] = struct{}{}
		resolved = append(resolved, request)
	}
	for _, id := range ids {
		item, ok := items[id]
		if !ok {
			if !strict {
				appendResolved(runResolution{Request: vscode.RunRequest{ID: id, Label: id}})
				continue
			}
			return nil, cli.NewUsageError("unknown test id %q", id)
		}
		targetID := item.ID
		if item.CanonicalID != "" {
			targetID = item.CanonicalID
		}
		target, ok := items[targetID]
		if !ok {
			return nil, cli.NewUsageError("test id %q resolves to unknown canonical id %q", id, targetID)
		}
		for {
			if desiredStateGroupItem(target) {
				children := runnableDesiredStateGroupChildren(doc.Items, targetID)
				if len(children) == 0 {
					return nil, cli.NewUsageError("desired-state group %q has no runnable rows", targetID)
				}
				if !target.Runnable {
					return nil, cli.NewUsageError("test id %q resolves to non-runnable desired-state group %q", id, targetID)
				}
				for _, child := range children {
					appendResolved(runResolution{Request: runRequestForTestItem(child)})
				}
				break
			}
			if nonDesiredStateGroupItemWithDescendants(doc.Items, target) {
				children := runnableDescendantLeafItems(doc.Items, targetID)
				if len(children) == 0 {
					ancestor, ok := nearestRunnableAncestor(items, target)
					if !ok {
						return nil, cli.NewUsageError("group %q has no runnable descendants", targetID)
					}
					targetID = ancestor.ID
					target = ancestor
					continue
				}
				for _, child := range children {
					appendResolved(runResolution{Request: runRequestForTestItem(child), ExpandedGroupChild: true})
				}
				break
			}
			if !target.Runnable && (strict || item.CanonicalID != "") {
				return nil, cli.NewUsageError("test id %q resolves to non-runnable id %q", id, targetID)
			}
			appendResolved(runResolution{Request: runRequestForTestItem(target), ExclusiveSiblingIDs: exclusiveDesiredStateSiblingIDs(doc.Items, target)})
			break
		}
	}
	return resolved, nil
}

type runResolution struct {
	Request             vscode.RunRequest
	ExpandedGroupChild  bool
	ExclusiveSiblingIDs []string
}

func desiredStateGroupItem(item vscode.TestItem) bool {
	if item.Kind != "group" || item.ParentID == "" || !strings.HasPrefix(item.ID, item.ParentID+"::group::") {
		return false
	}
	for _, limitation := range item.Limitations {
		if limitation == "mutually_exclusive=true" || limitation == "mutually_exclusive=false" {
			return true
		}
	}
	return false
}

func nonDesiredStateGroupItemWithDescendants(items []vscode.TestItem, item vscode.TestItem) bool {
	if item.Kind != "group" || desiredStateGroupItem(item) {
		return false
	}
	for _, candidate := range items {
		if candidate.ParentID == item.ID {
			return true
		}
	}
	return false
}

func runnableDesiredStateGroupChildren(items []vscode.TestItem, groupID string) []vscode.TestItem {
	children := make([]vscode.TestItem, 0)
	for _, item := range items {
		if item.ParentID == groupID && item.Runnable {
			children = append(children, item)
		}
	}
	return children
}

func exclusiveDesiredStateSiblingIDs(items []vscode.TestItem, selected vscode.TestItem) []string {
	if selected.ParentID == "" {
		return nil
	}
	parent, ok := testItemByID(items, selected.ParentID)
	if !ok || !desiredStateGroupItem(parent) || !hasLimitation(parent.Limitations, "mutually_exclusive=true") {
		return nil
	}
	siblings := make([]string, 0)
	for _, item := range items {
		if item.ParentID != selected.ParentID || item.ID == selected.ID || !item.Runnable {
			continue
		}
		siblings = append(siblings, item.ID)
	}
	return siblings
}

func testItemByID(items []vscode.TestItem, id string) (vscode.TestItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return vscode.TestItem{}, false
}

// DHF-REQ: keel/requirement-86
func nearestRunnableAncestor(items map[string]vscode.TestItem, item vscode.TestItem) (vscode.TestItem, bool) {
	for parentID := item.ParentID; parentID != ""; {
		parent, ok := items[parentID]
		if !ok {
			return vscode.TestItem{}, false
		}
		if parent.Runnable {
			return parent, true
		}
		parentID = parent.ParentID
	}
	return vscode.TestItem{}, false
}

func hasLimitation(limitations []string, limitation string) bool {
	for _, got := range limitations {
		if got == limitation {
			return true
		}
	}
	return false
}

func runnableDescendantLeafItems(items []vscode.TestItem, groupID string) []vscode.TestItem {
	childrenByParent := make(map[string][]vscode.TestItem)
	for _, item := range items {
		if item.ParentID == "" {
			continue
		}
		childrenByParent[item.ParentID] = append(childrenByParent[item.ParentID], item)
	}

	leaves := make([]vscode.TestItem, 0)
	var walk func(string)
	walk = func(parentID string) {
		for _, child := range childrenByParent[parentID] {
			if len(childrenByParent[child.ID]) > 0 {
				walk(child.ID)
				continue
			}
			if child.Runnable {
				leaves = append(leaves, child)
			}
		}
	}
	walk(groupID)
	return leaves
}

func runRequestForTestItem(item vscode.TestItem) vscode.RunRequest {
	label := item.Label
	if label == "" {
		label = item.ID
	}
	return vscode.RunRequest{ID: item.ID, Label: label}
}

func handleConfigInit(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		if len(args) != 0 {
			err := cli.NewUsageError("test-bridge config init takes no arguments: got %q", args)
			logBridgeDispatch(rt, "config init", bridgeDispatchLog{Args: args, Err: err})
			return err
		}
		logBridgeDispatch(rt, "config init", bridgeDispatchLog{Args: args})
		_, err := InitConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
}

func handleConfigUpgrade(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		if len(args) != 0 {
			err := cli.NewUsageError("test-bridge config upgrade takes no arguments: got %q", args)
			logBridgeDispatch(rt, "config upgrade", bridgeDispatchLog{Args: args, Err: err})
			return err
		}
		logBridgeDispatch(rt, "config upgrade", bridgeDispatchLog{Args: args})
		_, err := UpgradeConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
}

func parseIDs(args []string, allowEmpty bool, allowFormat bool) ([]string, error) {
	ids := make([]string, 0)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if !allowFormat {
				return nil, cli.NewUsageError("unknown flag \"--format\"")
			}
			if i+1 >= len(args) || args[i+1] != "json" {
				return nil, cli.NewUsageError("--format supports only json")
			}
			i++
		case "--id":
			if i+1 >= len(args) {
				return nil, cli.NewUsageError("--id requires a test id")
			}
			i++
			ids = append(ids, args[i])
		default:
			return nil, cli.NewUsageError("unknown test-bridge tests argument %q", args[i])
		}
	}
	if !allowEmpty && len(ids) == 0 {
		return nil, cli.NewUsageError("--id is required")
	}
	return ids, nil
}

func writeDocument(rt Runtime, doc any) error {
	if err := ValidateDocument(doc); err != nil {
		return err
	}
	return EncodeDocument(rt.Protocol, doc)
}

// EncodeDocument writes doc to out as canonical test-bridge protocol JSON: one
// JSON object followed by a newline, with HTML escaping disabled. It performs no
// schema validation — callers that emit schema-typed protocol documents validate
// through ValidateDocument (or the package dispatch path) first. Consumer
// devtools route their protocol output through this function so JSON assembly
// stays owned by keel/testbridge rather than being hand-rolled per consumer.
//
// DHF-REQ: keel/requirement-63
func EncodeDocument(out io.Writer, doc any) error {
	if out == nil {
		out = io.Discard
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func runtimeOrDefault(ctx context.Context, bridge Bridge) Runtime {
	rt, ok := RuntimeFrom(ctx)
	if !ok {
		return Runtime{Root: bridge.Workspace().Root, Protocol: io.Discard}
	}
	if rt.Root == "" {
		rt.Root = bridge.Workspace().Root
	}
	if rt.Protocol == nil {
		rt.Protocol = io.Discard
	}
	return rt
}

// DHF-REQ: keel/requirement-78
func logBridgeDispatch(rt Runtime, verb string, record bridgeDispatchLog) {
	if rt.Log == nil {
		return
	}
	attrs := []any{
		"verb", verb,
		"args", append([]string{}, record.Args...),
	}
	if record.IDs != nil {
		attrs = append(attrs, "ids", append([]string{}, record.IDs...))
	}
	if record.DryRun != nil {
		attrs = append(attrs, "dry_run", *record.DryRun)
	}
	if record.Err != nil {
		attrs = append(attrs, "error", record.Err.Error())
	}
	rt.Log.Info("testbridge dispatch", attrs...)
}

type bridgeDispatchLog struct {
	Args   []string
	IDs    []string
	DryRun *bool
	Err    error
}

func runtimeRoot(rt Runtime, bridge Bridge) string {
	if rt.Root != "" {
		return rt.Root
	}
	return bridge.Workspace().Root
}

func newRunWriter(rt Runtime, workspace Workspace, runID string) (vscode.RunEventWriter, func(), error) {
	root := rt.Root
	if root == "" {
		root = workspace.Root
	}
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, nil, err
	}
	external, err := os.Create(filepath.Join(runDir, runID+".jsonl"))
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = external.Close() }
	stamper := vscode.EventStamper{
		Now:       rt.Now,
		RunID:     runID,
		Source:    "vscode",
		Workspace: workspaceNode(workspace, root),
		Logf: func(message string) {
			if rt.Log != nil {
				rt.Log.Warn("testbridge protocol event rejected", "detail", message)
			}
		},
	}
	out := rt.Protocol
	if out == nil {
		out = io.Discard
	}
	runLog := bridgeRunLog{logger: rt.Log}
	return func(event vscode.RunEvent) {
		stamped := stamper.Stamp(event)
		if err := ValidateDocument(stamped); err != nil {
			if rt.Log != nil {
				rt.Log.Error("validate testbridge run event", "error", err.Error())
			}
			return
		}
		line, err := vscode.MarshalRunEventJSONL(stamped)
		if err != nil {
			if rt.Log != nil {
				rt.Log.Error("marshal testbridge run event", "error", err.Error())
			}
			return
		}
		_, _ = out.Write(line)
		_, _ = external.Write(line)
		runLog.observe(stamped)
		if stamped.Event == "run_finished" {
			if err := pruneCompletedRunStreams(runDir, externalRunStreamRetentionLimit); err != nil && rt.Log != nil {
				rt.Log.Warn("prune testbridge run streams", "error", err.Error())
			}
		}
	}, closeFn, nil
}

type completedRunStream struct {
	path        string
	name        string
	completedAt time.Time
}

// DHF-REQ: keel/requirement-92
func pruneCompletedRunStreams(runDir string, keep int) error {
	if keep < 1 {
		return nil
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return err
	}
	completed := make([]completedRunStream, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(runDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		completedAt, ok := completedRunStreamTime(path, info.ModTime())
		if !ok {
			continue
		}
		completed = append(completed, completedRunStream{path: path, name: entry.Name(), completedAt: completedAt})
	}
	if len(completed) <= keep {
		return nil
	}
	sort.Slice(completed, func(i, j int) bool {
		if completed[i].completedAt.Equal(completed[j].completedAt) {
			return completed[i].name > completed[j].name
		}
		return completed[i].completedAt.After(completed[j].completedAt)
	})
	var errs []error
	for _, stream := range completed[keep:] {
		if err := os.Remove(stream.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func completedRunStreamTime(path string, fallback time.Time) (time.Time, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	completedAt := time.Time{}
	completed := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event vscode.RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Event != "run_finished" {
			continue
		}
		completed = true
		if !event.Time.IsZero() && (completedAt.IsZero() || event.Time.After(completedAt)) {
			completedAt = event.Time
		}
	}
	if completed && completedAt.IsZero() {
		completedAt = fallback
	}
	return completedAt, completed
}

type bridgeRunLog struct {
	logger   *slog.Logger
	terminal []vscode.RunEvent
}

// DHF-REQ: keel/requirement-78
func (l *bridgeRunLog) observe(event vscode.RunEvent) {
	if l.logger == nil {
		return
	}
	if isTerminalRunEvent(event) {
		l.terminal = append(l.terminal, event)
		return
	}
	if event.Event != "run_finished" || event.ExitCode == nil {
		return
	}
	exitCode := *event.ExitCode
	for _, terminal := range l.terminal {
		attrs := []any{
			"test_id", terminal.TestID,
			"verdict", terminal.Event,
			"exit_code", exitCode,
		}
		if terminal.Message != "" {
			attrs = append(attrs, "message", terminal.Message)
		}
		l.logger.Info("testbridge terminal event", attrs...)
	}
}

func isTerminalRunEvent(event vscode.RunEvent) bool {
	switch event.Event {
	case "errored":
		return true
	case "passed", "failed", "skipped", "cancelled":
		return event.TestID != ""
	default:
		return false
	}
}

func workspaceNode(workspace Workspace, root string) string {
	if workspace.Node != "" {
		return workspace.Node
	}
	if root != "" {
		return filepath.Base(root)
	}
	return "unknown"
}

func runtimeNow(rt Runtime) time.Time {
	if rt.Now != nil {
		return rt.Now().UTC()
	}
	return time.Now().UTC()
}

// DHF-REQ: keel/requirement-58, keel/requirement-67
func acquireRunLock(root string, ids []string, token string) (func() error, error) {
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	path := RunLockPath(root)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("keel/testbridge: run lock already exists at %s", path)
		}
		return nil, fmt.Errorf("keel/testbridge: create run lock: %w", err)
	}
	lock := vscode.RunLockFile{
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		IDs:       append([]string{}, ids...),
		Token:     token,
	}
	if err := ValidateDocument(lock); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	encErr := json.NewEncoder(file).Encode(lock)
	closeErr := file.Close()
	if encErr != nil || closeErr != nil {
		_ = os.Remove(path)
		if encErr != nil {
			return nil, encErr
		}
		return nil, closeErr
	}
	return func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		var current vscode.RunLockFile
		if err := json.Unmarshal(data, &current); err != nil {
			return err
		}
		if current.Token != token {
			return fmt.Errorf("keel/testbridge: run lock token mismatch at %s", path)
		}
		return os.Remove(path)
	}, nil
}

// RunLockPath returns the package-owned cross-run lock path.
func RunLockPath(root string) string {
	return filepath.Join(root, ".devtools", "vscode-runs", "run.lock")
}

func newRunID(rt Runtime) string {
	if rt.RunID != nil {
		if id := rt.RunID(); id != "" {
			return id
		}
	}
	now := time.Now()
	if rt.Now != nil {
		now = rt.Now()
	}
	return "run-" + now.UTC().Format("20060102T150405.000000000Z")
}

func runResolutionIDs(requests []runResolution) []string {
	out := make([]string, 0, len(requests))
	for _, request := range requests {
		out = append(out, request.Request.ID)
	}
	return out
}

func runResolutionRequests(requests []runResolution) []vscode.RunRequest {
	out := make([]vscode.RunRequest, 0, len(requests))
	for _, request := range requests {
		out = append(out, request.Request)
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

// RunError reports a non-zero run result while preserving CLI-level error
// handling for callers.
type RunError struct {
	ExitCode int
	Err      error
}

func (e RunError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("testbridge run exited %d", e.ExitCode)
	}
	return e.Err.Error()
}

func (e RunError) Unwrap() error { return e.Err }
