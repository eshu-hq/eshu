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

**Update (PR #5745 codex P1):** the index originally landed from this
investigation (`INCLUDE (entity_name, metadata)`) was a real production
robustness bug — `metadata` is unbounded and risks the btree ~2.7 KiB
per-tuple limit, which can fail `CREATE INDEX CONCURRENTLY` and leave an
INVALID index on a real K8s manifest. It was fixed (`INCLUDE (entity_name)`
only) and re-measured; see `## PR #5745 codex P1` and `## Corrected Variant
C` below for the bug, the fix, and the full honest re-measurement, which
also corrects the earlier doc's overclaimed "unconditional ~6-10x win" to
the actual, conditional result.

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

## Variant C (original, SUPERSEDED — unsafe) — partial covering index `INCLUDE (entity_name, metadata)`

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

### PR #5745 codex P1 — this revision is UNSAFE and was not shipped

A codex review on PR #5745 flagged a real production robustness bug in this
revision (migration 077, `:33`): `metadata` is an **unbounded** JSONB payload
for K8sResource rows — the Kubernetes YAML parser folds every label,
container image, and backend reference into it with no size cap. A btree
`INCLUDE` value is stored in the index leaf tuple and cannot exceed
Postgres's ~2.7 KiB per-tuple limit. A valid K8s manifest with enough or
large labels/references would make this index build fail with `index row
size exceeds btree maximum`, leaving an **INVALID** index and **failing
schema bootstrap** in production — a real risk regardless of this evidence
doc's seeded row sizes, which simply were not large enough to trip it. See
`## Corrected Variant C` below for the fix and full re-measurement; this
section is retained only as the historical record of the rejected design.

## Corrected Variant C — bounded partial index, `INCLUDE (entity_name)` only

The fix: drop `metadata` from `INCLUDE` entirely. Keep the index key
unchanged (that is what eliminates the `Sort` node). `entity_name` stays in
`INCLUDE` — it is a bounded Kubernetes object name (Kubernetes enforces a
253-character DNS-subdomain ceiling on names) and the SELECT list projects it
directly, so it is both safe for the per-tuple limit and useful for the
index-scan projection. The key columns (`repo_id`, `relative_path`,
`start_line`, `entity_id`) are all bounded identifier/path/integer values.
**No column in this index is an unbounded payload.**

```sql
CREATE INDEX content_entities_k8s_select_partial_idx
  ON content_entities (repo_id, relative_path, start_line, entity_id)
  INCLUDE (entity_name)
  WHERE entity_type = 'K8sResource';
```

Steady-state index size (after `REINDEX`, same 6,000-row partition): **536
kB** — 4.25x smaller than the unsafe metadata-covering revision's 2,280 kB.

### Re-measurement — the query now heap-fetches `metadata` (expected and safe)

Without `metadata` in the index, satisfying the SELECT list requires one
heap fetch per matching row, so this is now a plain `Index Scan`, not an
`Index Only Scan`. The re-measurement below is the **honest, complete**
result across the conditions that determine whether Postgres's planner
actually chooses the ordered-scan plan — the Sort-elimination mechanism
is real, but whether it fires by default depends on planner cost settings
and candidate-pool size, not solely on the index's existence.

**On the canonical 6,000-K8sResource worst-case partition** (same shape as
above, all five pre-existing indexes present, rebuilt fresh in this
container):

| Condition | Plan | Execution Time (ms) |
| --- | --- | ---: |
| Baseline, no new index | `Index Scan` on `content_entities_repo_idx` + `Sort` | 7.934, 8.040, 8.128, 8.256, 9.284 (mean 8.33) |
| New bounded index present, **default** `random_page_cost=4.0` (realistic: index co-exists with all base indexes) | planner **keeps** `content_entities_repo_idx` + `Sort` — **new index is not chosen** | 8.082, 8.124, 8.202, 8.247, 8.340 (mean 8.20) — **unchanged from baseline** |
| New index, isolated (competing `content_entities_repo_idx` dropped so only the new index can serve the equality predicates, `enable_bitmapscan/seqscan=off`) | `Index Scan` using the new index, no `Sort` | 1.761, 1.763, 1.848 (mean 1.79) |
| New index, all base indexes present, `SET random_page_cost = 1.1` (Postgres's own documented recommendation for SSD-backed storage; **not currently set anywhere in Eshu**) | planner **naturally** picks `Index Scan` using the new index, no `Sort` | 1.808, 1.862, 1.892 (mean 1.85) |

**Finding**: under Postgres's out-of-the-box default cost settings — what an
unconfigured Eshu Postgres actually runs with today — the planner does
**not** select the new index for this exact worst-case partition; it keeps
the pre-existing plan, and total execution time is **unchanged from having
no index at all** (~8.2 ms either way). The reason: satisfying the query now
requires one random heap-page visit per matching row (for `metadata`), and
at this row count (6,000 candidates, capped at 5,001) Postgres's default
`random_page_cost=4.0` estimates that cost as higher than the existing
bitmap/index-scan-plus-explicit-sort plan. The underlying mechanism (an
ordered index scan skips the `Sort` node) is real and reproducible — proven
both by isolating the new index from its competitor and by tuning
`random_page_cost` to a value appropriate for SSD storage — but it is
**conditional**, not automatic, at this specific candidate-pool size.

**A larger, also-realistic worst case**: nothing bounds a repo to exactly
6,000 K8sResource entities: a large K8s-heavy monorepo can plausibly have
tens of thousands, still hitting the same `repositorySemanticEntityLimit`
(5,001) cap. Reseeding `repo-1` with 30,000 K8sResource rows (24,000 more
Service rows appended, same partition otherwise) and rerunning the identical
query under **default, untuned** settings:

| Condition | Plan | Execution Time (ms) |
| --- | --- | ---: |
| Baseline, no new index (30,000 K8sResource rows) | Postgres already finds a decent plan on its own: `Incremental Sort` (`Presorted Key: relative_path`) using the pre-existing `content_entities_path_idx` | 3.006, 3.042, 3.155, 3.382, 3.947 (mean 3.31) |
| New bounded index present, **default** settings, no forcing | planner **naturally selects** `Index Scan` using the new index, no `Sort` | 1.839, 2.146, 3.066 (mean 2.35) |

At this larger, plausible scale the new index **is** chosen automatically —
a real, if more modest (~30% faster on average, not multiple-x), win over
what is already a reasonably good baseline plan. The early-termination
property of an ordered index scan (stop after the first 5,001 matching rows
in key order) matters more as the total candidate pool grows well past the
cap, while the competing plan's cost (`Incremental Sort`/full sort of the
whole candidate pool before applying `LIMIT`) grows with total candidates.
Table correlation was **not** the deciding factor here — `pg_stats`
correlation for `relative_path` was 0.69 on the 6,000-row seed and −0.16 on
the append-heavy 30,000-row seed (i.e., *worse*), yet the index was chosen
at the larger scale regardless.

### Row-set equivalence (corrected index)

Re-verified on the 6,000-row partition after the fix: full 5,001-row,
8-column dump of the production query with vs. without the corrected index
is byte-identical (`diff` — 0 differences). The index only changes the plan
Postgres may choose; it never changes the query's result set.

### Write-amplification (corrected index)

Re-measured the same way as before (6 repeated 5,000-row bulk-insert
batches, `DELETE` + `VACUUM` between runs):

| Batch (5,000 rows) | No K8s-related index | With corrected `content_entities_k8s_select_partial_idx` (`INCLUDE (entity_name)` only) | Delta |
| --- | ---: | ---: | ---: |
| K8sResource (Service) rows — indexed by the partial predicate | 59.28 ms mean / 55.52 ms median | 78.21 ms mean / 77.27 ms median | **+18.9 ms / +31.9%** (~3.8-4.4 µs/row) |
| Function rows — excluded by the partial predicate | unaffected by the `INCLUDE` column choice (structural: a partial index never touches a row that fails its predicate, regardless of what it `INCLUDE`s) — the prior measurement of **~0 extra cost (+2.1 ms / +3.7%, within noise)** still applies unchanged | | |

This is **lower** write cost than the original unsafe revision's +23.6 ms /
+31.7% (~4.7 µs/row) on the same batch, as expected: the corrected index
is narrower (536 kB vs. 2,280 kB steady-state).

### Tuple-size safety note

Every column in the corrected index is bounded: `repo_id` (short internal
identifier), `relative_path` (filesystem path, practically bounded by OS
path-length limits), `start_line` (4-byte integer), `entity_id` (a generated
bounded identifier string), and the `INCLUDE`d `entity_name` (Kubernetes
caps object names at 253 characters). None is an unbounded JSONB or
text-blob column, so this index cannot hit the btree ~2.7 KiB per-tuple
limit regardless of manifest size. A regression test
(`TestContentEntitiesK8sSelectPartialIndexExcludesUnboundedMetadata`) fails
the build if `metadata` (or any column) is ever added back to `INCLUDE`.

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
"win" is a scalability trap, not a real improvement.

**Verdict: DISPROVEN / rejected.** Unsafe without a supporting expression
index (which would need its own write-amplification proof and is out of
scope here). Also unnecessary: Go already filters `Kind == "Service"` for
free over an in-memory ≤5,001-row slice once the fetch itself completes
(whether that's the corrected Variant C's ~1.7-1.9 ms when its plan is
chosen, or the unchanged ~8.2 ms baseline when it is not — either way,
filtering an already-fetched small in-memory slice by `Kind` costs nothing
extra). This also matches the intentional design decision recorded in the
`ListRepoK8sSelectCandidates` doc comment (no SQL kind filter).

## Write-amplification analysis (corrected Variant C, the landed index)

`content_entities` is a hot, continuously-ingested table; every insert/update
of a row matching the partial predicate maintains this index. Measured with
6 repeated bulk-insert batches of 5,000 rows each, `DELETE` + `VACUUM`
between runs, same throwaway container, against the **corrected**
`INCLUDE (entity_name)`-only index:

| Batch (5,000 rows) | No K8s-related index | With corrected `content_entities_k8s_select_partial_idx` | Delta |
| --- | ---: | ---: | ---: |
| K8sResource (Service) rows — **indexed** by the partial predicate | 59.28 ms mean / 55.52 ms median | 78.21 ms mean / 77.27 ms median | **+18.9 ms / +31.9%** (~3.8-4.4 µs/row) |
| Function rows — **excluded** by the partial predicate | structurally unaffected by `INCLUDE` column choice; prior measurement stands | | **~0 (+2.1 ms / +3.7%, within noise)** |

For comparison: the original unsafe (superseded) `INCLUDE (entity_name,
metadata)` revision cost +23.6 ms / +31.7% (~4.7 µs/row) on the same batch —
the corrected, bounded index is both safer AND cheaper to write, because it
is narrower (536 kB steady-state vs. 2,280 kB). The non-partial Variant B
index (indexing every row regardless of `entity_type`) cost +29.4 ms /
+39.5% and would tax every entity type platform-wide; confining the index to
`WHERE entity_type = 'K8sResource'` (Variant C) keeps that cost off the vast
majority of `content_entities` writes, which is why a partial index — safe
or not — is the right shape regardless of the `INCLUDE` question.

## Decision

**Landed**: the corrected Variant C, the bounded partial index
`content_entities_k8s_select_partial_idx` — key
`(repo_id, relative_path, start_line, entity_id)`,
`INCLUDE (entity_name)` only, `WHERE entity_type = 'K8sResource'` — as
migration
`go/internal/storage/postgres/migrations/077_content_entities_k8s_select_partial_index.sql`.

Rationale:

- **Safety is non-negotiable and now satisfied.** The original revision
  risked "index row size exceeds btree maximum" on a real K8s manifest
  (PR #5745 codex P1) because it `INCLUDE`d the unbounded `metadata` JSONB.
  The corrected index contains only bounded columns and cannot hit that
  limit regardless of manifest size.
- **The win is real but conditional, not unconditional**, and this doc
  reports that honestly rather than the originally-claimed unconditional
  ~6-10x: on the canonical 6,000-row worst-case partition, under Postgres's
  default cost settings, the planner does **not** select this index — total
  execution time is unchanged from having no index at all (~8.2 ms either
  way). The Sort-elimination mechanism is real (proven at ~1.6-1.9 ms when
  isolated from its competing index, or under an SSD-appropriate
  `random_page_cost=1.1` that Postgres itself recommends for SSD storage but
  Eshu does not currently set), and it **is** selected automatically, under
  default settings, once the K8sResource candidate pool grows well past the
  cap (a plausible large-monorepo shape: ~30% faster on average at 30,000
  rows).
- **No downside if unused.** When the planner does not choose this index
  (the common case on default settings at moderate candidate-pool sizes), it
  costs nothing at read time — Postgres simply keeps using the existing
  plan — and its write cost is confined to K8sResource rows only, now lower
  than the original unsafe revision.
- The composite `(repo_id, entity_type)` index (Variant A) is **not**
  adopted: it only attacks the ~2.0 ms scan+filter component and measurably
  does not move total wall time. The narrower expression-key variant is
  **not** adopted: the planner will not use it under any tested
  configuration. The SQL `kind` pushdown (Variant D) is **not** adopted: it
  is unsafe (whole-table scan risk) without further work out of this issue's
  scope, and unnecessary regardless of which plan the corrected index gets.
- **Follow-up (not implemented here, out of this fix's scope):** documenting
  and/or setting an SSD-appropriate `random_page_cost` for Eshu's Postgres
  deployments would let this index (and plausibly others) pay off
  automatically on the canonical worst-case partition size, not only on the
  larger one. This is a database-wide tuning decision with its own
  before/after proof obligation across every affected query, not a
  single-migration change, and is intentionally not bundled into this fix.

## Hypothesis ledger

| candidate | cheapest proof | old | new | write cost | disposition |
| --- | --- | ---: | ---: | ---: | --- |
| A: composite `(repo_id, entity_type)` btree | EXPLAIN ANALYZE, 6,000-K8s repo | 11.2-19.7 ms | 11.2-11.7 ms (unchanged; isolated scan+filter 0.08-0.2 ms) | not measured (rejected on read result) | **rejected** |
| B: covering index, ORDER BY key + `INCLUDE(entity_name, metadata)`, non-partial | EXPLAIN ANALYZE | 11.2-19.7 ms | 1.68-2.32 ms | +29.4 ms/5000-row Service batch (~5.9 µs/row), taxes every entity_type | rejected in favor of C |
| C (original, SUPERSEDED): partial, `INCLUDE(entity_name, metadata)` | EXPLAIN ANALYZE + bulk-insert timing | 11.2-19.7 ms | 1.71-2.04 ms (Index Only Scan) | +23.6 ms/5000-row K8sResource batch (~4.7 µs/row); ~0 for non-K8s rows | **rejected — PR #5745 codex P1: unbounded metadata risks btree tuple-size limit and bootstrap failure** |
| C-alt: narrow expression-key variant (no metadata duplication) | EXPLAIN ANALYZE, forced scan disable | n/a | never chosen by planner | smaller (1,480 kB) but unusable | **rejected** (planner never selects it) |
| D: SQL `lower(metadata->>'kind') = 'service'` pushdown | EXPLAIN ANALYZE | 11.2-19.7 ms | 8.1-10.7 ms, but full `Seq Scan` (whole-table, not repo-scoped) | not measured (rejected on scan-shape risk) | **rejected** |
| C (corrected, LANDED): partial, `INCLUDE(entity_name)` only | EXPLAIN ANALYZE, 6,000-row and 30,000-row partitions, default and SSD-tuned cost settings | 6,000-row: 8.33 ms; 30,000-row: 3.31 ms | 6,000-row default: 8.20 ms (**unchanged — planner does not select it**); 6,000-row isolated/SSD-tuned: 1.79-1.85 ms; 30,000-row default: 2.35 ms (**planner selects it automatically**) | +18.9 ms/5000-row K8sResource batch (~3.8-4.4 µs/row, lower than original); ~0 for non-K8s rows | **proven, safe, conditional win — landed** |

## Correctness and concurrency proof

- Row-set equivalence (corrected, landed index): full 5,001-row, 8-column
  dump of the production query with vs. without the index is byte-identical
  (`diff` — 0 differences), proving the index only affects the chosen plan,
  never the query's result set. Re-verified after the P1 fix.
- Focused Go tests:
  `go test ./internal/storage/postgres -count=1` (full package, includes the
  new `TestContentEntitiesK8sSelectPartialIndexMigration`,
  `TestContentEntitiesK8sSelectPartialIndexExcludesUnboundedMetadata` (the
  P1 regression guard), and
  `TestContentEntitiesK8sSelectPartialIndexIsSingleConcurrentStatement`) —
  pass.
  `go test ./internal/query -run 'K8s|Candidate|SelectCandidates' -count=1`
  — pass (all pre-existing #5363 K8s-select behavior unchanged; this is a
  read-only index, no production Go code changed).
  `go test ./internal/storage/postgres -run
  'TestBootstrapDefinitionsAreOrderedAndComplete' -count=1` — pass.
- Concurrency/index-lifecycle proof (required for index candidates per
  eshu-performance-rigor): `TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive`
  (build tag `integration`, gated on `ESHU_POSTGRES_TEST_DSN`) seeds an
  isolated schema with the same worst-case partition, then proves (1) first
  application, (2) identical reapplication is a clean no-op
  (`CREATE INDEX CONCURRENTLY IF NOT EXISTS`), (3) `DROP INDEX CONCURRENTLY
  IF EXISTS` rollback on a populated, previously-indexed store, and (4)
  reapplication after rollback. Re-run live against the corrected DDL,
  `postgres:16` in Docker: `ESHU_POSTGRES_TEST_DSN=postgres://eshu:change-me@localhost:55490/<db>?sslmode=disable
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
