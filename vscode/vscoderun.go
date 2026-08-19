// Package vscode holds the consumer-neutral core of the VS Code test-runner
// bridge: the run-event/lock/location/artifact wire types and the
// WorkspaceProfile port through which a consumer dev tool injects its
// repo-specific behavior.
//
// This package imports no consumer-specific devtool code. It is the shared
// discover/desired-state/run protocol and projection engine that downstream devtools
// wire to their own workspace adapters.
package vscode

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RunEvent is one line of the neutral run-event JSONL protocol emitted to the
// VS Code extension. Field tags are the wire contract and must stay stable
// byte-for-byte across every consumer.
//
// DHF-REQ: keel/requirement-23
type RunEvent struct {
	Version    int          `json:"version"`
	Event      string       `json:"event"`
	Time       time.Time    `json:"time"`
	RunID      string       `json:"run_id,omitempty"`
	Source     string       `json:"source,omitempty"`
	Workspace  string       `json:"workspace,omitempty"`
	Live       *bool        `json:"live,omitempty"`
	Requested  []RunRequest `json:"requested,omitempty"`
	TestID     string       `json:"test_id,omitempty"`
	Message    string       `json:"message,omitempty"`
	DurationMS int64        `json:"duration_ms,omitempty"`
	Location   *RunLocation `json:"location,omitempty"`
	Artifact   *RunArtifact `json:"artifact,omitempty"`
	ExitCode   *int         `json:"exit_code,omitempty"`
}

// RunRequest records the exact test item a run was requested to execute.
type RunRequest struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// RunLocation is a source position reported alongside a run event.
type RunLocation struct {
	URI    string `json:"uri"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// RunArtifact is a run byproduct (trace, screenshot, report) surfaced to the UI.
type RunArtifact struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
	Kind string `json:"kind"`
}

// RunLockFile is the on-disk representation of the cross-run lock that keeps a
// single VS Code test run active per workspace.
type RunLockFile struct {
	PID       int      `json:"pid"`
	CreatedAt string   `json:"created_at"`
	IDs       []string `json:"ids"`
	Token     string   `json:"token,omitempty"`
}

// DiscoveryDocument is the JSON document emitted by a consumer devtool's
// `test-bridge tests discover --format json` command.
//
// DHF-REQ: keel/requirement-23, keel/requirement-34
type DiscoveryDocument struct {
	Version      int                   `json:"version"`
	Workspace    string                `json:"workspace"`
	ModulePath   string                `json:"module_path"`
	GeneratedAt  time.Time             `json:"generated_at"`
	Capabilities DiscoveryCapabilities `json:"capabilities"`
	Items        []TestItem            `json:"items"`
}

// DiscoveryCapabilities advertises Test Explorer behavior supported by the
// producer.
type DiscoveryCapabilities struct {
	ClearResults              bool     `json:"clear_results"`
	RefreshInvalidatesResults bool     `json:"refresh_invalidates_results"`
	NeutralParentRollups      bool     `json:"neutral_parent_rollups"`
	ClearResultsTestIDs       []string `json:"clear_results_test_ids,omitempty"`
	ClearStateTestIDs         []string `json:"clear_state_test_ids,omitempty"`
	// ReconcileResults is the bridge-computed rendered truth for
	// mutually-exclusive desired-state rows: one stamp per row with a run
	// id (active row passed, every other row skipped). Consumers replay
	// the entries verbatim through a non-persisted test run on every
	// discovery refresh, overwriting stale (including persistence-restored)
	// results.
	// DHF-REQ: keel/requirement-97
	ReconcileResults []ReconcileResult `json:"reconcile_results,omitempty"`
}

// ReconcileResult is one bridge-computed rendered-state stamp for a
// mutually-exclusive desired-state row.
//
// DHF-REQ: keel/requirement-97
type ReconcileResult struct {
	TestID  string `json:"test_id"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// TestItem is one node in the discovered VS Code test tree.
type TestItem struct {
	ID                string   `json:"id"`
	ParentID          string   `json:"parent_id,omitempty"`
	Label             string   `json:"label"`
	Kind              string   `json:"kind"`
	Framework         string   `json:"framework,omitempty"`
	Runner            string   `json:"runner,omitempty"`
	RunnerLabel       string   `json:"runner_label,omitempty"`
	URI               string   `json:"uri,omitempty"`
	Range             *Range   `json:"range,omitempty"`
	Runnable          bool     `json:"runnable"`
	Profiles          []string `json:"profiles"`
	LaneID            string   `json:"lane_id,omitempty"`
	PlaywrightProject string   `json:"playwright_project,omitempty"`
	CanonicalID       string   `json:"canonical_id,omitempty"`
	RequiredResources []string `json:"required_resources,omitempty"`
	// Description is the producer's own human-readable prose for this item —
	// one string, not a composed line. Sequencing prose against anything else
	// belongs to the consumer, so the producer never joins.
	//
	// DHF-REQ: keel/requirement-138
	Description string `json:"description,omitempty"`
	// Findings are the typed validation findings raised against this item.
	// Absent when the item raised none.
	//
	// DHF-REQ: keel/requirement-138
	Findings []Finding `json:"findings,omitempty"`
	// Conditions are the persistent non-result conditions standing against
	// this item at discovery time — a source file that will not parse, a
	// prerequisite that cannot be satisfied. They are not run results and
	// have no run behind them, so they travel apart from both the prose
	// channel and the run-event stream. Absent when the item carries none.
	//
	// DHF-REQ: keel/requirement-140
	Conditions []Condition `json:"conditions,omitempty"`
	// LastRun is the typed measurement of the newest run attributable to this
	// item alone. Absent — never zeroed — when no run is attributable.
	//
	// DHF-REQ: keel/requirement-138
	LastRun           *LastRunFacts           `json:"last_run,omitempty"`
	DesiredStateGroup *DesiredStateGroupFacts `json:"desired_state_group,omitempty"`
	DesiredStateRow   *DesiredStateRowFacts   `json:"desired_state_row,omitempty"`
}

// UnmarshalJSON refuses a discovery item that still carries the removed
// `limitations` array, so a producer that has not migrated to the scalar
// `description` channel fails loudly instead of having its prose silently
// dropped. It mirrors the refusal DesiredStateDocument already applies to the
// fields its v3 shape removed.
//
// DHF-REQ: keel/requirement-138
func (t *TestItem) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["limitations"]; ok {
		return fmt.Errorf("keel/vscode: removed field %q on discovery item; the prose channel is the scalar %q", "limitations", "description")
	}
	type testItem TestItem
	var decoded testItem
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = TestItem(decoded)
	return nil
}

