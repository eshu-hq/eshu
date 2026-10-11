# Config-artifact read model: derive protocol and fence (#7126)

This companion belongs to
[Derive repository config artifacts when content is written](7126-ingest-time-config-artifacts.md).
It holds the tables, the content-version fence, the derive protocol, the
walker, the shared SQL and the shim details. Four arbiter rulings of
2026-10-10 (the ruling, the re-ruling, re-ruling 2 and re-ruling 3) decided
the fence, the derive protocol, the reader condition, the name step, the row
key and the S3 arms. The S3 ruling of 2026-10-11 decided the trigger names,
the arm 3 and arm 5 reruns and the PR3b-1 split. An item marked **Proposed**
is the author's addition and is not part of any ruling. Section 7 keeps the
lost-update sequence (R-1) that made the re-ruling replace the first fence.

## 1. Tables

Names are proposals. The migration number is chosen at build time.

| Table | Key | Columns |
|---|---|---|
| `config_artifact_content_versions` | `repo_id` | `version` (BIGINT), `bumped_xid` (xid8), `bumped_at` |
| `config_artifact_repo_state` | `repo_id` | `content_version` (the version the derive read before its list), `extractor_version` (integer), `row_count` (NULL until the first success), `files_listed`, `candidate_count`, `candidates_read`, `list_truncated`, `scope_id` and `generation_id` (advisory), `derived_at`, `failure_count`, `last_error_class`, `last_failure_at`. **Proposed** for the walker's skip rule: `last_failure_extractor_version` |
| `config_artifact_paths` | `(repo_id, row_digest)` | `raw_path`, `relative_path`, `evidence_kind`, `name_scoped`, `row_digest` (generated) |
| `config_artifact_backfill_markers`, `config_artifact_reconcile_cursor` | name | as in migration 109 |

- The trigger migration (PR3b-1b, after the table migration) seeds one
  version row with `version = 0` for each `SELECT DISTINCT repo_id FROM
  content_files`, as migration 169 does its backfill. The walker's universe is
  then O(repositories), not a scan of `content_files`. Order (S3 ruling):
  create the four triggers first, then seed with `ON CONFLICT DO NOTHING`, in
  one transaction. `CREATE TRIGGER` blocks content writes until that
  transaction commits, so no write can land between the seed scan and the
  trigger. The seed scan duration is then a content-write stall, and the
  migration proof must time it.
- **Proposed** rule: delete or reset a version row only together with the
  repository's state row, in one transaction. A version row re-created while
  a state row survives would count up from 1 again. After enough bumps it
  would equal the old `state.content_version`, and readers would trust stale
  rows. PR3b-1a adds a test that no Go statement deletes from the versions
  table except a documented retirement path. The repository retirement design
  of #7766 (draft PR #7871) requires a disposition for every repo-keyed
  table. It must give the versions and state tables one shared disposition.
- `raw_path` is the path of the build with no name, stored verbatim. For a
  scoped row it is the cleaned path, before the strip step (section 4). Each
  row's construction site sets `name_scoped`: true in
  `appendConfigArtifactRow` (HCL and compose), false elsewhere (kustomize and
  Ansible). The flag is a function of `evidence_kind`. S0 found 16 kinds, 10
  scoped and 6 unscoped, none in both. A guard test reads both call-site
  families from source and fails if a kind is in both sets or in neither.
- The dirty set is a join, not a table: no state row, or
  `state.content_version` different from `version`, or an old
  `extractor_version`, or `failure_count > 0`. The walker uses it to find
  work. It is wider than the reader's test in section 4: rows that match the
  current version stay trusted after a later failed derive, as they are still
  exact.
- Key width (G8), measured by S2. A primary key of the four TEXT columns
  fails with `54000 index row size 2712 exceeds btree version 4 maximum 2704`
  at a 2,610-byte incompressible path (2,609 passes). A compressible path of
  100,000 bytes passes. The limit depends on the data, so a length limit is
  rejected.
- Key (re-rulings 2 and 3): `(repo_id, row_digest)`, with `row_digest` a
  stored generated column, defined once in the schema and never in Go:

  ```sql
  -- IMMUTABLE is true per database: server_encoding is fixed for the
  -- lifetime of a database, and a dump and restore recomputes generated
  -- columns on load, so a stored digest never crosses an encoding.
  CREATE FUNCTION config_artifact_utf8(t text) RETURNS bytea
  LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
  RETURN convert_to(t, 'UTF8');

  row_digest BYTEA GENERATED ALWAYS AS (sha256(config_artifact_utf8(
      length(path)::text || ':' || path ||
      length(relative_path)::text || ':' || relative_path ||
      evidence_kind))) STORED
  ```

  The fields are length-prefixed because a path can contain `|`. The four
  columns stay as data. A schema test recomputes the digest in SQL for every
  seeded row and compares. No Go code computes it. S2 measured the paths read
  at 2.14 ms for 300 ids with the digest key (Bitmap Heap Scan) and 5.2 MB
  for 17,175 rows, against 7.0 MB for the four-column key. The digest-key
  reader join itself is not measured. S3 arm 1 includes the cost of the
  generated column.
