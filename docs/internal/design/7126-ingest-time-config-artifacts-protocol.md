# Config-artifact read model: derive protocol and fence (#7126)

This companion belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md).
It holds the tables, the fence, the derive protocol, the walker, the shared
SQL and the shim details that the arbiter ruling of 2026-10-10 decided. Section
7 records one open issue (R-1) in the ruled protocol. It needs a re-ruling
before the mark protocol is built.

## 1. Tables

Names are proposals. The migration number is chosen at build time.

| Table | Key | Columns |
|---|---|---|
| `config_artifact_repo_state` | `repo_id` | `extractor_version` (of the stored rows, NULL before the first success), `row_count`, `files_listed`, `candidate_count`, `candidates_read`, `list_truncated`, `scope_id` and `generation_id` (advisory), `derived_at`, `failure_count`, `last_error_class`, `last_failure_at` |
| `config_artifact_paths` | `(repo_id, raw_path, relative_path, evidence_kind)` | `name_scoped` |
| `config_artifact_dirty_repos` | `repo_id` | `mark_seq` (from one global sequence), `mark_txid`, `marked_at`, `reason` |
| `config_artifact_backfill_markers`, `config_artifact_reconcile_cursor` | name | as in migration 109 |

- `raw_path` is the path value before `normalizeRepoScopedPath`. `name_scoped`
  is true for rows built through `appendConfigArtifactRow` (HCL and compose)
  and false for kustomize and Ansible rows.
- A deleted and re-inserted mark must never repeat a `mark_seq` value, so the
  value comes from a sequence, not from a per-row counter.
- Key width (G8): four TEXT columns can exceed the btree entry limit for a
  pathological HCL expression. S2 seeds long paths. If any row exceeds the
  limit, the key becomes a digest column and the four columns stay as data.
  Otherwise the derive records `last_error_class = row_too_wide` and the
  repository stays on the live path.
- A successful derive resets `failure_count` to 0.

## 2. Writer sessions and the fence trigger (F2)

A new setting, `eshu.config_artifact_writer = 'derive'`, marks a connection as
derive-aware. `ingester`, `projector` and `bootstrap-index` set it on the
connections their content writer uses. These binaries write `content_files`
only through `ContentWriter.Write`. The reducer and the API never set it.

Triggers on `content_files` follow the shape of migration 109:

- INSERT and UPDATE: statement-level, with transition tables, guarded by
  `WHEN (current_setting('eshu.config_artifact_writer', true) IS DISTINCT FROM 'derive')`.
- DELETE: row-level, with the session test first.
- TRUNCATE: as migration 109 handles it.

Each trigger bumps the mark once per unaware transaction per repository:

```sql
INSERT INTO config_artifact_dirty_repos AS d (repo_id, mark_seq, mark_txid, marked_at, reason)
SELECT r.repo_id, nextval('config_artifact_mark_seq'), txid_current(), now(), 'unaware_write'
FROM (SELECT DISTINCT repo_id FROM new_rows) AS r
ON CONFLICT (repo_id) DO UPDATE
SET mark_seq = EXCLUDED.mark_seq, mark_txid = EXCLUDED.mark_txid,
    marked_at = EXCLUDED.marked_at, reason = EXCLUDED.reason
WHERE d.mark_txid IS DISTINCT FROM EXCLUDED.mark_txid;
```

`nextval` must run after the `DISTINCT`, as above. A `SELECT DISTINCT repo_id,
nextval(...)` keeps one row per sequence value, not one per repository. When
the `WHERE` is false, the upsert still takes the row lock, so a conditional
delete waits for the writer to commit. Dead tuples: one per repository per
unaware transaction.

Generation retention runs in the reducer, so its `content_files` prune fires
the trigger. If S3 shows the trigger costs more than 5% of the prune phase,
retention writes explicit marks in its own transaction, in ascending
`repo_id` order, instead.

