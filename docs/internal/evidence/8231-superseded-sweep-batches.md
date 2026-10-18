# #8231 superseded sweep: batch and listing measurements

This note proves the #8231 superseded sweep's statements before the flagged
sweep ships: the terms and documents batches keep the #7427 measured plan
shape (the page and ctid DELETE are byte-identical with the pre-delete's,
pinned by `TestSupersededSweepStatementsFailClosed`), the fail-closed
eligibility line adds no measurable cost, and the new candidate listing and
stats delete are index-driven single-digit-buffer reads.

## Fixture and host

Host: disposable PostgreSQL 18 container (`postgres:18`) on the shared
build host, database `eshu`, isolated bootstrapped schema per test via the
retention leaf's live fixture. One scope with a 45,000-term / 4,500-document
superseded generation, a same-size active generation re-minting the same
document ids on the same hash leaf, and one stats row per generation.
`EXPLAIN (ANALYZE, BUFFERS)` on the shipped statements; DELETE batches run
inside a rolled-back transaction so the measurement deletes nothing.

Backend/version: PostgreSQL 18 (container `postgres:18`) on x86_64.
Input shape: 45,000 terms + 4,500 documents in the swept generation, 20,000-row
page bound, default plan mode (no forced generic plan).
Terminal counts: each batch deletes exactly its page; the sweep drains the
45,000-row generation in 3 terms batches plus the re-sweep, 1 documents
batch, and 1 stats batch (proven by
`TestSweepSupersededLiveKeepsTheActiveReaderUnchanged`).

## Measurements

| Statement | Shared buffers | Wall (indicative, shared host) |
| --- | --- | --- |
| terms first batch (cursor `('', '')`) | 53,661 | 1,747 ms |
| terms late batch (cursor mid-keyspace) | 53,662 | 1,747 ms |
| documents batch (4,500 rows) | 9,207 | 36 ms |
| stats batch (1 row) | 9 | < 0.1 ms |
| candidate listing (1 candidate) | 9 | 0.07 ms |
| terms batch, ineligible (pending generation) | 6 | 0.02 ms |

Plan shape, all batches: the eligibility CTE resolves with two primary-key
lookups (`scope_generations` scope+generation index, `ingestion_scopes`
pkey) plus one bitmap probe of `fact_work_items_status_idx`; the terms page
is an Index Scan on the leaf primary key with Index Searches 1; the DELETE
re-scans the `(scope_id, generation_id)` prefix and filters by ctid (25,000
rows removed by filter here: the active generation's re-minted ids on the
same leaf, the #7427 c7 shape). The candidate listing is an index scan over
`scope_generations` with EXISTS probes, 9 buffers.

No-Regression Evidence: the first and late terms batches cost the same
(53,661 vs 53,662 buffers), so the keyset walk does not degrade as the
cursor advances; the #8231 eligibility CTE costs 5 buffers eligible and 3
ineligible (vs 3 in #7427 for the same shape without the NOT NULL line), so
the fail-closed `active_generation_id IS NOT NULL` line adds no measurable
read cost; an ineligible generation costs one 6-buffer statement and commits
nothing. WAL behavior inherits #7427 (1.08 MB per 20,000-row terms batch):
the page and DELETE bodies are byte-identical with the measured pre-delete
statements, so no new WAL shape is introduced.

Performance Evidence: the new statements (candidate listing at 9 buffers,
stats delete at 9 buffers) are index-driven reads and a single-row delete;
neither scans a table. The sweep's per-pass cost is therefore bounded by the
same per-batch budget as the pre-delete (2,000,000 rows through the shared
row cap), and the listing adds one sub-millisecond statement per sweep.

Observability Evidence: rows land on
`eshu_dp_search_index_superseded_rows_swept_total{table}` (terms, documents,
stats) recorded by the retention runner; listing and sweep wall time land on
the `census_search_index_superseded` and `sweep_search_index_superseded`
phases; every committed batch logs `generation retention search index
superseded batch committed` with `scope_id`, `generation_id`, `table`, and
`batch_rows_deleted`, and each run logs the `... superseded sweep completed`
summary with `candidates`, `rows_swept`, `batches`, and
`unswept_generations`. Emission is covered by
`TestGenerationRetentionRunnerRecordsSupersededSweep` (counter values per
table, phase histogram point, log lines) and
`TestSweepSupersededDrainsTermsDocumentsAndStats` (5 committed batches, 5
batch log lines).

## Why the change is safe

Accuracy first: every batch re-checks eligibility in its own snapshot and
fails closed on a NULL active pointer or a missing generation row (live
proof: `TestSweepSupersededLiveSkipsPinnedAndInflight`); readers join the
active generation, whose rows and reader-visible stats are byte-identical
before and after (live proof:
`TestSweepSupersededLiveKeepsTheActiveReaderUnchanged`). Performance:
measured above — same plan shape and per-batch cost as the shipped
pre-delete, new statements index-driven. Concurrency: each batch is its own
transaction under the same per-generation advisory lock class the pre-delete
and prune already share, with `lock_timeout` below `deadlock_timeout`, so a
sweep batch never deadlocks a prune of the same generation id. Enablement
stays held (flag defaults off) until the #8231 gate-(b) primary
dead-tuple/bloat evidence lands; this note measures batch cost, not reclaim.
