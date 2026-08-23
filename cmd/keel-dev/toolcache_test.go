package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goInstallStub writes a stand-in `go` onto dir that services
// `go install <pkg>@<version>` by writing an executable into $GOBIN which
// reports that version. It makes the install path hermetic: no network, no real
// toolchain, but the same argv and the same GOBIN contract the resolver relies
// on.
func goInstallStub(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
# The resolver scrubs PATH down to the stub dir, so restore the coreutils this
# stub itself needs.
PATH="/usr/bin:/bin:$PATH"
if [ "$1" != "install" ]; then
  echo "stub go: unexpected invocation: $*" >&2
  exit 2
fi
spec="$2"
pkg="${spec%@*}"
version="${spec##*@}"
name="$(basename "$pkg")"
if [ -z "$GOBIN" ]; then
  echo "stub go: GOBIN unset" >&2
  exit 3
fi
mkdir -p "$GOBIN"
printf '#!/bin/sh\necho "%s has version %s"\n' "$name" "$version" >"$GOBIN/$name"
chmod +x "$GOBIN/$name"
`
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// failingGoStub writes a `go` that always fails, standing in for an install that
// cannot complete (no network, bad module path, disk full).
func failingGoStub(t *testing.T, dir string) {
	t.Helper()
	script := "#!/bin/sh\necho 'stub go: install refused' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// noisyFailingGoStub writes a `go` that fails after emitting on stdout, so the
// install-failure message can be checked for the child's *stdout* — the stream
// this call site tees into keel/log and therefore only captures because it sets
// Request.CaptureWithTee (keel/requirement-150).
func noisyFailingGoStub(t *testing.T, dir string) {
	t.Helper()
	script := "#!/bin/sh\necho 'stub go: module lookup disabled'\necho 'stub go: install refused' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestToolInstallFailureQuotesChildStdout: the install-failure message quotes
// what the child said on stdout as well as stderr. The stdout tee that feeds the
// line-wise keel/log records would otherwise suppress the Result capture, so this
// pins the CaptureWithTee opt-in at the call site — without it the operator loses
// the half of the diagnosis that arrives on stdout.
//
// DHF-TEST: keel/requirement-150
func TestToolInstallFailureQuotesChildStdout(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	bin := scrubPATH(t)
	noisyFailingGoStub(t, bin)

	_, err := resolveOne(t, goToolPin("faketool", "v2.0.0", "v2.0.0"))
	if err == nil {
		t.Fatal("resolveOne returned nil error for a failing install")
	}
	for _, want := range []string{"stub go: module lookup disabled", "stub go: install refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("install-failure error must quote the child output %q, got %v", want, err)
		}
	}
}

func goToolPin(name, want, version string) toolPin {
	return toolPin{
		name:        name,
		versionArgs: []string{"--version"},
		want:        want,
		install:     toolInstall{method: toolInstallGo, pkg: "example.com/cmd/" + name, version: version},
	}
}

func pathToolPin(name, want string) toolPin {
	return toolPin{
		name:        name,
		versionArgs: []string{"--version"},
		want:        want,
		install:     toolInstall{method: toolInstallPath},
	}
}

// resolveOne resolves a single pin through a fresh resolver.
func resolveOne(t *testing.T, pin toolPin) (string, error) {
	t.Helper()
	r := newToolResolver(map[string]toolPin{pin.name: pin})
	return r.resolve(context.Background(), discardLogger(), pin.name)
}

// TestToolCacheResolvesEachPinnedVersionToItsOwnEntry is the keel/ac-465 proof:
// two branches pinning different versions of the SAME tool resolve to two
// distinct cache entries, both verify green, and the second run of the first pin
// needs no reinstallation — the install stub is removed for that run, so a cache
// miss would fail it.
//
// DHF-TEST: keel/requirement-12 (keel/ac-465)
func TestToolCacheResolvesEachPinnedVersionToItsOwnEntry(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(toolCacheEnv, cache)
	bin := scrubPATH(t)
	goInstallStub(t, bin)

	pinA := goToolPin("faketool", "v1.0.0", "v1.0.0")
	pinB := goToolPin("faketool", "v2.0.0", "v2.0.0")

	pathA, err := resolveOne(t, pinA)
	if err != nil {
		t.Fatalf("resolving pin A: %v", err)
	}
	pathB, err := resolveOne(t, pinB)
	if err != nil {
		t.Fatalf("resolving pin B: %v", err)
	}

	if pathA == pathB {
		t.Fatalf("two pinned versions resolved to the same binary: %s", pathA)
	}
	wantA := filepath.Join(cache, "faketool", "v1.0.0", "faketool")
	wantB := filepath.Join(cache, "faketool", "v2.0.0", "faketool")
	if pathA != wantA || pathB != wantB {
		t.Fatalf("cache paths = %s, %s; want %s, %s", pathA, pathB, wantA, wantB)
	}
	for _, p := range []string{pathA, pathB} {
		if info, err := os.Stat(p); err != nil || info.Mode()&0o111 == 0 {
			t.Fatalf("cache entry %s is not an executable file (err=%v)", p, err)
		}
	}

	// No reinstallation between runs: with the install stub gone, pin A must
	// still resolve green from its cache entry.
	if err := os.Remove(filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	again, err := resolveOne(t, pinA)
	if err != nil {
		t.Fatalf("cached pin must resolve without reinstalling, got %v", err)
	}
	if again != wantA {
		t.Fatalf("second resolve = %s, want cached %s", again, wantA)
	}
}

// TestToolCacheWrongVersionEntryFailsLoud: a cache entry that reports the wrong
// version is a hard failure naming the tool and the pinned version — the cache
// changes which binary is resolved, never whether the check runs (keel/ac-42).
//
// DHF-TEST: keel/requirement-12 (keel/ac-42)
func TestToolCacheWrongVersionEntryFailsLoud(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(toolCacheEnv, cache)
	bin := scrubPATH(t)
	goInstallStub(t, bin)

	// Seed the keyed entry with a binary that lies about its version.
	entry := filepath.Join(cache, "faketool", "v2.0.0")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecStub(t, entry, "faketool", "faketool has version v1.0.0", 0)

	_, err := resolveOne(t, goToolPin("faketool", "v2.0.0", "v2.0.0"))
	if err == nil {
		t.Fatal("a wrong-version cache entry must fail the gate, got nil")
	}
	if !strings.Contains(err.Error(), "faketool") || !strings.Contains(err.Error(), "v2.0.0") {
		t.Fatalf("mismatch error must name the tool and the pinned version: %v", err)
	}
}

// TestToolCacheCorruptEntryIsReinstalled: a cache entry that is not a usable
// executable is treated as absent and reinstalled; when the install itself
// cannot run, the gate reds naming the tool rather than passing.
//
// DHF-TEST: keel/requirement-12 (keel/ac-42)
func TestToolCacheCorruptEntryIsReinstalled(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(toolCacheEnv, cache)
	bin := scrubPATH(t)

	entry := filepath.Join(cache, "faketool", "v2.0.0")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	// A truncated, non-executable leftover from an interrupted install.
	if err := os.WriteFile(filepath.Join(entry, "faketool"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	failingGoStub(t, bin)
	if _, err := resolveOne(t, goToolPin("faketool", "v2.0.0", "v2.0.0")); err == nil {
		t.Fatal("a corrupt cache entry with a failing install must fail the gate, got nil")
	} else if !strings.Contains(err.Error(), "faketool") {
		t.Fatalf("install-failure error must name the tool: %v", err)
	}

	// With a working install the same corrupt entry is replaced and verifies.
	goInstallStub(t, bin)
	got, err := resolveOne(t, goToolPin("faketool", "v2.0.0", "v2.0.0"))
	if err != nil {
		t.Fatalf("corrupt entry should be reinstalled, got %v", err)
	}
	if got != filepath.Join(entry, "faketool") {
		t.Fatalf("resolved = %s, want the keyed cache entry", got)
	}
}

// TestToolInstallFailureDoesNotFallBackToPATH: when the on-demand install fails,
// resolution is a hard error naming the tool, the version, and the install
// command — never the PATH binary, even when a correctly-versioned one is
// sitting there. A PATH fallback is what keel/issue-142 is about.
//
// DHF-TEST: keel/requirement-12 (keel/ac-465, keel/ac-42)
func TestToolInstallFailureDoesNotFallBackToPATH(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	bin := scrubPATH(t)
	failingGoStub(t, bin)
	// A perfectly good, correctly-pinned binary on PATH: it must not be used.
	writeExecStub(t, bin, "faketool", "faketool has version v2.0.0", 0)

	_, err := resolveOne(t, goToolPin("faketool", "v2.0.0", "v2.0.0"))
	if err == nil {
		t.Fatal("a failed install must not silently fall back to the PATH binary")
	}
	for _, want := range []string{"faketool", "v2.0.0", "go install", "example.com/cmd/faketool"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("install-failure error must contain %q, got %v", want, err)
		}
	}
}

// TestToolPinPathMethodResolvesFromPATH: pins whose install method is declared
// as `path` (tools keel does not install itself, e.g. cspell/shellcheck) keep
// resolving from PATH and keep their version verification.
//
// DHF-TEST: keel/requirement-12 (keel/ac-42)
func TestToolPinPathMethodResolvesFromPATH(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	bin := scrubPATH(t)
	writeExecStub(t, bin, "pathtool", "pathtool 3.2.1", 0)

	got, err := resolveOne(t, pathToolPin("pathtool", "3.2.1"))
	if err != nil {
		t.Fatalf("path-method pin should resolve from PATH, got %v", err)
	}
	if got != filepath.Join(bin, "pathtool") {
		t.Fatalf("resolved = %s, want the PATH binary", got)
	}

	if _, err := resolveOne(t, pathToolPin("pathtool", "9.9.9")); err == nil {
		t.Fatal("path-method pin at the wrong version must fail loud")
	}
	if _, err := resolveOne(t, pathToolPin("absenttool", "1.0.0")); err == nil {
		t.Fatal("path-method pin that is absent must fail loud")
	}
}

// TestToolPinPreflightReportsEveryMismatch: the preflight enumerates every
// drifted or missing pin in ONE pass. keel/issue-142 measured the cost of
// stopping at the first: three drifted pins took three full gate runs to find.
//
// DHF-TEST: keel/requirement-12 (keel/ac-42)
func TestToolPinPreflightReportsEveryMismatch(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	bin := scrubPATH(t)
	writeExecStub(t, bin, "drifted-one", "drifted-one 1.0.0", 0)
	writeExecStub(t, bin, "drifted-two", "drifted-two 1.0.0", 0)
	writeExecStub(t, bin, "goodtool", "goodtool 1.0.0", 0)
	// "absenttool" is deliberately not written.

	pins := map[string]toolPin{
		"drifted-one": pathToolPin("drifted-one", "2.0.0"),
		"drifted-two": pathToolPin("drifted-two", "3.0.0"),
		"goodtool":    pathToolPin("goodtool", "1.0.0"),
		"absenttool":  pathToolPin("absenttool", "1.0.0"),
	}
	err := newToolResolver(pins).verifyPins(context.Background(), discardLogger(),
		[]string{"drifted-one", "drifted-two", "goodtool", "absenttool"})
	if err == nil {
		t.Fatal("preflight must fail when pins are drifted or missing")
	}
	for _, want := range []string{"drifted-one", "drifted-two", "absenttool"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("preflight must report %q in the same pass: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "goodtool") {
		t.Fatalf("preflight must not report the satisfied pin: %v", err)
	}
}

// TestToolPinPreflightPassesWhenEveryPinIsSatisfied is the control for the
// report above: a fully satisfied pin set is silent and green.
//
// DHF-TEST: keel/requirement-12 (keel/ac-42)
func TestToolPinPreflightPassesWhenEveryPinIsSatisfied(t *testing.T) {
	t.Setenv(toolCacheEnv, t.TempDir())
	bin := scrubPATH(t)
	writeExecStub(t, bin, "goodtool", "goodtool 1.0.0", 0)
	writeExecStub(t, bin, "presencetool", "", 0)

	pins := map[string]toolPin{
		"goodtool":     pathToolPin("goodtool", "1.0.0"),
		"presencetool": {name: "presencetool", install: toolInstall{method: toolInstallPath}},
	}
	if err := newToolResolver(pins).verifyPins(context.Background(), discardLogger(), []string{"goodtool", "presencetool"}); err != nil {
		t.Fatalf("satisfied pin set must pass the preflight, got %v", err)
	}
}

// TestRunStepRunsTheCacheResolvedBinary proves the resolution change reaches the
// step execution itself: the subprocess is the cache entry, not the same-named
// binary on PATH. Without this the pin would be verified against one binary and
// the gate run against another.
//
// DHF-TEST: keel/requirement-12 (keel/ac-465)
func TestRunStepRunsTheCacheResolvedBinary(t *testing.T) {
	cache := t.TempDir()
	t.Setenv(toolCacheEnv, cache)
	bin := scrubPATH(t)
	// The PATH binary reports the right version but exits non-zero: if the step
	// ran it, the step would fail.
	writeExecStub(t, bin, "faketool", "faketool has version v2.0.0", 1)

	entry := filepath.Join(cache, "faketool", "v2.0.0")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecStub(t, entry, "faketool", "faketool has version v2.0.0", 0)

	pin := goToolPin("faketool", "v2.0.0", "v2.0.0")
	err := runStep(context.Background(), discardLogger(), ".", step{
		name: "faketool", tool: "faketool", program: "faketool",
		resolver: newToolResolver(map[string]toolPin{"faketool": pin}),
	})
	if err != nil {
		t.Fatalf("step must run the cache-resolved binary, got %v", err)
	}
}

// TestToolCacheRootHonorsOverrideAndDefault: the cache root is overridable for
// CI images and read-only homes, and otherwise sits under the user cache dir.
func TestToolCacheRootHonorsOverrideAndDefault(t *testing.T) {
	override := t.TempDir()
	t.Setenv(toolCacheEnv, override)
	got, err := toolCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Fatalf("cache root = %s, want the override %s", got, override)
	}

	t.Setenv(toolCacheEnv, "relative/path")
	if _, err := toolCacheRoot(); err == nil {
		t.Fatalf("a relative %s must be rejected", toolCacheEnv)
	}

	t.Setenv(toolCacheEnv, "")
	def, err := toolCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(def), "keel-dev/tools") {
		t.Fatalf("default cache root = %s, want a keel-dev/tools suffix", def)
	}
}

// TestCommittedConfigPinsDeclareAnInstallMethod: every committed pin says how it
// is materialized, so a new gate tool cannot land on host-global PATH resolution
// by omission.
//
// DHF-TEST: keel/requirement-12 (keel/ac-465)
func TestCommittedConfigPinsDeclareAnInstallMethod(t *testing.T) {
	root, err := findModuleRoot(".")
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}
	cfg, err := loadKeelDevConfig(root)
	if err != nil {
		t.Fatalf("loadKeelDevConfig: %v", err)
	}
	for name, pin := range cfg.toolPins() {
		switch pin.install.method {
		case toolInstallGo:
			if pin.install.pkg == "" || pin.install.version == "" {
				t.Errorf("pin %q declares method go without package/version: %#v", name, pin.install)
			}
		case toolInstallPath:
			// Explicitly host-global; documented in keel-dev.yaml.
		default:
			t.Errorf("pin %q has no install method", name)
		}
	}
	// The tool keel/issue-142 measured must be cache-resolved, not PATH-resolved.
	if got := cfg.toolPins()["golangci-lint"].install.method; got != toolInstallGo {
		t.Fatalf("golangci-lint install method = %q, want %q", got, toolInstallGo)
	}
}

// TestKeelDevConfigRejectsIncompleteInstallDeclaration: the install block is
// validated at the property, with the dotted path in the error.
func TestKeelDevConfigRejectsIncompleteInstallDeclaration(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{
			name: "unknown method",
			yaml: "tools:\n  pins:\n    - name: t\n      install:\n        method: curl\n",
			want: "install.method",
		},
		{
			name: "go without package",
			yaml: "tools:\n  pins:\n    - name: t\n      install:\n        method: go\n        version: v1.0.0\n",
			want: "install.package",
		},
		{
			name: "go without version",
			yaml: "tools:\n  pins:\n    - name: t\n      install:\n        method: go\n        package: example.com/cmd/t\n",
			want: "install.version",
		},
		{
			name: "path with package",
			yaml: "tools:\n  pins:\n    - name: t\n      install:\n        method: path\n        package: example.com/cmd/t\n",
			want: "install.package",
		},
		{
			name: "missing install",
			yaml: "tools:\n  pins:\n    - name: t\n",
			want: "install.method",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, keelDevConfigFile, tc.yaml)
			_, err := loadKeelDevConfig(dir)
			if err == nil {
				t.Fatalf("invalid install declaration should fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}
