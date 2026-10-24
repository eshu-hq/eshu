# #8915: guard the content_files upsert against unchanged rewrites

Issue #8915 reported that an unchanged re-upsert of findings-heavy
`content_files` rows sometimes took 4 to 13 times longer. The measurement on the
issue disproved the migration-138 trigger as the cause. The cause is write
amplification: the writer's `ON CONFLICT DO UPDATE` rewrote every row even when
nothing but `indexed_at` differed, and the cost of that rewrite is mostly WAL
waits. This note records the fix and what it was proved against.

## Scope

The guard covers same-generation replays: the same file, the same content, the
same `commit_sha` and `generation_id`. A replay under a new `generation_id` or
`commit_sha` changes a guarded column and still rewrites the row. That cost is
tracked in #8950; this change does not reduce it.

## Cause

Every unchanged round was a non-HOT update (0 HOT of about 1,900 updates). Two
things force that:

- `indexed_at` is an `INCLUDE` column of
  `content_files_language_repo_idx`, so changing it needs new index entries.
- `SET content = EXCLUDED.content` recomputes the stored `content_lower`, so both
  large trigram GIN indexes insert the whole file again.

Backend CPU was constant between fast and slow rounds; the variance is waiting
on `LWLock:WALWrite` and data-file reads. Details and the disproved theories
(trigger cost, GIN pending list) are in the #8915 measurement comment.

## Change

`upsertContentFileConflictClause` (`content_writer_sql.go`) is the one conflict
clause for `upsertContentFileQuery` and `upsertContentFileBatchSuffix`. It adds

```sql
WHERE (content_files.commit_sha, content_files.content_hash, content_files.line_count,
       content_files.language, content_files.artifact_type, content_files.template_dialect,
       content_files.iac_relevant, content_files.generation_id)
      IS DISTINCT FROM (EXCLUDED.commit_sha, ..., EXCLUDED.generation_id)
   OR content_files.content IS DISTINCT FROM EXCLUDED.content
```

`indexed_at` is left out on purpose: it changes on every write, so comparing it
would make the guard never skip. `content` is compared on its own so an edit
that kept its `content_hash` (a caller-supplied digest) still writes.

## Proof

Test files: `content_file_upsert_guard_live_test.go`,
`content_file_upsert_guard_columns_live_test.go`,
`content_file_upsert_guard_concurrency_live_test.go`,
`content_file_upsert_guard_bench_live_test.go`. All run against PostgreSQL 18.6
through the `live-postgres-readiness` runner; the benchmark is opt-in
(`ESHU_CONTENT_UPSERT_GUARD_BENCH=1`).

Red on the unmodified writer: 18 failures. Every unchanged-replay case fails
(`indexed_at_only` and each `*_null_to_null` case, on both statement shapes),
plus the writer-level replay, the changed-file sibling check, and the WAL bound
(a one-row, 200 KB replay wrote 74,705 and 96,859 WAL bytes). Green with the
guard: 56 passing subtests (62 with the six top-level tests). Removing the
guard again fails 21 tests and subtests, including both recheck "skip" cases, so the tests are
sensitive to it.

What the tests assert:

- An unchanged replay through `ContentWriter.Write` and through
  `ContentStore.UpsertFileBatch` leaves `xmin`, `ctid`, `indexed_at`, the
  secret-line side table (parity with the derivation) and the content-version
  fence untouched.
- Each guarded column, `content` alone, and each nullable column's value to
  other, value to NULL and NULL to value transitions update the row; NULL to
  NULL does not. Both statement shapes.
- A changed body re-derives its secret lines; a commit-only change writes; an
  untouched sibling in the same batch stays untouched; the fence bumps once for
  the changed statement.
- A one-row, 200 KB replay writes 54 WAL bytes (from `EXPLAIN (ANALYZE, WAL)`
  on the session), against 74,705 to 96,859 before. The changed replay still
  writes about 75 KB (TOAST compresses the synthetic body).

### Before and after