- Why the wrapper: `convert_to` and `textsend` are STABLE, so the plain
  expression fails with `generation expression is not immutable` (evidence
  appendix, section 5). Rejected (re-ruling 3): a Go-filled column (two
  writers of one row shape, and the schema cannot refuse a wrong digest),
  `md5` (128 bits, and not available on the FIPS-style QA build), a
  `text::bytea` cast (none exists), `textsend` (STABLE), and pgcrypto (an
  extension for one column). A `row_too_wide` fallback and a length
  precheck are rejected (re-ruling 2). SQLSTATE classification into
  `last_error_class` stays as the generic failure path.
- UTF8 invariant (re-ruling 3): PR1 already assumes a UTF8 database. PR3 adds
  one startup check. Each binary that derives (ingester, projector,
  bootstrap-index, reducer) and the API run `SHOW server_encoding` once at
  start and log `postgres.server_encoding` at INFO. On a value other than
  `UTF8` they log at ERROR, skip every derive with
  `derives_total{outcome=encoding_unsupported}`, and report
  `config_artifacts = not_installed` with the reason in `/admin/status`. The
  same check tests the four fence triggers (section 2): a missing one gives
  outcome `fence_missing` and reason `version_fence_missing`. The content
  Write proceeds, and reads stay live.
- Autovacuum (re-ruling 2): each derive deletes and re-inserts a
  repository's rows. Without a vacuum, S2 measured the 300-id read at 2.47 ms
  (9,202 `Heap Fetches`) against 1.76 ms after a vacuum (0 `Heap Fetches`),
  because an Index Only Scan needs the visibility map. The migration sets
  `autovacuum_vacuum_scale_factor` at most 0.02 on `config_artifact_paths`.
  **Proposed** note, not measured: the digest key reads the heap anyway, but
  dead tuples still cost.

## 2. The content-version trigger

Triggers on `content_files` fire for every session. No session setting, no
pre-write mark and no mark table exist.

- INSERT and UPDATE: statement-level, `REFERENCING NEW TABLE`. An `INSERT ...
  ON CONFLICT DO UPDATE` fires both. The guard below makes the second a no-op.
- DELETE: row-level, with the same guard. S3 may replace it with a
  statement-level `REFERENCING OLD TABLE` trigger.
- TRUNCATE: statement-level, bumps every version row through the same
  once-per-transaction guard.
- Names and order (S3 ruling). PostgreSQL fires the triggers of one event on
  one relation in alphabetical order by name, and row-level AFTER triggers
  before statement-level AFTER triggers (PostgreSQL 18 documentation, trigger
  definition). The fence triggers are `content_files_version_fence_insert`,
  `_update`, `_delete` and `_truncate`, with that prefix. Each sorts after
  the migration 138 triggers `content_files_secret_lines_insert` and
  `content_files_secret_lines_update` ('v' follows 's'), so the fence fires
  last. The version row lock lasts from the bump to the commit. Firing last
  makes that hold a commit, so two writers of one `repo_id` overlap their
  secret-line derivation and do not serialize it on the version row. The
  foreign key of migration 138 uses row-level `RI_ConstraintTrigger_*`
  triggers, which sort first, so its cascade runs before the bump. A schema
  test reads `pg_trigger` for `content_files` ordered by `tgname`. It asserts
  that the fence is the last statement-level AFTER trigger for INSERT and for
  UPDATE, and that the migration 138 names are present.

The body bumps each repository once per transaction. Re-ruling 2 accepted
the `ORDER BY`:

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
- Lock order (re-ruling 2): a statement-level trigger takes the version row
  locks of a multi-repository statement in the order of its `SELECT`. `ORDER
  BY r.repo_id` makes that order ascending, so two statement-level
  multi-repository statements cannot deadlock on version rows.
- Trigger timing (re-ruling 3): a row-level AFTER trigger fires at the end
  of the statement, after the statement has locked every row it touches. So
  every path that bumps a version row, under either DELETE shape, locks
  content rows first and the version row last. A session that waits on a
  version row holds only content rows. No cross-table cycle through a
  version row exists for a prune against a Write, with or without a shared
  doomed key. This does not exclude a cycle on content rows alone: two
  multi-row statements that lock overlapping `content_files` rows in opposite
  order can deadlock, as they can today without the trigger.
