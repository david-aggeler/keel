# Step 03 — DHF Annotation

**Goal:** After each slice reaches green, annotate the implementing code and verifying tests with DHF traceability markers.

## Marker convention

Two marker types, placed as code comments in the implementing file and in the test file:

- **`DHF-REQ: <product>/requirement-<id>`** — placed on the implementing unit (function, type, handler, or module) that satisfies the requirement. Use the language-appropriate comment leader.
- **`DHF-TEST: <product>/requirement-<id>`** — placed on each test function that verifies the requirement.

Example (Go):

```go
// DHF-REQ: myproduct/requirement-42
func HandleFoo(w http.ResponseWriter, r *http.Request) { ... }

// DHF-TEST: myproduct/requirement-42
func TestHandleFoo_ReturnsBadRequestOnMissingBody(t *testing.T) { ... }
```

One line can carry multiple refs (comma-separated) when a single unit implements or verifies more than one requirement.

## Placement rule

- Annotate the smallest logical unit that satisfies the requirement — function or method, not the entire file.
- Annotate every test function that exercises the requirement, not just the primary test.

## Retest flow

The markers enable targeted retest: when a requirement or its acceptance criteria change, `rg "DHF-REQ: {product}/requirement-{id}"` finds the implementing code and `rg "DHF-TEST: {product}/requirement-{id}"` finds the tests that prove its ACs. The `review` verb checks coverage against the unit's resolved requirements and their ACs (kind-aware — see SKILL.md).

## After annotation

Commit the slice (implementing code + test + annotations) to the unit's worktree branch. Proceed to the next slice.
