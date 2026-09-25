# #6475 part A: service materialization lineage keyed by ingestion scope

Scope of this note: the storage and reducer-writer half of #6475 (migrations
122-124 and the `servicecatalog` lineage writer). The changed-since readers
(`resolveServiceChangedSinceScopeQuery` and the query/status fence) are part B
and are not changed here.

## Defect

`service_materialization_generations` held one active generation per
`service_id`, whatever ingestion scope wrote it. Two scopes that correlated the
same service id shared one lineage. The second scope's commit either collided on
the generation id (identical evidence gives an identical `StableID`, and the
`ON CONFLICT DO NOTHING` insert reads as an idempotent no-op) or superseded the
first scope's active generation (changed evidence).

Root-Cause Evidence: `ServiceMaterializationWrite` had no scope field, and
`ServiceCatalogCorrelationHandler.commitServiceGenerations` dropped
`intent.ScopeID` when it built the writes.
`ServiceMaterializationGenerationID` hashed `service_id` plus evidence only.
`supersedePriorServiceGenerationQuery` filtered on `service_id` alone, and
migration 025's unique index was `(service_id) WHERE status = 'active'`. The
unit regression `TestServiceMaterializationWriterScopesLineageByIngestionScope`
failed before the fix with `Committed = (A true, B false)`.

## Backfill witness (settles the design's open item)

A service-catalog reducer `Intent.IntentID` is the `fact_work_items.work_item_id`
the reducer queue claimed (`scanReducerIntent`, `reducer_queue_helpers.go`). The
intent is enqueued through `BuildServiceCatalogCorrelationReducerIntent` into
`fact_work_items`, so it is not a `shared_projection_intents` id. That row
carries `scope_id` (NOT NULL, FK to `ingestion_scopes`), and it cascades away
when generation retention deletes its `scope_generations` row. Migration 122
attributes a generation only through that primary-key lookup, restricted to
`domain = 'service_catalog_correlation'`. A generation whose work item is gone,
or which has no `source_intent_id`, stays `scope_id IS NULL` ("unattributed").

## Migration shape

- 122: `ADD COLUMN IF NOT EXISTS scope_id TEXT NULL` (catalog-only) plus the
  idempotent backfill `UPDATE ... WHERE scope_id IS NULL`.
- 123: a `DO` block that drops `service_materialization_generations_active_service_idx`
  only while its `indexdef` lacks `scope_id`.
- 124: lone `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS` of the same name on
  `(scope_id, service_id) WHERE status = 'active'`.

The name is reused on purpose. Migration 025 is immutable and recreates the
name under `IF NOT EXISTS` on any replay. Under a new name, an untracked replay
would rebuild the old single-active-per-service index, and that fails once two
scopes hold an active row for one service id. With the name reused and the drop
guarded, 025, 123, and 124 are all no-ops once the rescoped index exists.
`TestReplayGuardedRedefinitionsAreGuarded` pins that shape. It exempts this one
name from `TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay` only while
the guard and the live create's `scope_id` column are present. Seeded
violations: removing the guard, or making 124 unscoped, fails it.

The bootstrap is tracked (`eshu_schema_migrations`), so a tracked boot runs
122-124 once. A full-tree replay happens only on an untracked database or when
an interrupted file is retried. The live proof covers both paths.

## Live proof (local PostgreSQL 18, isolated schemas, 2026-09-25)

`TestServiceMaterializationActiveIndexReplayConvergesLive`:

- fresh: `ApplyBootstrap` produces `CREATE UNIQUE INDEX
  service_materialization_generations_active_service_idx ... (scope_id,
  service_id) WHERE (status = 'active'::text)`.
- upgrade: the prior release's definitions are applied through the tracked
  runner and seeded with legacy lineage (4 named rows plus 2000 bulk rows). The
  old index refuses a second active row for one service id. `ApplyBootstrap`
  then converges: the witnessed row becomes `scope-a`; the row with a deleted
  witness, the one with no intent id, and the one naming another domain's work
  item all stay NULL.
- Two actives for one service id (scopes a and b) insert, and a second active
  for one `(scope, service)` is refused.
- A second tracked `ApplyBootstrap` followed by an untracked full-tree
  `ApplyDefinitions` replay goes through the per-statement index recorder.
  There are no index changes; OID, relfilenode, and definition are identical
  before and after, for example OID/relfilenode `329565` before, after the
  tracked boot, and after the untracked replay.

Mutations: with 122's `UPDATE` removed, `gen-witnessed` stays NULL (FAIL). With
123's `indexdef NOT LIKE` guard removed, the recorder reports
`"DO $$" dropped index ...` then `built index ...` (OID `343271 -> 343294`)
(FAIL).

`TestServiceMaterializationWriterKeepsScopedLineagesLive` runs the production
`PostgresServiceMaterializationWriter` over `database/sql`. Scopes a and b write
identical evidence for one service id and both commit with distinct ids. A
changed scope-a write supersedes only scope a's generation, and the
unattributed legacy active stays active. Concurrent writes from scopes c and d
both succeed, leaving 5 active rows. Mutation: with the supersede's
`scope_id = $4` made vacuous, the test fails with
`scope B superseded [service-gen:...], want nothing`.

Timing (local shared host, not a production wall-time claim): the replay test
took 15-84 s across five runs, dominated by two full bootstraps per subtest
and by host load; the writer test took 4-21 s.

## Plan evidence

No-Regression Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the writer's supersede
and activate statements was run on local PostgreSQL 18, inside a rolled-back
transaction after inserting the pending row, on 40,000 lineage rows each
(before: the prior schema, 10,000 services x 4 generations, one scope; after:
5,000 services x 2 scopes x 4 generations), third warm run:

| statement | before | after |
|---|---|---|
| supersede | Index Scan on `..._active_service_idx`, `Index Cond: (service_id = ...)`, shared hit=16, 0.117 ms | Index Scan on `..._active_service_idx`, `Index Cond: ((scope_id = ...) AND (service_id = ...))`, shared hit=16, 0.119 ms |
| activate | Index Scan on `..._service_idx`, `Index Cond: ((service_id = ...) AND (status = 'pending'))`, shared hit=18, 0.087 ms | same index and cond, `scope_id` moves to the filter, shared hit=18, 0.098 ms |

Both plans stay single-row index probes with identical buffer counts, so no new
secondary index is needed. The rescoped unique index replaces the old one,
which leaves write amplification per generation unchanged. This is a plan-shape
no-regression claim, not a wall-time measurement. One cost is expected: the
generation id now includes the scope, so the first re-materialization of each
service after upgrade commits one new generation per service (its snapshot rows
classify `unchanged`).

No-Observability-Change: the lineage commit still runs inside the instrumented
`service_catalog_correlation` reducer intent (`reducer.run` span,
`postgres.exec` spans on every statement). No metric, span, log key, or status
field is added or renamed. A scope-less write now fails validation with an
error that names the service, and that error surfaces through the intent's
existing failure path.
