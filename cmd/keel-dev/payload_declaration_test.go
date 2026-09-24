package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/david-aggeler/keel/cli"
	logging "github.com/david-aggeler/keel/log"
	"github.com/david-aggeler/keel/testbridge"
)

// payloadProbe is a two-verb tree whose handlers each write one payload line to
// both protocol carriers — keel-dev's run state and the test-bridge runtime.
func payloadProbe(written *[]string) (undeclared, declared *cli.CommandSpec) {
	handler := func(ctx context.Context, _ []string) error {
		if _, err := io.WriteString(stateFrom(ctx).protocol, "state-payload\n"); err != nil {
			return err
		}
		rt, _ := testbridge.RuntimeFrom(ctx)
		if _, err := io.WriteString(rt.Protocol, "runtime-payload\n"); err != nil {
			return err
		}
		*written = append(*written, "ok")
		return nil
	}
	return &cli.CommandSpec{Name: "quiet", Handler: handler},
		declarePayload(&cli.CommandSpec{Name: "loud", Subcommands: []*cli.CommandSpec{{Name: "leaf", Handler: handler}}})
}

// TestVerbWithoutPayloadDeclarationCannotReachStdout proves the inverted
// default: a verb that declares nothing has no stdout — its first payload write
// fails loudly and nothing reaches the process stream — while a declared verb,
// at any depth of its subtree, writes to stdout through both carriers.
//
// DHF-TEST: keel/requirement-164, keel/requirement-38
func TestVerbWithoutPayloadDeclarationCannotReachStdout(t *testing.T) {
	var written []string
	undeclared, declared := payloadProbe(&written)

	stdout, _ := captureProcessStreams(t, func() {
		ctx := withRunState(context.Background(), logging.Discard(), nil, t.TempDir())
		err := undeclared.Handler(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "declared no payload") {
			t.Errorf("undeclared verb write = %v, want the declared-no-payload refusal", err)
		}
	})
	if stdout != "" || len(written) != 0 {
		t.Fatalf("undeclared verb reached stdout: %q (writes %v)", stdout, written)
	}

	stdout, _ = captureProcessStreams(t, func() {
		ctx := withRunState(context.Background(), logging.Discard(), nil, t.TempDir())
		if err := declared.Subcommands[0].Handler(ctx, nil); err != nil {
			t.Errorf("declared verb write: %v", err)
		}
	})
	if stdout != "state-payload\nruntime-payload\n" {
		t.Fatalf("declared verb stdout = %q, want both payload lines", stdout)
	}
}

// TestPayloadDeclarationKeepsAnInjectedWriter proves declarePayload rebinds only
// the undeclared default: a caller that bound its own protocol writer keeps it.
//
// DHF-TEST: keel/requirement-164
func TestPayloadDeclarationKeepsAnInjectedWriter(t *testing.T) {
	var written []string
	_, declared := payloadProbe(&written)
	var buf bytes.Buffer
	ctx := withRunStateProtocol(context.Background(), logging.Discard(), nil, t.TempDir(), &buf)
	if err := declared.Subcommands[0].Handler(ctx, nil); err != nil {
		t.Fatalf("declared verb with injected writer: %v", err)
	}
	if buf.String() != "state-payload\nruntime-payload\n" {
		t.Fatalf("injected writer = %q, want both payload lines", buf.String())
	}
}
