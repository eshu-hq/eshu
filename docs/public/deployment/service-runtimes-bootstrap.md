# Bootstrap Runtime Services

Use this page for the two bootstrap paths: schema bootstrap and one-shot
bootstrap indexing. Use [Service Runtimes](service-runtimes.md) for the full
runtime map.

## Schema Bootstrap

`eshu-bootstrap-data-plane` applies Postgres migrations and graph-backend
schema DDL, then exits. Historical migrations may backfill or update application
tables; ordinary collection and indexing run in other services.

It owns this sequence:

1. Apply Postgres storage schema.
2. Apply graph constraints and indexes through the configured backend.
3. Record the graph backend, schema fingerprint, and explicit compatibility
   list only after every graph statement succeeds.
4. Exit with code `0` on success.

Invalid graph backend values fail startup. Invalid or non-positive graph schema
statement timeouts fail before DDL runs.

Postgres records successful SQL files in the current schema's
`eshu_schema_migrations` table by path, variant, and checksum. A later run skips
recorded files; an invalid concurrent index triggers its migration's recovery
path. A changed checksum fails before pending DDL starts, with one narrow
exception (#7002): migration `093` was briefly edited in place and already
applied to real deployments before being restored to its originally shipped
bytes, so a ledger recorded against either edited-in-place checksum is
accepted as an alias for `093` specifically, never for any other migration.
Each acceptance logs `bootstrap.postgres.migration.checksum_alias_accepted`
with `path`, `variant`, `recorded_checksum`, and `current_checksum`; because
the alias never rewrites the ledger row, the same event fires again on every
subsequent boot of that database until the row is manually corrected. An
existing database without the ledger must execute the historical files once to
establish receipts.
Before that first rollout, preserve a recoverable database copy, quiesce
application readers and writers, and rehearse the replay against the copy with a
bounded maintenance window. Do not mark every file applied from a graph schema marker: graph schema
completion does not certify all Postgres DDL or data transformations. An
interrupted replay retains completed receipts; preserve them when retrying.

Some index-building migrations use `CREATE INDEX CONCURRENTLY`, so those do not
block writes to the table they build on. They take longer than a blocking
build and extend schema bootstrap: migration `099` builds an index on
`fact_records`, and that build scales with table size. A build that fails part
way leaves an invalid index behind, which the next schema apply drops by name
before retrying, so a failed upgrade does not need manual cleanup. Until that
retry, though, the invalid index still costs write overhead on every insert
while serving no reads, so a failed build is worth restarting promptly rather
than leaving in place.

Migration `131` (#7125) is the one migration that blocks content writes for the
length of a backfill. It creates the `content_file_secret_lines` side table,
its derivation function, and two statement-level triggers on `content_files`,
then derives findings for every existing file in the same transaction. The
foreign key and the triggers take `SHARE ROW EXCLUSIVE` on `content_files`
until that transaction commits, so ingester and bootstrap-index content writes
wait, and reads continue. The window is the backfill: about 0.27 ms of Postgres
CPU per file, measured at about 38 s for 145k files on the ops-qa corpus, and it
grows linearly with `content_files`. A new install has no rows to backfill. A
migration that cannot get the lock inside `lock_timeout` applies nothing and
retries like any other (above); once the lock is held, the backfill runs without
a statement timeout. A waiting second bootstrapper is bounded by
`ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT` (default 3 m), and a backfill on roughly
500k files or more approaches that bound, so an upgrade of a corpus that large
must plan for that window: the migration's backfill is one transaction, and the
batched, repository-partitioned rebuild described below runs only for a
bootstrap-index bulk load.

Roll the schema bootstrap out before the API pods that read the table. If a new
API pod starts before `131` is applied, the missing readiness table keeps its
hardcoded-secrets investigation on the legacy content scan until migration
completes. After `131` the triggers keep the table current for every
writer, including older binaries, so no writer fence is needed. A manual
`TRUNCATE content_files` now needs `CASCADE` (or must also name
`content_file_secret_lines`).

One bootstrapper at a time owns the Postgres schema through a session
advisory lock. A second bootstrapper started while the first is still
applying (a retried Job, or bootstrap-index next to `db-migrate`) waits for
the owner instead of failing after one statement timeout: it tries the lock
once a second, logs `bootstrap.postgres.ownership.waiting` on the first
failed try and then every 15 s with the holder's pid, state, connection age
and application name (when the client set one), and gives up only after
`ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT` (default 3 m). Waiters are not
queued: when the lock frees they race for it, each bounded by its own wait.
A migration statement that hits `lock_timeout` (5 s, SQLSTATE 55P03)
because another session holds a conflicting table lock, such as an
anti-wraparound autovacuum, applied nothing and is retried with doubling
backoff (5 s up to 15 s) until `ESHU_SCHEMA_LOCK_RETRY_BUDGET` (default 3 m)
of wall-clock time is spent; the budget is one deadline shared by every
statement of the run and counts the `lock_timeout` each failed attempt
waited, so contended statements cannot multiply it. Each retry logs
`bootstrap.postgres.migration.lock_wait`
and the eventual success logs `bootstrap.postgres.migration.lock_recovered`
with the attempt count. Any other statement failure still fails the
bootstrap on the first attempt. `db-migrate` (`eshu-bootstrap-data-plane`)
and `bootstrap-index` both read the two variables; the local supervisor
applies its schema without the ownership lock and is unaffected. The two
defaults sum to 6 m, leaving 4 m of the schema bootstrap Job's
`activeDeadlineSeconds` (600 in the chart) for pod start, the migrations'
own work and the graph schema; keep that relation when raising either
bound, because a bound the Job cannot reach never prints its holder
diagnostic (#6956).

A `CREATE INDEX CONCURRENTLY` / `DROP INDEX CONCURRENTLY` statement (see the
`CREATE INDEX CONCURRENTLY` paragraph above) is exempt from `lock_timeout`
entirely rather than retried (#7004): it takes `ShareUpdateExclusiveLock`,
which never conflicts with the `RowExclusiveLock` ordinary application
writes take, so a build waiting -- either to acquire that lock behind
another session's conflicting DDL/VACUUM, or internally for transactions
open when the build started to finish -- never blocks writers. Retrying such
a build with `lock_timeout` restarts its table scan from zero every attempt,
so on a database with steady transactions longer than the timeout the
statement never converges before `ESHU_SCHEMA_LOCK_RETRY_BUDGET` runs out;
waiting it out avoids that failure mode. The same disabled `lock_timeout`
also applies to the invalid-index cleanup (`DROP INDEX CONCURRENTLY IF
EXISTS`) that runs ahead of a retried build, for the same reason. The build
logs `bootstrap.postgres.migration.concurrent_index_build.starting` and
`.finished` (with `duration_ms`) instead of the `lock_wait`/`lock_recovered`
pair, since it is never subject to a timeout to retry after.

This wait is **not** bounded by the schema bootstrap Job's
`activeDeadlineSeconds`: that deadline kills the bootstrap client only.
Both `db-migrate` and `bootstrap-index` run with a background context and no
signal handling, so killing the pod does not cancel the statement on the
Postgres server -- the backend keeps building (or waiting on a conflicting
lock) to completion or error, holding the session schema advisory lock the
whole time. A build that completes leaves a **valid** index and releases
that lock; the next bootstrap run then waits on the same advisory lock (the
ownership wait described above) and may itself need retrying or
investigating if the orphan ran unusually long. A build that is canceled,
terminated, or errors leaves an **invalid** index instead, which the next
run's cleanup drops before rebuilding it. An upgrade against a database
whose target table is large, or whose transactions routinely run long,
should expect that build to take longer than the default 600 s Job deadline
even though the deadline itself cannot stop it; raise
`activeDeadlineSeconds` so the Job's own status reflects that reality rather
than reporting a timeout while the build keeps running unattended on the
server.

## Deployment Contract

Compose runs `db-migrate` with `/usr/local/bin/eshu-bootstrap-data-plane` after
Postgres and the graph backend are healthy. Steady-state services depend on
that one-shot service completing successfully.

Graph-writing runtimes (`eshu-bootstrap-index`, ingester/projector, standalone
projector, and resolution engine) read the latest Postgres
`graph_schema_applications` row for their backend at startup. They start only
when the latest applied fingerprint exactly matches their compiled schema
fingerprint or explicitly lists it as compatible. Missing markers and
incompatible latest markers fail startup with guidance to run
`eshu-bootstrap-data-plane` before graph writes begin.

Rolling upgrades are conservative: exact-match is the default. Additive schema
changes may declare older writer fingerprints compatible in the marker row.
Destructive schema changes leave the list empty so stale pods refuse before
runtime graph writes fail.

The graph schema bootstrap also drops constraints that later releases retire.
`DROP CONSTRAINT ... IF EXISTS` runs before every other statement. On Neo4j the
first retired set (#7095) covers `tf_module_unique`, `helm_chart_unique`,
`helm_values_unique`, `kustomize_unique`, and `tg_config_unique`; NornicDB
retires three of them (#7097), described below. Their keys were narrower than
the canonical `uid` identity, so the delta that moved a block failed with
`ConstraintValidationFailed` on Neo4j.

On Neo4j, rolling back to a release whose fingerprint the marker lists as
compatible does not re-create those constraints. `eshu-bootstrap-data-plane`
finds its own fingerprint in the marker's compatible list and skips graph DDL.
The constraints stay dropped, and the older release writes against the
constraint-free schema. Its writers MERGE on `uid`, so they write the same
graph.

On Neo4j, the older schema's DDL runs only with
`ESHU_GRAPH_SCHEMA_FORCE_REAPPLY`, for a release older than the compatible
window, or when the marker is missing (the `eshu-bootstrap-index` path). In
those cases, first drop
`kustomize_overlay_path`, `helm_values_path`, and `terragrunt_config_path`.
Otherwise the path constraints fail with `IndexAlreadyExists`. Then resolve
TerraformModule and HelmChart nodes that share `(name, path)`, which this schema
permits. Otherwise the composite constraints fail with
`ConstraintCreationFailed`. Either failure stops the strict bootstrap.

NornicDB retires the three single-property members of that set (#7097):
`kustomize_unique`, `helm_values_unique`, and `tg_config_unique`. NornicDB never
created the composite `tf_module_unique` and `helm_chart_unique`, and it never
had the three `path` indexes, so there is nothing to drop first on a rollback.
The same delta failed on NornicDB with `Neo.TransientError.Transaction.Outdated`
(a UNIQUE constraint violation). NornicDB marks that error transient, so the
work item retried instead of dead-lettering.

NornicDB gets no replacement `path` index. The delta retract filters
`repo_id`, `evidence_source`, `n.path IN $file_paths`, and `generation_id`
together, and on the pinned NornicDB that shape is a label scan with or without
a `path` index or constraint. A separate probe found that NornicDB does seek a
`path` index for `path = $p` and for `IN` alone, but not for the `IN` combined
with the other predicates the retract uses. No other query anchors these three
labels on `path`; they anchor on `uid`. The retract's label scan is a
pre-existing performance gap, not a regression from this change. See
`docs/internal/evidence/7097-nornicdb-narrow-uid-constraints.md`.

This moves the NornicDB schema fingerprint, so the first
`eshu-bootstrap-data-plane` run after the upgrade re-applies the schema on an
existing store. With the default opportunistic adoption, that run finds the
retired constraints still present, refuses to adopt, and forwards only the three
`DROP CONSTRAINT ... IF EXISTS` statements to NornicDB, because every other
object already exists and is skipped before it reaches the backend. No index
is rebuilt, so the upgrade adds three cheap drops and no property-index
backfill. Setting `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING=false` disables that filter
and re-runs the whole DDL pass, which re-runs every `CREATE INDEX IF NOT EXISTS`
on a populated graph; leave it unset for this upgrade. The previous NornicDB
fingerprint stays in the marker's compatible list, so pods still on the older
release keep writing during a rolling upgrade, and a rollback to that release
skips graph DDL, so the three constraints stay dropped. Re-creating them by
forcing the older DDL on a populated graph is a slow, needless step; do not do
it.

That startup check decides whether a writer may **start**. A writer already past
it keeps writing unless something checks again, and with
`schemaBootstrap.useHelmHooks=true` the bootstrap Job records the new marker
while the previous generation of pods is still serving. The ingester and
projector therefore re-read the marker on their write path as well, roughly
every 30 seconds, and refuse a write once the applied marker stops admitting
them. The refusal is retryable: the work stays in the queue for the pod that
replaces them, and it appears on a retrying queue row carrying the same
expected/applied fingerprint message the startup refusal uses. A marker the
writer cannot read is not a refusal — it holds the last decision, so an
unreachable Postgres does not stop those writers at once.

Three kinds of writer never make that call, so nothing checks a marker for them
after startup:

- A pod from a release built before the write-path check existed.
- The resolution engine, whose graph writers are checked at startup only.
- `eshu-bootstrap-index`, which is checked at startup and then writes to
  completion. It is a one-shot seeder, so it takes no fence by design.

Deployment ordering does not close this. It decides when a writer may start, not
whether one already running stops: the Helm schema-bootstrap Job is a
pre-upgrade hook, so it records the marker while the outgoing pods are still
serving at their configured replica count, and Compose's `depends_on` gate is a
start condition on a container that is not yet running.

So upgrading across a schema change that leaves the compatible list empty still
means stopping the old writers before bootstrap records the marker — or
accepting that they keep writing until they are replaced:

- **Kubernetes.** Scale ingester, projector, and resolution engine to zero, run
  the upgrade, then scale back up.
- **Direct and Compose installs.** Do the same for the long-lived runtimes, and
  let any active `eshu-bootstrap-index` run finish or stop it first. A
  bootstrap-index run that overlaps schema bootstrap keeps writing across the
  new marker, the same way the long-lived runtimes do.

The infra read model (#6793) does not rely on this ordering. Migration 109
fences writers at the database: a content write from a binary that does not
maintain the read model marks its repository, the infra aggregate routes serve
from the graph while any repository is marked, and the reducer repairs the
marks. Rolling upgrades need no special ordering for that table. A connection
pooler in front of the DSN must forward the `eshu.infra_inventory_writer`
session setting; a stripped setting shows as `postgres.session_unfenced` and a
non-zero `eshu_dp_infra_inventory_dirty_repos`.

Helm renders `deploy/helm/eshu/templates/job-schema-bootstrap.yaml`. With
`schemaBootstrap.useHelmHooks=true`, the Job runs as a pre-install/pre-upgrade
hook. Do not attach schema verification to every runtime pod; repeated graph
schema checks can saturate a large existing backend during rolling updates.

Do not combine Helm-hook schema bootstrap with bundled NornicDB:

```yaml
schemaBootstrap:
  useHelmHooks: true
nornicdb:
  enabled: true
  capabilities:
    relationshipMergePropertyIdentity: true
```

Helm rejects that render because hooks run before the chart-managed NornicDB
Service and Deployment exist. Deploy NornicDB separately first, or set
`schemaBootstrap.useHelmHooks=false` and provide ordering through your release
or GitOps workflow.

## Environment

| Variable | Required | Purpose |
| --- | --- | --- |
| `ESHU_POSTGRES_DSN` | yes | Postgres connection string. |
| `ESHU_GRAPH_BACKEND` | no | `nornicdb` or `neo4j`; default is `nornicdb`. |
| `NEO4J_URI` | yes | Bolt URI for NornicDB or Neo4j. |
| `NEO4J_USERNAME` / `NEO4J_PASSWORD` | yes | Bolt client credentials. |
| `DEFAULT_DATABASE` | no | Bolt database name, default `nornic`. |
| `ESHU_GRAPH_SCHEMA_STATEMENT_TIMEOUT` | no | Per graph DDL statement deadline, default `2m`. |
| `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING` | no | Adopt a complete existing graph schema by writing the fingerprint marker. |
| `ESHU_GRAPH_SCHEMA_FORCE_REAPPLY` | no | Apply graph schema despite a matching marker. For disaster recovery, after the graph was wiped and Postgres kept. |

Existing-schema adoption inspects `SHOW CONSTRAINTS` and `SHOW INDEXES`, then
fails closed if inspection errors. Unset adoption is opportunistic for NornicDB
and disabled for Neo4j; truthy values require adoption support. A graph that
still has an object the schema drops (the retired constraints above) is
not adopted, so the DDL pass runs the drop. When inspection
finds an incomplete NornicDB schema, bootstrap forwards only missing objects to
the strict DDL pass. Existing indexes and constraints are skipped before they
reach the backend, avoiding repeated populated-index backfills during additive
schema upgrades.

## Bootstrap Index

`eshu-bootstrap-index` performs one-shot initial indexing. Use it to materialize
an initial repository set, reduce cold-start time on a new environment, validate
end-to-end indexing, or recover after operator-controlled reset work.

It is packaged for Docker Compose and direct process use. It is not a
steady-state workload in the public Helm chart, and it does not expose
`/healthz`, `/readyz`, `/metrics`, or `/admin/status`.

Deployment flows should run `eshu-bootstrap-data-plane` before
`eshu-bootstrap-index`. Direct local or CI bootstrap-index runs still verify the
latest graph schema marker before opening the projection writer. If no marker
exists, bootstrap-index applies the same strict checked-in graph schema, writes
the marker only after all graph statements succeed, then opens the normal
projection writer. If a latest marker exists but is incompatible, bootstrap-index
fails closed instead of applying schema or writing graph data.

That check runs at startup only. A bootstrap-index run already in flight when a
schema upgrade records a new marker keeps writing under the old one, so let it
finish or stop it before running schema bootstrap. See
[Deployment Contract](#deployment-contract).

Repeated restarts or long-running bootstrap activity are incidents. Use the
ingester, workflow coordinator, hosted collectors, and resolution engine for
normal freshness.

### Secret-line finalizer

Migration `131`'s triggers add about 0.6 ms of Postgres time per new file, which
a bulk load cannot absorb. `eshu-bootstrap-index` therefore treats the
hardcoded-secret side table the way it already treats the content substring
indexes: a bulk-load regime with a finalizer, gated by a state row.

- Every bootstrap-index connection runs
  `SET eshu.secret_lines_derive = 'deferred'`. Migration `131`'s two
  `content_files` triggers test that setting in their `WHEN` clause and skip the
  session's writes. Nothing runs `ALTER TABLE ... DISABLE TRIGGER`, so no lock is
  taken and every other writer (ingester, projector, manual SQL) keeps deriving.
  A pooler affects the setting in two directions. One that drops or never
  forwards it leaves that session deriving, which is correct and only slower. A
  transaction-mode pooler does not reset session settings between clients, so
  the `SET` can stay on a server connection later handed to another binary
  (the ingester, say), whose writes then skip derivation while the state is
  `ready`: findings are silently missing. `eshu-bootstrap-index` must therefore
  not run through a transaction-mode pooler. Eshu's deployment has no pooler;
  the same leak class is recorded for `eshu.infra_inventory_writer` in
  `docs/internal/evidence/6793-infra-read-model-fence.md`.
- Right after the schema applies and before any content write, bootstrap-index
  moves `content_file_secret_lines_state` to `not_built` and increments its
  `epoch`. From then until the finalizer publishes `ready`, the hardcoded-secret
  investigation runs the legacy content scan instead of the side table. It
  returns the same findings more slowly and says so
  (`coverage.read_path = "legacy_scan"` plus a limitation).
- After the pipeline drains, the finalizer runs beside the content substring
  index build. It re-derives the side table one repository per worker (four
  workers), in batches of 500 files. Each batch locks its files `FOR SHARE`,
  deletes exactly those files' rows, derives them from the locked content, and
  commits. A live ingester write to a row a batch holds waits until that batch
  commits: the lock, the delete, and the derivation of up to 500 files, which is
  hundreds of milliseconds at the default batch size and is tunable through the
  batch size. The finalizer's own lock waits are bounded by `lock_timeout`
  (250 ms, below the server `deadlock_timeout`); when a writer holds a row the
  batch needs, the finalizer rolls back and retries, so it never forms a wait
  cycle with a writer, a writer is never the deadlock victim, and no row is lost
  or duplicated. It then publishes `ready` for its epoch only; a newer bulk load
  leaves readers on the legacy scan.
- The finalizer is idempotent. A crash or a failed run leaves the state
  `failed` (readers stay correct on the legacy scan); rerunning
  `eshu-bootstrap-index`, which opens a new epoch and finalizes again, repairs
  it.
- One bootstrap-index at a time is enforced, not just documented.
  bootstrap-index takes a session advisory lock, key `(5318,1)`, on a pinned
  connection right after opening the database, before the schema apply and
  before `BeginDeferral`, and releases it after the finalizers. A second run
  waits for it, bounded by `ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT` (default 3 m,
  the same knob as the schema wait; the two waits run in sequence, each with
  its own full bound, so the worst case before work starts is twice the value,
  6 m by default), logging
  `bootstrap.postgres.ownership.waiting` with `lock=bulk_load`, then fails
  naming the holder (pid, application name, connection age). Without it, two
  overlapping runs could publish `ready` over rows the second run was still
  writing without derivation. The lock dies with its backend, so a killed pod
  frees it at once; after a node death without a TCP close it lingers until
  keepalive. Find the holder with
  `SELECT l.pid, a.application_name, a.backend_start FROM pg_locks l JOIN pg_stat_activity a USING (pid) WHERE l.locktype='advisory' AND l.classid=5318 AND l.objid=1 AND l.granted;`
  and clear an orphan with `SELECT pg_terminate_backend(<pid>)`. Lock order is
  fixed: the run lock, then the schema lock `(5318,0)`; `db-migrate` takes only
  the schema lock and never waits on the run lock. A release that finds the lock
  already gone fails the run loudly (a rerun rebuilds the table).

Check readiness:

```sql
SELECT state, epoch, build_started_at, build_completed_at, failure_class
FROM content_file_secret_lines_state;
```

Watch a run with the log events `secret_lines.finalize_started`,
`secret_lines.finalize_progress`, `secret_lines.finalize_complete`, and
`secret_lines.finalize_failed`, the run lock's
`secret_lines.bulk_load_lock_acquired`, `secret_lines.bulk_load_lock_refused`,
`secret_lines.bulk_load_lock_released` and
`secret_lines.bulk_load_lock_release_failed`, the counters
`eshu_dp_secret_lines_backfill_batches_total{outcome}` and
`eshu_dp_secret_lines_backfill_files_total`, and
`eshu_dp_bootstrap_pipeline_phase_seconds{bootstrap_phase="secret_lines_finalization"}`.
A read served by the legacy scan is counted in
`eshu_dp_hardcoded_secret_reads_total{source="legacy_scan"}`; any sustained
non-zero rate after bootstrap-index finished means the finalizer failed.

## Related Pages

- [Helm Runtime Values](../deploy/kubernetes/helm-runtime-values.md)
- [Storage](../deploy/kubernetes/storage.md)
- [Bootstrap Index Service](../services/bootstrap-index.md)
- [Telemetry Overview](../reference/telemetry/index.md)
