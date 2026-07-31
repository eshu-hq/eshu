# cmd/ingester

## Purpose

`cmd/ingester` is the long-running binary (`eshu-ingester`) that owns
repository sync, parsing, fact emission, and source-local projection into the
configured graph backend. It runs as a `StatefulSet` in Kubernetes and is the
only runtime that mounts the shared workspace PVC. Cross-domain materialization
belongs to the reducer; HTTP reads belong to the API and MCP server; schema DDL
belongs to `eshu-bootstrap-data-plane`.

## Where this fits in the pipeline

```mermaid
flowchart LR
  A["git source\n(remote or filesystem)"] --> B["cmd/ingester\nGitSource + NativeRepositorySnapshotter"]
  B --> C["Postgres fact store\nIngestionStore"]
  C --> D["Projector queue\nNewProjectorQueue"]
  D --> E["Resolution Engine\n(reducer + source-local projector)"]
  E --> F["Graph backend"]
  E --> G["Postgres content store"]
```

## Internal flow

```mermaid
flowchart TB
  A["main.run\ntelemetry + Postgres + canonical writer"] --> B["buildIngesterService\ncompositeRunner"]
  B --> C["collectorSvc\ncollector.Service.Run"]
  B --> D["projectorSvc\nprojector.Service.Run"]
  C --> E["GitSource.Next\ndiscover + snapshot workers"]
  E --> F["IngestionStore\ndurable fact write"]
  F --> G{"batch drained?"}
  G -- yes --> H["AfterBatchDrained\nshard drain barrier\nbackfill/reopen leader"]
  G -- no --> E
  D --> I["projectorQueue.Claim"]
  I --> J["projector.Runtime.Project\ncanonical write + content write + intent enqueue"]
  J --> K["projectorQueue.Ack"]
```

## Lifecycle / workflow

`main.run` bootstraps OTEL telemetry via `telemetry.NewBootstrap("ingester")`
and `telemetry.NewProviders`, opens Postgres through `runtimecfg.OpenPostgres`,
and builds the canonical graph writer (`sourcecypher.NewCanonicalNodeWriter`
backed by the adapter selected via `ESHU_GRAPH_BACKEND`). It then calls
`buildIngesterService`, which assembles a `compositeRunner` through
`newCompositeRunner` so `collector.Service` and `projector.Service` run
concurrently. Transient per-unit faults are owned by each service's own Run loop
(durable dead-letter replay) and do not tear down the peer. Only a *fatal* error
from either service cancels the other; the composite runner then waits a bounded
drain grace for the sibling to finish its in-flight unit and joins every
terminal error (see
`docs/internal/design/3501-ingester-composite-runner-failure-isolation.md`).

`signal.NotifyContext` on `SIGINT` and `SIGTERM` propagates cancellation through
`compositeRunner.Run`. `app.NewHostedWithStatusServer` mounts `/healthz`,
`/readyz`, `/metrics`, `/admin/status`, and `/admin/recovery` alongside the
composite runner.

`projectorQueue.Ack` (step `K` above) carries one runtime delta-trigger
(issue #5593): `buildIngesterProjectorService` wires
`postgres.ConfigStateDriftTrigger` to `postgres.ConfigStateDriftRuntimeTrigger`,
reusing the same admission-aware `reducerWriter` the projector runtime already
enqueues intents through. When the activating scope is `state_snapshot:*` —
committed by `collector-terraform-state` through the normal ingestion
boundary and drained by this same `projectorSvc` — the hook enqueues one
`config_state_drift` reducer intent immediately, so a Terraform state change
that lands between bootstrap-index runs is drift-evaluated without waiting
for the next one. The hook is scoped to this binary's `ProjectorQueue` only:
`cmd/bootstrap-index/wiring.go` deliberately does not wire it, because it
would evaluate drift before bootstrap's finite corpus has necessarily
finished activating every repo. See
`go/internal/storage/postgres/projector_queue_config_state_drift_trigger_hook.go`
for the full ordering rationale.

The trigger above has no redrive/retry of its own for a "no config repo owns
this backend" rejection -- a bounded, ledger-backed redrive for that exact
rejection was built and removed across three issue #5593 review rounds (the
final round found it created an unbounded ~20-minute perpetual retry loop
for every operator-owned backend, once scheduling was narrowed to run from
inside `Handle()` itself). See `ConfigStateDriftRuntimeTrigger`'s doc comment
in `go/internal/storage/postgres/drift_runtime_trigger.go` for the full
history. The race it would have covered self-heals on the next real
`terraform apply` (a new state_snapshot generation, evaluated independently
by this same trigger); a state that never changes again after racing once
is the accepted residual gap.

When `ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED` is true, the ingester wraps the
normal repository selector with a webhook-trigger selector. Accepted queued
GitHub, GitLab, and Bitbucket triggers are claimed first, synced as targeted
repositories, then handed to the same snapshot and fact-emission path as
scheduled polling. Unsupported provider triggers are marked failed instead of
being routed through a guessed clone path.

Set `ESHU_REPO_SCHEDULED_SYNC_ENABLED=false` when the ingester should only
process queued webhook refresh triggers and must not fall back to broad
scheduled repository selection. This mode requires
`ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED=true`; startup fails if scheduled sync is
disabled without a trigger handoff path.

