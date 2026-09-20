# #6785: Value-flow cloud-sink refresh after late producers

Status: **decided 2026-09-20: Option A singleton fixpoint-only refresh, 4
producers** (workload_materialization, workload_cloud_relationship_materialization,
iam_can_perform_materialization, aws_resource_materialization). The
account-keyed migration from earlier notes had no owner source and is dropped;
B-7 (565 pass, sl-value-flow-cloud-sink count=1 with no refresh) proves the
corpus drain converges without it, so the refresh is production-race insurance
for continuous ingestion, where no later generation re-runs the summary.

Singleton anchor (no fanout SQL change): migration seeds scope `eshu:global`
(kind `global`) with one perpetually-active generation plus a
`code_value_flow_refresh:global` item. The existing fanout
`current_consumers` join matches it via active_generation_id, so producer
completions in ANY later generation reopen it — the later-generation
re-enqueue falls out of the existing join. Retention prunes superseded
generations only; nothing ever supersedes the lone global generation.

Emit gate (Option A): each producer handler reports `refresh_affected_repos`
(graph gate over written keys, `affected.ShouldEmitRefresh`) alongside the
existing `CanonicalWrites`. ACK emits when `CanonicalWrites` is positive and
the signal is absent (unwired producers fail open) or positive; an explicit
zero suppresses the event. Relevance rides the already-passed
`reducer.Result` into single/batch ACK SQL (`value_flow_refresh_ack.go`):
one emitting statement per producer domain with the gate outcome as a flag,
bumping `cross_scope_completion_ack_epoch` so the rolling-upgrade fallback
trigger never double-emits. Migration 093's CHECK, trigger domain list, and
the fanout consumer index admit the new domains (112 converges older
installs; the consumer index moves to a v2 name per the no-rebuild rule).

Residual (file separately): the refresh adds one more global
retract-and-rewrite writer beside the concurrent per-repo summary fixpoint
runs (pre-existing summary-vs-summary race class); the refresh serializes
against itself on a global conflict key.

Open question below is retained as the rejected-alternatives record.

## Problem

The value-flow fixpoint writes the cloud-sink edge
`(:Function)-[:TAINT_FLOWS_TO {cloud:true, sink_kind:"iam_privileged_action"}]->(:Function)`.
It reads the graph chain
`INVOKES_CLOUD_ACTION`, `RUNS_IN -> Workload`, `Workload <-INSTANCE_OF- WorkloadInstance -USES-> principal -CAN_PERFORM-> resource`
(`go/internal/reducer/code/value/cloud_sink_loader.go`). The only caller is the
`code_function_summary` handler, which the projector enqueues once per repo
generation (`go/internal/projector/code/function/summary/reducer_intent.go:62`).
The live B-7 run recorded on #6785 showed it running with `workload_row_count=0`
before `RUNS_IN`, `USES` and `CAN_PERFORM` existed. Nothing re-ran it, so the
corpus has 0 cloud-sink edges even though `CloudSinkTargetsByPairCypher` now
returns the full chain. Continuous ingestion has the same race.

## Pre-Edit Gate

1. **Entry point.** `cmd/reducer/main.go` builds the reducer service.
   `cmd/reducer/wiring_handlers.go:241` builds `FixpointEvidenceProjector` (with
   the `CodeInterprocProjectedEdgeStore` ledger) and hands it to the
   `code_function_summary` handler
   (`internal/reducer/defaults_additive_domains_incident_code.go:62`). The
   durable completion runner is `internal/reducer/cross_scope_completion_runner.go`,
   and it runs as a reducer service-side runner.
2. **Phase ordering.** Projector enqueues every domain intent for a scope
   generation. In the reducer, `code_function_summary` (repo scope),
   `workload_materialization` (repo scope, writes Workload/WorkloadInstance, and
   `RUNS_IN` rides the shared-projection path gated on it),
   `workload_cloud_relationship_materialization` (AWS scope, `USES`) and
   `iam_can_perform_materialization` (AWS scope, `CAN_PERFORM`) have **no
   ordering between them**. Their conflict keys are scope-scoped, so they
   drain concurrently across scopes.
3. **Data dependencies.** The fixpoint needs durable `function_summaries`,
   `function_sources` and `function_graph_ids` rows (written by the same
   handler, for all repos) and the graph chain above (written by three other
   domains in other scopes).
4. **Re-trigger.** None exists today; this note designs it.

## What the fixpoint actually computes (fact that drives the design)

