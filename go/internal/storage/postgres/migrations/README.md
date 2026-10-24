# internal/storage/postgres/migrations

The embedded Postgres bootstrap SQL files (`*.sql`) plus the Go leaf that
owns their `//go:embed` and ordering: `Definition`, `BootstrapDefinitions`,
and `Checksum`.

## Why this package exists

Before #6693 D2, root's `schema.go` carried `//go:embed migrations/*.sql`
even though the files it embeds live one directory away. That split the
migration content from the code that reads it. This leaf collapses the two:
the `.sql` files and the embed directive that reads them are one package, so
the directory is the single source of truth root already documented it as.

## Exported surface

- `Definition` -- one ordered bootstrap SQL payload: `Name`, `Path`, `SQL`,
  plus `Variant` and `FullChecksum` for the deferred-content-search-index
  variant (root's `BootstrapDefinitionsWithoutContentSearchIndexes` sets
  these; most callers leave them zero).
- `BootstrapDefinitions()` -- reads every `*.sql` file from the embed,
  derives `Name` by stripping the `NNN_` prefix and `.sql` suffix, and
  returns them sorted by `Path`. Files without an `NNN_` prefix are skipped.
- `Checksum(statement)` -- sha256 hex digest of a migration statement, the
  value recorded in the `eshu_schema_migrations` ledger's `checksum_sha256`
  column.

## Invariants

- **Never edit a shipped `.sql` file, for any reason, once it has merged.**
  Widen or fix behavior through a new migration instead. `#7002`:
  `093_cross_scope_completion_queue.sql` shipped, was edited in place twice
  (`#6785`, then `#6923`) to widen a `CHECK` constraint and a trigger's `WHEN`
  list, and both edits broke `applyTrackedDefinitions`'s checksum guard for
  every database that had already recorded 093 -- including a stuck QA
  rollout. `093` was restored to its originally shipped bytes; `112` and `120`
  are the correct pattern for that same widening (a new file, guarded by
  `pg_get_constraintdef`/`pg_get_triggerdef` `NOT LIKE` checks, that converges
  an existing database once and no-ops on every boot after). `112` and `120`
  are themselves now shipped and must never be edited either, including to
  "fix" the fresh-bootstrap comment at their own top: a fresh bootstrap runs
  `093` (original) then every later `.sql` file in path order, so the current
  full domain list lives in whichever upgrade file was added last (`120`
  today) -- see `checksum_alias.go` for the narrow,
  path-scoped exception that let already-applied databases from the `#6785`/
  `#6923` window keep working, and
  `../cross_scope_completion_schema_test.go`'s
  `TestCrossScopeCompletionSchemaCoversCatalogDomainsExactly` for the test
  that must track whichever file is current.
- `Path` stays exactly `go/internal/storage/postgres/migrations/<file>.sql`.
  The migration tracker (`schema_bootstrap_lock.go` in root) keys applied
  migrations by `path + variant + checksum_sha256`, so any change to `Name`,
  `Path`, `SQL`, or definition order makes bootstrap try to re-apply or
  diverge on migrations already applied to an existing database. See
  `embed_invariant_test.go`'s golden digest and
  `migration_checksum_manifest_test.go`'s per-file manifest (the latter names
  the exact file when one drifts or is deleted).
- The embed pattern is `*.sql` only. `embed.go`, `doc.go`, `README.md`, and
  `AGENTS.md` must never appear as definitions.
- No `.sql` file in this directory is added, removed, renamed, or edited by
  this package split -- the split only moves where the embed directive
  lives.
- Root's `BootstrapDefinitionsWithoutContentSearchIndexes` stays in root: it
  needs the content store's deferred DDL, which this leaf does not own.

## Catalog-only column and storage-parameter migrations

