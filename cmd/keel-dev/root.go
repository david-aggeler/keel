package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findModuleRoot walks up from dir until it finds the go.mod that declares the
// keel module, so ci/release always gate the keel repo regardless of which
// subdirectory (or foreign directory) keel-dev is invoked from. It refuses to
// run against any other module — a gate that silently validates the wrong tree
// is worse than one that errors.
func findModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	start := dir
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			if declaresKeel(string(data)) {
				return dir, nil
			}
			return "", fmt.Errorf("go.mod at %s does not declare module %s — refusing to gate a foreign module", dir, modulePath)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s — run keel-dev inside the keel checkout", start)
		}
		dir = parent
	}
}

// declaresKeel reports whether go.mod content declares the keel module path.
func declaresKeel(gomod string) bool {
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) == modulePath
		}
	}
	return false
}