`FixpointEvidenceProjector.ProjectValueFlowFixpointEvidence`
(`internal/reducer/code/value/fixpoint_evidence_loader.go:376+`) is **global**:

- `loadEffects`, `loadSources` and `loadGraphIDs` ignore `scopeID`/`generationID`.
  They read every repo (`loadFunctionSummariesSQL` has no `WHERE`,
  `internal/storage/postgres/summary_store.go:60`).
- `LoadCloudSinkTargets` is called with the global graph-id map.
- Retraction removes every edge of evidence source
  `reducer/code-interproc-fixpoint` (by ledger source uids), then rewrites the
  full solve. `scopeID`/`generationID` only stamp the new rows and ledger.

So one run refreshes cloud sinks for **every** repo; N per-repo runs repeat the
same global solve N times.

## What the existing durable completion path can express

`cross_scope_completion_events` (ACK-side emit in
`internal/storage/postgres/reducer_queue_ack.go`, claim/fanout in
`cross_scope_completion_queue.go` and `cross_scope_completion_fanout.go`):

- Holds **one pending row per producer domain** (partial unique index on
  `producer_domain` for `pending`/`retrying`). Several ACKs coalesce into that row
  by `producer_item_count`. The row has **no repo or scope payload**.
- Fanout reopens **every** current-active-generation `succeeded` work item of
  each consumer domain declared in `crossscope.dependencyCatalog`
  (`internal/reducer/crossscope/dependencies.go`). It isn't bounded by repo.
- Only `container_image_identity` and `ci_cd_run_correlation` emit today
  (`ReducerQueue.Ack` switch, `internal/storage/postgres/reducer_queue.go:419`).

Making `code_function_summary` a catalog consumer would reopen every repo's
summary on each producer drain, the same cost as the rejected reopen option.

## Open question for the owner

The binding decision asks for two things: (a) a refresh "for ONLY the affected
repositories" and (b) "using the existing durable completion-event path". The
code above means both can't hold together:

- The existing path is domain-grained, so it can only reopen all repos.
- Bounding by repo gives no cost benefit, because the refreshed computation is a
  single global solve plus a global retract and rewrite. Pick one:

**Option A (recommended): a global coalesced fixpoint refresh through the
completion path, with an affected-repo gate at emit time.**
- Add reducer domain `code_value_flow_refresh`. Its handler calls **only**
  `ProjectValueFlowFixpointEvidence` and skips summary, source and graph-id
  persistence (unchanged by the producers).
- The four producers (workload_materialization, workload_cloud_relationship_materialization, aws_resource_materialization and iam_can_perform_materialization) emit a completion event only when the ACKed run wrote
  more than 0 rows **and** the affected-repo gate below is non-empty. That means
  a producer-specific ACK query plus a handler-computed "relevant" flag carried
  on the result.
- The consumer is **one** work item at a fixed singleton identity
  (`EntityKey: "code_value_flow_refresh:global"`). Fanout reopens it once per
  captured event set, so any number of completions in a drain coalesce into
  roughly one solve.
- Open sub-point: the fanout joins consumers to `active_generations` by
  `(scope_id, generation_id)`. A singleton needs an anchor scope. That means a
  synthetic always-active scope row, or a new fanout branch for
  scope-independent consumers. The second one is new SQL on a queue/lease
  path, so it needs a contention proof per Evidence Rules.

**Option B: a per-repo refresh intent enqueued by the producer handler (not the
completion queue).**
- Follows the targeted-replay precedent `ReplayWorkloadMaterializationForFence`
  (`internal/reducer/repo_dependency_projection_replay.go`).
- Each producer reopens the `code_function_summary` work item of each affected
  repo's active generation (entity key `code_function_summary:<scope>`).
- It is truly repo-bounded, but it does N global solves and re-persists
  unchanged summaries. Correct, but it costs N× Option A. It also doesn't use
  the completion path, so it departs from (b).

**Option C: per-repo completion events** (target-scope column + per-target
fanout). Largest change, and still N global solves without A's coalescing.

## Affected-repository derivation (needed by A's gate and by B)