- Surviving hazard: two concurrent multi-repository statements that bump
  through row-level triggers take version rows in their own row order, so
  opposite orders can deadlock. PostgreSQL aborts one, and the loser retries
  (a prune at its next cycle, a Write through the projector's retry of the
  generation). The `ORDER BY` prevents this on every statement-level path.
  The multi-repository deleters in production are the retention prune (one
  wiring in `go/cmd/reducer/generation_retention_wiring.go`), TRUNCATE
  (statement-level, so the `ORDER BY` applies) and manual SQL. The retention
  runner (`go/internal/reducer/maintenance/retention/runner.go`) has no lock
  or lease. Its candidate selection uses `FOR UPDATE ... SKIP LOCKED` in
  `generation_retention_sql.go`, so two reducer replicas run concurrent
  prunes over disjoint scope sets. Two scopes can share one `repo_id`
  (migration 169), so with more than one reducer the hazard is reachable.
- The DELETE shape is a cost choice only: S3 arm 3, at most 5% of the
  `prune_content_files` phase on the real retention store. At equal cost
  prefer the `OLD TABLE` shape, because the `ORDER BY` then covers the prune
  too.
- The row lock lasts until the content transaction ends. For a Write that is
  one 500-row batch. For retention it lasts from the end of the prune's
  `content_files` DELETE statement to the end of the retention transaction.
  AFTER row triggers fire at the end of the statement, and the retention
  transaction deletes `content_files` in one statement, so it takes the
  version row locks after all its content row locks. A Write batch of a
  pruned repository waits for the retention commit, which also runs the
  ledger and `scope_generations` deletes. That wait sits inside the existing
  `eshu_dp_generation_retention_scope_lock_hold_seconds` contract. PR #7910
  is merged (`ba95c9a973`) and was re-checked on 2026-10-11: the content
  prune is still one DELETE statement, after the infra advisory locks and
  before the ledger and `scope_generations` deletes, so this section holds.
- `content_files` upserts and deletes run one after another inside one Write
  (`upsertContentFileBatches` in `content_writer_upserts.go`,
  `deleteContentFilesBatch`), so one Write touches the version row from one
  transaction at a time. Entity batches (`upsertContentEntityBatches`) run
  concurrently, but they never write `content_files`. Two concurrent Writes
  of one `repo_id` (two scopes) wait on each other per batch. S3 arm 5
  measures this.
- Generation retention's prune fires the DELETE trigger. Retention needs no
  explicit code, unless the arm 3 rerun fails both shapes (fallback below).
  Every writer, including an older binary or manual SQL, bumps the version,
  so no rollout rule exists.

**Fallback (Proposed).** It applies only if the arm 3 rerun fails both
shapes, and it needs an F2 amendment by ruling (without it, retention pays
the trigger): F2 says "no session setting", and the fallback adds a
transaction-local one. "An explicit bump in retention" must not mean "drop
the DELETE trigger": every other deleter must keep bumping. The design:

1. Retention's content statement bumps itself. The query keeps its CTEs and
   adds `deleted AS (DELETE FROM content_files AS t USING doomed AS d WHERE
   ... RETURNING t.repo_id)`. The trigger body above then runs over `SELECT
   DISTINCT repo_id FROM deleted ORDER BY repo_id`, in the same statement, so
   the bump commits with the delete. `RETURNING t.repo_id` carries one text
   per row, not the tuple. `RowsPruned["content_files"]` stays the DELETE
   count.
2. Lock order. The CTE completes before the outer INSERT reads its set, and
   `ORDER BY` needs the whole set, so the statement locks content rows first
   and version rows last, ascending, as the trigger does today. The `Mirror`
   advisory lock of a Write is a separate transaction, so no Write holds a
   content or version row while it waits for the lock retention holds.
3. The DELETE trigger is row-level and gated: `FOR EACH ROW WHEN
   (current_setting('eshu.config_artifact_delete_bump', true) IS DISTINCT
   FROM 'self')`. A false row-level WHEN queues no event (migration 109). A
   WHEN on a transition-table trigger does not avoid the capture (migration
   138), so the gated trigger cannot be `OLD TABLE`. Retention runs `SET
   LOCAL` of that setting to `'self'` just before its statement. Never `SET`:
   a leaked session value silences bumps for another binary.
