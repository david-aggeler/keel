# keel

Shared Go foundation for process execution and logging, consumed by
[openbrain] and [vela]. One public module, one tag, one version:

```
go get github.com/david-aggeler/keel
```

## Packages

| Package | Import path | What it is |
|---|---|---|
| `log` | `github.com/david-aggeler/keel/log` | Structured logging: JSON + human-readable console handlers sharing one redaction path and severity vocabulary, build identity, metrics, recent-log ring, operational errors. |
| `exec` | `github.com/david-aggeler/keel/exec` | `ProcessStart` — the single seam for launching subprocesses with uniform lifecycle observability: START (full untruncated command line + cwd), DURING (streamed output), END (exit code + duration), sensitive-arg redaction. |
| `exec/claude` | `github.com/david-aggeler/keel/exec/claude` | Adapter for headless `claude -p` invocations (streaming output). |
| `exec/codex` | `github.com/david-aggeler/keel/exec/codex` | Adapter for headless `codex exec --json` invocations: prompt in, streaming JSONL events out, determinate result/error contract, stub-tested. |

Layering: `log` ← `exec` ← {`exec/claude`, `exec/codex`}. No external
dependencies, no internal replace directives.

## Status

Extracted 2026-07-07 from openbrain's settled `pkg/` modules
(`logging`, `procexec`, `claudecli`, `codexcli`) as a content-faithful
move. Until keel's own CI and release loop are trusted, keel is
validated through openbrain's gates via a transitional `go.work`
bridge ("build from openbrain" mode); v0.1.0 is the first tagged
release.

## Development

Dev-process records (requirements, change requests, epics) live in the
`keel` product of the gold OpenBrain instance — not in this repo.

## License

Apache-2.0 — see [LICENSE](LICENSE).

[openbrain]: https://github.com/david-aggeler/openbrain
[vela]: https://github.com/david-aggeler/vela
