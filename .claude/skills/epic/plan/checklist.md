<!-- markdownlint-disable MD033 MD036 MD034 MD040 MD026 MD032 MD012 MD024 MD028 MD031 -->
# Epic Plan — Unit Decomposition Coverage Checklist

This checklist validates that an epic has been correctly decomposed into change-request unit records.

## Record Coverage

- [ ] Every intended unit exists as a `change_request` record under the epic (`list_change_request filter={"parent":"<epic_ref>"}` returns all expected units)
- [ ] No intended unit is missing from the record query
- [ ] No extra unit records exist that were not part of the approved plan

## Unit Record Quality

For each unit record (inspect via `get_change_request`):

- [ ] `status` is `draft` — units are thin husks at this stage; detail-at-pickup via `/change-request create`
- [ ] `parent` ref resolves to the correct parent epic
- [ ] `title` is clear and action-oriented
- [ ] `summary` is one sentence scoping the unit and the FRs it covers

## Requirement Coverage

- [ ] All FRs assigned to this epic in the coverage map are traceable to at least one unit by title or summary
- [ ] No FR is left without a unit covering it

## Dependency Check

- [ ] Units are scoped so each is independently implementable without depending on future units within the same epic
