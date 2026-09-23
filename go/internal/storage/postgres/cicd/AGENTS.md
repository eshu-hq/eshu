# AGENTS.md — storage/postgres/cicd guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `watermark.go` for the store, DDL constant, and schema-SQL accessor.
4. `watermark_test.go` for the store contract.
5. `../cicd_run_watermark_schema_test.go` (stays in root) for the
   DDL-vs-migration lockstep proof.
6. `../../collector/cicdrun/runwatermark/AGENTS.md` and
   `../../collector/cicdrun/ghactionsruntime/README.md` for the
   gap-detection consumer.

## Invariants

- `Save` must keep the `fencing_token <= EXCLUDED.fencing_token` conflict
  guard, the same pattern `AWSPaginationCheckpointStore` in the postgres root
  uses. A stale worker must not overwrite a newer claim's watermark.
- `Load` must NEVER gain a `generation_id`/`fencing_token` predicate:
  `ghactionsruntime` reads a PRIOR generation's watermark from a LATER
  generation's claim to detect a cross-cycle gap, so scoping `Load` to one
  generation would make gap detection permanently blind.
- Keep `migrations/078_cicd_run_watermarks.sql` and
  `CICDRunWatermarkSchemaSQL()` in lockstep
  (`TestCICDRunWatermarkSchemaMatchesBootstrapMigration`, which stays in the
  postgres root because it asserts against root's `BootstrapDefinitions`).
- Keep the package clause as `package cicdstore`; callers import the
  `storage/postgres/cicd` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with `TestCICDRunWatermarkSchemaMatchesBootstrapMigration`
  (in the postgres root) and `migrations/078_cicd_run_watermarks.sql` updated
  in lockstep; that test asserts the exact DDL text against the embedded
  migration source of truth.
- Change fencing predicates only with a stale-fence regression test and a
  contention proof per `eshu-postgres-rigor`.

## Failure modes

- A `Load` predicate scoped to one generation silently blinds cross-cycle gap
  detection with no compile or runtime error -- `ghactionsruntime` would
  simply stop reporting `runs_backfill_gap`.
- A DDL edit without updating the matching migration (or vice versa) fails
  `TestCICDRunWatermarkSchemaMatchesBootstrapMigration` in the postgres root,
  not in this package's own tests.
- Importing the parent `postgres` package from here recreates the import
  cycle `#6693`'s domain moves exist to remove.