// Finding is one typed validation finding a producer raises against a discovery
// item. The severity is the fact a consumer selects a surface by, so it travels
// as a closed enum rather than as a token inside a message a consumer would
// have to parse.
//
// DHF-REQ: keel/requirement-138
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Condition is one persistent non-result condition standing against a discovery
// item. Its lifetime is the reason it is not a run event: it holds until the
// producer stops reporting it, whereas a run result asserts an outcome for one
// execution that took place. The kind travels as a closed enum so a consumer
// selects behavior by it rather than by parsing the message.
//
// DHF-REQ: keel/requirement-140
type Condition struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// conditionKinds is the closed set of kind values a Condition may carry.
var conditionKinds = map[string]struct{}{
	"parse_error":              {},
	"prerequisite_unsatisfied": {},
}

// IsConditionKind reports whether kind is one of the closed enum values a
// producer may stamp on a Condition. It is the single producer-side answer to
// that question, so a validator elsewhere cannot drift from this value set.
//
// DHF-REQ: keel/requirement-140
func IsConditionKind(kind string) bool {
	_, ok := conditionKinds[kind]
	return ok
}

// LastRunFacts is the typed measurement of the newest run attributable to one
// discovery item. DurationMS and ExitCode are pointers so that a measured zero
// stays distinguishable from an absent measurement — the conflation the
// omitempty int64 on RunEvent.DurationMS already permits elsewhere on the wire.
//
// DHF-REQ: keel/requirement-138
type LastRunFacts struct {
	At         time.Time `json:"at"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	ExitCode   *int      `json:"exit_code,omitempty"`
}

// findingSeverities is the closed set of severity values a Finding may carry.
var findingSeverities = map[string]struct{}{
	"error":   {},
	"warning": {},
}

// IsFindingSeverity reports whether severity is one of the closed enum values a
// producer may stamp on a Finding. It is the single producer-side answer to
// that question, so a validator elsewhere cannot drift from this value set.
//
// DHF-REQ: keel/requirement-138
func IsFindingSeverity(severity string) bool {
	_, ok := findingSeverities[severity]
	return ok
}

// IsErrorSeverity reports whether a finding's typed severity selects the
// consumer's persistent error surface rather than the composed description.
// The severity travels as a closed enum precisely so that this decision reads
// one member and parses no message (keel/ac-559).
//
// DHF-REQ: keel/requirement-140
func IsErrorSeverity(finding Finding) bool {
	return finding.Severity == "error"
}

// DesiredStateGroupFacts are the typed desired-state facts a discovery item
// carries when it is a desired-state group. Its presence is what identifies
// the item as one.
//
// DHF-REQ: keel/requirement-127
type DesiredStateGroupFacts struct {
	MutuallyExclusive bool `json:"mutually_exclusive"`
}

// DesiredStateRowFacts are the typed desired-state facts a discovery item
// carries when it is a row inside a desired-state group. Its presence is what
// identifies the item as one.
//
// DHF-REQ: keel/requirement-127
type DesiredStateRowFacts struct {
	Current string `json:"current"`
	Action  string `json:"action"`
	Active  bool   `json:"active"`
}

// desiredStateActions is the closed set of action values a desired-state row
// may carry, on the desired-state document and on the discovery item alike.
var desiredStateActions = map[string]struct{}{
	"reuse":                 {},
	"manual_setup_required": {},
	"reconcile":             {},
	"reconcile_during_run":  {},
}

// IsDesiredStateAction reports whether action is one of the closed enum values
// the producer emits.
//
// DHF-REQ: keel/requirement-127
func IsDesiredStateAction(action string) bool {
	_, ok := desiredStateActions[action]
	return ok
}

// Range is a zero-based source range in a test item.
type Range struct {
	StartLine   int `json:"start_line"`
	StartColumn int `json:"start_column"`
	EndLine     int `json:"end_line"`
	EndColumn   int `json:"end_column"`
}

// DesiredStateDocument is the JSON document emitted by
// `test-bridge tests desired-state --format json`.
//
// DHF-REQ: keel/requirement-23, keel/requirement-34, keel/requirement-60, keel/requirement-77
type DesiredStateDocument struct {
	Version        int                 `json:"version"`
	Devtool        DevtoolMetadata     `json:"devtool"`
	Workspace      string              `json:"workspace"`
	GeneratedAt    time.Time           `json:"generated_at"`
	Groups         []DesiredStateGroup `json:"groups"`
	TeardownPolicy string              `json:"teardown_policy,omitempty"`
}

// DevtoolMetadata identifies the producer that generated a desired-state document.
type DevtoolMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

// UnmarshalJSON accepts the v3 desired-state shape and rejects removed v2 fields
// before a caller can accidentally treat a legacy document as v3.
func (p *DesiredStateDocument) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, field := range []string{"items", "required_resources", "checks", "actions", "teardown", "limitations"} {
		if _, ok := raw[field]; ok {
			return fmt.Errorf("removed field %q in desired-state v3", field)
		}
	}
	type desiredStateDocument DesiredStateDocument
	var decoded desiredStateDocument
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = DesiredStateDocument(decoded)
	return nil
}

// DesiredState describes the target and current state for a required resource.
// RunID, when present, is the canonical devtool-served id that makes the row
// runnable through the ordinary run interaction (test-bridge tests run --id):
// a consumer that serves run_id MUST resolve that id in its run path. Rows
// without RunID are informational and are never submitted on the wire.
//
// DHF-REQ: keel/requirement-60
type DesiredState struct {
	RunID    string `json:"run_id,omitempty"`
	Resource string `json:"resource"`
	Kind     string `json:"kind"`
	Desired  string `json:"desired"`
	Current  string `json:"current"`
	Status   string `json:"status"`
	Action   string `json:"action"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
	Reusable bool   `json:"reusable"`
	Owned    bool   `json:"owned"`
	Active   bool   `json:"active,omitempty"`
}

