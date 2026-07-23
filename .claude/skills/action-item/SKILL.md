---
name: action-item
description: "Near-zero-friction capture of durable follow-ups / things-to-remember in gold for the keel product. Use when the user says: '/action-item', 'action-item'"
allowed-tools: mcp__gold__create_action_item, mcp__gold__update_action_item, mcp__gold__list_action_item, mcp__gold__get_action_item, mcp__gold__search_action_item, mcp__gold__get_template_for
targets_templates:
  - action_item-template
x-openbrain-source: action-item/v3
x-openbrain-content-source-hash: sha256:1482eec7368ec02fffa6cffdd271cb7e9049759f211359819a87bb1e9e76e245
x-openbrain-content-hash: sha256:91dcf07112c21962fc742fa774c5af78474ee8c8ac7714ade61279a4a4a1a0cd
---

# Action Item Skill

Capture durable follow-ups — "remember to do X" — without dragging them into the
defect tracker. An `action_item` is the **capture layer**: cheap, durable,
low-ceremony. It is NOT a defect (`issue`), NOT a step inside a unit (`task`,
which is volatile and parent-mandatory), and NOT session context
(`session_handoff`, which evaporates). The unique ID is assigned by the server
on create.

This skill is deliberately minimal. The verb you reach for constantly is
**capture**; everything else is occasional.

## MCP

Tools and target template are declared in the frontmatter (`allowed-tools`,
`targets_templates`); invoke a tool as `mcp__gold__<tool>`. Before
authoring, fetch the template with `get_template_for dto_type=action_item` — it
is authoritative for fields and enums. ID minting and field updates are
server-side.

## Verbs

| Verb | Intent | Action |
|---|---|---|
| **capture** (default) | Jot a follow-up | `create_action_item product=keel title="<one line>"`. Optionally set `details`, `source` (`review`/`merge_sweep`/`session_observation`/`user_request`/`idea`/`other`), `parent` (any record ref, or omit to float free), `owner`, `due`, `deferred_until`/`deferred_pending`. This is the fast path. |
| **list** | See what's open | `list_action_item product=keel` (`include_deferred=true` to include deferred). No server-side status filter — filter client-side for `status=open`, by `parent`, or by `due`/overdue. Render as a markdown table. |
| **done** | Mark completed | `update_action_item product=keel id=<id> fields={status:"done", close_reason:"done"}`. |
| **drop** | Abandon it | `update_action_item product=keel id=<id> fields={status:"dropped", close_reason:"<wontdo|obsolete>"}`. |

When in doubt between capture and update: no existing action_item ID in the
message → capture.

## Updating without dropping fields

`update_action_item` has two write modes. Always use the **`fields`** sparse
overlay for a partial change (done, drop, defer):

- **`fields={...}`** — supply only the keys you are changing; every other field
  (`title`, `details`, `source`, `parent`, …) is preserved. This is the correct
  way to close or defer.
- **Top-level field args** merge by default, but `replace=true` REPLACES the
  whole payload, dropping anything not resupplied. Never use `replace=true` for
  a status change.

## Capture vs. commit — when to promote

An action_item is the capture layer. When you decide to commit, **promote** it
into the right commitment record and link back so the thread survives:

- It's actually a defect → create an `issue`, then
  `update_action_item id=<id> fields={status:"done", close_reason:"promoted", promoted_to:"<product>/issue-<n>"}`.
- It's a decision to change something → `/change-request create`, then promote the
  same way (`promoted_to:"<product>/change_request-<n>"`).
- It's execution steps under a unit → add `task` records under that unit.

There is no dedicated `promote` verb in this version — you create the target
record by hand (or just ask the agent to "create an issue/change request from
this action item"), then stamp `promoted_to` + `close_reason=promoted` on the
source so the link is preserved.