Git-backed repository selection uses the same runtime logger as the rest of the
ingester. During first startup or webhook-triggered sync, clone/fetch emits
structured `git repository sync started`, `git repository sync progress`,
`git repository sync completed`, and `git repository sync failed` records before
snapshot workers start. The fields are bounded for hosted operators: operation,
provider kind, repository id, repository ordinal/count, elapsed seconds, branch
when known, and failure class. Credential-bearing URLs are redacted and full
local checkout paths are not logged.

Hosted fetch refreshes parse `git ls-remote --symref` HEAD output as a
two-field symbolic ref, so `ref: refs/heads/main` resolves to `main` instead of
the invalid `ref:` branch. When a managed shallow checkout reports an old
`.git/shallow.lock` created by an interrupted fetch, the ingester removes only
that stale lock under the managed repo and retries the fetch once.

No-Regression Evidence: `go test ./internal/collector -run
'TestUpdateRepositoryParsesSymrefHeadBranchFromLsRemote|TestUpdateRepositoryRecoversOldShallowLockAndRetriesFetch'
-count=1` covers hosted HEAD parsing and stale shallow-lock recovery without
changing clone/fetch progress logging semantics.

Observability Evidence: existing `git repository sync failed` and `git
repository sync completed` logs still surface operation, repository ordinal,
branch, elapsed time, and failure class; the retry path keeps the original fetch
progress writer so a repeated failure remains visible.

After each full collector batch drain, `AfterBatchDrained` records the shard's
arrival in `deferred_maintenance_barriers` /
`deferred_maintenance_barrier_arrivals`. Multi-shard ingesters wait until every
`ESHU_REPO_SHARD_INDEX` for the current epoch has arrived; the completing shard
becomes the maintenance leader.

Barrier arrivals are recorded under an exclusive advisory lock on a single
barrier-state key, but the leader **commits that transaction, releasing the
lock, before it runs any maintenance** (`deferred_maintenance_barrier.go`).
Maintenance itself is not fleet-serialized. `BackfillAllRelationshipEvidence`
commits in bounded per-repository-batch transactions, each taking only its own
repositories' exclusive advisory locks — namespaced under
`deferred_relationship_maintenance` and acquired in sorted repository order to
stay deadlock-free — and normal source fact commits take the matching *shared*
lock for their own repository partition only (`deferred_maintenance_lock.go`).
The reopen pass that follows runs in one transaction of its own and takes no
advisory lock at all: it only flips `fact_work_items` rows in status
`succeeded`, which the reducer claim path never selects — it claims `status IN
('pending', 'retrying', 'claimed', 'running')` (`reducer_queue_claim_query.go`,
`reducer_queue_batch_query.go`).

So the barrier is what orders the phases — no shard runs maintenance until every
shard has drained its source batch, and the epoch is marked complete only after
maintenance succeeds — while a concurrent source commit waits at most for the
in-flight batch holding its own repository, never for the whole pass. This is
the ingester's form of the collection → backfill → reopen ordering
`go/cmd/bootstrap-index/README.md` describes as Phase 1 → Phase 3. A failure
exits the ingester to prevent partial maintenance state.

The reopen pass replays `deployment_mapping`, `code_import_repo_edge`, and the
cross-scope correlation domains in `CrossScopeCorrelationReopenDomains`, all in
one transaction. Before #5846 the correlation domains were replayed only by
`eshu-bootstrap-index`, so under normal ingestion a `container_image_identity`,
`ci_cd_run_correlation`, or `supply_chain_impact` decision that lost the
cross-scope activation race kept its empty-join output indefinitely. Because
this runs on every drain rather than once, the correlation listing is bounded by
a per-scope replay floor: only work items on the scope's active generation or
newer, falling back to its latest generation when there is no usable active one,
and never a work item whose own generation terminally failed — when that failed
generation is the scope's latest the fallback picks it, yet nothing it re-decides
can be read while the scope's active pointer is `NULL`; and in the mixed shape,
where a newer generation fails while an older one stays active and the pointer is
not `NULL`, the same predicate keeps the scope's active floor and replays it while
the failed newer generation drops out.
That keeps the pass O(active scopes) rather than O(active scopes x generations),
and it is not free — the correlation half had no pre-change baseline at all. See
the cross-scope correlation reopen section in
`go/internal/storage/postgres/README.md` for the bound, its measured per-drain
cost, and what that measurement does not cover.

Those drains are paced by ingestion, not by a timer. `collector.Service` runs
`AfterBatchDrained` only when the source batch exhausts after at least one
committed generation, or via the `AfterEmptyBatchDrained` escape described
below (`go/internal/collector/service.go:226`). `committedSinceDrain` is
cleared on every drain (`:231`) and set again on every commit (`:272`), so a
shard that keeps committing drains once per commit-to-idle cycle whether or
not the escape is enabled.