4. Four tests. (a) After the retention transaction commits, the setting is
   empty. (b) Retention bumps once per affected repository (arm 6: 1). (c)
   Seeded RED: remove the self-bumping INSERT with the gate on, and the test
   fails because the versions stay unchanged. (d) A source guard: no
   production statement sets the setting outside `GenerationRetentionStore`.
5. Every other deleter keeps the trigger (inventory: evidence appendix,
   section 8). A missed deleter is silent and persistent: versions stay
   equal, readers trust the rows, and `config_paths` serves a path whose file
   is gone until the next Write of that repository or an `extractor_version`
   bump.

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
- On an error, a panic or a timeout it counts
  `derives_total{outcome=error|panic|timeout}`, increments `failure_count`,
  records `last_error_class` and `last_failure_at`, logs
  `config_artifacts.derive.failed` at ERROR with the repository id, and
  returns nil from `Write`. A failure-only update never changes the rows or
  `content_version`.
- If the content moved, a failed derive leaves `state.content_version`
  behind the current version, so readers fall back live and the walker
  retries. If the content did not move, the rows stay exact and trusted, and
  only the walker retries.
- The content rows are already committed when the derive starts. A failed
  derive cannot roll them back, and it never makes the projector retry the
  generation.

This departs from `deriveInfraInventory`, which fails the Write. That choice
is right for a SQL-only copy. It is wrong for a Go parse over arbitrary
content, because a deterministic parse bug would dead-letter the content
projection and lose graph and content truth. The lock is held only for step 3.

## 4. The reader and the walker

The reader is one statement on the replica. It inner-joins (the join type
is **Proposed**) state and
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

1. For a `name_scoped` row, `path` is `raw_path` with the name prefix
   stripped. The read step uses the strip half of `normalizeRepoScopedPath`
   only. It must not clean again: the clean is not idempotent when the cleaned
   result has leading or trailing whitespace. For any other row,
   `path = raw_path`.
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
- After 5 failures the walker skips a repository. **Proposed** exit: the
  skip ends at the next content Write of the repository, which always
  derives, or when `extractor_version` changes. The state row records the
  extractor version of the last failure, and the skip applies only while it
  equals the current one. A successful derive resets `failure_count` to 0.
  `/admin/status` lists skipped repositories. Reads stay live for them while
  their rows are stale.
- The backfill is the first full walk. It records the marker when every
  repository has a current state row.
