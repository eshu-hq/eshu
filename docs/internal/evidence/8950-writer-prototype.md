# #8950 PR-B prototype: two-statement writer on the real ContentWriter

Issue #8950 asks whether rewriting unchanged content under a new generation
can cost a metadata update instead of a full row rewrite. PR-A (#9130) merged
the outcome counter that measures how often production hits that shape. This
note records the PR-B prototype: the real `ContentWriter` batch path splits
classified batches into a metadata-only UPDATE plus the guarded upsert, with
a bounded conflict retry, proven on a findings-heavy batch.

## Classification

- Wrong lifecycle cost, not slowness: a new generation rewrites every row
  through the guarded upsert even when only lineage changed.
- Write amplification: `SET content` forces a new TOAST value and new entries
  in both trigram GIN indexes, so the update is never HOT.

Rejected: a single statement with conditional `SET content = <same value>`.
Assigning the column marks it modified, so the GIN entries churn anyway; only
not assigning `content` (a separate statement) keeps the update HOT.

## What the prototype does

`classifyAndWriteFileBatch` (in `content_writer_metadata_only_update.go`)
runs the pre-read, sends `metadata_only` rows to
`updateMetadataOnlyContentFileBatch`, and sends everything the UPDATE did not
touch through the existing guarded upsert. The UPDATE sets only `commit_sha`
and `generation_id`, rechecks the guard's full row-equality twin in SQL
(content text included, never the digest proxy), and reports updated keys with
`RETURNING`; a concurrent edit between the pre-read and the UPDATE misses and
falls through to the upsert, which keeps last-writer-wins. `indexed_at` is not
moved: it sits in the language covering index's INCLUDE, and moving it costs
the HOT update. Unobserved writers and unclassified batches keep the single
statement byte-identical to before.

A deadlocked batch retries the whole unit (fresh pre-read, idempotent
statements) up to 8 attempts with jittered backoff, mirroring the projector
claim's conflict discipline; the claim needs 3 because SKIP LOCKED almost
never conflicts, while the 6-writer proof below deadlocks roughly every second
batch. A CTE pre-lock was rejected instead: lock order through `UPDATE..FROM`
is the planner's choice, not a contract, while retry is precedented and costs
nothing uncontended. Counters record once, for the attempt that succeeds.

## Proof

Backend: disposable `postgres:18.6-bookworm` (Docker, this aarch64 box,
`shared_buffers=1GB`), all 253 migrations, 12 files of ~200 KiB with a
secret-like line every tenth line (secret trigger and both trigram indexes in
play), table at `fillfactor = 50` with `VACUUM FULL` applied test-locally. No
migration in this step. Seed at gen-1/commit c1, rewrite identical bodies at
gen-2/commit c2 through the real `ContentWriter`.

| | Legacy single upsert | Prototype two-statement |
|---|---|---|
| WAL (LSN diff) | 900,384 bytes (0.86 MiB) | 2,616 bytes |
| Wall | ~512 ms | ~232 ms |
| `pg_total_relation_size` delta | +1,040,384 bytes | 0 bytes |
| HOT (`xmin` moved, `ctid` page kept, every `content_files` index byte-identical) | 0 of 12 | 12 of 12 |
| `indexed_at`, `content` | moved | preserved |
| Secret lines | re-derived | untouched, still present |
| Config-artifact fence version | bumped | bumped |
| Language rollup `MAX(indexed_at)` | advances | seed stamp (see open question) |

Tests: `TestContentWriterMetadataOnlyRewriteIsHOTLive` (the failing
regression: index growth + 0 HOT on the old path), `...SavesWalWallLive`
(the table above, plus the merged counter reading 12/12 rows and 100% of body
bytes `metadata_only` for a new-generation full-snapshot replay),
`TestContentWriterMetadataOnlyConcurrentNoTornRowsLive` (forced interleave:
a holder commits an edit while two metadata writers wait on its row lock —
2 lock waiters observed — every row ends as one writer's complete triple),
`TestContentWriterCountsMixedUpsertOutcomes` (updated to the two-statement
shape: 1 UPDATE of 1 group + 1 INSERT of 4 groups, counters unchanged),
`TestObservedBatchRetriesDeadlockedUpdate` + `TestContentFileWriteConflictClass`
(the retry mechanism hermetically). The pre-existing 6-writer stress
(`TestContentFileUpsertOutcomeConcurrentWritersLive`) deadlocks constantly on
the new path — 181 server-side deadlocks across the runs here — and passes 5/5
with exact counts, all absorbed by the retry.

Mutation check: the SET-list test fails if any column but the lineage pair is
assigned; the recheck test fails if any guard column is dropped from the
UPDATE or `indexed_at` appears in it; the placeholder-cast test fails on an
untyped VALUES (42883 on the real database).

## Open questions and remains

- Arbiter ruling needed before rollout: freezing `indexed_at` on
  commit/generation-only rewrites means the language inventory's
  `last_indexed_at` no longer advances on a pure replay through the converted
  paths (it already does not advance on identical replays since #8915).
  Unobserved writers and unclassified batches still take the single upsert,
  which moves `indexed_at` on a lineage-only replay as before. State:
  RULED — on the converted paths (classified batches + the single-row
  `ContentStore` path) the rollout implements the arbiter's ordered meaning
  ("last written with a change beyond commit_sha/generation_id lineage"),
  and the PR body states the operator consequence plainly.
- Storage migration: shipped as migration 236 (`ALTER TABLE content_files SET
  (fillfactor = 50)`, catalog-only, single statement). The rollout bar below
  replaces the `VACUUM FULL` rewrite used here: no page rewrite anywhere in
  the migration or automation.
- No index change is needed: the rollout moves no indexed column, and the
  read-path guard below confirms the language inventory plan stays index-only
  with no timing regression.
- Single-row `ContentStore` path converted in the rollout to the same
  metadata-only behavior (6/6 HOT, `indexed_at` preserved, live proof below).
- Exact counts from the statements' own results still open (tally stays the
  pre-read proxy); production share still unmeasured (needs a deployed metric
  window); final `eshu-code-review`, `make pre-push`/`pre-pr`, and PR belong to
  the orchestrator.

## Rollout control: indexed_at in the SET list shows HOT 0 (RED arm)

`TestContentWriterMetadataOnlyIndexedAtMoveIsNotHOTLive` runs the same 12-file
findings-heavy batch as the prototype HOT proof through a variant of the
shipped metadata-only UPDATE with `indexed_at = clock_timestamp()` added to
the SET list — the only difference from the HOT path.

Command (disposable `postgres:18.6-bookworm`, this aarch64 box,
`shared_buffers=1GB`, all migrations, table at `fillfactor = 50` with
`VACUUM FULL` applied test-locally, `pg_stat_force_next_flush` around the
stats reads):

```text
ESHU_POSTGRES_TEST_DSN='postgres://postgres:postgres@localhost:55440/postgres?sslmode=disable' \
  go test ./internal/storage/postgres/ \
  -run TestContentWriterMetadataOnlyIndexedAtMoveIsNotHOTLive -count=1 -v
```

Result: the variant touched all 12 rows (`RETURNING` count 12, every `xmin`
moved, every `indexed_at` moved) with `n_tup_upd` delta 12 and
`n_tup_hot_upd` delta 0, WAL 465,296 bytes. Moving an indexed (INCLUDE)
column forces a new index entry, so the update can never stay on its page.
This proves the HOT result comes from leaving `indexed_at` alone, not from
fillfactor or body shape.

## Rollout bar: production-shaped corpus, autovacuum ON, real writer

Scratch harness (`zz_8950_bar_scratch_live_test.go`, deleted after the run;
results below, raw log in the resume dir): disposable `postgres:18.6-bookworm`,
this aarch64 box, `shared_buffers=1GB`, autovacuum ON (default; verified
`autovacuum=on`, no `autovacuum_enabled=false` reloption), no `VACUUM FULL`,
`CLUSTER`, or `pg_repack` anywhere. Corpus: 30,000 lognormal background rows
(median 4 KiB, sigma 1.2, capped 400 KiB; 80% go) plus 50 findings-heavy rows
(469 KiB, 491 findings each) — 30,050 rows in one repository, every full replay
a `FullSnapshot` `Write` through the real `ContentWriter` (61 batches of 500),
stable per-path sha1 digests so the pre-read classifies honestly. Fresh
bootstrap applies all 254 migrations including 236 (`reloptions` read back as
`{fillfactor=50}` — the fresh-install proof), then the control arm RESETs the
fillfactor to dense packing. Stats deltas use `pg_stat_force_next_flush` and
are schema-scoped; sizes are `pg_relation_size` heap, TOAST, every index, and
`pg_total_relation_size`. Settle waits (15 s poll, 20 min cap) run autovacuum
to `dead=0` before each steady-state read.

| Replay | `upd` | `hot` | HOT% | WAL (LSN diff) | wall |
|---|---|---|---|---|---|
| seed gen-1 (ff100 dense) | 0 inserts | — | — | 943,145,024 | 85.5 s |
| control-1 gen-2 (ff100) | 30,050 | 5,098 | 17.0 | 747,782,240 | 65.1 s |
| control-2 gen-3 (ff100) | 30,050 | 7,755 | 25.8 | 691,304,352 | 44.7 s |
| turnover gen-4 (ff50) | 30,050 | 10,124 | 33.7 | 562,810,224 | 41.6 s |
| durable-1 gen-5 (ff50) | 30,050 | 30,050 | 100.0 | 77,416,664 | 29.2 s |
| durable-2 gen-6 (ff50) | 30,050 | 30,050 | 100.0 | 14,101,664 | 29.3 s |
| durable-3 gen-7 (ff50) | 30,050 | 30,050 | 100.0 | 15,236,792 | 29.0 s |

The ff100 control (17.0%/25.8%, matching the theory harness's 17.0%/26.4% on
an independent harness) never reaches 90% unaided, so the migration is
necessary. The turnover round is the one-time relocation cost (rows move onto
ff50 pages, old dense pages go dead until vacuum). The three following
consecutive new-generation full replays are each 100% HOT with no `VACUUM FULL`
between — the durability bar (>=90% per round) passes. `ALTER ... SET
(fillfactor = 50)` itself changed no size (total 320,217,088 before and
after): catalog-only, as migration 236's comment states.

Steady-state sizes, ff100 (2 control replays + settle) vs ff50 (turnover + 3
durable replays + settle), bytes:

| Relation | ff100 steady | ff50 steady | delta |
|---|---|---|---|
| heap | 78,708,736 | 106,250,240 | +35.0% |
| TOAST | 124,780,544 | 124,780,544 | 0 |
| `content_files_pkey` | 3,014,656 | 3,014,656 | 0 |
| `content_files_repo_path_idx` | 3,014,656 | 3,014,656 | 0 |
| `content_files_repo_path_pattern_idx` | 3,014,656 | 3,014,656 | 0 |
| `content_files_language_repo_idx` | 3,211,264 | 3,579,904 | +11.5% |
| `content_files_content_trgm_idx` | 48,513,024 | 48,513,024 | 0 |
| `content_files_content_lower_trgm_idx` | 46,792,704 | 48,463,872 | +3.6% |
| `content_files_relative_path_trgm_idx` | 5,849,088 | 6,291,456 | +7.6% |
| `content_files_artifact_type_idx` | 491,520 | 548,864 | +11.7% |
| `content_files_iac_relevant_idx` | 491,520 | 548,864 | +11.7% |
| `content_files_template_dialect_idx` | 491,520 | 548,864 | +11.7% |
| indexes total | 114,884,608 | 117,538,816 | +2.3% |
| `pg_total_relation_size` | 320,217,088 | 350,420,992 | +9.4% |

Total-relation growth is +9.4%, inside the +10% bar, so no step to
fillfactor 70 was needed. Heap-alone is 3.1x the fresh-dense seed (vs the
2.0–2.2x fresh-table figure from the issue's scratch run: an ALTERed table
keeps its old dense pages allocated until a rewrite reclaims them; vacuum
cleared all dead tuples but the file does not shrink without `VACUUM FULL`,
which the rollout deliberately does not run). TOAST is byte-identical and the
GIN trgm indexes on content are flat or near-flat: HOT adds no entries, and
the small btree growth is the turnover round's one-time non-HOT relocation.

Read-path guard (medians of 11 client-timed runs, `ANALYZE` before each
side; full plans on a 5,000-row real-writer corpus): language inventory
`SELECT repo_id, count(*), max(indexed_at) ... WHERE language = 'go' GROUP BY
repo_id` 1.678 ms (ff100) vs 1.700 ms (ff50): +1.3%; single content fetch by
`(repo_id, relative_path)` 93 us vs 84 us: faster. No regression over 10% on
either read, so promotion proceeds. The covering index serves the inventory
read (`Index Only Scan using content_files_language_repo_idx`, 425 buffers on
the corroboration corpus; the dense ff100 side legitimately seqscans, 2,035
buffers — page-count cost, not a behavior change), and the fetch uses
`content_files_repo_path_pattern_idx` on both sides. No index change ships.

Single-row path proof: `TestContentStoreSingleRowMetadataOnlyPreservesIndexedAtLive`
seeds and rewrites 6 rows through `ContentStore.UpsertFileBatch` (new
generation, same bodies, clock advanced): `n_tup_upd`/`n_tup_hot_upd` delta 6/6,
every `xmin` moved, every `indexed_at` kept, every row on its page.

Single-row UPDATE-miss overhead (P2-2 proof, 2026-10-26): scratch harness
(`zz_8950_single_row_overhead_scratch_live_test.go`, deleted after the run;
numbers below), same disposable backend as the prototype proof, 120 rows of
~8 KiB with secret lines, medians of 5 fresh-repo iterations: insert through
`UpsertFileBatch` (UPDATE miss + upsert, 2 statements/row) 556.9 ms
(4,640 µs/row) vs insert through the direct single-row upsert (1
statement/row) 618.7 ms (5,156 µs/row) — delta −61.8 ms, i.e. noise favoring
the two-statement arm, not overhead. The miss itself is one PK probe
(`EXPLAIN ANALYZE`: index scan on `content_files_repo_path_pattern_idx`,
3 buffer hits, 0.044 ms execution) against ~5 ms/row of trigger and TOAST
work. Lineage-only rewrite through the store costs 227.1 ms (1,893 µs/row,
1 statement); content change 800.7 ms (6,673 µs/row). No optimization
shipped: any absence pre-check would cost its own round-trip, and skipping
the UPDATE would forfeit the HOT/`indexed_at` behavior the rollout converted
this path for.

Post-deploy watch: `n_tup_hot_upd`/`n_tup_upd` for `content_files` (the
reference page's HOT-share query) plus
`eshu_dp_content_file_upserts_total{outcome="metadata_only"}` for the rewrite
share and `eshu_dp_content_file_write_conflict_retries_total` for retry rate.
Production outcome share stays NOT_CHECKED: it needs a deployed metric window.