The lock-only upsert of migration 109 (`ON CONFLICT DO UPDATE ... WHERE
false` with no bump) must not be used for this mark. With a Go parse outside
the transaction it loses an update:

1. Aware Write W1 marks `mark_seq = 5` and commits its content.
2. W1's derive reads `mark_seq = 5` and the files.
3. Unaware writer U writes content. A lock-only trigger takes the mark row
   lock and leaves `mark_seq = 5`.
4. W1's conditional delete waits for U, re-evaluates, still sees 5, and
   deletes the mark.
5. The stored rows miss U's content, and no mark remains to repair them.

Migration 109 does not have this hole, because its derive is SQL in the same
transaction, after the mark delete.

## 3. The derive protocol and its failure semantics (F1, G2, G6)

A Write derive and a walker derive run the same steps:

1. **Mark.** A Write upserts the mark with a bump before its first content
   statement (`reason = write`). The walker upserts it with a bump at the
   start (`reason = reconcile`, `backfill` or `version`).
2. **Read the mark.** After the content commits, read `mark_seq` as `s0`.
3. **Derive with no lock.** List the files, read the content of the selected
   candidates, and run the extractors. No transaction is open.
4. **Write in one short transaction.** Take the repository advisory lock.
   Run `DELETE FROM config_artifact_dirty_repos WHERE repo_id = $1 AND
   mark_seq = $2` with `s0`. If it deletes 0 rows, roll back and keep the old
   rows: another writer owns the next derive. Otherwise delete the old rows,
   insert the new rows, upsert the state row, and commit.

Failure semantics:

- The derive recovers a panic and runs under a deadline. The proposed
  variable is `ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT`, default 10 s.
- On an error, a panic or a timeout it counts
  `derives_total{outcome=error|panic|timeout}`, increments `failure_count`,
  records `last_error_class` and `last_failure_at`, leaves the mark, logs
  `config_artifacts.derive.failed` at ERROR with the repository id, and
  returns nil from `Write`.
- The content rows are already committed when the derive starts. A failed
  derive cannot roll them back, and it never makes the projector retry the
  generation.

This departs from `deriveInfraInventory`, which fails the Write. That choice
is right for a SQL-only copy. It is wrong for a Go parse over arbitrary
content, because a deterministic parse bug would dead-letter the content
projection and lose graph and content truth.

The lock is held only for step 4. Retention takes every affected repository
lock in order, so a lock held across a 0.1-2 s parse would delay it.

## 4. The walker (F5)

The reducer runs the walker, in the pattern of
`cmd/reducer/infra_inventory_reconcile_wiring.go`.

- It visits repositories in this order: dirty marks, then state rows with an
  old `extractor_version`, then a cursor walk of all repositories.
- K workers partition the work by `repo_id`. K is environment-tunable,
  default 4. No two workers derive one repository at the same time.
- Each derive uses the protocol of section 3.
- After 5 failures the walker skips a repository until its next content
  Write. `/admin/status` lists skipped repositories. Reads stay live for them.
- The backfill is the first full walk. It records the marker when every
  repository has a current state row.
- An `extractor_version` bump re-derives every repository. The fallback
  window is about (S1 per-repository derive time) x 815 / K.

## 5. Shared `content_files` SQL and the package home (F6)

`ContentReader`, `ListRepoFiles` and `GetFileContent` live in package
`go/internal/query` today. PR1's `NonBlankRepoFilePaths` and PR2's batched
read sit beside them. The derive runs in `storage/postgres`, which must not
import a query handler package.

PR3a moves one SQL constant and one row mapper per statement (list, single
read, non-blank check, batched read) into a package that both the live reader
and the derive import. The extractors and the per-repository function move
with them.

`go list -deps` at `b7ca5f2f0a` shows no import cycle in either direction
today. Neither `query/querycontract`, `query/repositoryartifacts` nor
`internal/content` depends on `storage/postgres`, and `storage/postgres` does
not depend on `query/querycontract`. Ownership, not a cycle, is the reason
for the neutral package.

