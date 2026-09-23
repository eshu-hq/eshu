# internal/storage/postgres/cicd

Postgres-backed CI/CD run watermark store, closing the #5429 cross-cycle
gap-detection watermark across process restarts and collector replicas.

## Purpose

`cicd` (package `cicdstore`) persists one durable watermark per
`(scope_id, repository)` target: the newest GitHub Actions run ID a claim
cycle observed, plus the generation and fencing token that wrote it. It
implements `internal/collector/cicdrun/runwatermark.Store`, and
`cmd/collector-cicd-run`'s claimed-service wiring is its only production
caller.

It moved out of the postgres root under #6693; the store, DDL constant, and
schema-SQL accessor kept their exact names.

## Ownership boundary

This package owns the `cicd_run_watermarks` table's DDL and the
`CICDRunWatermarkStore` read/write surface. It does not own the embedded
bootstrap migration (`migrations/078_cicd_run_watermarks.sql`, owned by the
sibling `migrations` package) or the `runwatermark.Key` /
`runwatermark.Watermark` / `runwatermark.Store` domain types (owned by
`internal/collector/cicdrun/runwatermark`). `TestCICDRunWatermarkSchemaMatchesBootstrapMigration`,
which proves this package's DDL constant matches the embedded migration,
stays in the postgres root because it asserts against root's
`BootstrapDefinitions` bootstrap registry -- the same placement
`TestBootstrapDefinitionsIncludeSemanticExtractionQueue` uses for the
`semantic` leaf.

## Exported surface

- `CICDRunWatermarkStore` with `NewCICDRunWatermarkStore(database db.ExecQueryer)`.
- `Load(ctx, key)` returns the stored watermark for a key regardless of which
  generation wrote it.
- `Save(ctx, value)` upserts a watermark; a fencing token strictly older than
  the stored row's is rejected with `runwatermark.ErrStaleFence`.
- `EnsureSchema(ctx)` applies the CI/CD run watermark DDL (an idempotent
  convenience; production bootstrap applies the DDL through the embedded
  migration instead).
- `CICDRunWatermarkSchemaSQL()` returns the DDL text.

Every symbol keeps the exact name, method set, and semantics it had in the
root package. There are no aliases left behind in root and no forwarding
wrappers here.

## Dependencies

- `internal/storage/postgres/db` -- `db.ExecQueryer` for the store's database
  handle.
- `internal/collector/cicdrun/runwatermark` -- `runwatermark.Key`,
  `runwatermark.Watermark`, `runwatermark.Store`, `runwatermark.ErrStaleFence`.
- `database/sql`, `context`, `fmt`, `time` -- standard library.

This package imports no other Eshu package -- not even the postgres root. A
`cicd` import of root (or of any package that imports root) would create an
import cycle.

## Telemetry

`CICDRunWatermarkStore` emits no metrics or spans of its own. Gap detection
is observed through `ghactionsruntime`'s existing
`eshu_dp_ci_cd_run_partial_generations_total{reason="runs_backfill_gap"}` and
`ci_cd_run.observe` span error recording, which cover both a detected gap and
a store I/O failure (a failed Load/Save fails the claim and records on the
observe span). A dedicated load/save/stale-fence event counter mirroring
`eshu_dp_aws_pagination_checkpoint_events_total` was scoped out of #5429;
wire one if per-store-operation telemetry becomes necessary.

No-Observability-Change (#6693): this is a package-path move only; no new
metric, span, log field, status field, worker, queue, lease, or retry was
added, and none of the above was removed.

## Gotchas / invariants

- `Save` must keep the `fencing_token <= EXCLUDED.fencing_token` conflict
  guard, the same pattern `AWSPaginationCheckpointStore` in the postgres root
  uses.
- `Load` must NEVER gain a `generation_id`/`fencing_token` predicate:
  `ghactionsruntime` reads a PRIOR generation's watermark from a LATER
  generation's claim to detect a cross-cycle gap, so scoping `Load` to one
  generation would make gap detection permanently blind.
- Keep `migrations/078_cicd_run_watermarks.sql` and
  `CICDRunWatermarkSchemaSQL()` in lockstep; the root-resident
  `TestCICDRunWatermarkSchemaMatchesBootstrapMigration` proves it.
- Never import the parent `postgres` package from here.