`TestContentFileUpsertGuardReplayBenchLive`: 50 files of about 469 KB with 491
findings each, one 50-row batch statement through the real `ContentWriter`,
under `EXPLAIN (ANALYZE, BUFFERS, WAL)`. Five replays of identical content per
side, same host, local PostgreSQL 18.6, shared host at load 8 to 21.

| Side | Statement time, median (range) | WAL, median | Write wall, median |
|---|---|---|---|
| Before (guard removed) | 11,037 ms (4,087 to 26,155 ms) | 65.84 MiB | 13,446 ms |
| After | 134 ms (134 to 135 ms) | 0.00 MiB | 2,489 ms |

A later three-replay run at the rebased head, on a busier host, measured 165 ms
and 0.01 MiB of WAL for the same statement (the 50 skipped rows still take a
row lock each).

The range before is 6.4 times from smallest to largest. The "after" write wall
of 2.5 s is client side (building and sending about 23 MB of parameters), not
database time. Same-generation replays only: a new-generation rewrite pays the
"before" cost.

Not measured: a built-binary replay, `wal_buffers` or autovacuum effects, and
the share of production writes that are same-generation replays.

## Risks checked

1. **`indexed_at` stays old on an exact replay.** The readers are the
   `MAX(indexed_at)` coverage reads (`QueryMaxIndexedAtWithRowQueryer` for
   `content_last_indexed_at`, the language inventory `last_indexed_at`, and the
   dead-code investigation coverage block). None compares it with a clock or
   derives staleness from it: `freshness_state` is `content_index_available` or
   `not_reported`, set by whether files exist. The value now means "when content
   last changed". The one other reader, the migration-169 generation-tag
   backfill, ran once at migration time. The public wording is updated in the
   dead-code reachability spec.
2. **No UPDATE trigger and no version-fence bump on a skipped row.** The
   migration-138 secret-line triggers and the migration-186 fence stage only on
   an UPDATE, INSERT or DELETE that happens. A skipped row writes nothing, and
   nothing an indexed column feeds has changed, so the side table and the
   derived config artifacts remain valid. The test asserts both: fence version
   equal after an unchanged replay, plus one after a changed statement.
3. **`RowsAffected` is ignored by both writers.** `upsertContentFileBatch` and
   `ContentStore.UpsertFileBatch` discard the `sql.Result` (`_`); no caller
   counts rows from these statements. The conflict-clause comment now says not to
   read it as "files written".

## Concurrency

- Four `ContentWriter` sessions upsert the same 40-file batch for 12 rounds
  each, alternating the unchanged replay with a per-session changed variant, run
  under `-race`: no error, no `40P01`, each row holds exactly one session's
  variant, secret-line parity holds.
- Lock order is unchanged. `ON CONFLICT DO UPDATE` locks the conflicting row
  whether or not the `WHERE` passes, and the row order is the batch order, as
  before.
- EvalPlanQual: session A holds an uncommitted update of one row; session B
  upserts the same key and blocks on A's lock (observed as a `Lock` wait in
  `pg_stat_activity`). After A commits, B's guard is evaluated against A's
  committed row: B writing A's content skips (the row keeps A's version); B
  writing the original content updates it back. Both statement shapes.

## Observability

No-Observability-Change: the change adds no metric, span, worker, queue, lease
or knob. The `upsert_files` stage of the existing `content writer stage
completed` log records the drop, and `pg_stat_user_tables.n_tup_upd` for
`content_files` falls by the skipped rows. A skipped-row counter would need
`RowsAffected` per batch; it is not added here.

Performance Evidence: see the before and after table above. Same corpus,
statement, host and replay count on both sides; same-generation replays only.

No-Regression Evidence: the changed-row path runs the same update list as
before. The guard adds one row-versus-row compare and one `content` compare on
a row already locked; on the 50-file batch the skipped statement takes 134 ms
against 4,087 ms or more before. A changed 200 KB row writes 74,784 to 74,883
WAL bytes with the guard and 74,904 to 89,691 without it.