The `AfterEmptyBatchDrained` escape set here from `ESHU_REPO_SHARD_COUNT > 1`
(`wiring.go:220`) fires on every idle poll for as long as this shard has never
committed a generation, gated by the `everCommitted` latch
(`go/internal/collector/service.go`): `everCommitted` starts false, latches
true permanently on the shard's first commit (`:273`), and the escape checks
`!everCommitted` (`:226`). A shard that commits regularly only ever exercises
the escape during its pre-first-commit startup window, after which
`committedSinceDrain` is decisive for the rest of the process — exactly as
before #5852. That window is not bounded in code: it is one escape-driven drain
per idle poll until this shard's own first commit, where the old latch allowed
exactly one per process. Every escape-driven call passes `hasCommitted=false`
to `AfterBatchDrained`, so it never opens a new barrier epoch on its own; it
becomes a full barrier round-trip — and, if it is the epoch's last arriver, a
corpus-wide `RunDeferredRelationshipMaintenance` pass — only when it finds an
epoch another shard already opened to join. When no epoch is open it is a
single read-only check that returns immediately. In practice the pre-first-commit
window is one poll, because the ingester queues repositories from startup —
but a shard that starts with an empty queue and gains work later pays one
check per poll until it does. A shard that never commits — because it owns no
repositories under the current shard assignment — keeps the escape live
indefinitely, re-checking the deferred-maintenance barrier every idle poll for
as long as the process runs, joining whatever epoch is open at the time
without ever opening one itself.
`TestServiceRunEmptyBatchEscapeAddsExactlyOneDrainPerProcess`
pins the single-commit-window behavior (an eventually-busy shard adds exactly
one escape-driven drain to its whole process lifetime), and
`TestServiceRunCallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard`
pins the recurring behavior for a shard that never commits at all — both now
also assert `hasCommitted` is reported correctly on every call
(`go/internal/collector/service_empty_batch_test.go`).

The reopen replays every domain in a single unordered
transaction, so a `container_image_identity` -> `ci_cd_run_correlation` ->
`supply_chain_impact` chain advances by at most one link per drain: a corpus
that goes quiet right after the head decision commits keeps the tail's
empty-join output until the next committed generation or an
`eshu-bootstrap-index` run. Those are the two levers; #5709 ends the dependence
on drain count.

For `ESHU_REPO_SHARD_COUNT > 1`, an empty selected batch still drains, so a
shard that owns no repositories keeps CHECKING the barrier every epoch, not
just its first — #5852 fixed the collector-level latch so this recurs for the
life of the process, matching the deferred-maintenance barrier's requirement
that every shard arrive at every epoch that is actually open
(`go/internal/storage/postgres/deferred_maintenance_barrier.go`). "Checking"
and "arriving" are deliberately different verbs here: the #5852 fix alone made
every one of those checks capable of *opening* a new epoch too, which is
correct only when the checking shard has real work to report. On a quiet
restart where no shard anywhere has anything to commit, that meant every idle
poll opened an epoch, the completing shard ran the corpus-wide
`RunDeferredRelationshipMaintenance` pass against an unchanged corpus, the
epoch completed, and the very next idle poll opened another one — forever. The
follow-up fix makes a never-committed shard's check join-only:
`AfterBatchDrained`'s `hasCommitted` argument (true only when this drain
follows a real commit) flows into
`DeferredMaintenanceBarrierConfig.HasCommitted`, and
`ensureDeferredMaintenanceBarrierEpoch` lets any shard join an epoch that is
already open regardless of that flag — preserving the original #5852 stall
fix — but only opens a new epoch when it is true. A fleet where nothing has
committed anywhere opens zero epochs and runs zero maintenance passes,
including for `ESHU_REPO_SHARD_COUNT == 1`, whose short-circuit had the exact
same defect before the follow-up: it ran maintenance unconditionally on every
idle poll rather than only when this single shard had actually committed.

What both fixes deliberately leave unbounded is
`waitDeferredMaintenanceBarrierCompletion` itself: it still has no arrival
deadline or partial-arrival quorum, because this barrier's whole point is to
stop deferred maintenance from running while any shard might still be
mid-ingest, and a timeout or quorum bypass would run maintenance against a
fleet that has not actually reached a quiescent point — trading correctness
for liveness. A shard that has genuinely crashed or lost connectivity (as
opposed to one that is alive but has nothing to ingest) still strands the
barrier with no deadline; that failure mode is observable via the
stall-watchdog log described below, not eliminated. Changing shard count while
an epoch is open fails closed; operators should let the current epoch complete
before scaling charted ingester replicas.

A shard genuinely stuck in `waitDeferredMaintenanceBarrierCompletion` — dead,
partitioned, or otherwise never arriving — logs a WARN
(`deferred maintenance barrier still waiting for completion`) at least every
30s while it waits, carrying the epoch, shard count, shard index, elapsed wait
duration, the current arrival count, the sorted `arrived_shard_indexes`, and
the sorted `missing_shard_indexes` (or an `arrived_shards_error` field if the
arrival lookup itself fails). Naming the missing shard indexes directly, not
just a count, is what lets an operator find which process is silent from this
one log line instead of correlating silence across every shard's own logs.

