---
name: staff
description: "Lists the cast of keel persona-equipped subagents — Winston, Cassandra, Amelia, Diogenes, Atticus, Vera, Sera, Verity, John, Sally — as a GFM table. Use when the user says: '/staff', 'who's on the team', 'show the cast', 'list agents', 'list personas'"
x-openbrain-source: staff/v1
x-openbrain-content-source-hash: sha256:70afaf8b633dcac9d6b623dcda497e0a86ebf44e2d55792ae110014323a5b3b5
x-openbrain-content-hash: sha256:be9e17558657a6f9a570bfb2e7cd551f185292467a1a49388f1ac9f23e1f88cb
---

# Staff — list the project's personas

Render every agent under `.claude/agents/` as a GFM markdown table. Source of truth is the agent files themselves — do not hardcode the cast, since new personas may be added over time.

## How to run

From the repo root (or any subdirectory inside it):

```bash
python3 .claude/skills/staff/scripts/list_staff.py
```

The script walks up from cwd to find `.claude/agents/`, parses each agent's frontmatter (`name`, `description`) and persona heading, and prints a GFM table to stdout.

## Output

Paste the script's output **directly into the response as a markdown table** — *not* inside a fenced code block. Per the user-level convention in `~/.claude/CLAUDE.md`, GFM tables are reflowed by the renderer, while fenced blocks are not, and remote sessions often render through narrow panes.

The table has these columns:

| Column | Source |
|---|---|
| Icon | The emoji from the `You are <icon> **<Name>**` line |
| Name | The persona name (Winston, Cassandra, Amelia, Diogenes, Atticus, Vera, Sera, Verity, John, Sally) |
| Role | The frontmatter `name` field (architect, coder, reviewer, tester) |
| Tagline | The `## Persona — <icon> <Name>, the <tagline>` heading |
| Description | First sentence of the frontmatter `description` |

Rows are sorted by role (alphabetical) so the order is stable across runs.

## When an agent has no persona overlay

Some agents may be added without the persona pattern (no `You are <icon> **<Name>**` line and no `## Persona — …` heading). The script still emits a row for them, falling back to em-dashes (`—`) for icon and tagline. This keeps the cast view honest about who is and isn't persona-equipped, instead of silently hiding bare agents.

## Don't

- Don't write the table by hand from memory — always run the script. The cast can change without this skill being updated.
- Don't wrap the output in a fenced code block. The user reads through panes that wrap fixed-width text.
- Don't add columns the script doesn't produce. If the user wants more (tools, model, etc.), update the script — keep prose and script aligned.
