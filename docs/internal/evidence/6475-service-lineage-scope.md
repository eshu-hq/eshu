# #6475 part A: service materialization lineage keyed by ingestion scope

Scope of this note: the storage and reducer-writer half of #6475 (migrations
126-129 and the `servicecatalog` lineage writer), plus the one reader change
part A cannot ship without: a deterministic active pick in
`resolveServiceChangedSinceScopeQuery`. Binding that reader and the
query/status fence to the caller's grant is part B.

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
when generation retention deletes its `scope_generations` row. Migration 127
attributes a generation only through that primary-key lookup, restricted to
`domain = 'service_catalog_correlation'`. A generation whose work item is gone,
or which has no `source_intent_id`, stays `scope_id IS NULL` ("unattributed").

## Migration shape

- 126: `ADD COLUMN IF NOT EXISTS scope_id TEXT NULL`. It is catalog-only, so
  its ACCESS EXCLUSIVE lock covers only the catalog update.
- 127: the idempotent backfill `UPDATE ... WHERE scope_id IS NULL`, in a file
  of its own. It runs in its own implicit transaction and holds ROW EXCLUSIVE
  plus per-row locks, so it does not extend 126's ACCESS EXCLUSIVE lock (which
  blocks readers) across the backfill scan.
- 128: a `DO` block that drops `service_materialization_generations_active_service_idx`
  only while its `indexdef` lacks `scope_id`.
- 129: lone `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS` of the same name on
  `(scope_id, service_id) WHERE status = 'active'`.

The name is reused on purpose. Migration 025 is immutable and recreates the
name under `IF NOT EXISTS` on any replay. Under a new name, an untracked replay
would rebuild the old single-active-per-service index, and that fails once two
scopes hold an active row for one service id. With the name reused and the drop
guarded, 025, 128, and 129 are all no-ops once the rescoped index exists.
`TestReplayGuardedRedefinitionsAreGuarded` pins that shape. It exempts this one
name from `TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay` only while
the guard and the live create's `scope_id` column are present. Seeded
violations: removing the guard, or making 129 unscoped, fails it.

The bootstrap is tracked (`eshu_schema_migrations`), so a tracked boot runs
126-129 once. A full-tree replay happens only on an untracked database or when
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

Mutations, rerun on the 126-129 layout: with 127's `UPDATE` removed,
`gen-witnessed` stays NULL (FAIL). With 128's `indexdef NOT LIKE` guard removed,
the recorder reports `"DO $$" dropped index ...` then `built index ...` on both
the fresh and the upgrade replay (FAIL).

`TestServiceMaterializationWriterKeepsScopedLineagesLive` runs the production
`PostgresServiceMaterializationWriter` over `database/sql`. Scopes a and b write
identical evidence for one service id and both commit with distinct ids. A
changed scope-a write supersedes only scope a's generation, and the
unattributed legacy active stays active. Concurrent writes from scopes c and d
both succeed, leaving 5 active rows. Mutation: with the supersede's
`scope_id = $4` made vacuous, the test fails with
`scope B superseded [service-gen:...], want nothing`.

`TestServiceChangedSinceResolvePicksAttributedNewestActiveLive` pins the
reader's active pick. Once a service id can hold several actives (one per
scope, plus an unattributed legacy row the writer never supersedes), the
unordered `LIMIT 1` followed insert order. Before the fix three of five cases
failed, including `legacy-first: current active generation = ".../gen-legacy",
want ".../gen-scoped-new"`. With `ORDER BY (active.scope_id IS NULL),
active.activated_at DESC NULLS LAST, active.generation_id DESC` every case
passes in both insert orders, and the stale unattributed row loses even when it
is newer. That query is the only reader that picks an active row for a service
id. The prior-generation lookup takes an exact id, and the counts and samples
take ids already resolved. `has_pending` stays unscoped because a pending row
exists only inside the writer's own commit transaction.

All three proofs run untagged in the blocking reducer contention gate
(`.github/workflows/reducer-contention-gate.yml`). There,
`ESHU_REQUIRE_SERVICE_LINEAGE_SCOPE_PROOF=1` turns an unset DSN into a failure.
`TestReducerContentionPostgresProofsRunInTheReducerContentionGate` fails if a
name leaves the `-run` filter or the require flag is dropped; both were seeded
and failed. Locally they skip without a DSN.

Timing (local shared host, not a production wall-time claim): under `-race`
the replay test took 22 s, the writer test 6 s, and the resolve test 4 s. The
gate timeout was raised from 180 s to 300 s to hold them. Without `-race` the
replay test took 15-84 s across runs, depending on host load.

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

The resolve query's added `ORDER BY` sorts only the service's active rows. On
the same 40,000-row seed (5,000 services x 2 scopes, one scope NULL), the plan
is unchanged except for a 2-row in-memory quicksort. It reads `shared hit=8`
both before and after, at 0.037 ms before and 0.033 ms after.

## Upgrade and rollout caveats

- Lock window (F5): between 128's drop and 129's build, nothing enforces one
  active per `(scope, service)`. A previous-release reducer still running
  during the upgrade supersedes by `service_id` alone. If two such writers race
  on one service inside that window, they can leave two actives; 129 then fails
  and the next bootstrap retries it. Stopping the previous release's reducers
  before the upgrade rules this out, and 128's header says so.
- Rolling deploy and rollback (F6): a previous-release writer against the new
  schema writes NULL-scope rows and supersedes by `service_id` only, so it can
  retire a scoped generation. That is not self-healing on identical evidence:
  the new writer's next write for that scope and service derives the same
  generation id, its insert is an `ON CONFLICT DO NOTHING` no-op, and the scope
  keeps no active generation until its evidence changes. Rolling back leaves
  the scoped rows and the rescoped index in place, and the old writer
  tolerates them because its own rows are NULL-scope. Running both releases'
  reducers at once is therefore unsupported for this lineage.
- Renumbering (F8): #6679 and #7126 landed migrations 122-125 first, so this
  change was renumbered from 122-125 to 126-129 (same order, same content)
  after rebasing. A later collision repeats the same checklist: the migration
  file names and the numbers inside their comments (which change the
  checksums), the manifest lines, the embed golden digest and count (regenerate
  them; do not text-merge), the `orderedBootstrapDefinitionNames` order, the
  `replayGuardedRedefinitions` `DropPath`/`LivePath` literals, and
  `serviceLineageScopeFirstMigration` in the replay live test. If that constant
  goes stale, another branch's migration sorts into the wrong half of the
  upgrade split without a loud failure.

No-Observability-Change: the lineage commit still runs inside the instrumented
`service_catalog_correlation` reducer intent (`reducer.run` span,
`postgres.exec` spans on every statement). No metric, span, log key, or status
field is added or renamed. A scope-less write now fails validation with an
error that names the service, and that error surfaces through the intent's
existing failure path.