No-Regression Evidence (#5852): `go test ./internal/collector -run
'TestServiceRun(CallsEmptyBatchDrainHookOnEveryIdlePollForANeverCommittingShard|EmptyBatchEscapeAddsExactlyOneDrainPerProcess)'
-count=1` proves a shard that never commits keeps re-arriving at the barrier
on every idle poll (the escape recurring for its whole process lifetime)
while a shard that does eventually commit still adds only its one
pre-first-commit drain, exactly as before. `go test ./internal/storage/postgres
-run TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionLogsStallBeforeCompleting
-count=1` proves the stall-watchdog log fires before the barrier completes.

No-Regression Evidence (#5852 follow-up, join-only barrier arrivals): `go test
./internal/storage/postgres -run
'TestIngestionStoreShardDrainBarrier(QuietRestartNeverOpensEpochAcrossManyIdlePolls|SingleShardNeverCommittedSkipsMaintenance|SingleShardHasCommittedRunsMaintenance|NeverCommittedShardJoinsAlreadyOpenEpochAndBecomesLeader)|TestEnsureDeferredMaintenanceBarrierEpochNeverCommittedShard(DoesNotOpenNewEpoch|JoinsAlreadyOpenEpoch)'
-count=1` proves a quiet fleet (multi-shard and `ShardCount==1`) opens zero
epochs and runs zero maintenance passes across many idle polls, while a
never-committed shard can still join, and even lead, an epoch a committing
shard already opened. `go test ./internal/storage/postgres -run
TestIngestionStoreWaitDeferredMaintenanceBarrierCompletionStallLogNamesMissingShards
-count=1` proves the stall warning names the specific missing shard indexes,
not just a count.

No-Regression Evidence: `go test ./internal/storage/postgres -run
'TestIngestionStore(CommitScopeGenerationTakesSharedMaintenanceBarrier|RunDeferredRelationshipMaintenanceTakesExclusiveBarrier|ShardDrainBarrier)'
-count=1` covers the shared commit barrier, exclusive deferred-maintenance
barrier, and multi-shard drain rendezvous. `go test ./cmd/ingester -run
'TestBuildIngesterCollectorService(DefersRelationshipBackfillToBatchDrain|RunsDrainHookForEmptyShardedBatches)|TestBuildIngesterServiceProducesCompositeRunner'
-count=1` covers the batch-drain hook wiring and empty-shard participation.

No-Regression Evidence: #3073 keeps the barrier transaction order but closes the
latest-epoch `Rows` before inserting a new barrier epoch, preventing a Postgres
driver from executing the insert while the transaction still has an active
cursor. `go test ./internal/storage/postgres -run
'Test(IngestionStoreShardDrainBarrier|EnsureDeferredMaintenanceBarrierEpoch|BootstrapDefinitionsIncludeDeferredMaintenanceBarrier)'
-count=1` failed before the cursor was closed and passes after the fix.

No-Observability-Change: #3073 adds no metric, label, span, log field, worker,
queue, lease, runtime knob, or graph write. Operators still diagnose this path
through existing barrier wait/completion logs, `deferred_relationship_maintenance_failure`
errors, and Postgres query instrumentation.

Observability Evidence: source commits log a
`deferred_maintenance_shared_barrier` commit stage, and the existing Postgres
instrumentation emits query duration for the advisory-lock statements. Deferred
maintenance barrier wait/completion logs report epoch, shard count, arrived
shard count, and leader shard index. Failures continue to exit the ingester with
a structured `deferred_relationship_maintenance_failure` failure class.

Observability Evidence (#5852): adds the stall-watchdog WARN log described
above, on the existing `deferred_maintenance_barrier` phase, at a 30s minimum
interval while a shard waits. No new metric, span, or runtime knob — a
`waitDeferredMaintenanceBarrierCompletion` retrofit did not warrant a new OTEL
instrument (with its own contract/verifier/dashboard obligations under
`telemetry-coverage-discipline`) when a bounded, searchable log line already
gives an operator the epoch, arrival count, and elapsed wait needed to
recognize and act on a genuine stall.

The collector service also wires the shared collector generation dead-letter
store. Commit failures before projector work exists are surfaced through
`/admin/status` and can be marked for source-level replay through
`/admin/replay-collector-generations` after the commit failure is fixed.

The projector service runs in the same process and drains the projector queue
filled by the collector. Worker count defaults to `min(NumCPU, 8)`; on
`local_authoritative` + NornicDB it defaults to the developer or host CPU count
so the local authoritative path matches the production-proven concurrency
profile. The NornicDB phase-group executor keeps canonical retractions outside
matching upsert groups so slow cleanup and normal entity writes are timed and
reported as separate phases. Directory and file writes remain separate bounded
phases, while entity containment is folded into row-scoped entity upserts by
default for NornicDB after high-cardinality Java proof runs showed the older
file-scoped shape over-fragmented canonical writes.

The embedded projector queue's retry policy is loaded via
`runtimecfg.LoadRetryPolicyConfig(getenv, "PROJECTOR")` (same stage prefix the
standalone projector binary uses) and threaded onto `postgres.ProjectorQueue`
in `buildIngesterProjectorService`, including `MaxRetryDelay`
(`ESHU_PROJECTOR_MAX_RETRY_DELAY`, default `1h`) and `JitterFraction`
(`ESHU_PROJECTOR_RETRY_JITTER_FRACTION`, default `0.1`). `ProjectorQueue.Fail`
schedules retries with exponential backoff plus jitter, not a fixed delay, so
many work items failing at the same instant do not reconverge on one
`visible_at` and self-reinforce into a retry storm (#4450);
`eshu_dp_projector_retry_surge_total` tracks the scheduled-retry rate by
`failure_class`.

Positive list-seeded canonical retract statements are split into 25-key chunks
inside the NornicDB phase-group executor before execution. Negative `NOT IN`
cleanup statements stay intact because splitting a keep-list would make each
chunk delete valid current files from other chunks.

No-Regression Evidence: `go test ./cmd/ingester -run
'TestNornicDBPhaseGroupExecutor.*RetractFilePaths' -count=1` proves positive
retract file-path chunks split while negative keep-list cleanup remains one
statement.

Observability Evidence: phase-group retract errors now include the original
statement ordinal and chunk part (`part x/y`) while preserving the sanitized
statement summary used in queue failure details.

## Exported surface

`cmd/ingester` is a `main` package. There is no exported Go API. The contract
is the process interface: environment variables, signal handling, direct
`eshu-ingester --version` / `eshu-ingester -v` probes, and the admin HTTP surface
listed above. Version probes run through `buildinfo.PrintVersionFlag` before
telemetry, Postgres, or graph setup begins.

## Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| ESHU_POSTGRES_DSN | required | Postgres connection string |
| ESHU_GRAPH_BACKEND | nornicdb | neo4j or nornicdb |
| NEO4J_URI | required | Bolt URI |
| NEO4J_USERNAME | required | Bolt auth username |
| NEO4J_PASSWORD | required | Bolt auth password |
| ESHU_SNAPSHOT_WORKERS | min(NumCPU,8) | Concurrent snapshot goroutines |
| ESHU_PARSE_WORKERS | min(NumCPU,8) | Concurrent file-parse workers per snapshot |
| ESHU_LARGE_REPO_FILE_THRESHOLD | 1000 | File-count threshold for large-repo semaphore |
| ESHU_LARGE_REPO_MAX_CONCURRENT | 2 | Max concurrent large-repo snapshots |
| ESHU_PROJECTOR_WORKERS | min(NumCPU,8); local_authoritative NornicDB: NumCPU | Projector worker count |
| ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK | 10000; set 0 to disable | Reducer queue depth threshold where ingester source-local projection defers new reducer intent enqueues |
| ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK | 500; set 0 to disable | Graph-write backpressure: defers reducer intent enqueues once the count of reducer rows retrying with `failure_class=graph_write_timeout` reaches this value. Scoped to the graph-write-timeout class so readiness-not-ready retry backlogs (`secrets_iam_endpoint_not_ready` and other `*_n` classes) never false-throttle admission |
| ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK | 100 | Hysteresis floor; admission resumes only after the graph-write-timeout retrying depth falls below this value. Must be less than the retrying high-water mark |
| ESHU_REDUCER_ADMISSION_POLL_INTERVAL | 1s | Reducer queue depth recheck interval while admission is deferring |
| ESHU_LARGE_GEN_THRESHOLD | 10000 | Fact-count threshold for large-generation semaphore |
| ESHU_LARGE_GEN_MAX_CONCURRENT | 2 | Max concurrent large-generation projections |
| ESHU_CANONICAL_WRITE_TIMEOUT | 30s | Graph write timeout |
| ESHU_NEO4J_PROFILE_GROUP_STATEMENTS | false | Opt-in Neo4j grouped-write statement attempt logs for performance diagnostics |
| ESHU_NORNICDB_CANONICAL_GROUPED_WRITES | false | Conformance toggle; on NornicDB it commits per dependency phase — whole-materialization atomic is unsupported (#4027) |
| ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT | true | Fold entity containment into row-scoped entity upserts; set false only for fallback comparisons |
| ESHU_NORNICDB_PHASE_GROUP_STATEMENTS | 500 | NornicDB phase group statement cap |
| ESHU_NORNICDB_ENTITY_BATCH_SIZE | 100 | Entity upsert row cap |
| ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY | NumCPU clamped to 16 | Parallel chunk dispatch for canonical entity phases. Clamped to 16. Set to 1 to keep serial dispatch. |
| ESHU_QUERY_PROFILE | — | local_lightweight or local_authoritative |
| ESHU_DISABLE_NEO4J | — | Force local-lightweight writer when true |
| SCIP_INDEXER | false | Enable external SCIP indexers only when set to 1/true/yes/on and the selected language binary is available; unset, unrecognized, false/0/no/off keep native-only parsing |
| SCIP_LANGUAGES | python,typescript,javascript,go,rust,java,cpp,c | Languages eligible for SCIP indexing |
| SCIP_WORKERS | 4 | Bounded concurrent SCIP language/subtree indexer processes across concurrent repository snapshots |
| ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION | — | Fault-injection: scope generation ID for one-shot retry |
| ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED | false | Check queued webhook refresh triggers before scheduled repository polling |
| ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER | ingester | Lease owner written when claiming queued webhook triggers |
| ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT | 100 | Max webhook triggers claimed per selector pass |
| ESHU_REPO_SCHEDULED_SYNC_ENABLED | true | Enable broad scheduled repository selection when no webhook triggers are queued |
| ESHU_REPO_SHARD_COUNT | 1 | Deterministic repository shard count. Helm sets this from `ingester.replicas` when replicas are greater than one. |
| ESHU_REPO_SHARD_INDEX | 0 | Deterministic zero-based repository shard index. Helm sets this from the StatefulSet pod ordinal when replicas are greater than one. |
| ESHU_PPROF_ADDR | unset (disabled) | Opt-in `net/http/pprof` endpoint via `runtime.NewPprofServer`; port-only inputs bind to `127.0.0.1` |

Per-label NornicDB tuning knobs (ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES,
ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS, and the file/function/struct
batch overrides) are documented in `docs/public/reference/nornicdb-tuning.md`.

## Dependencies

- `internal/collector` — `collector.Service`, `GitSource`,
  `NativeRepositorySelector`, `NativeRepositorySnapshotter`
- `internal/projector` — `projector.Service`, `projector.Runtime`,
  `projector.CanonicalWriter`, `projector.RetryInjector`
- `internal/storage/postgres` — `IngestionStore`, `NewProjectorQueue`,
  `NewReducerQueue`, `NewFactStore`, `NewContentWriter`, queue observers
- `internal/storage/cypher` — `sourcecypher.NewCanonicalNodeWriter`
- `internal/runtime` — `OpenPostgres`, `LoadGraphBackend`, `OpenNeo4jDriver`,
  `ConfigureMemoryLimit`, `LoadRetryPolicyConfig`
- `internal/app` — `app.NewHostedWithStatusServer`, `app.Runner`
- `internal/telemetry` — bootstrap, providers, instruments
- `internal/recovery` — `recovery.NewHandler` for the `/admin/recovery` route

## Telemetry

The ingester inherits collector and projector telemetry. Key signals:

- `eshu_dp_repo_snapshot_duration_seconds` — per-repo snapshot time; elevated
  values point to large or slow-to-parse repositories
- `eshu_dp_repos_snapshotted_total{status="failed"}` — snapshot errors
- `eshu_dp_facts_emitted_total` vs `eshu_dp_facts_committed_total` — a growing
  gap signals `IngestionStore` write pressure
- `eshu_dp_large_repo_semaphore_wait_seconds` — contention for the large-repo
  semaphore; raise ESHU_LARGE_REPO_MAX_CONCURRENT cautiously with memory in view
- `eshu_dp_projections_completed_total{status="failed"}` — projector failures;
  check `failure_class` in structured logs
- `eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}` — graph
  write bottleneck
- Compose metrics endpoint: `http://localhost:19465/metrics`
- `git repository sync *` structured logs — clone/fetch lifecycle and progress
  during hosted repository sync before snapshot and parse stages begin

