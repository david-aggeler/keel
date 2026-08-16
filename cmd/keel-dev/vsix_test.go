package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logging "github.com/david-aggeler/keel/log"
)

// DHF-TEST: keel/requirement-11, keel/requirement-40
func TestHandleVSIXGateRejectsArgsAndReportsMissingToolchain(t *testing.T) {
	if err := handleVSIXGate(context.Background(), []string{"extra"}); err == nil || !strings.Contains(err.Error(), "unknown vsix command") {
		t.Fatalf("handleVSIXGate extra args err = %v, want usage error", err)
	}

	t.Setenv("PATH", t.TempDir())
	ctx := withRunStateProtocol(context.Background(), logging.Discard(), nil, t.TempDir(), io.Discard)
	err := handleVSIXGate(ctx, []string{"ci"})
	if err == nil || !strings.Contains(err.Error(), `required tool "node" not found`) {
		t.Fatalf("handleVSIXGate missing toolchain err = %v, want node missing", err)
	}
}

// DHF-TEST: keel/requirement-11, keel/requirement-79
func TestEvaluateVSIXCoverageSummaryValidatesFixtureAndTotals(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "coverage-summary.json")
	logger := logging.Discard()

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "excluded test fixture",
			body: `{"total":{"statements":{"pct":99}},"src/test/fixture.ts":{"statements":{"pct":100}}}`,
			want: "excluded test fixture",
		},
		{
			name: "missing total",
			body: `{"total":{"statements":{}}}`,
			want: "has no total statement coverage",
		},
		{
			name: "below floor",
			body: `{"total":{"statements":{"pct":10}}}`,
			want: "below the",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			err := evaluateVSIXCoverageSummary(logger, path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("evaluateVSIXCoverageSummary err = %v, want containing %q", err, tc.want)
			}
		})
	}

	if err := os.WriteFile(path, []byte(`{"total":{"statements":{"pct":80}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := evaluateVSIXCoverageSummary(logger, path); err != nil {
		t.Fatalf("evaluateVSIXCoverageSummary valid summary: %v", err)
	}

	if err := evaluateVSIXCoverageSummary(logger, filepath.Join(root, "missing.json")); err == nil || !strings.Contains(err.Error(), "coverage summary") {
		t.Fatalf("missing summary err = %v, want read failure", err)
	}
	if err := os.WriteFile(path, []byte("{bad json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := evaluateVSIXCoverageSummary(logger, path); err == nil || !strings.Contains(err.Error(), "parse vsix coverage summary") {
		t.Fatalf("malformed summary err = %v, want parse failure", err)
	}
}

// DHF-TEST: keel/requirement-119
func TestValidateVSIXSupportPolicyBindsPolicyManifestAndTypeCeilings(t *testing.T) {
	root := t.TempDir()
	writeVSIXPolicyFixture(t, root, "^1.125.0", "24", "^1.125.0", "1.102.0", "^22.20.1")
	if err := validateVSIXSupportPolicy(root); err != nil {
		t.Fatalf("validateVSIXSupportPolicy valid fixture: %v", err)
	}

	for _, tc := range []struct {
		name          string
		policyEngine  string
		nodeMajor     string
		manifestFloor string
		typesVSCode   string
		typesNode     string
		want          string
	}{
		{
			name:          "manifest does not match policy",
			policyEngine:  "^1.125.0",
			nodeMajor:     "24",
			manifestFloor: "^1.102.0",
			typesVSCode:   "1.102.0",
			typesNode:     "^22.20.1",
			want:          "engines.vscode",
		},
		{
			name:          "vscode types are ahead of declared engine",
			policyEngine:  "^1.125.0",
			nodeMajor:     "24",
			manifestFloor: "^1.125.0",
			typesVSCode:   "1.126.0",
			typesNode:     "^22.20.1",
			want:          "@types/vscode",
		},
		{
			name:          "vscode types patch is ahead of declared engine",
			policyEngine:  "^1.125.0",
			nodeMajor:     "24",
			manifestFloor: "^1.125.0",
			typesVSCode:   "1.125.1",
			typesNode:     "^22.20.1",
			want:          "@types/vscode",
		},
		{
			name:          "node types are ahead of declared runtime",
			policyEngine:  "^1.125.0",
			nodeMajor:     "24",
			manifestFloor: "^1.125.0",
			typesVSCode:   "1.125.0",
			typesNode:     "^26.2.0",
			want:          "@types/node",
		},
		{
			name:          "policy omits runtime mapping",
			policyEngine:  "^1.125.0",
			nodeMajor:     "",
			manifestFloor: "^1.125.0",
			typesVSCode:   "1.125.0",
			typesNode:     "^22.20.1",
			want:          "VS Code runtime Node major",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeVSIXPolicyFixture(t, root, tc.policyEngine, tc.nodeMajor, tc.manifestFloor, tc.typesVSCode, tc.typesNode)
			err := validateVSIXSupportPolicy(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateVSIXSupportPolicy err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

// DHF-TEST: keel/requirement-119 (keel/ac-459)
func TestValidateVSIXSupportPolicyRequiresHeldDependencyNotes(t *testing.T) {
	root := t.TempDir()
	writeVSIXPolicyFixture(t, root, "^1.125.0", "24", "^1.125.0", "1.102.0", "^22.20.1")

	policy := "# Keel Test Bridge Supported VS Code\n\n" +
		"DHF-REQ: keel/requirement-119\n\n" +
		"Minimum supported VS Code: ^1.125.0\n" +
		"VS Code runtime Node major: 24\n\n" +
		"Reason: fixture\n"
	if err := os.WriteFile(filepath.Join(root, "vsix", "SUPPORTED_VSCODE.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validateVSIXSupportPolicy(root)
	if err == nil || !strings.Contains(err.Error(), "Dependency hold notes") {
		t.Fatalf("validateVSIXSupportPolicy err = %v, want dependency hold note failure", err)
	}
}

// DHF-TEST: keel/requirement-119 (keel/ac-466)
func TestValidateVSIXSupportPolicyRequiresRuntimeNodeCitation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		citation string
		want     string
	}{
		{
			name:     "citation line absent",
			citation: "",
			want:     "VS Code runtime Node major source",
		},
		{
			name:     "citation names no VS Code release",
			citation: "Electron 42.3.0 with Node.js 24.15.0 — https://github.com/ewanharris/vscode-versions",
			want:     "declared VS Code release 1.125.0",
		},
		{
			name:     "citation names no Electron version",
			citation: "VS Code 1.125.0 ships Node.js 24.15.0 — https://github.com/ewanharris/vscode-versions",
			want:     "Electron version",
		},
		{
			name:     "citation names no Node version",
			citation: "VS Code 1.125.0 ships Electron 42.3.0 — https://github.com/ewanharris/vscode-versions",
			want:     "Node version",
		},
		{
			name:     "cited Node major contradicts the declared value",
			citation: "VS Code 1.125.0 ships Electron 42.3.0 with Node.js 26.2.0 — https://github.com/ewanharris/vscode-versions",
			want:     "does not match the declared VS Code runtime Node major 24",
		},
		{
			name:     "citation cites no external URL",
			citation: "VS Code 1.125.0 ships Electron 42.3.0 with Node.js 24.15.0",
			want:     "external source URL",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeVSIXPolicyFixtureWithCitation(t, root, "24", tc.citation)
			err := validateVSIXSupportPolicy(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateVSIXSupportPolicy err = %v, want containing %q", err, tc.want)
			}
		})
	}

	root := t.TempDir()
	writeVSIXPolicyFixtureWithCitation(t, root, "24",
		"VS Code 1.125.0 ships Electron 42.3.0 with Node.js 24.15.0 — https://github.com/ewanharris/vscode-versions")
	if err := validateVSIXSupportPolicy(root); err != nil {
		t.Fatalf("validateVSIXSupportPolicy with a well-formed citation: %v", err)
	}
}

// DHF-TEST: keel/requirement-119
func TestValidateVSIXSupportPolicyRejectsMalformedPolicyAndManifest(t *testing.T) {
	root := t.TempDir()
	if err := validateVSIXSupportPolicy(root); err == nil || !strings.Contains(err.Error(), "SUPPORTED_VSCODE.md") {
		t.Fatalf("validateVSIXSupportPolicy missing policy err = %v, want policy path", err)
	}

	for _, tc := range []struct {
		name    string
		policy  string
		pkg     string
		want    string
		noVSIX  bool
		noPkg   bool
		badJSON bool
	}{
		{
			name:   "missing minimum",
			policy: "Reason: fixture\nVS Code runtime Node major: 24\n",
			pkg:    validVSIXPolicyPackage("^1.125.0", "1.125.0", "^22.20.1"),
			want:   "Minimum supported VS Code",
		},
		{
			name:   "missing reason",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: 24\n",
			pkg:    validVSIXPolicyPackage("^1.125.0", "1.125.0", "^22.20.1"),
			want:   "Reason",
		},
		{
			name:   "invalid node major",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: old\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("^1.125.0", "1.125.0", "^22.20.1"),
			want:   "positive integer",
		},
		{
			name:   "minimum is not caret semver",
			policy: "Minimum supported VS Code: 1.125.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("1.125.0", "1.125.0", "^22.20.1"),
			want:   "caret semver",
		},
		{
			name:   "minimum has invalid minor",
			policy: "Minimum supported VS Code: ^1.x.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("^1.x.0", "1.125.0", "^22.20.1"),
			want:   "invalid minor",
		},
		{
			name:   "minimum has invalid patch",
			policy: "Minimum supported VS Code: ^1.125.x\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("^1.125.x", "1.125.0", "^22.20.1"),
			want:   "invalid patch",
		},
		{
			name:  "missing manifest",
			noPkg: true,
			policy: "Minimum supported VS Code: ^1.125.0\n" +
				"VS Code runtime Node major: 24\nReason: fixture\n",
			want: "package.json",
		},
		{
			name:    "malformed manifest json",
			badJSON: true,
			policy: "Minimum supported VS Code: ^1.125.0\n" +
				"VS Code runtime Node major: 24\nReason: fixture\n",
			want: "parse",
		},
		{
			name:   "missing vscode types",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    `{"engines":{"vscode":"^1.125.0"},"devDependencies":{"@types/node":"^22.20.1"}}`,
			want:   "missing @types/vscode",
		},
		{
			name:   "missing node types",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    `{"engines":{"vscode":"^1.125.0"},"devDependencies":{"@types/vscode":"1.125.0"}}`,
			want:   "missing @types/node",
		},
		{
			name:   "vscode type version has invalid major",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("^1.125.0", "x.125.0", "^22.20.1"),
			want:   "invalid major",
		},
		{
			name:   "node type version has too few components",
			policy: "Minimum supported VS Code: ^1.125.0\nVS Code runtime Node major: 24\nReason: fixture\n",
			pkg:    validVSIXPolicyPackage("^1.125.0", "1.125.0", "^22"),
			want:   "major.minor.patch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if !tc.noVSIX {
				if err := os.MkdirAll(filepath.Join(root, "vsix"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.policy != "" {
				if err := os.WriteFile(filepath.Join(root, "vsix", "SUPPORTED_VSCODE.md"), []byte(tc.policy), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.noPkg {
				body := tc.pkg
				if tc.badJSON {
					body = "{bad json\n"
				}
				if err := os.WriteFile(filepath.Join(root, "vsix", "package.json"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := validateVSIXSupportPolicy(root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateVSIXSupportPolicy err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func writeVSIXPolicyFixture(t *testing.T, root, policyEngine, nodeMajor, manifestFloor, typesVSCode, typesNode string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "vsix"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := "# Keel Test Bridge Supported VS Code\n\n" +
		"DHF-REQ: keel/requirement-119\n\n" +
		"Minimum supported VS Code: " + policyEngine + "\n"
	if nodeMajor != "" {
		policy += "VS Code runtime Node major: " + nodeMajor + "\n" +
			"VS Code runtime Node major source: VS Code " + strings.TrimPrefix(policyEngine, "^") +
			" ships Electron 42.3.0 with Node.js " + nodeMajor + ".15.0 — https://github.com/ewanharris/vscode-versions\n"
	}
	policy += "\nReason: owner decision on 2026-08-14 raised the floor so the VSIX toolchain can track current releases.\n"
	policy += "\nDependency hold notes:\n\n" +
		"- `@types/vscode` is held at `" + strings.TrimPrefix(typesVSCode, "^") + "` (current: `1.125.0`).\n" +
		"  Reason: it must not describe APIs above the declared VS Code engine floor.\n" +
		"  Release condition: `keel/change_request-180` raises it to the declared floor.\n" +
		"- `@types/node` is held at `" + strings.TrimPrefix(typesNode, "^") + "` (current: `26.2.0`).\n" +
		"  Reason: it must not describe a Node runtime above the VS Code release named by the declared floor.\n" +
		"  Release condition: `keel/change_request-180` completes the coupled VSIX toolchain update.\n" +
		"- `typescript` is held at `5.9.3` (current: `7.0.2`).\n" +
		"  Reason: TypeScript 7 cannot compile this workspace against the old Node type line.\n" +
		"  Release condition: `keel/change_request-180` moves the type packages and TypeScript together.\n"
	if err := os.WriteFile(filepath.Join(root, "vsix", "SUPPORTED_VSCODE.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vsix", "package.json"), []byte(validVSIXPolicyPackage(manifestFloor, typesVSCode, typesNode)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeVSIXPolicyFixtureWithCitation writes an otherwise-valid policy fixture whose
// runtime-Node-major citation line is replaced by citation, or omitted when citation
// is empty.
func writeVSIXPolicyFixtureWithCitation(t *testing.T, root, nodeMajor, citation string) {
	t.Helper()
	writeVSIXPolicyFixture(t, root, "^1.125.0", nodeMajor, "^1.125.0", "1.125.0", "^"+nodeMajor+".15.0")
	path := filepath.Join(root, "vsix", "SUPPORTED_VSCODE.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "VS Code runtime Node major source:") {
			if citation == "" {
				continue
			}
			line = "VS Code runtime Node major source: " + citation
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validVSIXPolicyPackage(manifestFloor, typesVSCode, typesNode string) string {
	return `{
  "engines": { "vscode": "` + manifestFloor + `" },
  "devDependencies": {
    "@types/node": "` + typesNode + `",
    "@types/vscode": "` + typesVSCode + `",
    "typescript": "^5.9.3"
  }
}`
}
