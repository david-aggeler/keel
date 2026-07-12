# keel

Shared Go foundation for process execution and logging, consumed by
downstream Go services. One public module, one tag, one version:

```
go get github.com/david-aggeler/keel
```

## Purpose

For whatever reason, LLMs in 2026 are still struggling in some areas, and I'm
using these base libraries in my other projects. keel covers the
infrastructure concerns every one of those projects needs and that agents
keep getting subtly wrong: logging, CLI structure, and subprocess handling.
The code here is close to 100% AI generated.

### Logging

An opinionated implementation: one logger, several sinks, all sharing the
same redaction path and severity vocabulary.

- Console: plain text
- Console: sparse AI — minimal progress noise, but a precise log pointer on error
- Console: full JSON records
- File: plain text and JSONL
- OpenTelemetry: optional OTLP export target via `log/otel` — importing it is
  what opts you into the OpenTelemetry SDK dependency; the core stays
  dependency-free

Plain-text console:

```
21:00:53 INFO  process start event_type=process_start program=sh command_line=sh -c "…" working_dir=/projects/keel
21:00:53 ERROR  process output event_type=process_output stream=stderr data=child stderr line
21:00:53 INFO  process end event_type=process_end exit_code=0 elapsed_ms=0
```

Sparse-AI console — compact records, and errors carry an exact file+line
pointer into the run's `.jsonl` log:

```json
{"level":"INFO","event":"process_start","message":"process start","fields":{"program":"sh","service":"keel-demo"}}
{"level":"ERROR","event":"log","message":"keel-demo failed","fields":{"err":{"exit_code":4,"log_file":".logs/20260712T190059Z.jsonl","start_line":13,"hint":"inspect .logs/20260712T190059Z.jsonl from line 13"}}}
```

### CLI

CLIs are currently one of the strongest interfaces for LLMs to work with, so
a structured way to declare commands, flags, and generated help text is
essential. Consumers describe their CLI as a tree of command specs; dispatch,
flag validation, and rendered help all come from that single model.

Plain-text help (`keel-demo --help`):

```
keel-demo runs the log and exec showcase.

Usage:
  keel-demo [--mode human|ai|json]
  keel-demo help [command]

Global flags:
  --mode human|ai|json
      Console mode. (default human)

Commands:
  workflow  Parent command with nested help.
```

The same help in JSON mode (`keel-demo --mode json --help`) arrives as a
single machine-readable record:

```json
{"ts":"2026-07-12T21:01:06+02:00","level":"INFO","msg":"keel-demo help","service":"keel-demo","event_type":"help","command":"keel-demo","help":"keel-demo runs the log and exec showcase.\n\nUsage:\n  keel-demo [--mode human|ai|json]\n…","mode":"json"}
```

### Process Management

Ties closely with logging: every subprocess launch gets uniform START/END
lifecycle records, proper stdout/stderr handling, and log-line filtering
designed to stay token-efficient for LLM consumers.

## Requirements

Dev-process records (requirements, change requests, epics) live outside the repo.

## License

Apache-2.0 — see [LICENSE](LICENSE).

## Packages

| Package | Import path | What it is |
|---|---|---|
| `log` | `github.com/david-aggeler/keel/log` | Structured logging: JSON + human-readable console handlers sharing one redaction path and severity vocabulary, build identity, metrics, recent-log ring, operational errors. |
| `cli` | `github.com/david-aggeler/keel/cli` | Shared command tree, generated help, and usage-error contract for first-party developer CLIs: one `CommandSpec` model drives dispatch, flag validation, and rendered help. |
| `log/otel` | `github.com/david-aggeler/keel/log/otel` | Optional OpenTelemetry log exporter bridge (OTLP). Quarantined sibling: only importing it pulls in the OpenTelemetry SDK; core `log` stays dependency-free. |
| `exec` | `github.com/david-aggeler/keel/exec` | `ProcessStart` — the single seam for launching subprocesses with uniform lifecycle observability: START (full untruncated command line + cwd), DURING (streamed output), END (exit code + duration), sensitive-arg redaction. |
| `exec/claude` | `github.com/david-aggeler/keel/exec/claude` | Adapter for headless `claude -p` invocations (streaming output). |
| `exec/codex` | `github.com/david-aggeler/keel/exec/codex` | Adapter for headless `codex exec --json` invocations: prompt in, streaming JSONL events out, determinate result/error contract, stub-tested. |

Layering: `log` ← `exec` ← {`exec/claude`, `exec/codex`}; `cli` stands
alone. No external dependencies in the core compile graph — `log/otel` is
the one deliberate exception — and no internal replace directives.

## Keel Test Bridge (VSIX)

`vsix/` holds the Keel Test Bridge, a VS Code extension that surfaces keel's
devtool test protocol in the VS Code Testing UI. It activates only when a
workspace contains `.vscode/test-bridge.json` and is versioned in lockstep
with the Go module — one tag covers both. Each release attaches the built
`.vsix` as a GitHub release asset.

## Usage

Condensed from the runnable `example_test.go` files in each package —
see those for the full, tested versions.

### Logging

```go
import logging "github.com/david-aggeler/keel/log"

logger, err := logging.New(logging.Config{Service: "demo", Console: logging.ConsoleJSON})
if err != nil {
    // handle
}
logger.Info("service starting", "port", 8080)

// With returns a derived logger that stamps the given attributes on
// every subsequent record.
reqLog := logger.With("request_id", "r-42")
reqLog.Info("handling request")
```

### Process execution

```go
import procexec "github.com/david-aggeler/keel/exec"

proc, err := procexec.ProcessStart(context.Background(), procexec.Request{
    Program: "echo",
    Args:    []string{"hello from keel/exec"},
    Logger:  logger, // START/END lifecycle records land here
})
if err != nil {
    // handle
}
res, err := proc.Wait() // ProcessStart returns immediately; Wait blocks
fmt.Print(res.Stdout)
```

### CLI

```go
import "github.com/david-aggeler/keel/cli"

root := &cli.CommandSpec{
    Name: "keel-demo",
    Config: cli.Config{
        Program: "keel-demo",
        Usage:   "keel-demo <command> [args]",
        GlobalFlags: []cli.FlagSpec{
            {Name: "mode", Value: "human|ai|json", Default: "human", Short: "Console protocol."},
        },
    },
    Subcommands: []*cli.CommandSpec{{
        Name:  "echo",
        Use:   "echo <text>",
        Short: "Print text.",
        Handler: func(_ context.Context, args []string) error {
            fmt.Println(strings.Join(args, " "))
            return nil
        },
    }},
}

cfg, words, _ := cli.ParseGlobalConfig(os.Args[1:]) // shared global flags first
_ = cfg
err := root.Dispatch(context.Background(), words)   // walks the tree, runs the handler
```