## 6. Shim details

1. **S0 name-free rows** (local Go test). Over the fixture corpus and PR1's
   generated trees, `BuildRepositoryConfigArtifacts(name, files)` must equal
   the name-free build plus the read-side name step, for three or more
   names. The names include an empty name and a name that is a path component.
   The corpus must hold a kustomize or Ansible row whose path contains that
   component, so that a name step applied to every row fails the test.
   Pass: 0 differing rows.
2. **S2 read plan** (local PostgreSQL 18). Seed 815 repositories with about 21
   rows each, plus long paths. Use `PREPARE`/`EXECUTE` with
   `plan_cache_mode=force_generic_plan` for the row read with 300 ids, and for
   the state and dirty reads. Pass: primary-key index scans only, execution at
   most 5 ms, no sort or hash beyond the `ANY`.
3. **S3 fence cost** (local PostgreSQL 18, in the shape of
   `content_writer_infra_fence_bench_live_test.go`). Arms: no trigger, fenced
   session, unaware session. Shapes: a 500-row `content_files` upsert with
   production-width content, the retention prune at 100,000 rows, and one
   unaware 50,000-row insert. Add the arm that R-1 proposes (the trigger for
   every session). Pass: fenced at most 5% on the upsert, one mark rewrite per
   transaction per repository, and the prune at most 5% of the prune phase.
4. **S6 cap census** (ops-qa, read-only). For all 815 repositories, count
   repositories with more than 50 candidates and more than 5,000 files, and
   the uncapped candidate total.
5. **S1 derive cost** (local, PR1, PR1b and PR2 stacked). Use the real
   `ContentWriter.Write` on the golden corpus and on the 300-repository,
   8,000-file synthetic set of the CPU bench. Pass: at most 10% of content
   stage p95, at most 2 s for the worst repository, and HCL parse CPU near the
   bench rate of 94 ms per MB.
6. **S4 equality** (PR3b gate). Symmetric difference `0/0` for every
   golden-corpus repository. On ops-qa, shadow mode plus an offline
   differential under its own authorization.

## 7. Open issue R-1: a lost update between two aware derives

Found while writing this companion. It needs an arbiter re-ruling before
PR3b-1 builds the mark protocol. The rest of PR3b-1 can proceed.

The ruled protocol (section 3) can lose an update when a Write and the walker
overlap on one repository. Both are aware, so no trigger fires:

1. Write W marks `mark_seq = 6` before its content.
2. The walker marks `mark_seq = 7` and reads `s0 = 7`.
3. The walker lists and reads content before W's content batches commit.
4. The walker deletes the mark with `mark_seq = 7`. It succeeds, and the rows
   lack W's content.
5. W's content commits. W's derive finds no mark, deletes 0 rows, and rolls
   back.
6. The stored rows are stale, no mark remains, and readers trust them.

Two Writes for one `repo_id` from two scopes (migration 169) give the same
result. Using the `mark_seq` that a writer's own mark returned does not help.
A crash between a pre-write mark and the content commit has a similar gap.

Proposed correction, for the arbiter:

- The trigger bumps a per-repository content version for every session, once
  per transaction per repository. Aware sessions are not exempt. A content
  change and its bump then commit atomically.
- The state row records the version that the derive read before its file
  list. A reader trusts the rows only when that version equals the current
  version.
- Under the repository lock, a derive writes only if the stored version is
  older than the version it read. An older derive never replaces a newer one.
- No pre-write mark and no conditional delete are needed. The writer session
  setting is not needed for this fence.
- A failed derive leaves the stored version behind the current version, so
  readers fall back live, as the ruling requires.
- Cost: `content_files` batches run one after another inside one Write
  (`upsertContentFileBatches`), so a per-transaction bump adds no intra-Write
  contention. S3 measures this arm.