- An `extractor_version` bump re-derives every repository. The fallback
  window is about (S1 per-repository derive time) x 815 / K. The figure 815
  is the repository count the QA environment listed on 2026-10-10 (local kit note
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

   A second guard pins that each `evidence_kind` maps to exactly one
   `name_scoped` value. Pass: 0 differing rows.

   **Ran 2026-10-10: PASS**, 0 differing rows, with three seeded RED
   mutants (evidence appendix, section 4). The two loader-level merge cases
   (two related repositories with one graph name, and the anchor repeated in
   the related list) are not part of S0. They belong to S4 and the PR3c
   tests.
2. **S2 read plan** (local PostgreSQL 18). Seed 815 repositories with about 21
   rows each, plus long paths. Use `PREPARE`/`EXECUTE` with
   `plan_cache_mode=force_generic_plan` for the reader's three-table join with
   300 ids and for the dirty join over 815 repositories. Pass (re-ruling 2):
   execution at most 5 ms at 300 ids, and no scan of a table that grows with
   file count. The hash join over the versions table is accepted. Forced
   primary-key plans were slower (2.9-3.2 ms).

   **Ran 2026-10-10: PASS** on time, with the four-column key: reader join
   2.44 ms, dirty join 0.18 ms. With the digest key only the paths read was
   measured (2.14 ms). The digest-key reader join is not measured. Details
   and caveats are in section 4 of the evidence appendix.
3. **S3 fence cost** (local PostgreSQL 18, in the shape of
   `content_writer_infra_fence_bench_live_test.go`, interleaved rounds,
   medians with ranges, first round discarded). Arm A has no trigger. Arm B
   has the version trigger. Arms 1-6 must pass before PR3b-1b builds the
   trigger:

   | Arm | Shape | Pass |
   |---|---|---|
   | 1 | Full Write, 5,000 files, 500-row batches, production-width content. The derive's row insert includes the generated `row_digest` | B at most A + 5% on Write wall time. A primary-key-only micro-shim at most 0.5 us per row |
   | 2 | Delta Write, 1 file changed, 3 tombstones | B at most A + 5% |
   | 3 | Retention prune, 100,000 rows over 50 repositories, one transaction | Row-level and `OLD TABLE` shapes. Denominator: `PhaseDurations["prune_content_files"]` of the real `GenerationRetentionStore` statement, under its `SET LOCAL work_mem = '64MB'`, at 1 KiB inline and 8 KiB TOASTed bodies, both required. The chosen shape at most +5% on both classes. Else the other shape. Else the Proposed fallback of section 2 (needs an F2 amendment by ruling. Without it retention pays the trigger). `ScopeLockHold` delta is informational, against the QA-environment p95 of `eshu_dp_generation_retention_scope_lock_hold_seconds` (**Proposed**). Binding rerun spec: evidence appendix, section 7 |
   | 4 | One 50,000-row single-statement insert | B at most A + 5% |
   | 5 | Two concurrent Writes, one `repo_id`, 5,000 files each | B at most A + 10% per Write. The `version_row` lock wait at most one contended batch (B-last per-Write median / 10), with A as control, by wait class, plus an overlapping-keys run. 0 deadlocks in 20 rounds. Slower of the pair is informational. Binding rerun spec, with every bar: evidence appendix, section 7 |
   | 6 | Version-row churn, counted by `xmin` changes or `n_tup_upd` | One rewrite per repository per content transaction, never per Write (re-ruling 2). A Write sends tombstones and reaped paths through one delete list, chunked at 500. It expects ceil((tombstones + reaped paths) / 500) delete transactions. It adds one per 500-row upsert batch. Arm 1 = 10. Arm 2 = 2 (one delete chunk, one upsert batch). Arm 3 = 1 per affected repository. Arm 4 = 1 |

   The protocol needs two things only: every transaction that changes
   `content_files` bumps at least once, together with its content, and the
   derive reads `v0` after the Write's last content statement. The derive is
   the last step of a Write, so the second holds.

4. **S6 cap census** (QA environment, read-only). For all 815 repositories, count
   repositories with more than 50 candidates and more than 5,000 files, and
   the uncapped candidate total.
5. **S1 derive cost** (local, PR1, PR1b and PR2 stacked). Use the real
   `ContentWriter.Write` on the golden corpus and on the 300-repository,
   8,000-file synthetic set of the CPU bench. Pass: at most 10% of content
   stage p95, at most 2 s for the worst repository, and HCL parse CPU near the
   bench rate of 94 ms per MB.
6. **S4 equality** (PR3b gate). Symmetric difference `0/0` for every
   golden-corpus repository, plus two loader-level merge cases (one graph
   name for two repositories, the anchor repeated in the related list). On
   the QA environment, shadow mode plus an offline differential under its own
   authorization.

PR3b-1b also adds live interleaving tests, run before the trigger ships:

- The R-1 sequence of section 7 (a Write with three batches and a walker
  derive between batches 1 and 2), the two-scope sequence, and the prune
  sequence. After both sides finish, either the state version equals the
  current version with W's rows, or the state version is behind and the
  reader falls back.
- Arm 7 (re-ruling 2), a correctness test, not a timing arm, with three
  cases:
  1. **Proposed** split of the ruled case (a), so that the shape assertion
     cannot pass without a DELETE (the coordinator tells the arbiter):
     - 1a. Two multi-repository INSERT or upsert statements over overlapping
       repository sets, in opposite row orders. They are statement-level in
       every variant, so they test the `ORDER BY`: 0 deadlocks.
     - 1b. Two multi-repository DELETE statements over overlapping
       repository sets, in opposite row orders, under both DELETE shapes.
       This is the surviving hazard.
  2. A prune and a Write of one repository with disjoint keys.
  3. A prune and a Write that share one doomed key (re-ruling 3: a
     convergence check). The Write batch waits (`pg_stat_activity`
     `wait_event_type = 'Lock'`) until the retention transaction commits,
     then proceeds. No deadlock. After the Write's derive,
     `state.content_version` equals `versions.version` and the rows equal the
     live answer. **Proposed**: the test records in `pg_locks` which lock
     the Write waits on. With a shared key the first wait is likely on the
     content row the prune deleted, not on the version row that the ruling
     names.

  Cases 1b and 2 run under both DELETE shapes. For case 1b the `OLD TABLE`
  shape must show 0 deadlocks. The row-level shape must show 0 deadlocks, or
  every victim retried to a final state where `state.content_version` equals
  `versions.version` and the rows equal the live answer.

## 7. Decided: R-1, the lost update that replaced the first fence

The first ruling used a dirty mark with a sequence number. Aware writers set
the mark before their content, and a derive deleted it only if its sequence
was unchanged. A Write's content lands as several commits over seconds, so a
mark set before the content does not cover it:

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