Migrations 148 (`delta_baseline_commit_sha`, #7319), 149
(`projection_write_started_at`, #7389), and 210
(`delta_union_base_commit_sha`, #8780) each add one nullable
`scope_generations` column with no default, backfill, CHECK, or index, so
`ADD COLUMN` changes only the catalog and its ACCESS EXCLUSIVE lock is bounded
by the runner's lock_timeout and retry. Migration 150 sets
`scope_generations` `fillfactor = 90` (SHARE UPDATE EXCLUSIVE, catalog-only):
existing pages are not rewritten, and new pages keep the free space that makes
the #7389 write-start marker a HOT update. Keep each of these a single
statement; see `docs/internal/evidence/7389-superseded-writer-overlay.md`.

## Keep table-locking DDL and bulk DML in separate files (#7961)

The migrator sends each file as one message (except the grandfathered 169,
phased since #7946: see the deployment note below), and PostgreSQL runs a
multi-statement message as one implicit transaction. A lock that an early
statement takes is held until the last statement commits. Do not put
table-locking DDL on an existing table in the same file as bulk DML. Put the
DDL in one migration and the backfill in the next, as 126 and 127 do.

Every migration must also stay re-runnable: the local supervisor re-applies
every definition on each start, with no ledger. For a new table, use the
split form: the table and its indexes with `IF NOT EXISTS` in migration N, and
the idempotent seed or backfill in migration N+1. Do not write a plain
`CREATE TABLE` to earn the exemption below. It is not re-runnable and fails on
the second local start.

The `migration-immutability` gate checks every added file
(`scripts/lib/migration_lock_shape.py`). It is a static check:

- Table-locking DDL: ALTER TABLE, ALTER INDEX, ALTER MATERIALIZED VIEW,
  CREATE INDEX or DROP INDEX without CONCURRENTLY, REINDEX or REFRESH
  MATERIALIZED VIEW without CONCURRENTLY, CREATE or DROP TRIGGER, CREATE or
  DROP RULE, DROP TABLE, CLUSTER, VACUUM FULL, TRUNCATE, and LOCK in any mode
  except ACCESS SHARE, ROW SHARE, or ROW EXCLUSIVE (those block neither
  readers nor writers). LOCK IN SHARE UPDATE EXCLUSIVE MODE is refused too: it
  blocks VACUUM, ANALYZE, and index builds for the whole backfill.
- Bulk DML: UPDATE, DELETE, MERGE, INSERT ... SELECT, a data-modifying CTE.
- It reads top-level statements, DO blocks (every IF, ELSIF, CASE, LOOP, and
  EXCEPTION branch), and same-file functions that an executed statement
  calls. An EXECUTE counts as DDL or DML when single-quoted or dollar-quoted
  text in its body names one. This is conservative: a RAISE message that
  names UPDATE beside an EXECUTE of DDL is refused too.
- Only a plain `CREATE TABLE` in the same file exempts DDL on that table.
  `CREATE TABLE IF NOT EXISTS` does not, because the table may already exist.
- It ignores statement order: DDL after the last DML is refused too.
- It does not see: trigger functions that the file's DML fires, functions
  from other migrations, single-quoted SQL function bodies, EXECUTE text that
  no literal contains, or a foreign key on an exempt new table, which locks
  the referenced table (SHARE ROW EXCLUSIVE).

There is no allowlist. 16 of the 188 shipped files have the refused shape.
They are immutable and are never re-checked.

## Migration 169 deployment note (#7961)

Do not edit 169, including its header. The file is immutable and #7946 pins
its checksum. The header says the backfill "takes row locks only" and is
"online-safe". Read it with these facts:

- Images with #7946 (main from merge 1ac63e871a): the tracked migrator runs
  169 in three committed phases on the advisory-lock connection.
  1. `add_column`: ACCESS EXCLUSIVE on `content_files`, catalog-only, held
     for under 1 ms. The lock request waits at most the 5 s `lock_timeout`
     per attempt, inside the 3-minute retry budget. While it waits, new
     readers queue behind it, so a long transaction on `content_files` at
     that moment can delay reads by up to 5 s per attempt. An autovacuum
     worker on `content_files` also holds a conflicting lock. An ordinary
     autovacuum yields only after `deadlock_timeout` (1 s by default). An
     anti-wraparound autovacuum does not yield, so the ALTER then waits for the
     full `lock_timeout` and retries. A heap-scaled run saw that
     case: add_column took 1,002 ms and a read waited 907 ms.
  2. `backfill`: the UPDATE holds ROW EXCLUSIVE and row locks only. Reads
     proceed.
  3. `analyze`: SHARE UPDATE EXCLUSIVE, which blocks neither reads nor
     writes.
- Images without #7946 (794b56a3fb and earlier): the migrator sends the file
  as one message. ACCESS EXCLUSIVE is then held through the whole backfill.
  On the #7961 fixture (157,422 rows, production index set), a read was
  blocked for 96.3 s. Do not roll such an image to an environment that has
  not applied 169.
- Any other tool that sends the whole file as one simple query has the same
  effect. Apply 169 only through the Eshu migrator.

On the production index set, the backfill costs about 0.65 to 0.89 ms and
58 KB of WAL per row. The figures and the live proof
(`../migration_169_reader_bound_live_test.go`) are in
`docs/internal/evidence/7760-generation-tag.md`.

## Migrations 188 to 190 deployment note (#7420)

188, 189 and 190 drop the per-fact foreign key
`fact_id REFERENCES fact_records(fact_id) ON DELETE CASCADE` from
`relationship_reference_candidate_keys`, `package_manifest_consumption_keys`
and `package_registry_identity_keys`. Read these facts before rollout:

- Deploy the #7420 PR1 image first (explicit key deletes at both retention
  fact delete sites, migrations 182 and 183). After the drop, those deletes
  are the only thing that removes key rows with their facts. The census
  `factkeys.OrphanCensusSQL` reads zero while they hold. If one release
  carries both PRs, a reducer from the older image that still runs retention
  after `db-migrate` leaves the key rows of the generations it prunes: run
  the census after the rollout and clear what it reports with
  `factkeys.OrphanCleanupSQL` (both quoted in the 3 AM runbook).
- Locks, measured on PostgreSQL 18.6: `DROP CONSTRAINT` takes ACCESS
  EXCLUSIVE on the key table and on `fact_records`. It is catalog-only (3.4 to
  17.6 ms on a 3M-fact fixture). A plain `ALTER TABLE ... DROP CONSTRAINT`
  locks the key table first, then waits for `fact_records` while it holds
  that lock. Ingestion writes a fact, then its keys, so the plain form
  deadlocks it (SQLSTATE 40P01). The migrator does not retry 40P01. Each file
  is therefore one `DO` block: it takes `LOCK TABLE fact_records IN ACCESS
  EXCLUSIVE MODE` (it waits while it holds nothing, bounded by the 5 s
  `lock_timeout`), then `LOCK TABLE <key table> IN ACCESS EXCLUSIVE MODE
  NOWAIT`, then the drop. It never waits while it holds a lock, so it cannot
  deadlock. If the key table is busy, the block fails at once with 55P03.
- While the block waits for `fact_records`, new readers and writers of
  `fact_records` queue behind it for up to 5 s per attempt, as with 169's
  `add_column` phase. Roll out when no long transaction holds `fact_records`.
- Each 55P03 is retried with backoff within the 3-minute lock retry budget
  (`schema_bootstrap_lock.go`). Each attempt logs a WARN `postgres schema
  migration waiting for a lock held by another session`
  (`event_name=bootstrap.postgres.migration.lock_wait`). If the budget runs
  out, `db-migrate` fails with
  `apply <name>: lock retry budget 3m0s for this bootstrap run exhausted after
  N attempts on <path> (...): ERROR: could not obtain lock on relation
  "<key table>" (SQLSTATE 55P03)`. It records nothing in the ledger and keeps
  the foreign key, so the next run retries; nothing is skipped
  (`TestFactKeyForeignKeyDropFailsLoudlyWhenKeyTableStaysBusyLive`).
- The local supervisor re-applies every definition at each start. The block
  finds no foreign key in `pg_constraint` and takes no table lock
  (`TestFactKeyForeignKeyDropMigrationsAreDeadlockFreeLive`). A plain
  `DROP CONSTRAINT IF EXISTS` would still take ACCESS EXCLUSIVE on the key
  table at every start.
- `migration_lock_shape.py check` classes `LOCK` as table-locking DDL. The
  files hold no DML, so each exits 0.
- Rollback: after the drop, a code-only rollback is safe only to the PR1
  image, which deletes the key rows explicitly. An image older than PR1
  relies on the cascade and would leave orphan key rows after each prune.
  Before rolling back that far, put the keys back with
  `docs/internal/evidence/7420-drop-key-sidecar-fks-reverse.sql` in psql.
  It starts with `SET lock_timeout = '5s';`: psql sets none, and without it
  a waiting lock request stalls every new writer of `fact_records` behind it
  until it is granted. Then census at zero, then `ADD ... NOT VALID` (SHARE
  ROW EXCLUSIVE on both tables, same lock order; held for the catalog change
  once granted, rerun the block on 55P03), then `VALIDATE CONSTRAINT` (SHARE
  UPDATE EXCLUSIVE on the key table, ROW SHARE on `fact_records`; one
  anti-join scan, 1.1 s for 3M key rows on the fixture, so size it from the
  table). Do not run the reverse procedure while an image with 188 to 190 can
  start the local supervisor, which drops the keys again. To roll forward
  later, run its step 4 (it deletes the 188 to 190 rows from
  `eshu_schema_migrations`) first: the tracked migrator skips recorded
  migrations, so without it the keys silently stay.

Evidence: `docs/internal/evidence/7420-drop-key-sidecar-fks.md`.

## Migration 191 deployment note (#7427)

191 drops the `generation_id` foreign key of `eshu_search_index_terms` to
`scope_generations` (the parent and its 64 leaves). Read these facts before
rollout:

- Deploy dependency: the image that carries 191 must also carry #7427 PR1
  and PR2 (the pre-delete, the fail-closed freshness check, the census and
  the sweep). After the drop, retention's search index pre-delete (drained generations only) and the
  orphan census and sweep are what remove terms rows; readers join the active
  generation, so orphans are never read.
- No quiesce step: retention and the search writers keep running (arbiter
  ruling). The block is deadlock-free against them; each attempt waits at
  most the 5 s `lock_timeout` or fails with 55P03, which the migrator
  retries. Quieting writers is only a contingency for repeated 55P03 beyond
  the retry budget.
- Autovacuum on any terms leaf is a 55P03 cause that does not go away by
  itself: the `NOWAIT` request never waits, so it never makes autovacuum
  cancel itself, and every retry inside that vacuum fails at once, naming the
  leaf. Find it as `backend_type = 'autovacuum worker'` in `pg_stat_activity`
  with `eshu_search_index_terms_pNN` in its query. Cancel a regular worker with
  `pg_cancel_backend(pid)` and rerun db-migrate; leave an anti-wraparound
  vacuum (`(to prevent wraparound)`) to finish. The roughly 0.4^12 all-fail
  estimate in the evidence note holds only for uncorrelated holders.
- After the deploy, run
  `docs/internal/evidence/7427-drop-terms-generation-fk-postdeploy.sql`
  (no terms key in `pg_constraint`, the 191 ledger row, the orphan census)
  and read `eshu_dp_search_index_orphan_generations{table}`.
- Locks, measured on PostgreSQL 18.6: ACCESS EXCLUSIVE on the terms parent,
  its 64 leaves and `scope_generations`, catalog-only, held 8-16 ms (8.2-9.7 ms
  from the `scope_generations` grant to commit end; 10.6-15.6 ms timed from
  before the catalog lookup). A plain
  `ALTER TABLE ... DROP CONSTRAINT` locks the terms tables first and then
  waits for `scope_generations`; retention takes the reverse order, so the
  plain form deadlocks it (SQLSTATE 40P01, not retried). The file is one `DO`
  block: it finds the key in `pg_constraint` (either name:
  `eshu_search_index_terms_generation_id_fkey` or, after the 039a cutover,
  `eshu_search_index_terms_shadow_generation_id_fkey`), waits for
  `scope_generations` while it holds nothing (bounded by the 5 s
  `lock_timeout`), then takes the terms parent with `NOWAIT`. A terms
  statement in flight makes it fail at once with 55P03, which the migrator
  retries within its 3-minute budget. While it waits, new users of
  `scope_generations` queue behind it for up to 5 s per attempt.
- A re-run finds no key and takes no table lock.
- Rollback: a code-only rollback is safe to the PR2 image. An image older
  than PR1 relies on the cascade; run
  `docs/internal/evidence/7427-drop-terms-generation-fk-reverse.sql` first
  (NOT VALID, then VALIDATE, with `SET lock_timeout`). Its last step deletes
  the 191 ledger row so a later roll-forward drops the key again.

Evidence: `docs/internal/evidence/7427-drop-terms-generation-fk.md`.

## Migrations 212 to 216: supply-chain runtime fallback indexes (#8835)

These five immutable files add separate partial indexes for padded workload,
service, and environment values. The first four index the exact `BTRIM` scalar
expression. Migration 212 indexes the `fact_id` of workload-identity rows whose
entity keys contain padded strings; the reader still checks the complete
normalized entity value. Its work grows with the number of padded identity rows.
The 100,000-padded-row fixture returned the selective hit and miss in 215.121
and 207.802 ms, within the existing 500 ms plan bound, while reading all
100,002 qualifying index entries. This is fixture evidence, not a bound for a
larger production population.

Each file contains one `CREATE INDEX CONCURRENTLY` so ordinary fact writers can
continue during an upgrade. The tracked bootstrap path checks for invalid
concurrent indexes, drops them, and retries the build; `IF NOT EXISTS` alone
would retain an invalid index. Preserve the raw-value indexes from migrations
079 and 080. An application-code rollback leaves the additive indexes in place
and the old raw predicates usable. If an operator deliberately drops one of
these new indexes on a disposable rollback, its recorded migration must also
be removed before the tracked migrator can recreate it.

## Verification

```bash
cd go && go test ./internal/storage/postgres/migrations -count=1
cd go && go test ./internal/storage/postgres/... -count=1
```
