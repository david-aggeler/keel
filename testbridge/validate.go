package testbridge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/david-aggeler/keel/vscode"
)

var testItemIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*(::[A-Za-z0-9_.@/$:-]+)+$`)

// ValidateDocument validates package-emitted protocol documents against the
// embedded keel/vscode schema family. The stdlib has no JSON Schema evaluator,
// so this function first resolves the embedded schema and then enforces the
// schema's required fields and closed enums through the generated wire types.
//
// DHF-REQ: keel/requirement-58
func ValidateDocument(doc any) error {
	switch v := doc.(type) {
	case vscode.DiscoveryDocument:
		if err := requireSchema(vscode.SchemaDiscovery); err != nil {
			return err
		}
		return validateDiscovery(v)
	case vscode.SetupPlan:
		if err := requireSchema(vscode.SchemaSetupPlan); err != nil {
			return err
		}
		return validateSetupPlan(v)
	case vscode.RunEvent:
		if err := requireSchema(vscode.SchemaRunEvent); err != nil {
			return err
		}
		return validateRunEvent(v)
	case vscode.RunLockFile:
		if err := requireSchema(vscode.SchemaRunLock); err != nil {
			return err
		}
		return validateRunLock(v)
	case vscode.TestBridgeConfig:
		if err := requireSchema(vscode.SchemaTestBridgeConfig); err != nil {
			return err
		}
		return validateConfig(v)
	default:
		return fmt.Errorf("keel/testbridge: unsupported protocol document %T", doc)
	}
}

func requireSchema(name vscode.SchemaName) error {
	data, err := vscode.SchemaBytes(name)
	if err != nil {
		return err
	}
	var schema struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("keel/testbridge: parse embedded schema %s: %w", name, err)
	}
	if schema.ID == "" {
		return fmt.Errorf("keel/testbridge: embedded schema %s has no $id", name)
	}
	return nil
}

func validateDiscovery(doc vscode.DiscoveryDocument) error {
	if doc.Version != 1 {
		return fmt.Errorf("keel/testbridge: discovery version = %d, want 1", doc.Version)
	}
	if doc.Workspace == "" || doc.ModulePath == "" || doc.GeneratedAt.IsZero() {
		return fmt.Errorf("keel/testbridge: discovery missing workspace, module_path, or generated_at")
	}
	for _, item := range doc.Items {
		if item.ID == "" || item.Label == "" || item.Kind == "" {
			return fmt.Errorf("keel/testbridge: discovery item missing id, label, or kind")
		}
		if !testItemIDPattern.MatchString(item.ID) {
			return fmt.Errorf("keel/testbridge: discovery item id %q does not match schema pattern", item.ID)
		}
		if !in(item.Kind, "root", "lane", "package", "file", "suite", "test", "project", "group", "maintenance") {
			return fmt.Errorf("keel/testbridge: discovery item %q has invalid kind %q", item.ID, item.Kind)
		}
		for _, profile := range item.Profiles {
			if !in(profile, "run", "debug", "coverage") {
				return fmt.Errorf("keel/testbridge: discovery item %q has invalid profile %q", item.ID, profile)
			}
		}
	}
	return nil
}

// DHF-REQ: keel/requirement-60
func validateSetupPlan(plan vscode.SetupPlan) error {
	if plan.Version != 2 {
		return fmt.Errorf("keel/testbridge: setup-plan version = %d, want 2", plan.Version)
	}
	if plan.Devtool.Name == "" || plan.Devtool.Version == "" || plan.Workspace == "" || plan.GeneratedAt.IsZero() {
		return fmt.Errorf("keel/testbridge: setup-plan missing devtool, workspace, or generated_at")
	}
	for _, item := range plan.Items {
		if item.ID == "" {
			return fmt.Errorf("keel/testbridge: setup-plan item missing id")
		}
	}
	for _, group := range plan.Groups {
		if group.Label == "" {
			return fmt.Errorf("keel/testbridge: desired-state group missing label")
		}
		if len(group.Rows) == 0 {
			return fmt.Errorf("keel/testbridge: desired-state group %q has no rows", group.Label)
		}
		active := 0
		for _, state := range group.Rows {
			if err := validateDesiredStateRow(state); err != nil {
				return err
			}
			if state.Active {
				active++
			}
		}
		if group.MutuallyExclusive && active != 1 {
			return fmt.Errorf("keel/testbridge: desired-state exclusive group %q has %d active rows, want exactly one active row", group.Label, active)
		}
	}
	for _, check := range plan.Checks {
		if check.ID == "" {
			return fmt.Errorf("keel/testbridge: setup-plan check missing id")
		}
	}
	for _, action := range plan.Actions {
		if action.Resource == "" || action.Status == "" {
			return fmt.Errorf("keel/testbridge: setup-plan action missing resource or status")
		}
		if !in(action.Status, "reuse", "setup_required", "reconcile", "reconcile_during_run", "manual_setup_required") {
			return fmt.Errorf("keel/testbridge: setup-plan action %q has invalid status %q", action.Resource, action.Status)
		}
	}
	if plan.Teardown.Policy == "" {
		return fmt.Errorf("keel/testbridge: setup-plan teardown policy is required")
	}
	return nil
}

func validateDesiredStateRow(state vscode.DesiredState) error {
	if state.Resource == "" || state.Kind == "" || state.Desired == "" || state.Current == "" || state.Status == "" || state.Action == "" {
		return fmt.Errorf("keel/testbridge: desired-state row %q missing required fields", state.Resource)
	}
	if !in(state.Kind, "tool", "dependency", "binary", "host-port-set", "fixture-data", "credential", "service", "unknown") {
		return fmt.Errorf("keel/testbridge: desired-state row %q has invalid kind %q", state.Resource, state.Kind)
	}
	if !in(state.Status, "satisfied", "blocked", "reconcilable") {
		return fmt.Errorf("keel/testbridge: desired-state row %q has invalid status %q", state.Resource, state.Status)
	}
	if !in(state.Action, "reuse", "manual_setup_required", "reconcile", "reconcile_during_run") {
		return fmt.Errorf("keel/testbridge: desired-state row %q has invalid action %q", state.Resource, state.Action)
	}
	return nil
}

func validateRunEvent(event vscode.RunEvent) error {
	if event.Version != 1 || event.Event == "" || event.Time.IsZero() {
		return fmt.Errorf("keel/testbridge: run-event missing version, event, or time")
	}
	if !vscode.IsKnownRunEvent(event.Event) {
		return fmt.Errorf("keel/testbridge: run-event has invalid event %q", event.Event)
	}
	if event.Source != "" && !in(event.Source, "vscode", "external") {
		return fmt.Errorf("keel/testbridge: run-event has invalid source %q", event.Source)
	}
	if event.DurationMS < 0 {
		return fmt.Errorf("keel/testbridge: run-event has negative duration_ms")
	}
	if event.Artifact != nil && !in(event.Artifact.Kind, "log", "trace", "screenshot", "video", "coverage", "report", "other") {
		return fmt.Errorf("keel/testbridge: run-event artifact has invalid kind %q", event.Artifact.Kind)
	}
	return nil
}

func validateRunLock(lock vscode.RunLockFile) error {
	if lock.PID < 1 || lock.CreatedAt == "" {
		return fmt.Errorf("keel/testbridge: run-lock missing pid or created_at")
	}
	if _, err := time.Parse(time.RFC3339Nano, lock.CreatedAt); err != nil {
		return fmt.Errorf("keel/testbridge: run-lock created_at: %w", err)
	}
	for _, id := range lock.IDs {
		if id == "" {
			return fmt.Errorf("keel/testbridge: run-lock contains empty id")
		}
	}
	if lock.Token == "" {
		return fmt.Errorf("keel/testbridge: run-lock token is required")
	}
	return nil
}

func validateConfig(cfg vscode.TestBridgeConfig) error {
	if cfg.Version != vscode.CurrentConfigVersion {
		return fmt.Errorf("keel/testbridge: config version = %d, want %d", cfg.Version, vscode.CurrentConfigVersion)
	}
	if cfg.Command == "" || cfg.DisplayName == "" {
		return fmt.Errorf("keel/testbridge: config missing command or displayName")
	}
	if hasProtocolTokens(cfg.Args) {
		return fmt.Errorf("keel/testbridge: config v3 args must be launcher-only")
	}
	return nil
}

func hasProtocolTokens(args []string) bool {
	for i, arg := range args {
		if arg == "test-bridge" {
			return true
		}
		if arg == "vscode" && i+1 < len(args) && (args[i+1] == "tests" || args[i+1] == "config") {
			return true
		}
	}
	return false
}

func in(got string, allowed ...string) bool {
	for _, want := range allowed {
		if got == want {
			return true
		}
	}
	return false
}
