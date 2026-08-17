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
	"runtime"
	"sort"
	"strings"
	"syscall"
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
	// ProbeDeadline bounds one desired-state probe execution. Zero selects
	// DefaultDesiredStateProbeDeadline.
	//
	// DHF-REQ: keel/requirement-129
	ProbeDeadline time.Duration
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
// writer. RunRequest.IDs may carry more than one selection when the bridge
// batches an editor multi-select or Run All (see runRemainingSelections): a
// conforming Run executes EVERY requested id — not a single representative —
// stopping at the first failing selection and returning its non-zero exit, so
// no member is silently unexecuted with exit 0 (keel/requirement-99). The
// reference consumer keel-demo-dev honors this; keel-dev's keelTestBridge.Run
// now iterates the same way (keel/issue-92).
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
	MaintenanceGroupID        = "testbridge" + "::maintenance"
	MaintenanceDetectLanesID  = MaintenanceGroupID + "::detect-lanes"
	MaintenanceUnlockID       = MaintenanceGroupID + "::unlock"
	MaintenanceClearResultsID = MaintenanceGroupID + "::clear-results"
	MaintenanceClearStateID   = MaintenanceGroupID + "::clear-state"
)

// DesiredStateGroupIDSuffix is the stable marker that identifies a consumer's
// desired-state anchor — the group the bridge derives desired-state rows under.
// A consumer names its anchor DesiredStateGroupID(<node>); the bridge matches
// that id. The anchor's Label is presentation only and carries no protocol
// meaning, so renaming or localizing the row cannot change derivation.
//
// DHF-REQ: keel/requirement-126
const DesiredStateGroupIDSuffix = "::desired-state"

// ErrInvalidDesiredStateNode names the refusal of a consumer node the
// desired-state marker pair cannot round-trip. Callers match it with
// errors.Is.
//
// DHF-REQ: keel/requirement-126
var ErrInvalidDesiredStateNode = errors.New("keel/testbridge: invalid desired-state node")

// isDesiredStateNode is the one definition of a valid consumer node: non-empty
// and a single segment. Both halves of the exported marker pair consult it, so
// the constructor cannot admit a node the recognizer refuses.
//
// DHF-REQ: keel/requirement-126
func isDesiredStateNode(node string) bool {
	return node != "" && !strings.Contains(node, "::")
}

// requireDesiredStateNode refuses a node that cannot round-trip, naming the
// node and the rule it broke.
func requireDesiredStateNode(node string) error {
	if isDesiredStateNode(node) {
		return nil
	}
	return fmt.Errorf("%w: %q must be non-empty and must not contain %q", ErrInvalidDesiredStateNode, node, "::")
}

// DesiredStateGroupID returns the desired-state anchor id for a consumer node:
// the node, then DesiredStateGroupIDSuffix. The node must be non-empty and a
// single segment — it must not contain "::" — because that is what
// IsDesiredStateGroupID accepts and what keeps ids derived beneath an anchor
// from reading as a second anchor. A node that breaks the rule is refused with
// ErrInvalidDesiredStateNode rather than turned into an id the bridge would
// silently ignore.
//
// DHF-REQ: keel/requirement-126
func DesiredStateGroupID(node string) (string, error) {
	if err := requireDesiredStateNode(node); err != nil {
		return "", err
	}
	return node + DesiredStateGroupIDSuffix, nil
}

// IsDesiredStateGroupID reports whether id is a consumer's desired-state
// anchor: a non-empty single-segment node followed by
// DesiredStateGroupIDSuffix. Ids derived beneath an anchor — group rows and the
// provider-failure diagnostic — are descendants, never a second anchor.
//
// DHF-REQ: keel/requirement-126
func IsDesiredStateGroupID(id string) bool {
	node, ok := strings.CutSuffix(id, DesiredStateGroupIDSuffix)
	return ok && isDesiredStateNode(node)
}

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

// LaneFileProvider supplies the persisted .vscode/test-lanes.json rows when a
// consumer needs bridge-owned detect-lanes to preserve richer lane semantics.
type LaneFileProvider interface {
	LaneFile(context.Context) (LaneFile, error)
}

// LaneFile is the bridge-written .vscode/test-lanes.json document.
type LaneFile struct {
	Version int            `json:"version"`
	Lanes   []LaneFileLane `json:"lanes"`
}