// DesiredStateGroup is a consumer-declared desired-state row cluster.
type DesiredStateGroup struct {
	Label             string         `json:"label"`
	Order             int            `json:"order"`
	MutuallyExclusive bool           `json:"mutually_exclusive"`
	Rows              []DesiredState `json:"rows"`
}

// RunEventWriter is the sink a projector emits run events to.
type RunEventWriter func(RunEvent)

// WorkspaceProfile is the injection seam the neutral test-runner engine reads
// its consumer-specific behavior from. Sub-unit A defines the scalar seams
// the leaf projectors and engine already depend on; the richer orchestration
// seams (lane resolution, resource reconciliation, task execution, prereq
// checks) are folded in as the run orchestration is parameterized on this port
// in a later sub-unit. A consumer dev tool supplies a concrete implementation;
// the engine holds only this interface so it imports nothing consumer-specific.
type WorkspaceProfile interface {
	// Repo is the absolute path to the consumer's repository root.
	Repo() string
	// ModulePath returns the Go module path for the workspace, or "" when
	// it cannot be determined.
	ModulePath() string
	// LogDir is the directory run logs are written under.
	LogDir() string
	// MaxOutputBytes is the buffer cap applied to a child test process's
	// captured output.
	MaxOutputBytes() int
	// RemediationHint is the consumer-specific guidance appended when desired-state
	// reconciliation is blocked.
	RemediationHint() string
	// ConsumerID identifies the consumer dev tool that owns a run (e.g.
	// "vscode"), for cross-consumer run attribution.
	ConsumerID() string
	// Node identifies the worktree or node a run executes on, for cross-node
	// run attribution.
	Node() string
	// PrepareLane reconciles the lane's required desired state (the desired-state
	// document) before a run. A non-empty LaneReadiness.Blocked means at least one
	// prerequisite could not be met and the run must not proceed. This is the
	// preparation/desired-state hook the neutral engine drives ahead of every
	// run so a missing prerequisite surfaces as a structured lane-blocked
	// result rather than a silent failure.
	PrepareLane(ctx context.Context, laneID string) LaneReadiness
}
