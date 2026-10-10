# Config-artifact read model: derive protocol and fence (#7126)

This companion belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md).
It holds the tables, the content-version fence, the derive protocol, the
walker, the shared SQL and the shim details. The arbiter ruling and re-ruling
of 2026-10-10 decided the fence, the derive protocol, the reader condition,
the name step and the S3 arms. An item marked **Proposed** is the author's
addition and is not part of either ruling. Section 7 keeps the lost-update
sequence (R-1) that made the re-ruling replace the first fence.

## 1. Tables

Names are proposals. The migration number is chosen at build time.

| Table | Key | Columns |
|---|---|---|
| `config_artifact_content_versions` | `repo_id` | `version` (BIGINT), `bumped_xid` (xid8), `bumped_at` |
| `config_artifact_repo_state` | `repo_id` | `content_version` (the version the derive read before its list), `extractor_version` (integer), `row_count` (NULL until the first success), `files_listed`, `candidate_count`, `candidates_read`, `list_truncated`, `scope_id` and `generation_id` (advisory), `derived_at`, `failure_count`, `last_error_class`, `last_failure_at` |
| `config_artifact_paths` | `(repo_id, raw_path, relative_path, evidence_kind)` | `name_scoped` |
| `config_artifact_backfill_markers`, `config_artifact_reconcile_cursor` | name | as in migration 109 |

- The migration seeds one version row with `version = 0` for each `SELECT
  DISTINCT repo_id FROM content_files`, as migration 169 does its backfill.
  The walker's universe is then O(repositories), not a scan of
  `content_files`. Proposed order, not part of the ruling: create the trigger
  first, then seed with `ON CONFLICT DO NOTHING`, in one transaction. `CREATE
  TRIGGER` blocks content writes until that transaction commits, so no write
  can land between the seed scan and the trigger. The seed scan duration is
  then a content-write stall, and the migration proof must time it.
- **Proposed** rule: delete or reset a version row only together with the
  repository's state row, in one transaction. A version row re-created while
  a state row survives would count up from 1 again. After enough bumps it
  would equal the old `state.content_version`, and readers would trust stale
  rows. PR3b-1 adds a test that no Go statement deletes from the versions
  table except a documented retirement path. The repository retirement design
  of #7766 (draft PR #7871) requires a disposition for every repo-keyed
  table. It must give the versions and state tables one shared disposition.
- `raw_path` is the path value before `normalizeRepoScopedPath`. Each row's
  construction site sets `name_scoped`: true in `appendConfigArtifactRow` (HCL
  and compose), false elsewhere (kustomize and Ansible).
- The dirty set is a join, not a table: no state row, or
  `state.content_version` different from `version`, or an old
  `extractor_version`, or `failure_count > 0`. The walker uses it to find
  work. It is wider than the reader's test in section 4: a repository whose
  rows match the current version stays trusted after a later failed derive,
  because those rows are still exact.
- Key width (G8): four TEXT columns can exceed the btree entry limit for a
  pathological HCL expression. S2 seeds long paths. If any row exceeds the
  limit, the key becomes a digest column and the four columns stay as data.
  Otherwise the derive records `last_error_class = row_too_wide` and the
  repository stays on the live path.

## 2. The content-version trigger

Triggers on `content_files` fire for every session. No session setting, no
pre-write mark and no mark table exist.

- INSERT and UPDATE: statement-level, `REFERENCING NEW TABLE`. An `INSERT ...
  ON CONFLICT DO UPDATE` fires both. The guard below makes the second a no-op.
- DELETE: row-level, with the same guard. S3 may replace it with a
  statement-level `REFERENCING OLD TABLE` trigger.
- TRUNCATE: statement-level, bumps every version row through the same
  once-per-transaction guard.

The body bumps each repository once per transaction. The `ORDER BY` is a
**Proposed** addition to the re-ruled body (see the note below):

```sql
INSERT INTO config_artifact_content_versions AS v (repo_id, version, bumped_xid, bumped_at)
SELECT r.repo_id, 1, pg_current_xact_id(), now()
FROM (SELECT DISTINCT repo_id FROM new_rows) AS r
ORDER BY r.repo_id
ON CONFLICT (repo_id) DO UPDATE
SET version = v.version + 1, bumped_xid = EXCLUDED.bumped_xid, bumped_at = EXCLUDED.bumped_at
WHERE v.bumped_xid IS DISTINCT FROM EXCLUDED.bumped_xid
```