## Operational notes

- The ingester is the only runtime that should hold the workspace PVC in
  Kubernetes. Do not attach the volume to other workloads.
- Env-driven repository sharding filters repository selection before clone or
  snapshot work by `ESHU_REPO_SHARD_COUNT` and `ESHU_REPO_SHARD_INDEX`.
  Charted horizontal ingesters set shard count from `ingester.replicas` and
  shard index from the StatefulSet `apps.kubernetes.io/pod-index` label. Keep
  the default `volumeClaimTemplates` shape so each shard owns one workspace PVC;
  the chart rejects shared `existingClaim` storage and static shard env
  overrides for multi-replica ingesters.
- Version probes are pre-startup checks. Keep `buildinfo.PrintVersionFlag` at
  the top of `main` so container images can report their build without
  requiring database credentials.
- Align ESHU_SNAPSHOT_WORKERS and ESHU_PARSE_WORKERS with CPU requests to avoid
  CPU throttling under concurrent parsing load. The local-authoritative owner
  sets both to the developer machine's CPU count unless explicit env vars are
  already present.
- If the projector queue age (`eshu_dp_queue_oldest_age_seconds{queue="projector"}`)
  rises while `eshu_dp_repos_snapshotted_total` grows, the projector cannot drain
  as fast as the collector fills. Check projector worker count and graph write
  latency before raising snapshot workers.
