# #10874: oversized search-index partitions p29 and p07

Issue #10874 (epic #10860) tracks two `eshu_search_index_terms` hash
partitions far above their siblings: p29 at 193 GB and p07 at 36 GB. This
note records the banked live-QA facts (read-only, gathered by prior lanes),
what this lane shipped (per-partition size census, autovacuum reloptions,
parity harness), and the reviewed operational proposal for the rebuild —
which runs on a physical copy first and never on live QA in this issue.

## Banked live facts (QA reader, SELECT-only, 2026-11-19)

Source: prior lanes 1-3 of this drive (reused as discovery, not re-queried
here). Exact `count(*)` with `PGOPTIONS` read-only guard, zero writes.

| Observation | Value |
|---|---|
| p29 | 193 GB total = 28 GB heap + 165 GB pkey; 386,974 exact live rows and falling during the read |
| p07 | 37 GB total = 2,080 MB heap + 35 GB pkey; 914,876 exact live rows |
| Index share | 85.3% of p29 bytes, 94.5% of p07 bytes: the defect is index high-water bloat, not heap |
| p29 skew | one scope `r_33474fcb` held 117,901/386,974 rows in superseded generation `0dad10da` (superseded 14:30, mid-drain: counts fell 637,901 to 117,901 between two reads) |
| p07 skew | one scope `r_ecb7cfc7` held 814,175/914,886 rows in ONE active generation `e6e035bd` (active, must survive any rebuild verbatim) |
| Hash routing | `satisfies_hash_partition` verified live: `r_33474fcb` -> (64,29), `r_ecb7cfc7` -> (64,7), negative control false: skew is large scope_ids, not misrouting |
| Healthy contrast | p03: 32 GB at ~21M est rows (~1.5 KiB/row) vs p29 ~500 KiB/row, p07 ~40 KiB/row |
| reltuples trust | p29 est 2,302,180 vs exact 386,974 (6x over); p07 est 510,171 vs exact 914,886 (44% under): never a reclaim figure |
| Schema | `PARTITION BY HASH(scope_id) MODULUS 64` (003b, 039a); the pkey `(scope_id, generation_id, term_key, document_id)` is the ONLY index per leaf; reloptions NULL on all inspected leaves; no pgstattuple/pg_freespacemap |
| Churn driver | writer DELETE-all + COPY-fresh per generation build plus batched retention drains (predrain, orphan sweep, #8231 superseded sweep); predrain already reports `TermsDeleted`/`DocumentsDeleted` per generation; missing-deletes ruled out (drain observed live mid-flight) |

Top-5 largest sibling leaves by `pg_total_relation_size` (exact bytes):
p03 34,493,464,576; p18 26,710,106,112; p62 17,195,106,304;
p59 16,625,410,048; p48 13,856,219,216.

## No-Regression Evidence: the per-pass census costs one ~5 ms catalog-only statement

Baseline: before this lane the retention runner issued zero statements about
partition sizes; the only size signal was the stale `reltuples` estimate
(6x off exact on p29), which is why p29/p07 grew to 193/37 GB unnoticed.

After: `TakePartitionSizeCensus` issues exactly one statement per due pass
(pinned by `TestTakePartitionSizeCensusReadsOneCatalogPass`, which fails on
any second statement), and the runner runs it at most once per poll interval
(pinned by `TestGenerationRetentionRunnerCensusesPartitionSizesOncePerPollInterval`).

Backend/version: throwaway PostgreSQL 18.6 cluster on this machine
(socket-only, dropped after the run), schema `PARTITION BY HASH(scope_id)`
with 64 empty-index leaves matching production DDL, 200,000 terms rows +
20,000 documents rows loaded, `VACUUM ANALYZE` run. Input shape: the exact
`partitionSizeCensusQuery` text from `sql.go`, `EXPLAIN (ANALYZE, COSTS OFF,
TIMING OFF)` x5: 5.515 / 5.697 / 5.161 / 5.271 / 5.238 ms. The plan touches
only catalog relations (`pg_class`, `pg_index`, `pg_namespace`,
`pg_stat_user_tables`); zero scans on any `eshu_search_index_terms_pNN`
leaf. The statement takes the same `ACCESS SHARE` leaf locks as the existing
per-leaf orphan-census SELECTs, and a census error is recorded on
`eshu_dp_generation_retention_failures_total{reason="search_index_partition_size_census"}`
without failing the pass (pinned by runner test), so a catalog hiccup can
never stall retention.

The parity snapshot (`TakePartitionParitySnapshot`) is a full-leaf scan by
design (0.3 ms over ~2k rows on the same fixture, not a scale claim) and
never runs on the hot path: only on the copy, before and after the
rebuild-equivalent operation. Telemetry/log/status proof: `go test`
`./internal/storage/postgres/search/index/retention/`
`./internal/reducer/maintenance/retention/` `./internal/telemetry/` all
green in this lane, plus the three new tests above; live copy wall-time /
WAL / lock-wait numbers stay NOT_CHECKED until the copy run (table below).

## Observability Evidence: new series are bounded and closed-label

`eshu_dp_search_index_partition_bytes{partition,component}`,
`eshu_dp_search_index_partition_estimated_rows{partition}`,
`eshu_dp_search_index_partition_bytes_per_row{partition}`:
`partition` is the closed p00-p63 set; `component` is heap/index/total on
the bytes gauge and total-only on the bytes-per-row gauge;
scope_id and generation_id never reach labels (they ride the WARN log line
and span events only, per the existing `SpanGenerationRetentionExpiredReport`
precedent). One `generation retention search index partition size suspect`
WARN per suspect leaf per pass; one
`census_search_index_partition_sizes` sample per pass on the phase
histogram. Covered by `TestSearchIndexPartitionInstrumentsRecordClosedLabels`
and `TestGenerationRetentionRunnerRecordsPartitionSizes`.

## What this lane shipped

- `TakePartitionSizeCensus` + `ClassifyPartitionSize` + parity snapshot
  (`go/internal/storage/postgres/search/index/retention/partition_size.go`,
  queries in `sql.go`): one catalog-only SELECT per pass; WARN at 4 GiB total
  with 8 KiB/row, PAGE at 16 GiB with 32 KiB/row (initial values, to
  calibrate against the all-64-leaf copy distribution).
- Runner hook on the orphan-sweep cadence
  (`go/internal/reducer/maintenance/retention/partition_size_census.go`,
  wired in `go/cmd/reducer/generation_retention_wiring.go`) with three
  gauges (`eshu_dp_search_index_partition_bytes{partition,component}`,
  `eshu_dp_search_index_partition_estimated_rows{partition}`,
  `eshu_dp_search_index_partition_bytes_per_row{partition}`),
  one WARN line per suspect leaf, and a failure counter that never fails
  the pass.
- Migration 313: per-leaf autovacuum reloptions (0.02/0.05, the #9975
  starting point) on all 64 terms leaves plus the documents table, one
  catalog-only DO block, no rewrite.
- Docs: this note, the [telemetry section](../../public/reference/telemetry/generation-retention-search-index.md#per-partition-size-census-10874),
  and the metrics/coverage rows.

## Operational proposal: reclaim p29/p07 (copy first, live owner-gated)

Scope: `eshu_search_index_terms_p29` + its pkey and `_p07` + its pkey only,
leaf by leaf, never the partitioned parent as a unit. All other 62 leaves
are touched only if the Decision-3 census flags them.
`eshu_search_index_documents` and `_stats` are out of scope until separately
measured.

Step A (the reclaim that matters): `REINDEX INDEX CONCURRENTLY` on each
leaf pkey, one leaf at a time. The index is 165/193 GB (p29) and 35/37 GB
(p07); concurrent BM25 reads and writer clear+insert batches keep running,
taking no `ACCESS EXCLUSIVE` on the leaf except brief catalog phases.
Follow with plain `VACUUM` (not `FULL`) + `ANALYZE` on the leaf so heap
free space becomes reusable and `reltuples` refreshes.

Step B (gated, heap): only if post-Step-A heap bytes/row on the copy still
exceeds the Decision-3 PAGE threshold, propose a shadow-leaf swap (CREATE
new leaf-like table, COPY live rows in PK order, build PK, then DETACH old
/ ATTACH new in one short transaction) in a maintenance window with the
affected scopes' writers fenced via the per-generation advisory lock class
7427 — never a bare `VACUUM FULL` on the live leaf and never
DETACH-without-replacement (parent INSERTs for that hash remainder fail
and pruned SELECTs silently lose those scopes). The Step-B decision is
taken from copy numbers, in the proposal, not pre-approved here.

Duration is measured on a copy ONLY, and the copy must be physical
(volume/PVC snapshot clone or base backup of QA into a throwaway instance)
so it carries the real bloat — a logical `CREATE TABLE AS` copy has no
bloat and proves nothing. On the copy record per leaf: wall time of
`REINDEX CONCURRENTLY`, `pg_stat_progress_create_index` samples, WAL delta
(`pg_current_wal_lsn` before/after), `pg_total_relation_size` /
`pg_relation_size` before/after, exact `count(*)` AND a PK checksum
(`sum(hashtext(scope_id || generation_id || term_key || document_id)::bigint)`)
before/after (must be identical), lock waits sampled from
`pg_stat_activity` during the build, and a concurrent read probe
(production BM25 query shape) + writer probe (one clear+batch-insert cycle
for a scope hashing to that leaf) running throughout. Report exact
seconds; do not extrapolate from a smaller leaf. The parity harness
(`TakePartitionParitySnapshot` / `ComparePartitionParity`) is the
machine-checked form of the count+checksum comparison.

Rollback: Step A is self-rolling-back — a failed/interrupted CIC leaves an
INVALID `*_ccnew` index; drop it, old index untouched (delete-path per
owner rollback policy; no downgrade rehearsal). Step B rollback = keep the
detached old leaf table un-dropped until post-swap count+checksum verify
passes; re-attach old / detach new restores the prior state (rows written
to the new leaf during the verify window must be fenced out by the writer
fence, which is why the fence is mandatory). Any live execution later
needs its own owner sign-off; this ruling authorizes the copy measurement
and this written proposal only.

## Copy-measurement record (NOT RUN in this lane)

| Leaf | CIC wall time | WAL bytes | Total before/after | Heap before/after | Rows before/after | Checksum equal | Read p95 during | Writer probe | Step-B go/no-go |
|---|---|---|---|---|---|---|---|---|---|
| p29 | NOT_CHECKED | NOT_CHECKED | 193 GB / NOT_CHECKED | 28 GB / NOT_CHECKED | 386,974 / NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED |
| p07 | NOT_CHECKED | NOT_CHECKED | 37 GB / NOT_CHECKED | 2,080 MB / NOT_CHECKED | 914,886 / NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED |

No physical copy exists in this lane's environment (no volume/PVC snapshot
tooling reachable here), so before/after sizes are the banked live "before"
numbers above; the "after" column fills when the copy run lands. No
`REBUILD`, `VACUUM`, `REINDEX`, or DDL ran on live QA in this lane: zero
writes, per the brief.

## Explicitly not done here

- Primary-side `pg_stat` per leaf (`n_dead_tup`, last autovacuum): the
  replica returns zeros and the reader grant bars the primary read; the
  coordinator reads the primary via `read-dsn` before locking constants.
- Narrower-PK or generation-independent-postings redesign: the real
  long-term lever (PK is ~65% of terms bytes per #7932) but unmeasured and
  out of #10874 scope. No issue filed: per arbiter, it is pursued only as
  its own measured proposal, not as drive-by scope in this lane.
- `eshu_search_index_documents` (62 GB, unpartitioned) bloat-vs-payload
  split: separate assessment.
