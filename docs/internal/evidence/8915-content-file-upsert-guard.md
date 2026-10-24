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
tracked in #8950; this change does not reduce it. The headline numbers below are
statement time (the one file `INSERT ... ON CONFLICT` as `EXPLAIN ANALYZE` times
it); the whole `Write` call is slower, see "Where the rest of the write goes".

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

Test files (all in `go/internal/storage/postgres`, run against PostgreSQL 18.6
through the `live-postgres-readiness` runner):
`content_file_upsert_guard_live_test.go`,
`content_file_upsert_guard_columns_live_test.go`,
`content_file_upsert_guard_concurrency_live_test.go`,
`content_file_upsert_guard_contract_live_test.go`,
`content_file_upsert_guard_bench_live_test.go`, plus the hermetic
`content_writer_sql_guard_test.go`, `content_writer_duplicate_key_test.go` and, in `config/artifacts`,
`fence_guard_shared_clause_test.go`. The two replay benchmarks run a small
corpus by default (an unchanged replay must write at most 64 KiB of WAL; a batch
whose rows all change must write at least 64 KiB) and switch to the full
measurement corpus with `ESHU_CONTENT_UPSERT_GUARD_BENCH=1`, where they assert
nothing.

Red and green at this head: with the guard removed from the clause, 24 test
lines fail (8 tests and 16 subtests: seven live tests plus the hermetic lockstep
test). Every unchanged-replay case fails (`indexed_at_only` and each
`*_null_to_null` case, on both statement shapes), plus the writer-level replay,
the changed-file sibling check, both lock-recheck "skip" cases, the statement
trigger probe, the smoke benchmark (0.44 MiB of replay WAL against a 64 KiB
bound) and the one-row WAL bound (74,705 and 96,859 WAL bytes for one 200 KB
replay). With the guard, all 75 lines pass (14 tests and 61 subtests, under
`-race`). The four-session race, the duplicate-path test and the changed-batch
benchmark pass either way, as they should: they prove the guard adds no
deadlock, keeps the old duplicate contract, and does not skip real changes.
Earlier drafts reported 18, 21 and 22 failures; those were the same mutation
over smaller test sets (before the recheck tests, before the smoke benchmark),
not different code.

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
  on the session), against 74,705 to 96,859 before.
- The SET list, the two guard tuples and the INSERT columns stay in lockstep
  (`TestUpsertContentFileConflictClauseLockstep`, hermetic, with a seeded RED
  and GREEN pair), so a column added to one side but not the guard fails a
  test that needs no database.

### Before and after: unchanged replay

`TestContentFileUpsertGuardReplayBenchLive`: 50 files of about 469 KB with 491
findings each, one 50-row batch through the real `ContentWriter`, under
`EXPLAIN (ANALYZE, BUFFERS, WAL)`. Five identical replays per side, local
PostgreSQL 18.6, shared host. Two full runs per side are shown because the host
load changes the "before" number a lot.

| Side | Statement time, median (range) | WAL, median | Write wall, median |
|---|---|---|---|
| Before, run 1 (guard removed, load 8 to 21) | 11,037 ms (4,087 to 26,155 ms) | 65.84 MiB | 13,446 ms |
| Before, run 2 (guard removed, load 9 to 31) | 4,906 ms (4,330 to 24,108 ms) | 71.41 MiB | 7,264 ms |
| After, run 1 | 134 ms (134 to 135 ms) | 0.00 MiB | 2,489 ms |
| After, run 2 (load 31 at start) | 135 ms (134 to 136 ms) | 0.00 MiB | 2,512 ms |

A reviewer's runs on a quieter host: after 134 to 212 ms (one cold run 1,548 ms
with 768 shared reads and no WAL), before 4.17 to 4.25 s with 59 to 78 MiB of
WAL. The statement time improves 36 to 82 times and about 3 to 5 times
on the whole write; both are only for same-generation replays. A new-generation
rewrite pays the "before" cost.

### Where the rest of the write goes

After the guard, a replay's write wall is 2.5 s against a 135 ms statement. The
`content writer stage completed` log line for `upsert_files` reads 2.48 to
2.53 s per round, and `contentReferencesForFiles` (the hostname and service-name
extraction `upsertContentReferenceBatch` runs over every body after the INSERT)
takes 2.3 to 2.4 s alone over this 23 MB corpus, run in isolation by the
benchmark. That is client CPU, unchanged by this change, and about 95 percent of
the remaining replay write. It is the next measured bottleneck of this path; it
is not covered here.

### Before and after: a batch whose rows all change

The guard adds one row-versus-row compare, and a `content` compare when lengths
match, to every row that does not skip. `TestContentFileUpsertGuardChangedBatchBenchLive`
appends a line to every one of the same 50 files and writes the batch, five
rounds per side, same corpus and statement.

| Side | Statement time, median (range, first round cold) | WAL, median | Write wall, median |
|---|---|---|---|
| Guard removed | 6,282 ms (6,260 to 38,664 ms) | 75.63 MiB | 8,630 ms |
| Guard | 6,486 ms (6,299 to 38,636 ms) | 71.46 MiB | 8,848 ms |

The first round reads cold data on both sides (38.6 s each). The medians differ
by 3 percent, inside the host noise (load 9 to 31). A reviewer measured
6.96 s and 6.64 s with the guard against 6.24 s and 6.44 s without. I read this
as no measurable regression on the changed-row path, not as proof of zero.

### Fence source guard

