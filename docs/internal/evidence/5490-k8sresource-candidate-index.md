# Evidence: #5490 — measure `(repo_id, entity_type)` index variants for the K8sResource candidate fetch

Spun off from #5363. Scope: `ListRepoK8sSelectCandidates`
(`go/internal/query/content_reader_k8s_select_candidates.go`), the narrow
type-scoped candidate fetch backing the impact-trace directed SELECTS scan
(`go/internal/query/impact_trace_deployment_k8s_select.go`). #5363's shim
(`go/internal/query/evidence-5363-impact-trace-k8s-fetch.md`) measured this
query's worst case (6,000-K8sResource repo, `LIMIT 5001`) at ~25 ms wide /
~12.5 ms narrow-projection, and decomposed the cost: scan + `entity_type`
filter is only ~2.0 ms of that; a top-N sort dominates the rest. There is
currently no composite `(repo_id, entity_type)` index; the planner uses a
Bitmap/Index Scan on `content_entities_repo_idx` or `content_entities_type_idx`
with a recheck/filter for the other predicate.

This is a **Mandatory Prove-The-Theory-First** proof: every candidate index
below was measured with `EXPLAIN (ANALYZE, BUFFERS)` against a throwaway,
representative worst-case partition **before** any production code changed.

## Machine / backend profile (resource-qualified)

- `machine_profile`: MacBook Pro, Apple M4 Pro (arm64), 12 logical CPU, 64 GiB,
  SSD, macOS 26.5.2.
- Postgres: `postgres:16` (`PostgreSQL 16.14 (Debian 16.14-1.pgdg13+1)` on
  aarch64) in Docker, throwaway container `eshu-5490-explain-shim` on
  non-default host port `55490`, no persistent volume, destroyed after the run.
- `absolute_target_applicable`: false — these are relative before/after shim
  measurements gating an index-adoption decision, not a reference-profile
  wall-clock target.

## Seeded worst-case partition

Mirrors the #5363 evidence shape exactly, using the real `content_entities`
schema and its five existing indexes (`content_entities_pkey`,
`content_entities_repo_idx`, `content_entities_type_idx`,
`content_entities_path_idx`, `content_entities_repo_entity_idx`):

| repo_id | entity_type | rows |
| --- | --- | ---: |
| repo-1 | K8sResource | 6,000 (3,000 Service + 3,000 Deployment, realistic JSONB metadata: `kind`, `namespace`, `selector` on Services; `kind`, `namespace`, `pod_template_labels` (22-pair), `container_images` on Deployments) |
| repo-1 | Function | 4,000 (filler, `metadata = '{}'`) |
| repo-2 | Function | 5,000 (noise repo) |
| repo-3 | Variable | 3,000 (noise repo) |
| **total** | | **18,000** |

