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
  every database that had already recorded 093 -- including a stuck ops-qa
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

Migrations 148 (`delta_baseline_commit_sha`, #7319) and 149
(`projection_write_started_at`, #7389) each add one nullable
`scope_generations` column with no default, backfill, CHECK, or index, so
`ADD COLUMN` changes only the catalog and its ACCESS EXCLUSIVE lock is bounded
by the runner's lock_timeout and retry. Migration 150 sets
`scope_generations` `fillfactor = 90` (SHARE UPDATE EXCLUSIVE, catalog-only):
existing pages are not rewritten, and new pages keep the free space that makes
the #7389 write-start marker a HOT update. Keep each of these a single
statement; see `docs/internal/evidence/7389-superseded-writer-overlay.md`.

## Keep table-locking DDL and bulk DML in separate files (#7961)

The migrator sends each file as one message, and PostgreSQL runs a
multi-statement message as one implicit transaction. A lock that an early
statement takes is held until the last statement commits. Do not put
table-locking DDL on an existing table in the same file as bulk DML. Put the
DDL in one migration and the backfill in the next, as 126 and 127 do.

The `migration-immutability` gate checks every added file
(`scripts/lib/migration_lock_shape.py`). It is a static check:

- Table-locking DDL: ALTER TABLE, ALTER INDEX, ALTER MATERIALIZED VIEW,
  CREATE INDEX or DROP INDEX without CONCURRENTLY, REINDEX or REFRESH
  MATERIALIZED VIEW without CONCURRENTLY, CREATE or DROP TRIGGER, CREATE or
  DROP RULE, DROP TABLE, CLUSTER, VACUUM FULL, LOCK, TRUNCATE.
- Bulk DML: UPDATE, DELETE, MERGE, INSERT ... SELECT, a data-modifying CTE.
- It reads top-level statements, DO blocks (every IF, ELSIF, CASE, LOOP, and
  EXCEPTION branch), and same-file functions that an executed statement
  calls. An EXECUTE counts as DDL or DML when a string literal in its body
  names one.
- Only a plain `CREATE TABLE` in the same file exempts DDL on that table.
  `CREATE TABLE IF NOT EXISTS` does not, because the table may already exist.
- It does not see: trigger functions that the file's DML fires, functions
  from other migrations, single-quoted SQL function bodies, or EXECUTE text
  that no literal contains.

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

## Verification

```bash
cd go && go test ./internal/storage/postgres/migrations -count=1
cd go && go test ./internal/storage/postgres/... -count=1
```