`TestNoGoStatementChangesContentFilesRepoID` (config/artifacts) scans Go
literals for a `content_files` statement that changes `repo_id`. The shared
clause is one literal, so it now expects to see that clause once instead of two
upserts, and `TestContentFilesUpsertsShareOneConflictClause` proves both
statement constants reference the shared clause. The seeded pair fails on a
statement with its own clause and on a `repo_id` assignment planted in the
shared clause.

## Risks checked

1. **`indexed_at` stays old on an exact replay.** `rg indexed_at` over non-test
   Go code lists these readers of `content_files.indexed_at`:
   - the repository `MAX(indexed_at)` coverage reads
     (`QueryMaxIndexedAtWithRowQueryer` for `content_last_indexed_at`, the
     language inventory `last_indexed_at`, the dead-code investigation coverage
     block): none compares it with a clock, and `freshness_state` is set by
     whether files exist;
   - the search-document loader (`eshu_search_document_source_loader.go`), which
     feeds it to `searchdocs.ProjectContentFile` as the document's `UpdatedAt`,
     served as `updated_at` on a `repository_file` semantic-search result. No
     ranking, rerank or freshness code reads it (`content_hybrid_rerank.go`,
     `searchhybrid`, `semanticsearch` only pass it through). Its documented
     meaning is "source projection timestamp when available"
     (`search-document-projection.md`); for a file it now means the last write
     that changed the row, not the last replay. I judge that acceptable and
     closer to what `updated_at` implies; the projection reference says so;
   - the migration-169 generation-tag backfill, which ran once at migration
     time.
   `content_entities.indexed_at`, `content_file_references.indexed_at` and
   `content_file_ref_deltas.indexed_at` are other tables and are unchanged.
2. **A skipped row and the triggers.** A skipped row writes nothing, but the
   statement still fires the statement-level triggers. The migration-138
   secret-line and migration-186 version-fence UPDATE triggers are statement
   level (`tgtype` checked in the test); an `INSERT ... ON CONFLICT DO UPDATE`
   fires them even when it only inserts, and fires them with an empty transition
   table when every row is skipped. A probe trigger recorded transition-table row
   counts of 0 (seed insert), 0 (skipped replay) and 1 (changed replay). With
   nothing in the transition table they stage no repository and derive no
   secret line, so the side table and the fence stay put: the side-table parity
   and the fence version are asserted equal after an unchanged replay and one
   higher after a changed statement. The row-level triggers on `content_files`
   are only the foreign-key triggers.
3. **`RowsAffected` is ignored by both writers.** `upsertContentFileBatch` and
   `ContentStore.UpsertFileBatch` discard the `sql.Result` (`_`); no caller
   counts rows from these statements. The conflict-clause comment and the
   package `AGENTS.md` say not to read it as "files written".
4. **A duplicate (repo, path) inside one batch.** Before, every duplicate
   `(repo_id, relative_path)` key in one multi-row upsert failed with `ON CONFLICT DO UPDATE command cannot affect row
   a second time`. With the guard alone, a duplicate over a stored row that the
   first copy leaves unchanged would be skipped and a differing second copy would
   win silently, while a duplicate of a new key still failed; `ContentWriter.Write`
   does not de-duplicate files by path. That is a data-dependent silent success
   where there was a loud failure, so `upsertContentFileBatch` now rejects a
   duplicate `(repo_id, path)` pair before any statement runs (`duplicate path
   ... for repo ... in one batch`), which keeps the old contract for new and
   stored paths alike. The key is the pair, as the conflict target is: rows carry
   their own `repoID`, so one path under two repositories is two rows and is
   accepted (`TestUpsertContentFileBatchDuplicateKeyIsRepoAndPath`, hermetic, and
   `TestContentFileUpsertBatchAcceptsSamePathInDifferentReposLive`; an outside
   review caught that a path-only key would reject it). `ContentWriter.Write`
   builds every row with one repository, so today only the pair check matters.
   `TestContentWriterRejectsDuplicatePathInBatchLive` fails without the check
   (the stored-path cases succeed silently; the new-path case fails with the
   Postgres message instead of the duplicate-path one) and passes with it. No producer emits
   duplicates that worked before, since none could.

## Concurrency

- Four `ContentWriter` sessions upsert the same 40-file batch for 12 rounds
  each, alternating the unchanged replay with a per-session changed variant, run
  under `-race`: no error, no `40P01`, each row holds exactly one session's
  variant, secret-line parity holds.
- Lock order is unchanged. `ON CONFLICT DO UPDATE` locks the conflicting row
  whether or not the `WHERE` passes (a reviewer showed a second session's
  `FOR UPDATE NOWAIT` failing against a transaction that had only skipped the
  row), and the row order is the batch order, as before.
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

Performance Evidence: see the two before and after tables above. Same corpus,
statement, host and round count on both sides of each; the unchanged-replay
numbers hold for same-generation replays only, and the statement-time headline
is not the whole write.

No-Regression Evidence: the changed-row path runs the same update list as
before. On the 50-file all-changed batch the guard's median statement time is
6,486 ms against 6,282 ms without it (3 percent, inside the noise), and WAL is
71.46 MiB against 75.63 MiB.

## Not covered

A built-binary replay; `wal_buffers` and autovacuum effects; the share of
production writes that are same-generation replays; the 30-round soak that
issue #8915 acceptance criterion 3 asks for (this change has five-round runs
only, so the issue stays open); the cost of the 2.4 s reference extraction.
