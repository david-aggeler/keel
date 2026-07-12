# docs/to-gold — records staged for gold

These documents are gold records authored BEFORE their DTO types exist in the
gold SoR. Each file applies the matching template from `docs/templates/` and
carries YAML frontmatter with the record fields (`dto_type`, `title`,
`summary`, `status`, `related`, root-only `chapters`). Once the DTO types are
registered, ingest each body as the record's `details`, set the fields from
the frontmatter, and delete the file — this directory must trend to empty
(keel rule: dev records never live as repo markdown permanently).

| File | dto_type | Notes |
|---|---|---|
| `architecture_description-root.md` | architecture_description | ROOT (one per product); `chapters[]` lists the test-bridge chapter |
| `architecture_description-test-bridge.md` | architecture_description | chapter: VS Code Test Bridge & lanes (concept chapter) |
| `interface_spec.md` | interface_spec | one per product; §4 points at the lanes spec |
| `threat_model.md` | threat_model | one per product; initial STRIDE pass deliberately marked pending |
| `attachments/test-lanes-spec.md` | (attachment) | normative lanes contract; canonical copy already attached to `keel/exploration-2`; attach to the interface_spec on ingestion |

Cross-references between these files use PLACEHOLDER refs
(`keel/architecture_description-1`, `-2`, `keel/interface_spec-1`) — fix them
to the real sequence ids assigned at ingestion time.

Provenance: authored 2026-07-12 from the keel/exploration-2 dialogue
(test-bridge tree, lanes, contract) and repo analysis; reviewed by the
adversarial-reviewer + architect agents, all findings applied. Deciding
records: `keel/exploration-2` (concluded) and `keel/prototype-1`. The design
is carried by `keel/requirement-46…55` (approved, ac-131…149) and implemented
by `keel/change_request-53/54/55` (+ `-56` for this staging merge), all
queued in `keel/iteration-10`. Related defects: `keel/issue-38/39`
(reviewed), `keel/issue-40` (new).