- The trigger runs inside the content statement's transaction, so the bump
  and the content commit together. The first fence lacked this property.
- `pg_current_xact_id()` is 64-bit, so the guard has no wraparound. It needs
  PostgreSQL 13 or later.
- Lock order (**Proposed**): a statement that names several repositories
  (the retention prune, TRUNCATE, a multi-repository insert) takes their
  version row locks in the order the insert visits them. `ORDER BY
  r.repo_id` makes that order ascending, so two such statements cannot
  deadlock on version rows. S3 arm 7 tests it.
- The row lock lasts until the content transaction ends. For a Write that is
  one 500-row batch. For retention it lasts from the end of the prune's
  `content_files` DELETE statement to the end of the retention transaction.
  AFTER row triggers fire at the end of the statement, and the retention
  transaction deletes `content_files` in one statement, so it takes the
  version row locks after all its content row locks. A Write batch of a
  pruned repository waits for the retention commit, which also runs the
  ledger and `scope_generations` deletes. That wait sits inside the existing
  `eshu_dp_generation_retention_scope_lock_hold_seconds` contract. Open PR
  #7910 changes the retention files, so PR3b-1 re-checks this section
  against them.
- `content_files` upserts and deletes run one after another inside one Write
  (`upsertContentFileBatches`, `deleteContentFilesBatch`), so one Write
  touches the version row from one transaction at a time. Two concurrent
  Writes of one `repo_id` (two scopes) wait on each other per batch. S3 arm 5
  measures this.
- Generation retention's prune fires the DELETE trigger. Retention needs no
  explicit code. Every writer, including an older binary or manual SQL, bumps
  the version, so no rollout rule exists.

## 3. The derive protocol and its failure semantics

A Write derive and a walker derive run the same steps:

1. **Read the version.** `v0 = coalesce((SELECT version FROM
   config_artifact_content_versions WHERE repo_id = $1), 0)`, a committed
   read, before the list.
2. **Derive with no lock.** List the files, read the content of the selected
   candidates, and run the extractors. No transaction is open.
3. **Write in one short transaction.** Take the repository advisory lock and
   read the state row. Write only if one of these holds:
   - No state row exists, or it has no successful derive (`row_count` is
     NULL). This clause is **Proposed**: a failure-only state row has a NULL
     `content_version`, and `v0 > NULL` is NULL, so without the clause no
     derive could replace that row.
   - `v0 > state.content_version` and the derive's `extractor_version` is at
     least `state.extractor_version`.
   - `v0 = state.content_version` and the derive's `extractor_version` is
     greater than `state.extractor_version`.

   Otherwise roll back and count `derives_total{outcome=superseded}`. To
   write, delete the rows, insert the new rows, upsert the state with
   `content_version = v0`, the `extractor_version`, the counts and
   `failure_count = 0`, and commit. An older binary never replaces newer rows.

Why the rows are exact: content with a version up to `v0` committed before the
read of `v0`, so the list and reads see it. Content committed later bumped the
version past `v0`. Readers then see a mismatch until a later derive catches
up.

Failure semantics:

- The derive recovers a panic and runs under a deadline, default 10 s.
  **Proposed** variable name: `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`.
- **Proposed** bound on the Write path: after 5 consecutive failures, the
  Write skips the derive and counts `derives_total{outcome=skipped}`. Only the
  walker retries, at its own rate, and a walker success resets the count.
  Without this bound, a repository that always times out adds up to 10 s to
  every Write.
- On an error, a panic or a timeout it counts
  `derives_total{outcome=error|panic|timeout}`, increments `failure_count`,
  records `last_error_class` and `last_failure_at`, logs
  `config_artifacts.derive.failed` at ERROR with the repository id, and
  returns nil from `Write`. A failure-only update never changes the rows or
  `content_version`.
- A failed derive leaves `state.content_version` behind the current version.
  Readers fall back live and the walker retries.
- The content rows are already committed when the derive starts. A failed
  derive cannot roll them back, and it never makes the projector retry the
  generation.

This departs from `deriveInfraInventory`, which fails the Write. That choice
is right for a SQL-only copy. It is wrong for a Go parse over arbitrary
content, because a deterministic parse bug would dead-letter the content
projection and lose graph and content truth. The lock is held only for step 3.

## 4. The reader and the walker

