package vscode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DHF-TEST: keel/requirement-23
func TestVSCodePackageContainsNoVelaTraceRefs(t *testing.T) {
	donorReq := "vela/" + "requirement-"
	donorModule := "go.aggeler" + ".com/vela"
	donorSource := `"source":"` + "vela-dev" + `"`
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".json")) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), donorReq) || strings.Contains(string(body), donorModule) || strings.Contains(string(body), donorSource) {
			t.Fatalf("%s still contains donor-specific trace or source text", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