- Reducer admission is enabled by default at a 10000-row high-water mark. Set
  ESHU_REDUCER_ADMISSION_HIGH_WATER_MARK=0 to disable it, or set a positive
  value to tune the threshold. The gate wraps only the ingester's source-local
  reducer intent writer, reads reducer queue depth before enqueue, and waits
  outside SQL transactions before rechecking. Bootstrap projection, recovery
  replay, admin reopen, and reducer follow-up lanes bypass this gate so
  freshness-critical repair paths continue.
- Graph-write backpressure (#3560) adds a second admission gate keyed on the
  count of reducer rows retrying with `failure_class=graph_write_timeout`, the
  backend-neutral signal that canonical graph writes are timing out (a bounded
  write that exceeds `ESHU_CANONICAL_WRITE_TIMEOUT` requeues into `retrying` with
  the self-classified `graph_write_timeout` failure class). The signal is scoped
  to that class so a reducer readiness backlog (`secrets_iam_endpoint_not_ready`
  and other `*_n` not-ready classes that also persist as `retrying`) can never be
  mistaken for graph-write pressure and false-throttle unrelated admission. It
  defers at
  ESHU_REDUCER_ADMISSION_RETRYING_HIGH_WATER_MARK (default 500) and resumes only
  below ESHU_REDUCER_ADMISSION_RETRYING_LOW_WATER_MARK (default 100); the gap is
  hysteresis that stops the producer from flapping. Set the high-water mark to 0
  to disable it. This slows the producer so recoverable work stays in the
  retrying bucket instead of exhausting its attempt budget and dead-lettering as
  `retry_exhausted` — it is bounded admission, not worker serialization. See
  `docs/public/reference/nornicdb-tuning.md` for the root-cause writeup.

No-Regression Evidence: the default changes admission from disabled to an
enabled 10000-row backlog threshold without changing reducer worker counts,
claim batch size, queue schema, graph writes, or transaction scope. Below the
threshold, each source-local reducer enqueue performs one bounded queue-depth
read through `QueueObserverStore` and then calls the same `ReducerQueue.Enqueue`
path. At or above the threshold, it waits outside SQL transactions and rechecks
before enqueueing. Verified by
`go test ./cmd/ingester -run 'TestLoadReducerAdmissionConfigDefaultsToEnabledHighWaterMark|TestReducerIntentWriterWithAdmissionUsesDefaultHighWaterMark|TestReducerAdmissionDefaultConfigDefersAtDefaultHighWaterAndResumesBelow' -count=1`
and `go test ./cmd/ingester -run 'TestReducerAdmission|TestLoadReducerAdmissionConfig|TestReducerIntentWriterWithAdmission' -count=1`.
Focused benchmark on Apple M4 Pro with one source-local reducer intent and
synthetic queue depth below the threshold:
`go test ./cmd/ingester -run '^$' -bench 'BenchmarkReducerAdmission(Disabled|BelowHighWater|DefaultBelowHighWater|OneDeferral)$' -benchtime=1000x -count=1`
reported disabled 3.417 ns/op, manually enabled below high-water 49.25 ns/op,
default-active below high-water 39.29 ns/op, and one no-op deferral 87.96
ns/op; all four paths reported 0 allocs/op.

No-Regression Evidence (graph-write backpressure, #3560): the gate is failure-class
scoped and adds a second admission condition without changing reducer worker
counts, claim batch size, queue schema, graph writes, or transaction scope. When
the gate is disabled (retrying high-water mark 0) or the graph-write-timeout depth
is below the marks, the loop performs one bounded depth read and calls the
unchanged `ReducerQueue.Enqueue`. Verified by `go test ./cmd/ingester
./internal/storage/cypher ./internal/storage/postgres -count=1 -race`: a readiness
backlog of 800 `*_not_ready` retrying rows with zero graph-write-timeout depth
does NOT throttle (`TestReducerAdmissionReadinessBacklogDoesNotThrottle`), while a
graph-write-timeout backlog above the high-water mark does
(`TestReducerAdmissionGraphWriteTimeoutBacklogThrottles`), holds through the
high/low hysteresis gap, resumes on recovery, records the `graph_write_timeout`
failure class on the deferral
(`TestReducerAdmissionGraphWritePressureRecordsFailureClass`), and loses no intents
under 16 concurrent producers
(`TestReducerAdmissionGraphWritePressureConcurrentEnqueueShareState`). The reducer
queue preserves `graph_write_timeout` on the retrying row
(`TestReducerQueueFailRetriesGraphWriteTimeoutWithinAttemptBudget`) while a
readiness miss keeps its own class
(`TestReducerQueueFailRetriesReadinessBacklogKeepsOwnFailureClass`), and the scoped
depth query excludes `reducer_retryable` and readiness classes
(`TestReducerGraphWriteTimeoutDepthFiltersFailureClass`). The existing benchmarks
still cover the disabled and below-high-water fast paths.

Observability Evidence (graph-write backpressure, #3560): producer deferrals reuse
the existing `eshu_dp_reducer_admission_deferrals_total` counter and now carry a
`reason` attribute value (`graph_write_pressure` when the scoped graph-write-timeout
depth tripped the gate, `high_water` when total outstanding depth tripped the
original gate) plus a `failure_class` attribute naming the class that drove a
graph-write-pressure deferral (`graph_write_timeout`). This lets an operator
confirm at 3 AM that graph writes are timing out rather than a readiness backlog
piling up. The deferral log was moved from debug level to `Warn` and names
`reason`, `failure_class`, `queue_depth`, `high_water_mark`,
`retrying_high_water_mark`, `retrying_low_water_mark`, and `poll_interval`. The
graph-write-timeout retrying depth is exposed by
`QueueObserverStore.ReducerGraphWriteTimeoutDepth`; no counter or span name,
worker, queue domain, or runtime surface is removed.
- The `local_lightweight` profile (ESHU_QUERY_PROFILE=local_lightweight or
  ESHU_DISABLE_NEO4J=true) skips canonical graph writes entirely; useful for
  laptop code-search workflows where the graph backend is not running.
- The recovery route (`/admin/recovery`) mounts only when
  `NewRecoveryHandler` resolves the API key from the environment. A
  missing route means the key is absent, not that recovery is broken.

## Extension points

- Add a new graph backend by adding a `wiring_<backend>_*.go` file following
  the NornicDB pattern and handling the new ESHU_GRAPH_BACKEND value in
  `openIngesterCanonicalWriter`. The `compositeRunner` and projector wiring do
  not change.
- ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION wires `NewRetryOnceInjector`
  for bounded fault-injection testing; do not use in production.

## Gotchas / invariants

- `compositeRunner` isolates transient faults and only cancels the peer on a
  *fatal* error, then drains the sibling within a bounded grace and joins every
  terminal error with `errors.Join` (see
  `docs/internal/design/3501-ingester-composite-runner-failure-isolation.md`). A
  projector shutdown logged alongside a collector shutdown does not mean both
  failed independently; inspect the joined error and the
  `composite_runner_fatal` / `composite_runner_drain_timeout` log fields to see
  which runner failed and whether the drain was forced.
- `IngestionStore.SkipRelationshipBackfill = true` suppresses per-commit
  backfill; `AfterBatchDrained` handles global relationship maintenance after
  each full drain instead (`wiring.go:195-222`).
- NornicDB grouped writes remain disabled by default. The toggle is
  conformance-only; on NornicDB both states commit per dependency phase, because
  whole-materialization atomic canonical writes silently drop files nested under a
  directory — an UNWIND-driven MATCH cannot see a same-transaction MERGE (#4027).
  Whole-materialization atomic applies only to a same-transaction read-your-writes
  backend (Neo4j).
- NornicDB entity containment is batched into entity upserts by default
  (`wiring_nornicdb_env.go:38-47`). Set
  ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=false only for measured fallback
  comparisons against the older file-scoped shape.
- NornicDB phase grouping keeps canonical retraction statements outside
  matching upsert groups. Grouping a REMOVE-style retract with same-label
  UNWIND upserts can produce a Cypher shape that NornicDB rejects during
  rollback validation.
- ESHU_PROJECTOR_WORKERS defaults to NumCPU when
  ESHU_QUERY_PROFILE=local_authoritative and ESHU_GRAPH_BACKEND=nornicdb. The
  local-authoritative owner also injects that value for normal `eshu graph
  start` runs (`wiring.go:287-292`).
- The NornicDB canonical entity phases dispatch grouped chunks across the
  worker pool sized by ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY. When the
  configured concurrency is greater than one the dispatch uses
  `executeEntityPhaseGroupStreaming` in
  `go/internal/storage/nornicdb/phase_group_executor_streaming.go`: the pool stays open for one
  entity-phase call and pulls chunks from a long-lived channel as the
  producer buffers them, so the slowest chunk in one batch no longer stalls
  workers that have already finished their share. Within an entity label the
  chunks MERGE on disjoint entity_id keys so parallel commit is safe;
  retracts, singletons, and label transitions still synchronize the in-flight
  pool before sequencing dependent work. When concurrency is at most one the
  executor falls back to `executeEntityPhaseGroup` (the prior per-flush wave
  path) so callers without an opt-in see no behavior change.
- `ESHU_CANONICAL_RETRACT_BATCH` controls the per-iteration delete limit for
  the bounded full-refresh retract drain loop on NornicDB. Default: 2000.
  Accepted range: 1–10000; values outside that range fail startup. Each
  full-refresh retract statement (File removal,
  Directory removal, Entity removal) is rewritten at runtime into a loop that
  iterates until the graph reports zero nodes deleted, preventing the unbounded
  `DETACH DELETE` from timing out on large corpora (5000+ files, 10000+
  entities). The emitted WITH clause is shape-dependent (NornicDB v1.1.9
  quirk): relationship-anchored MATCH uses bare
  `WITH <var> LIMIT $__retract_batch`; bare-label MATCH uses
  `WITH <var> ORDER BY elementId(<var>) LIMIT $__retract_batch`. Using the
  wrong clause for the shape returns `__drained=0` (no nodes deleted).
  Delta retracts and positive-list retracts are unaffected.
  The knob is NornicDB-only; Neo4j uses the original single-statement path.
- `openIngesterCanonicalWriter` and its NornicDB/Neo4j tuning-knob loaders
  live in `wiring_canonical_writer_open.go`, split out of `wiring.go` once the
  latter regressed past the repo's 500-line cap. The move is line-for-line:
  no logic, ordering, or defaults changed.
  No-Regression Evidence: same function body relocated verbatim (`git diff`
  shows a pure move, confirmed by diffing pre/post function text); full
  `go/cmd/ingester` package test suite passes unchanged before and after the
  split (`go test ./cmd/ingester/... -count=1` — before: all pass; after: all
  pass, same test count). `go build ./...` and `golangci-lint run ./...`
  clean on the whole module post-split.
  No-Observability-Change: telemetry, span, and log call sites are untouched
  — only their containing file changed, not their behavior or instrument
  wiring.

## Related docs

- `docs/public/architecture.md` — ingester ownership and pipeline
- `docs/public/deployment/service-runtimes.md` — StatefulSet shape, metrics port, env vars
- `docs/public/reference/local-testing.md` — local verification gates
- `docs/public/reference/telemetry/index.md` — metric and span reference
- `docs/public/reference/nornicdb-tuning.md` — NornicDB knobs
- `go/internal/collector/README.md` — collector pipeline detail
- `go/internal/projector/README.md` — projector pipeline detail