The reader is one statement on the replica. It inner-joins state and
versions, and the rows, for the access-filtered set of related repository
ids. It returns a repository's rows only when `state.content_version =
versions.version`, `state.extractor_version` equals the current version, and
`state.row_count` is not NULL. One snapshot covers the comparison and the
rows. Every other repository is served live and counted by reason:

- `missing`: no state row, or no version row. With the inner join a missing
  version row never matches. Only a repository with no content since the
  seed lacks one, and its live answer is one empty list.
- `stale_version`: the versions differ.
- `stale_extractor`: the extractor version differs.
- `failed`: `row_count` is NULL, so no derive has succeeded yet.

The read-side name step (G1) runs per repository:

1. For a `name_scoped` row, `path = normalizeRepoScopedPath(raw_path, name)`.
   For any other row, `path = raw_path`.
2. Set `source_repo = name` on every row.

The merge then follows the live loader, which keeps one dedup map across all
sources: the rows of every repository (the anchor served live, the stored
repositories and the live fallbacks) merge in source order. The merged set is
deduplicated once on `(path, source_repo, relative_path, evidence_kind)` and
sorted once with `evidence_kind` as the fourth key. A per-repository dedup
would differ when two related repositories share one graph name, or when the
anchor also appears in the related list. Two raw scoped paths of one
repository that normalize to one path also collapse here.

The reducer runs the walker, in the pattern of
`cmd/reducer/infra_inventory_reconcile_wiring.go`:

- It finds work through the dirty join, failed derives and old extractor
  versions first, then walks all repositories with a cursor.
- K workers partition the work by `repo_id`. No two workers derive one
  repository at the same time. **Proposed**: K defaults to 4 through
  `ESHU_CONFIG_ARTIFACT_WALKER_WORKERS`.
- Each derive uses the protocol of section 3.
- After 5 failures the walker skips a repository until its next content
  Write. `/admin/status` lists skipped repositories. Reads stay live for them.
  With the Write-path bound of section 3, such a repository stays on the live
  path until an operator acts or the extractor changes.
- The backfill is the first full walk. It records the marker when every
  repository has a current state row.
- An `extractor_version` bump re-derives every repository. The fallback
  window is about (S1 per-repository derive time) x 815 / K. The figure 815
  is the repository count ops-qa listed on 2026-10-10 (local kit note
  `7126-story-span-probe-20261010.md`, not in the repository).

## 5. Shared `content_files` SQL and the package home (F6)

`ContentReader`, `ListRepoFiles` and `GetFileContent` live in package
`go/internal/query` today. PR1's `NonBlankRepoFilePaths` and PR2's batched
read sit beside them. The derive runs in `storage/postgres`, which must not
import a query handler package.

PR3a moves one SQL constant and one row mapper per statement (list, single
read, non-blank check, batched read) into a neutral package that both the live
reader and the derive import. The extractors and the per-repository function
move with them. The neutral package defines its own file type. The query
package adapts `querycontract.FileContent` to it in one line, so
`storage/postgres` does not inherit the dependencies of `querycontract`.

`go list -deps` at `b7ca5f2f0a` shows no import cycle in either direction
today. Ownership, not a cycle, is the reason for the neutral package.

## 6. Shim details

1. **S0 name-free rows** (local Go test). Over the fixture corpus and PR1's
   generated trees, `BuildRepositoryConfigArtifacts(name, files)` must equal
   the name-free build plus the read-side name step, for three or more names.
   The cases must include:
   - A kustomize row and an Ansible row whose path has the name as a
     component, where the step must not apply.
   - An HCL row and a compose row where the step must apply.
   - Two raw scoped paths that collapse to one path at read.
   - An empty name.
   - At the loader level: two related repositories with one graph name, and
     the anchor repeated in the related list. The merged answer must equal
     the live loader's.

   A second guard pins that each `evidence_kind` maps to exactly one
   `name_scoped` value. Pass: 0 differing rows.
2. **S2 read plan** (local PostgreSQL 18). Seed 815 repositories with about 21
   rows each, plus long paths. Use `PREPARE`/`EXECUTE` with
   `plan_cache_mode=force_generic_plan` for the reader's three-table join with
   300 ids and for the dirty join over 815 repositories. Pass: primary-key
   index scans only, execution at most 5 ms, no sort or hash beyond the `ANY`.
3. **S3 fence cost** (local PostgreSQL 18, in the shape of
   `content_writer_infra_fence_bench_live_test.go`, interleaved rounds,
   medians with ranges, first round discarded). Arm A has no trigger. Arm B
   has the version trigger. Arms 1-6 must pass before PR3b-1 builds the
   trigger:

   | Arm | Shape | Pass |
   |---|---|---|
   | 1 | Full Write, 5,000 files, 500-row batches, production-width content | B at most A + 5% on Write wall time. A primary-key-only micro-shim at most 0.5 us per row |
   | 2 | Delta Write, 1 file changed, 3 tombstones | B at most A + 5% |
   | 3 | Retention prune, 100,000 rows over 50 repositories, one transaction | Row-level and `OLD TABLE` shapes. The chosen shape at most 5% of the prune phase. Else the other shape. Else an explicit bump in retention |
   | 4 | One 50,000-row single-statement insert | B at most A + 5% |
   | 5 | Two concurrent Writes, one `repo_id`, 5,000 files each | B at most A + 10% per Write. Lock waits on the version row at most one batch time. 0 deadlocks in 20 rounds |
   | 6 | Version-row churn, counted by `xmin` changes or `n_tup_upd` | Exactly one rewrite per repository per content transaction. Expected counts: arm 1 = 10 (ten upsert batches). Arm 2 = 2 (one tombstone delete transaction, then one upsert transaction). Arm 3 = 1 per affected repository (50). Arm 4 = 1 per repository in the statement |
   | 7 (**Proposed**) | Two overlapping multi-repository statements, 20 rounds | 0 deadlocks. Arm 3 also records the Write lock wait against the ops-qa p95 of `eshu_dp_generation_retention_scope_lock_hold_seconds` |

   The re-ruling wrote "1 for shapes 2-4". That count is off for arm 2: a
   delta Write deletes its tombstones and upserts its changed file in two
   separate transactions (`deleteContentFilesBatch`, then
   `upsertContentFileBatches`). The coordinator tells the arbiter.

4. **S6 cap census** (ops-qa, read-only). For all 815 repositories, count
   repositories with more than 50 candidates and more than 5,000 files, and
   the uncapped candidate total.
5. **S1 derive cost** (local, PR1, PR1b and PR2 stacked). Use the real
   `ContentWriter.Write` on the golden corpus and on the 300-repository,
   8,000-file synthetic set of the CPU bench. Pass: at most 10% of content
   stage p95, at most 2 s for the worst repository, and HCL parse CPU near the
   bench rate of 94 ms per MB.
6. **S4 equality** (PR3b gate). Symmetric difference `0/0` for every
   golden-corpus repository, plus the two loader-level cases of S0 (one graph
   name for two repositories, the anchor repeated in the related list). On
   ops-qa, shadow mode plus an offline differential under its own
   authorization.

PR3b-1 also adds live interleaving tests: the R-1 sequence of section 7 (a
Write with three batches and a walker derive between batches 1 and 2), the
two-scope sequence, and the prune sequence. After both sides finish, either
the state version equals the current version with W's rows, or the state
version is behind and the reader falls back.

## 7. Decided: R-1, the lost update that replaced the first fence

The first ruling used a dirty mark with a sequence number. Aware writers set
the mark before their content, and a derive deleted the mark only if its
sequence was unchanged. A Write's content lands as several commits over
seconds, so a mark set before the content does not cover it:

1. Write W marks `mark_seq = 6` before its content.
2. The walker marks `mark_seq = 7` and reads 7.
3. The walker lists and reads content before W's content batches commit.
4. The walker deletes the mark with `mark_seq = 7`. It succeeds, and the rows
   lack W's content.
5. W's content commits. W's derive finds no mark, deletes 0 rows, and rolls
   back.
6. The stored rows are stale, no mark remains, and readers trust them.

Two Writes for one `repo_id` from two scopes give the same result. The hole
exists for any token written before, not with, the content. The re-ruling
adopted the version fence of sections 2 and 3, where the bump commits with the
content. It rejected two alternatives:

- A version computed from `content_files` itself (`MAX(xmin)`,
  `MAX(indexed_at)`, `count(*)`). A pure delete moves neither maximum, and the
  reader would aggregate up to 5,000 rows for each of about 300 repositories
  per request.
- An advisory lock around the whole Write. It serializes a Write against the
  walker and retention for the parse and all batches.