// LaneFileLane is one persisted lane row in a bridge-written lane file.
type LaneFileLane struct {
	ID                string              `json:"id"`
	Label             string              `json:"label"`
	Order             string              `json:"order"`
	Description       string              `json:"description"`
	Framework         string              `json:"framework,omitempty"`
	RequiredResources []string            `json:"required_resources,omitempty"`
	Members           []map[string]string `json:"members"`
	Prerequisites     []string            `json:"prerequisites"`
}

type bridgeLanesFile struct {
	Version int          `json:"version"`
	Lanes   []bridgeLane `json:"lanes"`
}

type bridgeLane struct {
	ID                string              `json:"id"`
	Label             string              `json:"label"`
	Order             string              `json:"order,omitempty"`
	Framework         string              `json:"framework,omitempty"`
	RequiredResources []string            `json:"required_resources,omitempty"`
	Members           []map[string]string `json:"members,omitempty"`
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
// test-bridge discover|desired-state|run and config-init|config-upgrade.
//
// DHF-REQ: keel/requirement-58, keel/requirement-60, keel/requirement-107
func CommandSpec(bridge Bridge) *cli.CommandSpec {
	var discoverFormat string
	var desiredStateFormat string
	var desiredStateIDs []string
	var runIDs []string
	var runDryRun bool
	var runSource string
	return &cli.CommandSpec{
		Subcommands: []*cli.CommandSpec{
			{
				Name:  "test-bridge",
				Short: "Serve VS Code test-bridge protocol commands.",
				Subcommands: []*cli.CommandSpec{
					{Name: "config-init", Use: "test-bridge config-init", Short: "Write .vscode/test-bridge.json if absent.", Group: "Config", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Handler: guardWorkspace(bridge, handleConfigInit(bridge))},
					{Name: "config-upgrade", Use: "test-bridge config-upgrade", Short: "Upgrade .vscode/test-bridge.json to the current schema.", Group: "Config", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Handler: guardWorkspace(bridge, handleConfigUpgrade(bridge))},
					{Name: "discover", Use: "test-bridge discover [--format json]", Short: "Emit the test discovery document.", Group: "Tests", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Flags: []cli.FlagSpec{{Name: "format", Value: "json", Default: "json", Enum: []string{"json"}, Short: "Output format.", StringTarget: &discoverFormat}}, Handler: guardWorkspace(bridge, handleDiscover(bridge, &discoverFormat))},
					{Name: "desired-state", Use: "test-bridge desired-state [--format json] [--id test-id]", Short: "Emit the read-only desired-state document.", Group: "Tests", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Flags: []cli.FlagSpec{{Name: "format", Value: "json", Default: "json", Enum: []string{"json"}, Short: "Output format.", StringTarget: &desiredStateFormat}, {Name: "id", Value: "test-id", Repeatable: true, Short: "Selected test id.", StringSliceTarget: &desiredStateIDs}}, Handler: guardWorkspace(bridge, handleDesiredState(bridge, &desiredStateFormat, &desiredStateIDs))},
					{Name: "run", Use: "test-bridge run [--dry-run] [--source surface] --id test-id", Short: "Run selected tests.", Group: "Tests", Positionals: []cli.PositionalSpec{{Name: "args", Min: 0, Max: 0}}, Flags: []cli.FlagSpec{{Name: "id", Value: "test-id", Repeatable: true, Required: true, Short: "Selected test id.", StringSliceTarget: &runIDs}, {Name: "dry-run", Short: "Resolve selected test ids without executing them.", BoolTarget: &runDryRun}, {Name: "source", Value: "surface", Default: defaultRunSource, Enum: runSourceSurfaces, Short: "Initiating surface stamped onto every run event.", StringTarget: &runSource}}, Handler: guardWorkspace(bridge, handleRun(bridge, &runIDs, &runDryRun, &runSource))},
				},
			},
		},
	}
}

// guardWorkspace validates the consumer's workspace once per dispatch, before
// any protocol work runs. CommandSpec is the bridge's only entry point, so
// every command surface inherits the check from here.
//
// DHF-REQ: keel/requirement-126
func guardWorkspace(bridge Bridge, next cli.Handler) cli.Handler {
	return func(ctx context.Context, args []string) error {
		if err := validateWorkspace(bridge.Workspace()); err != nil {
			return err
		}
		return next(ctx, args)
	}
}

func handleDiscover(bridge Bridge, format *string) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		_ = format
		logBridgeDispatch(rt, "discover", bridgeDispatchLog{Args: args})
		ctx = withProbePass(ctx, rt.ProbeDeadline)
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
		Framework:   "testbridge",
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

// DHF-REQ: keel/requirement-74, keel/requirement-83, keel/requirement-95
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
	items, reconcile, err := desiredStateDeclarationDiscoveryItems(ctx, runtimeRoot(rt, bridge), parent.ID, desiredState.Groups)
	if err != nil {
		return vscode.DiscoveryDocument{}, err
	}
	doc.Items = append(doc.Items, items...)
	doc.Capabilities.ReconcileResults = reconcile
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

// desiredStateParent resolves the anchor the consumer emitted, keyed on the
// exported marker. The anchor's Label never enters the decision.
//
// DHF-REQ: keel/requirement-126
func desiredStateParent(items []vscode.TestItem) (vscode.TestItem, bool) {
	for _, item := range items {
		if item.Kind == "group" && IsDesiredStateGroupID(item.ID) {
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

// desiredStateDeclarationDiscoveryItems derives the B-group discovery items
// and, alongside them, the bridge-computed reconcile_results capability
// content: one rendered-state stamp per mutually-exclusive row with a run
// id — the derived-active row passed, every other row (including the
// synthetic Unknown State peer) skipped. Consumers replay exactly those
// stamps on every refresh, overwriting stale rendered results.
//
// DHF-REQ: keel/requirement-75, keel/requirement-83, keel/requirement-88, keel/requirement-97
func desiredStateDeclarationDiscoveryItems(ctx context.Context, root, parentID string, groups []DesiredStateGroup) ([]vscode.TestItem, []vscode.ReconcileResult, error) {
	groups = append([]DesiredStateGroup(nil), groups...)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Order < groups[j].Order })
	items := make([]vscode.TestItem, 0)
	var reconcile []vscode.ReconcileResult
	for _, group := range groups {
		groupID := parentID + "::group::" + stableIDSegment(group.Label)
		derivedRows, err := deriveDesiredStateGroupRows(ctx, root, parentID, group)
		if err != nil {
			return nil, nil, err
		}
		runnable := !group.MutuallyExclusive && desiredStateGroupHasRunnableRows(group)
		profiles := []string{}
		if runnable {
			profiles = []string{"run"}
		}
		groupItem := vscode.TestItem{
			ID:                groupID,
			ParentID:          parentID,
			Label:             group.Label,
			SortText:          fmt.Sprintf("b.%03d", group.Order),
			Kind:              "group",
			Runnable:          runnable,
			Profiles:          profiles,
			DesiredStateGroup: &vscode.DesiredStateGroupFacts{MutuallyExclusive: group.MutuallyExclusive},
		}
		items = append(items, groupItem)
		for rowIndex, row := range derivedRows {
			items = append(items, desiredStateDeclarationDiscoveryItem(groupID, groupItem.SortText, rowIndex+1, row.Declaration, row.State))
		}
		if group.MutuallyExclusive {
			reconcile = append(reconcile, exclusiveGroupReconcileResults(derivedRows)...)
		}
	}
	return items, reconcile, nil
}

// exclusiveGroupReconcileResults renders one stamp per runnable row of an
// exclusive group: the derived-active row passed, every other row skipped,
// each message naming the active member.
//
// DHF-REQ: keel/requirement-97
func exclusiveGroupReconcileResults(derivedRows []derivedDesiredStateRow) []vscode.ReconcileResult {
	activeResource := ""
	for _, row := range derivedRows {
		if row.State.Active {
			activeResource = row.State.Resource
			break
		}
	}
	results := make([]vscode.ReconcileResult, 0, len(derivedRows))
	for _, row := range derivedRows {
		if row.State.RunID == "" {
			continue
		}
		if row.State.Active {
			results = append(results, vscode.ReconcileResult{
				TestID:  row.State.RunID,
				State:   "passed",
				Message: row.State.Resource + " is active",
			})
			continue
		}
		message := "not active"
		if activeResource != "" {
			message = "not active (" + activeResource + " is active)"
		}
		results = append(results, vscode.ReconcileResult{
			TestID:  row.State.RunID,
			State:   "skipped",
			Message: message,
		})
	}
	return results
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
		ID:       id,
		ParentID: parentID,
		Label:    fmt.Sprintf("%s: %s", row.Resource, row.Desired),
		SortText: fmt.Sprintf("%s.%03d", parentSort, rowIndex),
		Kind:     "group",
		Runnable: row.RunID != "",
		Profiles: profiles,
		DesiredStateRow: &vscode.DesiredStateRowFacts{
			Current: derived.Current,
			Action:  action,
			Active:  derived.Active,
		},
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

// DHF-REQ: keel/requirement-60, keel/requirement-107
func handleDesiredState(bridge Bridge, format *string, ids *[]string) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		_ = format
		selected := append([]string(nil), (*ids)...)
		logBridgeDispatch(rt, "desired-state", bridgeDispatchLog{Args: args, IDs: selected})
		ctx = withProbePass(ctx, rt.ProbeDeadline)
		doc, err := deriveDesiredStateDeclaration(ctx, bridge, selected)
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
	rootID, err := desiredStateRootID(bridge)
	if err != nil {
		return vscode.DesiredStateDocument{}, err
	}
	groups := make([]vscode.DesiredStateGroup, 0, len(declared.Groups))
	for _, group := range declared.Groups {
		derivedRows, err := deriveDesiredStateGroupRows(reportCtx, root, rootID, group)
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

func desiredStateRootID(bridge Bridge) (string, error) {
	node := bridge.Workspace().Node
	if node == "" {
		node = "testbridge"
	}
	return DesiredStateGroupID(node)
}

func isExclusiveUnknownRunID(id string) bool {
	return strings.Contains(id, "::desired-state::group::") && strings.HasSuffix(id, "::unknown")
}

// DHF-REQ: keel/requirement-129, keel/requirement-75
func deriveDesiredStateRow(ctx context.Context, root string, row DesiredStateRow) (vscode.DesiredState, error) {
	if row.Probe == nil {
		return vscode.DesiredState{}, fmt.Errorf("keel/testbridge: desired-state row %q has no probe", row.Resource)
	}
	result, err := executeDesiredStateProbe(ctx, root, row)
	if err != nil {
		var bound probeBoundError
		if !errors.As(err, &bound) {
			return vscode.DesiredState{}, err
		}
		logDesiredStateProbeAbandoned(ctx, bound)
		return abandonedDesiredState(row, bound), nil
	}
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
		// The probe is the single source for all four rendered facts. A row is
		// active exactly when its probe reports the resource satisfied; the
		// mutually-exclusive resolution keel/requirement-88 owns composes on top
		// of this derived base rather than on a consumer-declared value.
		Active: result.Satisfied,
	}, nil
}

// defaultRunSource is the run-event source a bridge run stamps when the caller
// declares no initiating surface. It is the historical value, so a caller that
// predates the --source flag keeps producing the stream it always produced.
//
// runSourceSurfaces are the surfaces a caller may declare. "external" is not
// among them: it names an imported stream, which no bridge run produces.
//
// DHF-REQ: keel/requirement-36
const defaultRunSource = "vscode"

var runSourceSurfaces = []string{defaultRunSource, editorRunSource}

// editorRunSource marks a run the VS Code editor itself initiated, so the
// extension's external-run mirror can skip the spool file that run writes.
const editorRunSource = "editor"

// DHF-REQ: keel/requirement-58, keel/requirement-107
// DHF-REQ: keel/requirement-86
// DHF-REQ: keel/requirement-36
func handleRun(bridge Bridge, ids *[]string, dryRun *bool, source *string) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		selected := append([]string(nil), (*ids)...)
		strict := *dryRun
		logBridgeDispatch(rt, "run", bridgeDispatchLog{Args: args, IDs: selected, DryRun: boolPtr(*dryRun)})
		ctx = withProbePass(ctx, rt.ProbeDeadline)
		requests, err := resolveRunRequests(ctx, bridge, selected, strict)
		if err != nil {
			logBridgeDispatch(rt, "run", bridgeDispatchLog{Args: args, IDs: selected, DryRun: boolPtr(*dryRun), Err: err})
			return err
		}
		if *dryRun {
			return nil
		}
		selected = runResolutionIDs(requests)
		runID := newRunID(rt)
		writer, closeWriter, err := newRunWriter(rt, bridge.Workspace(), runID, *source)
		if err != nil {
			return err
		}
		defer closeWriter()
		exitCode := 1
		writer(vscode.RunEvent{Event: "run_started", Live: boolPtr(true), Requested: runResolutionRequests(requests)})
		if !bridgeLockExempt(bridge, selected) {
			releaseLock, err := acquireRunLock(runtimeRoot(rt, bridge), selected, runID, rt.Log)
			if err != nil {
				vscode.EmitErroredForTestIDs(selected, err.Error(), writer)
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
		var erroredIDs []string
		exitCode, remaining, runErr := runDesiredStateSelections(ctx, bridge, requests, writer)
		if runErr != nil {
			erroredIDs = runResolutionIDs(requests)
		}
		if runErr == nil && len(remaining) > 0 {
			exitCode, erroredIDs, runErr = runRemainingSelections(ctx, bridge, remaining, runID, root, writer)
		}
		if runErr != nil {
			vscode.EmitErroredForTestIDs(erroredIDs, runErr.Error(), writer)
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

// DHF-REQ: keel/requirement-75, keel/requirement-88, keel/requirement-98
func runDesiredStateSelections(ctx context.Context, bridge Bridge, requests []runResolution, writer vscode.RunEventWriter) (int, []runResolution, error) {
	ids := runResolutionIDs(requests)
	declared, err := bridge.DesiredState(ctx, ids)
	if err != nil {
		// A failed resolution degrades visibly rather than aborting the run
		// (the symmetric choice with the discovery path's diagnostic item):
		// the selections still fall through to the remaining-selection path,
		// but the failure is stated in the event stream and in the log
		// instead of sharing the no-rows-matched return shape.
		// DHF-REQ: keel/requirement-124
		logDesiredStateResolutionFailure(runtimeOrDefault(ctx, bridge), ids, err)
		writer(vscode.RunEvent{
			Event:   "errored",
			Message: fmt.Sprintf("desired-state resolution failed for %s: %s", strings.Join(ids, ", "), err.Error()),
		})
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
			resetExit, resetErr := runExclusiveGroupReset(ctx, bridge, declared, id, runtimeRoot(rt, bridge), writer)
			if resetErr != nil {
				writer(vscode.RunEvent{Event: "failed", TestID: id, Message: resetErr.Error()})
				return 1, remaining, resetErr
			}
			if resetExit != 0 {
				writer(vscode.RunEvent{Event: "failed", TestID: id, Message: fmt.Sprintf("exclusive group reset exited %d", resetExit)})
				exitCode = 1
				continue
			}
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

// ExclusiveResetRequest identifies the mutually-exclusive desired-state group
// whose synthesized Unknown State reset peer was run.
type ExclusiveResetRequest struct {
	GroupLabel string
	RunID      string
	Root       string
}

// ExclusiveResetProvider is an optional consumer interface: deactivate
// whatever makes a member of the named mutually-exclusive group derive
// active, so the next derivation yields the Unknown State reset peer as the
// sole active row. Consumers that do not implement it keep the pass-through
// behavior with an explicit "group reset not supported" message.
//
// DHF-REQ: keel/requirement-98
type ExclusiveResetProvider interface {
	ResetExclusiveGroup(context.Context, ExclusiveResetRequest, vscode.RunEventWriter) (int, error)
}

// runExclusiveGroupReset dispatches the consumer reset hook for the exclusive
// group owning unknownID and emits the terminal passed event on success.
//
// DHF-REQ: keel/requirement-98
func runExclusiveGroupReset(ctx context.Context, bridge Bridge, declared DesiredStateDeclaration, unknownID, root string, writer vscode.RunEventWriter) (int, error) {
	resetter, ok := bridge.(ExclusiveResetProvider)
	if !ok {
		writer(vscode.RunEvent{Event: "passed", TestID: unknownID, Message: "selected Unknown State; group reset not supported by this devtool"})
		return 0, nil
	}
	label, ok := exclusiveGroupLabelForUnknownRunID(declared, unknownID)
	if !ok {
		return 0, fmt.Errorf("keel/testbridge: no mutually-exclusive group owns unknown id %q", unknownID)
	}
	exit, err := resetter.ResetExclusiveGroup(ctx, ExclusiveResetRequest{GroupLabel: label, RunID: unknownID, Root: root}, writer)
	if err != nil || exit != 0 {
		return exit, err
	}
	writer(vscode.RunEvent{Event: "passed", TestID: unknownID, Message: "reset exclusive group " + label})
	return 0, nil
}

// exclusiveGroupLabelForUnknownRunID matches a synthesized unknown run id back
// to its declared exclusive group via the stable label segment, independent of
// which parent id (discovery B-parent or desired-state root) minted the id.
func exclusiveGroupLabelForUnknownRunID(declared DesiredStateDeclaration, unknownID string) (string, bool) {
	for _, group := range declared.Groups {
		if group.MutuallyExclusive && strings.HasSuffix(unknownID, "::group::"+stableIDSegment(group.Label)+"::unknown") {
			return group.Label, true
		}
	}
	return "", false
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
func runRemainingSelections(ctx context.Context, bridge Bridge, requests []runResolution, runID, root string, writer vscode.RunEventWriter) (int, []string, error) {
	runBatch := func(batch []runResolution) (int, []string, error) {
		if len(batch) == 0 {
			return 0, nil, nil
		}
		ids := runResolutionIDs(batch)
		if err := ctx.Err(); err != nil {
			return 1, ids, err
		}
		exitCode, err := bridge.Run(ctx, RunRequest{IDs: ids, RunID: runID, Root: root}, writer)
		if err != nil || exitCode != 0 {
			return exitCode, ids, err
		}
		for _, request := range batch {
			emitExclusiveDesiredStateSiblingClears(request.ExclusiveSiblingIDs, request.Request.ID, writer)
		}
		return exitCode, nil, nil
	}

	batch := make([]runResolution, 0, len(requests))
	for _, request := range requests {
		if bridgeHandlesMaintenanceRun(request.Request.ID) {
			if exitCode, ids, err := runBatch(batch); err != nil || exitCode != 0 {
				return exitCode, ids, err
			}
			batch = batch[:0]
			if exitCode, err := runBridgeMaintenance(ctx, bridge, root, runID, request.Request.ID, writer); err != nil || exitCode != 0 {
				return exitCode, []string{request.Request.ID}, err
			}
			continue
		}
		if !request.ExpandedGroupChild {
			batch = append(batch, request)
			continue
		}
		if exitCode, ids, err := runBatch(batch); err != nil || exitCode != 0 {
			return exitCode, ids, err
		}
		batch = batch[:0]
		if exitCode, ids, err := runBatch([]runResolution{request}); err != nil || exitCode != 0 {
			return exitCode, ids, err
		}
	}
	return runBatch(batch)
}

func bridgeHandlesMaintenanceRun(id string) bool {
	switch id {
	case MaintenanceDetectLanesID, MaintenanceUnlockID, MaintenanceClearResultsID, MaintenanceClearStateID:
		return true
	default:
		return false
	}
}

// DHF-REQ: keel/requirement-87
func runBridgeMaintenance(ctx context.Context, bridge Bridge, root, runID, id string, writer vscode.RunEventWriter) (int, error) {
	writer(vscode.RunEvent{Event: "test_started", TestID: id})
	passedMessage := ""
	switch id {
	case MaintenanceDetectLanesID:
		message, err := writeBridgeDetectedLanes(ctx, bridge, root, writer)
		if err != nil {
			return 1, err
		}
		passedMessage = message
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
	writer(vscode.RunEvent{Event: "passed", TestID: id, Message: passedMessage})
	return 0, nil
}

func writeBridgeDetectedLanes(ctx context.Context, bridge Bridge, root string, writer vscode.RunEventWriter) (string, error) {
	var data []byte
	var err error
	var laneIDs []string
	if provider, ok := bridge.(LaneFileProvider); ok {
		provided, err := provider.LaneFile(ctx)
		if err != nil {
			return "", err
		}
		if provided.Version == 0 {
			provided.Version = 1
		}
		if provided.Lanes == nil {
			provided.Lanes = []LaneFileLane{}
		}
		for _, lane := range provided.Lanes {
			laneIDs = append(laneIDs, lane.ID)
		}
		data, err = json.MarshalIndent(provided, "", "  ")
		if err != nil {
			return "", err
		}
	} else if provider, ok := bridge.(LaneProvider); ok {
		provided, err := provider.Lanes(ctx)
		if err != nil {
			return "", err
		}
		file := bridgeLanesFile{Version: 1, Lanes: bridgeLaneFileRows(provided)}
		for _, lane := range file.Lanes {
			laneIDs = append(laneIDs, lane.ID)
		}
		data, err = json.MarshalIndent(file, "", "  ")
		if err != nil {
			return "", err
		}
	} else {
		writer(vscode.RunEvent{Event: "output", TestID: MaintenanceDetectLanesID, Message: "no LaneProvider; wrote empty .vscode/test-lanes.json"})
		data, err = json.MarshalIndent(bridgeLanesFile{Version: 1, Lanes: []bridgeLane{}}, "", "  ")
		if err != nil {
			return "", err
		}
	}
	path := filepath.Join(root, ".vscode", "test-lanes.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	for _, id := range laneIDs {
		writer(vscode.RunEvent{Event: "output", TestID: MaintenanceDetectLanesID, Message: fmt.Sprintf("detected %s", id)})
	}
	return "wrote .vscode/test-lanes.json", nil
}

func bridgeLaneFileRows(items []vscode.TestItem) []bridgeLane {
	lanes := make([]bridgeLane, 0, len(items))
	for _, item := range items {
		if item.ID == "" || (item.Kind != "" && item.Kind != "lane") {
			continue
		}
		id := bridgeLaneFileID(item)
		lanes = append(lanes, bridgeLane{
			ID:                id,
			Label:             bridgeLaneFileLabel(item, id),
			Order:             bridgeLaneFileOrder(item),
			Framework:         item.Framework,
			RequiredResources: append([]string(nil), item.RequiredResources...),
			Members:           bridgeLaneFileMembers(item, id),
		})
	}
	return lanes
}

func bridgeLaneFileID(item vscode.TestItem) string {
	if _, suffix, ok := strings.Cut(strings.TrimSpace(item.ID), "::lane::"); ok && suffix != "" {
		return suffix
	}
	if prefix, suffix, ok := strings.Cut(strings.TrimSpace(item.ID), "::"); ok && prefix != "" && suffix != "" {
		parts := strings.Split(suffix, "::")
		return parts[len(parts)-1]
	}
	return item.ID
}

func bridgeLaneFileLabel(item vscode.TestItem, id string) string {
	label := item.Label
	if label == "" {
		label = id
	}
	return label
}

func bridgeLaneFileOrder(item vscode.TestItem) string {
	if order := normalizedBridgeLaneOrder(item.SortText); order != "" {
		return order
	}
	if fields := strings.Fields(item.Label); len(fields) > 0 {
		return normalizedBridgeLaneOrder(fields[0])
	}
	return ""
}

func normalizedBridgeLaneOrder(value string) string {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if len(value) < 3 || value[1] != '.' {
		return ""
	}
	for _, r := range value[2:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return value
}

func bridgeLaneFileMembers(item vscode.TestItem, id string) []map[string]string {
	switch {
	case strings.Contains(id, "vsix"):
		return []map[string]string{{"root": "vsix"}}
	case strings.Contains(item.Framework, "go") || strings.HasPrefix(id, "go-") || id == "lint" || id == "test-fast" || id == "test-coverage" || id == "ci":
		return []map[string]string{{"root": "go"}}
	default:
		return nil
	}
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

// desiredStateGroupItem reports whether the item is a desired-state group. The
// typed facts are the carrier: an item that declares them is one, and the
// answer no longer depends on parsing a display string.
//
// DHF-REQ: keel/requirement-127
func desiredStateGroupItem(item vscode.TestItem) bool {
	return item.Kind == "group" && item.ParentID != "" && item.DesiredStateGroup != nil
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
	if !ok || !desiredStateGroupItem(parent) || !parent.DesiredStateGroup.MutuallyExclusive {
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
		logBridgeDispatch(rt, "config-init", bridgeDispatchLog{Args: args})
		_, err := InitConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
}

func handleConfigUpgrade(bridge Bridge) cli.Handler {
	return func(ctx context.Context, args []string) error {
		rt := runtimeOrDefault(ctx, bridge)
		logBridgeDispatch(rt, "config-upgrade", bridgeDispatchLog{Args: args})
		_, err := UpgradeConfig(runtimeRoot(rt, bridge), bridge.ConfigTemplate())
		return err
	}
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

// logDesiredStateResolutionFailure records a failed bridge.DesiredState call
// for the selected ids, so a degraded run states its cause.
//
// DHF-REQ: keel/requirement-124
func logDesiredStateResolutionFailure(rt Runtime, ids []string, err error) {
	if rt.Log == nil {
		return
	}
	rt.Log.Warn("testbridge desired-state resolution failed",
		"ids", append([]string{}, ids...),
		"error", err.Error(),
	)
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

// newRunWriter opens the run's spool file and returns the stamping writer. The
// source argument is the initiating surface the caller declared; an empty
// value falls back to defaultRunSource, so an in-process caller that knows
// nothing about surfaces produces the historical stream.
//
// DHF-REQ: keel/requirement-36
func newRunWriter(rt Runtime, workspace Workspace, runID, source string) (vscode.RunEventWriter, func(), error) {
	if source == "" {
		source = defaultRunSource
	}
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
		Source:    source,
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

// RunLockTokenEnv carries the lock holder's token to descendant processes.
// The run verb exports it after acquisition; a nested tests-run whose
// environment token matches the on-disk lock token runs inside the ancestor's
// critical section instead of being refused (requirement-96). The on-disk lock
// stays the single source of truth — the env var is only the capability handed
// down the process tree, never authoritative on its own.
const RunLockTokenEnv = "KEEL_TESTBRIDGE_RUN_LOCK_TOKEN"

// DHF-REQ: keel/requirement-58, keel/requirement-67, keel/requirement-96, keel/requirement-102
func acquireRunLock(root string, ids []string, token string, logger *slog.Logger) (func() error, error) {
	runDir := filepath.Join(root, ".devtools", "vscode-runs")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	path := RunLockPath(root)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("keel/testbridge: create run lock: %w", err)
		}
		// Collision branch order (requirement-102): ancestor-token
		// reentrancy → dead-PID reclaim → refuse.
		if heldByAncestorRun(path) {
			// Reentrant descent (requirement-96): the lock belongs to an
			// ancestor run that exported its token. Proceed without
			// acquiring; only the ancestor releases. Ancestor reentrancy
			// wins even if the recorded PID is dead — the token match is
			// evaluated before liveness.
			return func() error { return nil }, nil
		}
		// Steal-if-dead (requirement-102): reclaim a lock whose recorded
		// owner is gone; a live foreign holder is still refused. On success
		// file is an open handle to a freshly re-created lock — fall through
		// to stamp our own lock into it.
		file, err = reclaimDeadLock(path, logger)
		if err != nil {
			return nil, err
		}
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
	// Export the token so descendant processes inherit the critical section
	// (requirement-96). The release closure restores the prior environment.
	prevToken, hadToken := os.LookupEnv(RunLockTokenEnv)
	if err := os.Setenv(RunLockTokenEnv, token); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("keel/testbridge: export run lock token: %w", err)
	}
	return func() error {
		if hadToken {
			_ = os.Setenv(RunLockTokenEnv, prevToken)
		} else {
			_ = os.Unsetenv(RunLockTokenEnv)
		}
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

// heldByAncestorRun reports whether the existing lock at path was written by an
// ancestor run whose token this process inherited via RunLockTokenEnv. An
// absent env token, an unreadable or corrupt lock file, or a token mismatch all
// answer false, so the caller falls through to the refusal path (fail-closed).
//
// DHF-REQ: keel/requirement-96
func heldByAncestorRun(path string) bool {
	envToken := os.Getenv(RunLockTokenEnv)
	if envToken == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var current vscode.RunLockFile
	if err := json.Unmarshal(data, &current); err != nil {
		return false
	}
	return current.Token != "" && current.Token == envToken
}

// reclaimDeadLock decides the fate of an existing lock that is not held by a
// reentrant ancestor. When the recorded PID is a live process — or liveness
// is uncertain (EPERM), the lock is unreadable/corrupt, or no sane PID is
// recorded — it returns the standard refusal, preserving cross-run
// serialization for genuine concurrent runs (requirement-58, ac-365). When
// the recorded PID is provably dead it removes the stale lock, re-creates it
// O_EXCL, logs a WARN naming the stolen dead PID, and returns the open handle
// for acquireRunLock to stamp (ac-364). Losing the race to re-create (a live
// run beat us to it) also refuses.
//
// DHF-REQ: keel/requirement-102
func reclaimDeadLock(path string, logger *slog.Logger) (*os.File, error) {
	refuse := func() (*os.File, error) {
		return nil, fmt.Errorf("keel/testbridge: run lock already exists at %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return refuse()
	}
	var current vscode.RunLockFile
	if err := json.Unmarshal(data, &current); err != nil {
		return refuse()
	}
	if current.PID <= 0 || processAlive(current.PID) {
		return refuse()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("keel/testbridge: reclaim stale run lock at %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		// A live run re-created the lock between our remove and re-open.
		return refuse()
	}
	if logger != nil {
		logger.Warn("keel/testbridge: reclaimed run lock from dead process", "path", path, "dead_pid", current.PID)
	}
	return file, nil
}

// processAlive reports whether pid refers to a live process using a signal-0
// probe (kill -0 semantics): ESRCH ⇒ dead, EPERM ⇒ a process exists that we
// may not signal ⇒ treat as alive (never steal), any other error ⇒
// fail-closed to alive. On Windows, where signal-0 is unsupported, every
// recorded holder is treated as alive so steal-if-dead narrows to unix.
//
// DHF-REQ: keel/requirement-102
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	switch err := proc.Signal(syscall.Signal(0)); {
	case err == nil:
		return true
	case errors.Is(err, os.ErrProcessDone), errors.Is(err, syscall.ESRCH):
		return false
	default:
		// EPERM (alive but not ours) and any unexpected error fail closed.
		return true
	}
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
