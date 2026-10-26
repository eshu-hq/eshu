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
  `last_indexed_at` no longer advances on a pure replay (it already does not
  advance on identical replays since #8915). State: NOT_CHECKED.
- Storage migration: `ALTER TABLE content_files SET (fillfactor = 50)` is
  catalog-only and covers new pages first; existing pages need a rewrite
  (measured here with `VACUUM FULL`). Heap-alone growth at 50 was 2.14x in the
  issue's scratch run. Migration, backfill strategy, and the size bar belong
  to the implementation PR.
- No index change is needed: the prototype moves no indexed column, and the
  issue's measurement run concluded the language index stays as is.
- Single-row `ContentStore` path untouched (batch path only); exact counts
  from the statements' own results still open (tally stays the pre-read
  proxy); production share still unmeasured (needs a deployed metric window);
  final `eshu-code-review`, `make pre-push`/`pre-pr`, and PR belong to the
  orchestrator.
