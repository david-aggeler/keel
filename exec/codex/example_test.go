package codex_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/david-aggeler/keel/exec/codex"
)

// ExampleRun drives the adapter against a hermetic stub standing in for the real
// `codex` binary, so the example needs no codex install. OnEvent receives each
// decoded JSONL event as it streams; Event.Type is the semantic type regardless
// of codex's framing (the agent_message here arrives wrapped in an item.completed
// envelope). A real consumer leaves Bin empty to resolve "codex" on PATH.
//
// DHF-TEST: keel/user_need-1 (keel/ac-48)
func ExampleRun() {
	stub := writeCodexStub(
		`{"type":"thread.started","thread_id":"t-1"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
		`{"type":"result"}`,
	)
	defer func() { _ = os.Remove(stub) }()

	var types []string
	res, err := codex.Run(context.Background(), codex.Request{
		Prompt:  "inspect the repo",
		Bin:     stub,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnEvent: func(e codex.Event) { types = append(types, e.Type) },
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.ThreadID, strings.Join(types, ","))
	// Output: t-1 thread.started,agent_message,result
}

// writeCodexStub writes an executable /bin/sh stub that emits each line to
// stdout in order, standing in for the real codex binary. Returns its path.
func writeCodexStub(lines ...string) string {
	f, err := os.CreateTemp("", "keel-codex-stub-*.sh")
	if err != nil {
		panic(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, ln := range lines {
		b.WriteString("printf '%s\\n' '" + strings.ReplaceAll(ln, "'", `'\''`) + "'\n")
	}
	if _, err := f.WriteString(b.String()); err != nil {
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