| Producer completion | Affected repos |
| --- | --- |
| `workload_materialization` | the intent's own repo (`repo:<id>` entity key / `payload.repo_id`) |
| `workload_cloud_relationship_materialization` | written rows carry `workload_id`; repo = `(r:Repository)-[:DEFINES]->(:Workload {id})` |
| `iam_can_perform_materialization` | the `principal_uid`s of the CAN_PERFORM edges it **wrote** (principal and target may be in different scopes of one account, so never the completing scope's own resources); repos via `(r:Repository)-[:DEFINES]->(w:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(:CloudResource {uid: principal_uid})` |

USES now defers on WorkloadInstance readiness (not the reopen list); no option
relies on `crossScopeCorrelationReopenDomains`. Gate: keep a repo only if it has at least one `Function` with both
`INVOKES_CLOUD_ACTION` and `RUNS_IN` into an affected workload, which is the
same shape as `CloudSinkWorkloadRowsCypher`. An empty set means no emit. That
keeps the common case (no cloud-calling handlers) free.

## Full rerun vs fixpoint-only

Correctness comes first, and both are equally correct. The producers change
only graph edges that `LoadCloudSinkTargets` reads. Summary, source and
graph-id rows derive from repo facts the producers don't touch, and the
fixpoint reloads them globally anyway. On cost, fixpoint-only avoids the fact
reload, decode and `ReplaceSnapshot`/`ReplaceSources`/`ReplaceGraphIDs` writes.
The `FixpointCache` and durable component store keep unchanged components
reused, so the dominant cost is the cloud-sink graph read plus the global
retract and rewrite. Before shipping, measure the per-solve wall time on the
B-7 corpus (reducer log `value-flow fixpoint evidence loaded`).

## Idempotency, dedupe, ordering, retries, supersession, leases (Option A)

- **Idempotency.** Global retract then MERGE on `evidence_uid`; re-runs converge.
- **Dedupe.** The partial unique `producer_domain` row coalesces ACKs per
  producer. Fanout reopens the singleton at most once per captured set. A
  refresh that is already running gets `cross_scope_replay_required=TRUE`, so
  exactly one follow-up run happens and completions landing mid-solve are not
  lost.
- **Ordering.** The event is emitted in the same statement as the producer ACK,
  after its graph write committed. Fanout visibility is ≥250 ms later, so the
  refresh always reads the producer's edges.
- **Retries.** Existing bounded-backoff fanout `Retry`; normal reducer retry/dead-letter.
- **Generation supersession.** The fixpoint reads current durable and graph
  state, so an older producer generation's event only causes one extra solve
  over current truth. It never writes stale truth.
- **Leases.** One live lease per producer domain; one conflict key for the singleton.
- **Pre-existing concurrency risk.** Concurrent `code_function_summary` runs
  for different repos each do an unfenced global retract and rewrite. A refresh
  adds one more writer, so it should share one conflict key with the fixpoint
  step. File the summary-vs-summary race separately.

## Sequence (Option A)

```mermaid
sequenceDiagram
    participant P as Producer handler (workload / USES / CAN_PERFORM)
    participant Q as fact_work_items + ACK
    participant E as cross_scope_completion_events
    participant R as CrossScopeCompletionRunner
    participant F as code_value_flow_refresh handler
    participant G as Graph (NornicDB)
    P->>G: write RUNS_IN / USES / CAN_PERFORM
    P->>P: derive affected repos, gate on cloud-calling functions
    P->>Q: Ack(result{relevant:true})
    Q->>E: same txn: upsert pending row for producer_domain (coalesce)
    R->>E: Claim (one live lease per producer)
    R->>Q: Fanout: reopen singleton refresh item (or mark replay_required)
    Q->>F: claim singleton refresh
    F->>G: global solve + retract/rewrite fixpoint TAINT_FLOWS_TO
    F->>Q: Ack (no emit)
```

## Telemetry and tests (after the decision)

Telemetry: counter `eshu_dp_value_flow_refresh_gate_evaluations_total{domain, outcome}`
(`affected|suppressed|fail_open`, one point per producer gate evaluation),
span `reducer.value_flow_refresh_gate` carrying the `outcome` and
`affected_repo_count` attributes, and a `value-flow refresh completed`
structured log with `scope_id`, `generation_id`, `fixpoint_finding_count`,
`fixpoint_graph_rows` and `fixpoint_unresolved_endpoint_count`, documented
under `docs/public/reference/telemetry/`.

Tests: RED handler test where the chain lands after the first summary run and
nothing re-triggers. Unit tests for the emit gate and for coalescing (N
completions give one reopen). A refresh handler test that writes the cloud-sink
edge and is idempotent on re-run. A Postgres live contention test for any fanout
SQL change. A golden `required_self_loops` row on `StoreUploadReceipt`
`TAINT_FLOWS_TO` (min 1, max 1).
