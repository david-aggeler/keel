package testbridge_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/log/logtest"
	"github.com/david-aggeler/keel/testbridge"
)

// DHF-TEST: keel/requirement-168 (keel/ac-717)
func TestConfigInitReportsCreatedThenAlreadyPresent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")

	records, protocol := dispatchConfigVerb(t, root, "config-init")
	assertOneConfigOutcome(t, records, "config-init", path, "created")
	if protocol != "" {
		t.Fatalf("config-init created: protocol stream = %q, want empty", protocol)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}

	records, protocol = dispatchConfigVerb(t, root, "config-init")
	assertOneConfigOutcome(t, records, "config-init", path, "already present")
	if protocol != "" {
		t.Fatalf("config-init already present: protocol stream = %q, want empty", protocol)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config-init on an existing file changed it:\nbefore %s\nafter  %s", before, after)
	}
}

// DHF-TEST: keel/requirement-168 (keel/ac-718)
func TestConfigUpgradeReportsUpgradedThenAlreadyCurrent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "test-bridge.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"command":"bin/custom","args":["vscode","tests"],"displayName":"Custom"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	records, protocol := dispatchConfigVerb(t, root, "config-upgrade")
	assertOneConfigOutcome(t, records, "config-upgrade", path, "upgraded")
	if protocol != "" {
		t.Fatalf("config-upgrade upgraded: protocol stream = %q, want empty", protocol)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read upgraded config: %v", err)
	}

	records, protocol = dispatchConfigVerb(t, root, "config-upgrade")
	assertOneConfigOutcome(t, records, "config-upgrade", path, "already current")
	if protocol != "" {
		t.Fatalf("config-upgrade already current: protocol stream = %q, want empty", protocol)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config-upgrade on a current file changed it:\nbefore %s\nafter  %s", before, after)
	}
}

func dispatchConfigVerb(t *testing.T, root, verb string) ([]map[string]any, string) {
	t.Helper()
	capture := logtest.NewCapture()
	logger, err := logging.New(logging.Config{
		Service:  "testbridge-test",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{capture},
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer func() { _ = logger.Close() }()
	var protocol bytes.Buffer
	ctx := testbridge.WithRuntime(context.Background(), testbridge.Runtime{
		Root:     root,
		Protocol: &protocol,
		Log:      logger.Slog(),
	})
	if err := testbridge.CommandSpec(newFakeBridge(root)).Dispatch(ctx, []string{"test-bridge", verb}); err != nil {
		t.Fatalf("%s dispatch: %v", verb, err)
	}
	return capture.AllJSON(), protocol.String()
}

// assertOneConfigOutcome requires exactly one record at Info or above that
// names path, and that it carries the wanted outcome for verb.
func assertOneConfigOutcome(t *testing.T, records []map[string]any, verb, path, outcome string) {
	t.Helper()
	var matches []map[string]any
	for _, record := range records {
		if record["path"] != path {
			continue
		}
		matches = append(matches, record)
	}
	if len(matches) != 1 {
		t.Fatalf("%s: %d records name path %s, want exactly one\nrecords: %+v", verb, len(matches), path, records)
	}
	got := matches[0]
	if got["level"] != "INFO" && got["level"] != "WARN" && got["level"] != "ERROR" {
		t.Fatalf("%s: outcome record level = %v, want Info or above", verb, got["level"])
	}
	if got["verb"] != verb || got["outcome"] != outcome {
		t.Fatalf("%s: outcome record = %+v, want verb %q outcome %q", verb, got, verb, outcome)
	}
}
