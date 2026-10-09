# Remaining retention count arms (#7784)

`generationRetentionRowCountsQuery` counts the rows each prune deletes so
`BatchRowLimit` and the retention events stay exact. After #7396 and #7700,
five cascade populations stayed uncounted: `package_manifest_consumption_keys`
and `package_registry_identity_keys` (`fact_id REFERENCES fact_records ON
DELETE CASCADE`), `relationship_reference_candidate_keys` (same cascade),
`content_file_secret_lines` (`(repo_id, relative_path) REFERENCES
content_files ON DELETE CASCADE`), and
`shared_projection_unroutable_intents` (reaped explicitly per generation).
Every prune under-counted by these populations. This note records the EXPLAIN
measurement behind the five arms, taken before landing per the #7396 precedent.

Base: `01ceb1dd05` (2026-10-08).

## Shim (local only, deleted after measuring)

Local `postgres:18` container; each probe builds an isolated migrated schema
from the real bootstrap definitions. Seeded population per arm, with an
unrelated live generation alongside:

- 4,000 doomed facts x 5 keys rows (20,000 counted) per package-keys table,
- 4,000 relationship keys rows (4,000 counted),
- 500 doomed files x 10 secret lines (5,000 counted),
- 200 unroutable intents (200 counted).

The measured statements are the verbatim leg texts with `$1 = {gen-old}`,
under `EXPLAIN (ANALYZE, BUFFERS)`.

## Plan

All five counts come back exact. The two package-keys legs count through a
correlated scalar subquery, not a fourth `LEFT JOIN`: as a join the planner
reorders the leg under a generic plan into a scope-wide fact bitmap plus a
generation join filter, whose cost grows with the scope instead of the batch
(the key-index guard fired on exactly that shape). As a subquery the
analyzed-generic plan over the guard corpus (8 scopes x 5 superseded
generations, 3 candidates) keeps
`Bitmap Index Scan on fact_records_scope_generation_idx` with
`Index Cond: ((scope_id = ...) AND (generation_id = ...))` on both legs;
whole 34-leg query 50.8 ms. The secret leg probes the secret `PRIMARY KEY`
prefix on `(repo_id, relative_path)` through `doomed_files`, 14.3 ms for
5,000 rows. The relationship and unroutable legs probe their
`(scope_id, generation_id)` prefix indexes directly (index path confirmed
with seqscan disabled at fixture scale; same shape as the fifteen landed
scope-joined arms).

## No-Regression Evidence

No-Regression Evidence (#7784): the arms are additive (five more `UNION ALL`
legs) and change no existing leg. Backend `postgres:18`; terminal row counts
exact (`TestGenerationRetentionPrunesMigratedSchemaLive` asserts every doomed
row counted and deleted and every retained row excluded and surviving). The
grouped differential (`TestGenerationRetentionProbeMatchesGroupedPassLive`)
stays equal on the 13 oracle tables with the probe pinned at 34 tables, and
the plan guard (`TestGenerationRetentionProbePlansStayOnKeyIndexesLive`) stays
green in all four states (cold-custom, cold-generic, analyzed-generic,
seeded-red).

## Observability Evidence

Observability Evidence (#7784): no new signals. The new tables flow through
the existing retention-event `row_counts` map and the `RowsPruned` per-table
map, both asserted exact by the migrated-schema test above; an operator sees
the five tables in the same prune event as the other 29.
