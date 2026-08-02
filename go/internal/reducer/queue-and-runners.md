# Reducer Queue, Intent, And Runner Internals

Split from `README.md` (issue #5786). Intent lifecycle, the queue
claim/execute/ack loop, concurrency and phase-coordination detail,
and bootstrap ordering live here; keep the package overview in
`README.md`.

## Intent lifecycle

`Intent` (declared in `intent.go:138`) carries the durable queue contract.
States: `pending` → `claimed` → `running` → `succeeded` / `failed`.

- `IntentStatusPending`, `IntentStatusClaimed`, `IntentStatusRunning`,
  `IntentStatusSucceeded`, `IntentStatusFailed` — `intent.go:65–74`.
- `ResultStatusSucceeded`, `ResultStatusFailed`, `ResultStatusSuperseded` —
  `intent.go:81–87`.
- `ResultStatusSuperseded` short-circuits execution when
  `GenerationCheck` confirms a newer generation is active for the scope.

## Queue claim / execute / ack loop

`Service` (declared in `service.go:54`) coordinates the main loop:

- **Sequential** (`Workers <= 1`): `Claim` → `executeWithTelemetry` →
  `Ack` or `Fail` in order.
- **Concurrent** (`Workers > 1`): N goroutines compete. When `WorkSource`
  implements `BatchWorkSource` and `WorkSink` implements `BatchWorkSink`,
  the batch path reduces Postgres round-trips.
- **Heartbeat**: `startHeartbeat` (`service.go:409`) spawns a goroutine
  that calls `Heartbeat` at `HeartbeatInterval`; the heartbeat is stopped
  before `Ack` or `Fail` to avoid lease extension after the transaction
  commits.

`Service.Run` also starts `SharedProjectionRunner`, `CodeCallProjectionRunner`,
`RepoDependencyProjectionRunner`, and `GraphProjectionPhaseRepairer` as
concurrent goroutines. Any runner error cancels the shared context.

## Cross-scope producer completion

`container_image_identity` and `ci_cd_run_correlation` ACKs append one durable
producer-completion event in the same Postgres statement that marks the work
item batch succeeded. `CrossScopeCompletionRunner` leases those events with an
owner plus monotonic claim epoch, coalesces an exact bounded event set, updates
current-generation canonical consumer rows in place, and deletes only the
captured event set in the same transaction. A succeeded consumer returns to
pending; an already claimed or running consumer records
`cross_scope_replay_required` so its ACK must reopen it once. Pending and
retrying rows already guarantee a future execution, and dead-letter rows remain
terminal.

The dependency edges are identity -> CI/CD, identity -> supply-chain impact,
and CI/CD -> supply-chain impact. The direct identity -> supply-chain edge lets
new image truth refresh findings promptly; the later CI/CD completion refreshes
the supply-chain consumer again so an early supply-chain run cannot strand a
partial identity snapshot. Fanout never clones a work item, so retained queue
history cannot multiply the replay workload.

Failures persist as one bounded-backoff queued retry per producer domain,
merged with any event that arrived while the failed lease was live.
Expired leases are reclaimable, but every heartbeat, retry, and fanout is
fenced by the exact event, producer domain, owner, and claim epoch. The generic
queue gauges expose a bounded name such as
`queue=cross_scope_completion.container_image_identity`; a growing depth or
oldest age means convergence is delayed even if the main reducer queue is
otherwise empty. Successful cycle logs include the producer domain, coalesced
producer item count, scheduled consumer count, and fanout duration.

## Repo-dependency acceptance-unit concurrency

`RepoDependencyProjectionRunner` shards work by the source-repository
acceptance-unit identity, not by individual edge rows. The fixed shard mapping
keeps a repository's complete retract-then-rewrite cycle on one worker. That is
the per-repo safety gate: work for the same source repository remains serialized
and ordered, while unrelated repositories assigned to different shards can run
concurrently.

The command runtime exposes this lane through
`ESHU_REPO_DEPENDENCY_PROJECTION_WORKERS`. Only `1`, `2`, and `4` are supported;
the backend-aware default is the remotely proven `4` on NornicDB and the
unscaled `1` on Neo4j. Invalid values fall back to that backend default. Values
`1` and `2` remain explicit NornicDB resource-constrained fallbacks. Each
process derives a distinct lease owner by appending its hostname, PID, and a
boot-unique nonce to the
configured owner prefix. The default `45s` whole-cycle deadline covers the
Postgres repository lock, active-lease validation, graph replacement, intent
completion, and Postgres commit. The `5m` lease must exceed that deadline plus
the canonical graph-write timeout and a `30s` margin. Errors, cancellation, and
ambiguous commits retain the shard lease until expiry and force the same owner
to wait out the quarantine. Independent shards continue to run, so this safety
contract does not globally serialize repo-dependency work. Changing the worker
count does not weaken acceptance-unit atomicity and does not inherit the main
reducer's `ESHU_REDUCER_WORKERS` value.

## Graph projection phase coordination

`graph_projection_phase_state` is the durable readiness coordination table.
Phases and keyspaces are declared in `graph_projection_phase.go`.

Key phases:

| Phase constant | Meaning |
| --- | --- |
| `GraphProjectionPhaseCanonicalNodesCommitted` | Projector canonical node writes committed |
| `GraphProjectionPhaseSemanticNodesCommitted` | Semantic entity reducer writes committed |
| `GraphProjectionPhaseDeployableUnitCorrelation` | Deployable-unit correlation pass finished |
| `GraphProjectionPhaseDeploymentMapping` | `deployment_mapping` domain finished one bounded slice |
| `GraphProjectionPhaseWorkloadMaterialization` | `workload_materialization` domain finished |
| `GraphProjectionPhaseCrossSourceAnchorReady` | Reserved for DSL cross-source anchor publication |

`GraphProjectionPhasePublisher` (interface at `graph_projection_phase.go:117`)
is the only write path for phase rows. Use `publishIntentGraphPhase`
(`graph_projection_phase_publish.go`) inside handlers rather than calling the
publisher directly.

Canonical-node materializers publish `GraphProjectionPhaseCanonicalNodesCommitted`
on a per-node-type keyspace so an edge slice can gate on the exact node family it
joins: `GraphProjectionKeyspaceCloudResourceUID` (AWS resource nodes, issue #805)
and `GraphProjectionKeyspaceKubernetesWorkloadUID` (live `KubernetesWorkload`
nodes, issue #388). The durable claim/blockage gate in
`go/internal/storage/postgres` fences each *edge* domain on its node keyspace's
phase; that gate clause is added when the edge domain ships, so this prerequisite
slice publishes the `kubernetes_workload_uid` phase but adds no edge-gate clause.

`GraphProjectionPhaseRepairQueue` (`graph_projection_phase_repair.go:36`) and
`GraphProjectionPhaseRepairer` (`graph_projection_phase_repair_runner.go:58`)
handle the case where a graph write commits but the subsequent phase
publication fails; the repairer retries exact rows durably.

## Code-call materialization

`ExtractCodeCallRows` turns parser `function_calls` and SCIP call facts into
canonical `CALLS` or `REFERENCES` edge intents. Resolution stays evidence
bounded: same-file and parser-proven language metadata win before broader
repository matching, type and reflection references stay `REFERENCES`, and
duplicate facts for the same caller, callee, and reference line collapse before
graph writes.

Keep the detailed resolver ordering, language metadata rules, JavaScript
static-alias cache contract, SCIP bypass, and handler timing log in
[`code-call-materialization.md`](code-call-materialization.md).

No-Regression Evidence: `go test ./internal/reducer -run 'TestCodeCallMaterializationHandlerLoadsActiveCrossRepoSymbolDefinitions|TestExtractCodeCallRowsResolvesCrossRepo|TestCodeCallDefinitionSymbolKeysIgnoreGenerationFields' -count=1` failed before the production handler loaded active cross-scope definition facts, then passed after definition rows supplied stable symbol keys and calls matched those keys before repo-unique fallback. `go test ./internal/storage/postgres -run TestFactStoreLoadActiveCodeCallSymbolDefinitionFacts -count=1` proves the Postgres loader is active-generation, non-tombstone, file-kind, symbol-allowlist, and keyset-page bounded. Ambiguous symbol keys with more than one target are deliberately not indexed, preserving the existing unique-or-unresolved rule.

Observability Evidence: the existing `code call materialization completed` log now includes stable symbol key count, active definition fact count, and active symbol-definition load duration beside the existing fact count, repository count, row counts, and load/extract/intent/upsert timings. The change adds no metric instrument, metric label, span, route, runtime knob, queue table, or graph backend branch.

## Shared projection runner

`SharedProjectionRunner` (`shared_projection_runner.go:95`) iterates
shared-projection domains by domain and partition. `CodeCallProjectionRunner`
owns `code_calls` separately because it preserves repo-wide retraction
semantics while processing large accepted units in capped chunks. Edge domains
stay readiness-gated; the local-authoritative code-call drain gate schedules
work only and never changes admitted graph truth.

Keep the runner loop, back-off behavior, `LoadSharedProjectionConfig`
configuration contract, SQL trigger `EXECUTES` reachability rule, and
inheritance/SQL entity-type filters in
[`shared-projection.md`](shared-projection.md).

## Facts-First Bootstrap Ordering

The bootstrap pipeline in `go/cmd/bootstrap-index/main.go` enforces a
multi-pass ordering that the reducer must honor:

```text
Phase 1 — Collection + First-Pass Reduction
  Projector drains and emits canonical nodes. deployment_mapping can remain
  pending because resolved_relationships do not yet exist.

Phase 2 — Backfill
  BackfillAllRelationshipEvidence() (bootstrap-index/main.go:236)
  populates relationship_evidence_facts and publishes readiness rows.

Phase 3 — Deployment Mapping Reopen
  ReopenDeploymentMappingWorkItems() (bootstrap-index/main.go:273)
  reopens deployment_mapping so the reducer can create resolved_relationships.

Phase 4 — Second-Pass Consumers
  Any domain consuming resolved_relationships must have a re-trigger
  mechanism after Phase 3.
```

**Critical rule**: any reducer domain or sub-package that consumes
`resolved_relationships` must have a post-Phase-3 reopen or re-trigger
mechanism. Adding a new consumer without that mechanism creates an E2E-only
bug that is invisible in unit and integration tests.
