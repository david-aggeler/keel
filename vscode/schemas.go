package vscode

import (
	"embed"
	"fmt"
)

// SchemasFS embeds the discover/desired-state/run schema source of truth.
//
//go:embed schemas/*.json
var SchemasFS embed.FS

// SchemaName is one embedded VS Code protocol schema.
type SchemaName string

const (
	SchemaDiscovery        SchemaName = "discovery"
	SchemaDesiredState     SchemaName = "desired-state"
	SchemaRunEvent         SchemaName = "run-event"
	SchemaRunLock          SchemaName = "run-lock"
	SchemaTestBridgeConfig SchemaName = "test-bridge-config"
)

// SchemaBytes returns one embedded JSON Schema by logical name.
//
// DHF-REQ: keel/requirement-34
func SchemaBytes(name SchemaName) ([]byte, error) {
	switch name {
	case SchemaDiscovery, SchemaDesiredState, SchemaRunEvent, SchemaRunLock, SchemaTestBridgeConfig:
		return SchemasFS.ReadFile("schemas/" + string(name) + ".json")
	default:
		return nil, fmt.Errorf("keel/vscode: unknown schema %q", name)
	}
}
