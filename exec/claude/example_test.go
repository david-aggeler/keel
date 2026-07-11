package claude_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/david-aggeler/keel/exec/claude"
)

// ExampleRun drives the adapter against a hermetic stub standing in for the real
// `claude` binary — the same technique the package's tests use, so the example
// needs no claude install and never calls a model. A real consumer leaves Bin
// empty to resolve "claude" on PATH.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleRun() {
	stub := writeClaudeStub(`{"type":"result","is_error":false,"result":"PONG","num_turns":1,"total_cost_usd":0.01,"usage":{"input_tokens":12,"output_tokens":3}}`)
	defer func() { _ = os.Remove(stub) }()

	res, err := claude.Run(context.Background(), claude.Request{
		Prompt: "reply with PONG",
		Bin:    stub,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Text, res.NumTurns, res.Usage.TotalInput())
	// Output: PONG 1 12
}

// writeClaudeStub writes an executable /bin/sh stub that emits stdout verbatim,
// standing in for the real claude binary. Returns its path.
func writeClaudeStub(stdout string) string {
	f, err := os.CreateTemp("", "keel-claude-stub-*.sh")
	if err != nil {
		panic(err)
	}
	if _, err := f.WriteString("#!/bin/sh\ncat <<'STUBEOF'\n" + stdout + "\nSTUBEOF\n"); err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		panic(err)
	}
	return f.Name()
}
