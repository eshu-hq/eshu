# #7282: changed-since refuses windows with a delta generation at either end

Changed-since diffed the raw `fact_records` of two generations and ignored
`scope_generations.is_delta`. A delta generation holds only the changed files'
facts plus tombstones. A window with a delta generation at either end therefore
reported untouched keys as removed or superseded (delta current) or as added
(delta baseline). On the QA environment, one delta generation holds 170 non-reducer keys
against 68,946 in the full generation before it. One delta generation is active
there, and 899 more were active once.

The two resolution statements, `resolveChangedSinceScopeQuery` and
`resolveChangedSinceGenerationQuery`, now also return `is_delta` from the same
`scope_generations` row as each generation id. When either end is a delta
generation, `ComputeChangedSinceDelta` returns `unavailable_reason =
baseline_not_comparable` and never runs `changedSinceDeltaQuery`.

## Evidence

No-Regression Evidence: the only change to the hot statements is one extra column read in each resolution statement, taken from a row the statement already fetches. The old statement text is from `origin/main` `5583d45a64` and the new text from branch head `53cb0f0309`. Both ran against QA's live PostgreSQL 18 data, read-only, at image `sha-1de13f4`, on the 12,402-file repository scope.

- **Method:** `EXPLAIN (ANALYZE, BUFFERS)` of `EXECUTE` on prepared old and new statements. There were 6 rounds each, interleaved, and the first-runner alternated between rounds.
- **Plans:** identical node for node.
  - Scope statement: an `Index Scan` on `ingestion_scopes_pkey`, a nested-loop `Index Scan` on `scope_generations_pkey`, and an `Index Only Scan` subplan on `scope_generations_scope_idx`.
  - Generation statement: a `Bitmap Index Scan` on `scope_generations_scope_generation_idx` feeding a top-N heapsort.
- **Buffers:** equal, apart from 2-3 extra buffers on the two cold first runs, which are also the source of the maxima below.
- **Execution time** (the spread is noise at this scale):

  | Statement | Old median (range) | New median (range) |
  | --- | --- | --- |
  | Scope | 0.067 ms (0.058-0.072) | 0.064 ms (0.059-0.888) |
  | Generation | 0.049 ms (0.045-0.055) | 0.055 ms (0.045-0.222) |

- **A window with a delta generation at either end** now skips `changedSinceDeltaQuery`, the multi-second statement (#7127), entirely.
- **Every other window** runs the same statements as before.

Observability Evidence: the refusal sets the span attribute `eshu.changed_since.unavailable_reason` to `baseline_not_comparable`. The response carries `unavailable=true`, `unavailable_reason`, and `since_is_delta` and/or `current_is_delta`. The truth envelope's freshness is `unavailable`, with a detail that names the delta side. An operator can find refusals by span attribute and tell the two remedies apart from the response: pick a full baseline, or wait for a full generation.

## Proof

- **RED before the fix**, from the live test through the real statement on PostgreSQL 18:
  - Full generation, then delta: files reported `{Updated:1 Retired:1 Superseded:3}`, with the untouched files sampled as superseded.
  - Delta, then full: files reported `{Added:3 Unchanged:1}`.
- **GREEN after the fix:** `TestChangedSinceRefusesDeltaGenerationWindowLive` passes 3/3. That includes a delta strictly between two full endpoints, which stays correctly diffed.
- **Hermetic test:** the refusal issues exactly 2 statements and never reads `fact_records`.