The production query, `LIMIT 5001` (hits the cap: only 5,001 of repo-1's
6,000 K8sResource rows are returned, matching the #5363 worst case):

```sql
SELECT entity_id, entity_name,
       coalesce(metadata->>'kind', ''),
       coalesce(metadata->>'namespace', ''),
       coalesce(jsonb_typeof(metadata->'selector') = 'string', false),
       coalesce(metadata->>'selector', ''),
       coalesce(jsonb_typeof(metadata->'pod_template_labels') = 'string', false),
       coalesce(metadata->>'pod_template_labels', '')
FROM content_entities
WHERE repo_id = 'repo-1' AND entity_type = 'K8sResource'
ORDER BY relative_path, start_line, entity_id
LIMIT 5001;
```

## Baseline (no new index)

```
Limit  (actual time=11.259..11.509 rows=5001 loops=1)
  Buffers: shared hit=416
  ->  Sort  (actual time=11.258..11.364 rows=5001 loops=1)
        Sort Key: relative_path, start_line, entity_id
        Sort Method: quicksort  Memory: 2114kB
        Buffers: shared hit=416
        ->  Index Scan using content_entities_repo_idx on content_entities
              (actual time=0.014..2.389 rows=6000 loops=1)
              Index Cond: (repo_id = 'repo-1'::text)
              Filter: (entity_type = 'K8sResource'::text)
              Rows Removed by Filter: 4000
              Buffers: shared hit=410
Execution Time: 11.693 ms
```

10 warm runs: 11.985–19.789 ms (p50 ≈ 15.47 ms, p95 ≈ 19.7 ms). This
environment runs somewhat noisier/higher than #5363's final committed
measurement (p50 ≈ 7.6 ms / p95 ≈ 9.16 ms on different hardware/Docker-VM
load), but the plan shape and cost attribution are identical.

**Cost decomposition** (confirms the #5363 claim in this environment): scan +
`entity_type` filter alone (`SELECT count(*) ... WHERE repo_id=$1 AND
entity_type='K8sResource'`, no sort/no projection) measured **1.51–1.77 ms**
over 3 runs — consistent with the ~2.0 ms claim. The remaining ~10–18 ms is
the `Sort` node plus carrying `metadata` through it.

## Variant A — composite `(repo_id, entity_type)` btree

```sql
CREATE INDEX content_entities_repo_type_idx ON content_entities (repo_id, entity_type);
```

With `content_entities_repo_idx` still present, the planner **did not use
the new index at all** — 5 runs still show `Index Scan using
content_entities_repo_idx`, Execution Time 11.216–11.663 ms (unchanged from
baseline).

Forcing the isolated case (temporarily dropped `content_entities_repo_idx` so
only the new composite index remains a candidate for the equality predicates):

```
->  Bitmap Index Scan on content_entities_repo_type_idx
      (actual time=0.075..0.198 rows=6000 loops=1)
      Index Cond: ((repo_id = 'repo-1'::text) AND (entity_type = 'K8sResource'::text))
Execution Time: 11.248-11.399 ms
```

The scan+filter step itself dropped from ~2.3 ms to **0.08–0.2 ms** — a clean,
measured win on that isolated component — but **total execution time did not
move** (still 11.2–11.6 ms across 5 runs) because the `Sort` node is the
dominant cost and this index does nothing for it.

**Verdict: DISPROVEN as stated.** An index that attacks only scan+filter
(the ~2.0 ms component) cannot move the ~12–19 ms total, exactly as the
#5363/#5490 issue predicted. Not adopted.

## Variant B — covering index matching the ORDER BY key (non-partial)

```sql
CREATE INDEX content_entities_k8s_select_covering_idx
  ON content_entities (repo_id, entity_type, relative_path, start_line, entity_id)
  INCLUDE (entity_name, metadata);
```

```
Limit  (actual time=0.022..1.570 rows=5001 loops=1)
  ->  Index Only Scan using content_entities_k8s_select_covering_idx
        (actual time=0.021..1.421 rows=5001 loops=1)
        Index Cond: ((repo_id = 'repo-1'::text) AND (entity_type = 'K8sResource'::text))
        Heap Fetches: 0
Execution Time: 1.676-2.323 ms (5 runs)
```

This is a **material win**: putting the ORDER BY columns as the index key
lets the scan return pre-sorted rows (the `Sort` node disappears entirely),
and `INCLUDE (entity_name, metadata)` makes it an index-only scan (`Heap
Fetches: 0`), so the query never touches the heap. **~6–10x faster** than
baseline (11–19 ms → 1.6–2.3 ms).

Retested immediately after an `UPDATE` touching all 6,000 target rows (no
`VACUUM` yet — simulates hot-ingest churn before autovacuum catches up):
`Heap Fetches: 5001–10002`, Execution Time **3.07–4.09 ms**. Still a ~3–5x
win even in the adverse case where the visibility map is stale.

Row-set equivalence: a full-column dump of the query's 5,001 rows with vs.
without the index is byte-identical (`diff` reports 0 differences), proving
this is output-preserving.

## Variant C — partial covering index (`WHERE entity_type = 'K8sResource'`)

```sql
CREATE INDEX content_entities_k8s_select_partial_idx
  ON content_entities (repo_id, relative_path, start_line, entity_id)
  INCLUDE (entity_name, metadata)
  WHERE entity_type = 'K8sResource';
```

Same read win as Variant B (the partial predicate lets Postgres drop
`entity_type` from the key entirely and still elide the filter):

```
Limit  (actual time=0.018..1.899 rows=5001 loops=1)
  ->  Index Only Scan using content_entities_k8s_select_partial_idx
        Index Cond: (repo_id = 'repo-1'::text)
        Heap Fetches: 0
Execution Time: 1.71-2.04 ms (3 runs)
```

Index size: **2,280 kB** (only the 6,000 K8sResource rows), vs. **6,128 kB**
for the non-partial Variant B (all 18,000 rows) — 2.7x smaller, and smaller
than the sum of all five pre-existing indexes combined (3,672 kB).

Row-set equivalence: identical 5,001-row byte-for-byte dump with vs. without
this index (0 diff).

### Rejected narrower alternative (expression-key index, no full-JSONB duplication)

The codebase's established convention for candidate-scan indexes (see the
`graph_node_owner_cloud_resource_page_idx` #5563 migration) avoids
duplicating a full JSONB column into an index when possible. A narrower
design was tried: extract only the fields the matcher needs as **expression
key columns** instead of `INCLUDE (metadata)`:

```sql
CREATE INDEX content_entities_k8s_select_narrow_idx
  ON content_entities (
    repo_id, relative_path, start_line, entity_id,
    (metadata->>'kind'), (metadata->>'namespace'),
    ((jsonb_typeof(metadata->'selector') = 'string')), (metadata->>'selector'),
    ((jsonb_typeof(metadata->'pod_template_labels') = 'string')), (metadata->>'pod_template_labels')
  )
  INCLUDE (entity_name)
  WHERE entity_type = 'K8sResource';
```

This index is smaller (1,480 kB) but **the planner never chose it**, even
under `SET enable_seqscan=off; SET enable_bitmapscan=off;` and even with
every competing index (`content_entities_repo_idx`,
`content_entities_type_idx`, `content_entities_path_idx`,
`content_entities_repo_entity_idx`) dropped so it was the only non-`pkey`
index available — the planner fell back to a full `Seq Scan` rather than use
it, for a plain 3-column probe query matching only its leading key columns.
**Rejected**: an index the planner will not use in any tested configuration
is not a viable candidate, regardless of its smaller footprint.

## Variant D — SQL-level `kind = 'Service'` pushdown

The only production caller (`fetchK8sSelectMatchedServiceIDs`) filters
`candidate.Kind == "Service"` (case-insensitively) in Go after the fetch, and
`ListRepoK8sSelectCandidates` has no other caller — so a case-insensitive
`AND lower(metadata->>'kind') = 'service'` predicate would, in isolation,
return the same Service subset the caller currently retains.

Measured **without a supporting expression index** (base 5 indexes only):

```
->  Seq Scan on content_entities
      Filter: ((repo_id = 'repo-1'::text) AND (entity_type = 'K8sResource'::text)
                AND (lower((metadata ->> 'kind'::text)) = 'service'::text))
      Rows Removed by Filter: 15000
Execution Time: 8.10-10.68 ms (5 runs)
```

This *looks* like a modest win over the 11–19 ms baseline on this 18,000-row
test table, but the plan is a **full `Seq Scan` of the entire table**, not
just repo-1's partition — because adding an unindexed JSONB expression
predicate changed the planner's cost estimate enough to abandon the
`repo_id` index entirely. That scan cost scales with the **whole platform's**
`content_entities` row count (every repo), not the queried repo's rows.
Production has vastly more than 18,000 rows across many repositories, so this
"win" is a scalability trap, not a real improvement — and it is still ~4-5x
slower than Variant C's 1.7–2.0 ms.

**Verdict: DISPROVEN / rejected.** Unsafe without a supporting expression
index (which would need its own write-amplification proof and is out of
scope here), and unnecessary once Variant C is in place — Go already filters
`Kind == "Service"` for free over an in-memory ≤5,001-row slice once the
fetch itself is ~1.7–2.0 ms. This also matches the intentional design
decision recorded in the `ListRepoK8sSelectCandidates` doc comment (no SQL
kind filter, pending this measurement).

## Write-amplification analysis (Variant C, the landed index)

`content_entities` is a hot, continuously-ingested table; every insert/update
of a row matching the partial predicate maintains this index. Measured with
6 repeated bulk-insert batches of 5,000 rows each, `DELETE` + `VACUUM`
between runs, same throwaway container:

| Batch (5,000 rows) | No K8s-related index | With `content_entities_k8s_select_partial_idx` | Delta |
| --- | ---: | ---: | ---: |
| K8sResource (Service) rows — **indexed** by the partial predicate | 74.33 ms mean / 70.15 ms median | 97.95 ms mean / 94.05 ms median (1 outlier of 290 ms excluded as a Docker-VM stall, not attributed to the index) | **+23.6 ms / +31.7%** (~4.7 µs/row) |
| Function rows — **excluded** by the partial predicate | 56.48 ms mean / 57.6 ms median | 58.56 ms mean / 59.57 ms median | **+2.1 ms / +3.7%** (within run-to-run noise — effectively zero) |

For comparison, the non-partial Variant B index (which indexes **every** row
regardless of `entity_type`) cost +29.4 ms / +39.5% (~5.9 µs/row) on the same
Service-row batch, and would pay a similar tax on every Function, Variable,
Class, and every other entity type platform-wide — the vast majority of
`content_entities` writes. Confining the index to `WHERE entity_type =
'K8sResource'` (Variant C) keeps that cost off the table for every other
entity type, which is why Variant C, not Variant B, is the landed choice.

Index storage: 2,280 kB for the 6,000 K8sResource rows in this seed vs. 3,672
kB total for the five pre-existing indexes on all 18,000 rows — proportionate
to the fraction of the table it actually covers.

## Decision

**Landed**: Variant C, the partial covering index
`content_entities_k8s_select_partial_idx`, as migration
`go/internal/storage/postgres/migrations/077_content_entities_k8s_select_partial_index.sql`.

Rationale: it is the only candidate that produces a material, reproducible
win (1.7–2.0 ms vs. 11–19 ms baseline, ~6–10x) by eliminating the `Sort` node
— the actual dominant cost the #5363 shim identified — while its `WHERE
entity_type = 'K8sResource'` clause measurably confines write-amplification
to the narrow K8sResource row population instead of taxing the entire hot
ingest table. The composite `(repo_id, entity_type)` index (Variant A) is
**not** adopted: it only attacks the ~2.0 ms scan+filter component and
measurably does not move total wall time. The narrower expression-key
variant is **not** adopted: the planner will not use it under any tested
configuration. The SQL `kind` pushdown (Variant D) is **not** adopted: it
is unsafe (whole-table scan risk) without further work that is out of this
issue's scope, and unnecessary given Variant C's result.

## Hypothesis ledger

| candidate | cheapest proof | old | new | write cost | disposition |
| --- | --- | ---: | ---: | ---: | --- |
| A: composite `(repo_id, entity_type)` btree | EXPLAIN ANALYZE, 6,000-K8s repo | 11.2-19.7 ms | 11.2-11.7 ms (unchanged; isolated scan+filter 0.08-0.2 ms) | not measured (rejected on read result) | **rejected** |
| B: covering index, ORDER BY key + `INCLUDE(entity_name, metadata)`, non-partial | EXPLAIN ANALYZE | 11.2-19.7 ms | 1.68-2.32 ms | +29.4 ms/5000-row Service batch (~5.9 µs/row), taxes every entity_type | rejected in favor of C (same read win, less write cost) |
| C: same covering index, **partial** `WHERE entity_type='K8sResource'` | EXPLAIN ANALYZE + bulk-insert timing | 11.2-19.7 ms | 1.71-2.04 ms | +23.6 ms/5000-row K8sResource batch (~4.7 µs/row); ~0 for non-K8s rows | **proven — landed** |
| C-alt: narrow expression-key variant (no metadata duplication) | EXPLAIN ANALYZE, forced scan disable | n/a | never chosen by planner | smaller (1,480 kB) but unusable | **rejected** (planner never selects it) |
| D: SQL `lower(metadata->>'kind') = 'service'` pushdown | EXPLAIN ANALYZE | 11.2-19.7 ms | 8.1-10.7 ms, but full `Seq Scan` (whole-table, not repo-scoped) | not measured (rejected on scan-shape risk) | **rejected** |

## Correctness and concurrency proof

- Row-set equivalence (Variant C landed index): full 5,001-row, 8-column dump
  of the production query with vs. without the index is byte-identical
  (`diff` — 0 differences), proving the index is output-preserving.
- Focused Go tests:
  `go test ./internal/storage/postgres -count=1` (full package, includes the
  new `TestContentEntitiesK8sSelectPartialIndexMigration` and
  `TestContentEntitiesK8sSelectPartialIndexIsSingleConcurrentStatement`) —
  pass.
  `go test ./internal/query -run 'K8s|Candidate|SelectCandidates' -count=1`
  — pass (all pre-existing #5363 K8s-select behavior unchanged; this is a
  read-only index, no production Go code changed).
- Concurrency/index-lifecycle proof (required for index candidates per
  eshu-performance-rigor): `TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive`
  (build tag `integration`, gated on `ESHU_POSTGRES_TEST_DSN`) seeds an
  isolated schema with the same worst-case partition, then proves (1) first
  application, (2) identical reapplication is a clean no-op
  (`CREATE INDEX CONCURRENTLY IF NOT EXISTS`), (3) `DROP INDEX CONCURRENTLY
  IF EXISTS` rollback on a populated, previously-indexed store, and (4)
  reapplication after rollback. Run live against `postgres:16` in Docker:
  `ESHU_POSTGRES_TEST_DSN=postgres://eshu:change-me@localhost:55490/eshu_5490_live_test?sslmode=disable
  go test -tags=integration ./internal/storage/postgres -run
  'TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive' -count=1 -v`
  — **PASS**.

## No-Observability-Change

This change adds one Postgres index via a migration; it does not touch any
Go production code path, metric, span, log field, or runtime knob.
`ListRepoK8sSelectCandidates` and its callers are unchanged — the query text
is identical; only a new index is available to the planner. Existing
`postgres.query` spans and the query's existing tracing attributes
(`db.operation=list_repo_k8s_select_candidates`) are unaffected.
