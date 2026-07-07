# Step 01 — Dev Defaults Bootstrap

**Goal:** Ensure the product has a `dev_defaults` record before the interview reads from it.

## Actions

1. Call `list_dev_defaults product=<product>` to discover whether a singleton already exists for this product. (`get_dev_defaults` is a by-id read and requires both `product` and `id`, so it cannot be used for existence discovery.)
2. **If the call itself fails (MCP error — call did not succeed):** halt. This is a read failure — do not fall through to template bootstrap and do not proceed deviation-blind. Report the error to the operator and stop the interview. A read failure is not the same as a clean miss: bootstrapping on a failed call would seed a record the operator never saw and run the interview against unknown defaults.
3. **If the call succeeds and returns exactly one record (found):** store it; proceed to step 02. Do not overwrite — the operator owns edits after first creation.
4. **If the call succeeds and returns more than one record:** halt. The singleton invariant is violated — report the count to the operator and stop. Do not pick a record arbitrarily; the operator must resolve the duplicate before the interview can run.
5. **If the call succeeds and returns zero records (clean miss):**
   a. Call `get_template_for dto_type=dev_defaults` to retrieve the starter catalog.
   b. Fill `title`, `summary`, and `details` from the template verbatim (T1–T11 + the three `merge_gate.*` example rows).
   c. Call `create_dev_defaults` with those values.
   d. Inform the operator: "Created a dev defaults record from the template. Edit the `merge_gate.*` rows to match your stack's commands before first `close`."
   e. Store the newly created record; proceed to step 02.
