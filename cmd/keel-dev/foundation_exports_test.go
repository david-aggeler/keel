package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const foundationExportsModulePath = "github.com/david-aggeler/keel"

// DHF-TEST: keel/requirement-31
func TestFoundationExportsAreConsumerAgnostic(t *testing.T) {
	root := repoRootForTest(t)
	importPaths, err := foundationExportImportPaths(context.Background(), discardLogger(), root)
	if err != nil {
		t.Fatal(err)
	}
	var docFailures []string
	for _, importPath := range importPaths {
		cmd := exec.Command("go", "doc", importPath)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			docFailures = append(docFailures, "go doc "+importPath+": "+err.Error()+"\n"+string(out))
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.HasPrefix(line, "const ") &&
				!strings.HasPrefix(line, "func ") &&
				!strings.HasPrefix(line, "type ") {
				continue
			}
			for _, forbidden := range []string{"Vault", "vault"} {
				if strings.Contains(line, forbidden) {
					t.Errorf("go doc %s contains consumer-domain term %q in exported declaration %q", importPath, forbidden, line)
				}
			}
		}
	}
	if len(docFailures) != 0 {
		t.Fatalf("go doc failures:\n%s", strings.Join(docFailures, "\n"))
	}
}

// DHF-TEST: keel/requirement-85 (keel/ac-666)
func TestFoundationExportsIgnoreGitignoredTestOnlyPackages(t *testing.T) {
	root := repoRootForTest(t)
	scratch := filepath.Join(root, "scratchpad", "foundation-exports-test-only-package")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(scratch); err != nil {
			t.Errorf("cleanup scratch package: %v", err)
		}
	})
	if err := os.WriteFile(filepath.Join(scratch, "probe_test.go"), []byte("package probe_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "./cmd/keel-dev", "-run", "^TestFoundationExportsAreConsumerAgnostic$", "-count=1")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("foundation exports test should ignore gitignored test-only package: %v\n%s", err, out)
	}
}

// DHF-REQ: keel/requirement-85 (keel/ac-666, keel/ac-763)
func foundationExportImportPaths(ctx context.Context, logger *slog.Logger, root string) ([]string, error) {
	cfg, err := loadKeelDevConfig(root)
	if err != nil {
		return nil, err
	}
	files, err := trackedFilesWithExt(ctx, logger, root, cfg.Gate.Excludes, ".go")
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	for _, file := range files {
		dir := path.Dir(filepath.ToSlash(file))
		if dir == "." {
			seen[foundationExportsModulePath] = true
			continue
		}
		seen[foundationExportsModulePath+"/"+dir] = true
	}
	importPaths := make([]string, 0, len(seen))
	for importPath := range seen {
		importPaths = append(importPaths, importPath)
	}
	sort.Strings(importPaths)
	return importPaths, nil
}

// DHF-TEST: keel/requirement-85 (keel/ac-763)
func TestFoundationExportImportPathsUseCanonicalConfigShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
	}{
		{
			name: "four-space indentation",
			yaml: "gate:\n    excludes:\n      - excluded/**\n",
		},
		{
			name: "flow sequence",
			yaml: "gate:\n  excludes: [excluded/**]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			mustRun(t, root, "git", "init")
			writeFile(t, root, keelDevConfigFile, tc.yaml)
			writeFile(t, root, "go.mod", "module "+foundationExportsModulePath+"\n\ngo 1.25\n")
			for _, dir := range []string{"excluded", "kept"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
					t.Fatal(err)
				}
				writeFile(t, root, filepath.Join(dir, "probe.go"), "package "+dir+"\n")
			}
			mustRun(t, root, "git", "add", ".")

			got, err := foundationExportImportPaths(context.Background(), discardLogger(), root)
			if err != nil {
				t.Fatalf("foundationExportImportPaths: %v", err)
			}
			if slices.Contains(got, foundationExportsModulePath+"/excluded") {
				t.Fatalf("excluded package present in %v", got)
			}
			if !slices.Contains(got, foundationExportsModulePath+"/kept") {
				t.Fatalf("kept package absent from %v", got)
			}
		})
	}
}
