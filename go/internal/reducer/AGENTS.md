# AGENTS — internal/reducer

This file guides LLM assistants working in `go/internal/reducer`. Read it
before touching any file in this directory.

## Read first

1. `CLAUDE.md` **entirely** — especially "Facts-First Bootstrap Ordering",
   "Correlation Truth Gates", "Concurrency Workflow", and "Golden Rules 1–4".
2. `docs/public/architecture.md` — service boundaries and data flow.
3. `docs/public/deployment/service-runtimes.md` — Resolution Engine section.
4. `docs/public/reference/telemetry/index.md` — observability contract.
5. `go/internal/projector/README.md` — projector→reducer handoff and phase
   publication model.
6. `go/cmd/reducer/README.md` — runtime wiring context.

## Invariants (cite file:line)

- **Every domain must be cross-source, cross-scope, and truth-emitting** —
  `registry.go:53` `OwnershipShape.Validate`; registration fails unless the
  domain declares either durable canonical writes or bounded counter emission.
- **Intent lifecycle is fixed: pending → claimed → running → succeeded/failed** —
  `intent.go:65–74`; do not invent additional states.
- **Generation supersession short-circuits execution** — `runtime.go:336`
  checks `GenerationCheck` before dispatching to `Handler.Handle`; return
  `ResultStatusSuperseded` rather than projecting stale truth.
- **Heartbeat stops before Ack** — `service.go:337` calls `stopHeartbeat()`
  before `WorkSink.Ack`; do not reorder this or you risk lease extension
  after the transaction has committed.
- **`deployment_mapping` blocks on `resolved_relationships`** —
  `DomainDeploymentMapping` handler consumes relationships that do not exist
  until Phase 3 of the bootstrap pipeline. Any domain added as a consumer of
  `resolved_relationships` must have a post-Phase-3 reopen mechanism. See
  `bootstrap-index/main.go:273`.
- **Phase publications and graph writes are not atomic** — if a write
  commits but the publication fails, `GraphProjectionPhaseRepairQueue`
  captures the retry. Do not skip enqueueing to the repair queue when a
  publish fails.
- **Shared projection intent IDs are stable SHA256 hashes** —
  `shared_projection.go:62–74`; changing the default identity fields breaks
  in-flight idempotency. `IdentityKey` is a narrow override for domains that
  must store several rows under one durable `PartitionKey` without collapsing
  their `intent_id`s; audit every caller before using it.
- **Edge domains gate on readiness phases** — `sharedProjectionReadinessPhase`;
  `code_calls`, `inheritance_edges`, `sql_relationships`, and `rationale_edges`
  gate on `canonical_nodes_committed` because their targets are canonical or
  created inline. `semantic_nodes_committed` can stall them forever (#2867-#2869).
- **Promoted edge domains keep their handler's evidence source** — a domain moved
  onto the shared-projection runner must keep its original `evidence_source`, not
  the runner's global source: inheritance, SQL, and rationale keep
  `reducer/inheritance`, `reducer/sql-relationship`, and `reducer/rationale-edge`.
- **SQL trigger functions materialize as `EXECUTES` edges** —
  `ExtractSQLRelationshipRows` reads `function_name` from `SqlTrigger`
  metadata and writes trigger-to-`SqlFunction` `EXECUTES` rows
  (`sql_relationship_materialization.go:347`). Code dead-code uses those rows
  as incoming reachability for stored routines.
- **SQL migration files materialize as `MIGRATES` edges** — the `SqlMigration`
  case in `ExtractSQLRelationshipRows` reads `migration_targets` (`{kind,name,
  operation}`) from `SqlMigration` metadata and resolves each via
  `resolveSQLMigrationTarget`, which constrains by **both kind and name** and
  prefers a same-file match. **Ambiguity trap** (#5346): a repo that keeps both
  `schema.sql` and a migrations dir has two same-kind same-name objects (e.g.
  two `SqlTable "users"`); a target that resolves to more than one candidate
  across different files is **skipped and tallied** (`AmbiguousMigrationTargets`),
  never guessed — same never-fabricate discipline as READS_FROM. `select`-only
  mentions are excluded from migration targets (a backfill's read is not a
  migrate). DROP is not parsed yet (deferred).
- **All canonical graph writes go through `internal/storage/cypher`** — no
  handler may call a Neo4j or NornicDB driver directly.
- **`JavaScript` dynamic-call alias parsing is indexed once per function** —
  `buildCodeEntityIndex` caches static alias metadata
  (`code_call_materialization_index.go:45`) and
  `resolveDynamicJavaScriptCalleeEntityID` reuses that cache
  (`code_call_materialization_dynamic_javascript.go:41`). Do not move that
  work back into the per-call loop; generated JS bundles make that
  multiplicative. Cache negative scans too; a source with no static aliases
  must not be sent through the regex pass once per call.
- **Receiver-constrained call-resolution precision corpus** —
  `code_call_resolution_goldens_*_test.go` (issue #3156) locks in per-language
  cross-file resolution truth (`resolution_method` + derived confidence) and
  guards against false positives (ambiguous/dynamic/missing-dependency calls
  must not silently bind to a wrong target). When you improve a resolver,
  flip the matching documented gap (`idealMethod`/`gapIssue` or
  `falsePositiveGap`, tracked by #3198) into a strict assertion in the same
  change — the corpus FAILS if a tracked gap is fixed but its marker is left
  behind, so markers cannot go stale.
- **Security-alert reconciliation identity is provider-alert stable** —
  `security_alert_reconciliation_writer.go` must key reducer facts by provider,
  provider alert id or number, provider evidence scope, package id, and advisory
  ids. Do not include mutable canonical `repository_id` or source fact id in
  the replacement identity, or provider-only placeholders can remain active
  beside later matched or stale rows for the same provider alert.

## Common changes

### Add a new reducer domain

1. Add a `Domain` constant to `domain.go` and `knownDomains` map.
2. Write the handler struct satisfying the `Handler` interface.
3. Add the handler to `implementedDefaultDomainDefinitions` in `defaults.go`.
4. Add a `DomainDefinition` (with `OwnershipShape` and `TruthContract`) to
   `DefaultDomainDefinitions` in `registry.go` only when the domain is
   unconditionally wired. Adapter-gated domains such as
   `DomainAWSCloudRuntimeDrift` use an additive helper so the runtime cannot
   register a domain that has no durable publication path. Add the gated
   registration to the matching themed sibling helper in
   `defaults_additive_domains_*.go` (correlation, supply_chain, secrets_drift,
   cloud_nodes, cloud_relationships, cloud_posture, or incident_code) — not the
   `appendAdditiveDomainDefinitions` orchestrator, which only chains the helpers.
   Registration is keyed by `Domain` in `Registry.Register`, so the append order
   across helpers is not runtime-observable.
5. Wire the backend adapters in `cmd/reducer/main.go` `DefaultHandlers`.
6. If the domain consumes `resolved_relationships`, add a post-Phase-3
   reopen in `bootstrap-index/main.go` after ReopenDeploymentMappingWorkItems.
7. Add telemetry: at minimum the service-level `telemetry.SpanReducerRun` span
   and `eshu_dp_reducer_executions_total` counter, plus a domain counter when
   the domain is counter-emission truth such as package source correlation or
   AWS runtime drift.
8. Write a failing test first; confirm it fails for the right reason.

### Change reducer queue claim semantics

- Any change to `WorkSource.Claim`, `BatchWorkSource.ClaimBatch`, or
  `WorkSink.Ack`/`Fail` is a concurrency change. Follow CLAUDE.md
  "Concurrency Workflow" fully before writing code.
- Prove idempotency: a duplicate claim or partial failure must converge on
  the same graph truth, not produce duplicate or absent rows.

### Add a new graph projection phase or keyspace

1. Add the constant to `graph_projection_phase.go`.
2. Verify the new constant does not conflict with existing keyspace usage in
   `shared_projection.go:91–99`.
3. Update `internal/storage/postgres` schema DDL if a new readiness row
   shape is needed.
4. Update `sharedProjectionReadinessPhase` in `shared_projection.go` if the
   new phase gates a shared-projection domain.

### Change shared projection runner config

- Env var parsing lives in `LoadSharedProjectionConfig`
  (`shared_projection_runner.go:476`); constants live in `cmd/reducer/config.go`.
- Update both the runner config and the README config table in the same PR.

## Failure modes

- **Stuck `deployment_mapping`**: queue shows `deployment_mapping` items in
  `pending` or `failed` state long after bootstrap. Check whether
  ReopenDeploymentMappingWorkItems ran in the bootstrap pipeline;
  cross-reference `graph_projection_phase_state` for
  `backward_evidence_committed` rows.
- **Missing phase publication causing edge domain blocking**: shared
  projection logs "skipped intents until semantic readiness is committed"
  at high frequency. Check `graph_projection_phase_state` for
  `semantic_nodes_committed` or `canonical_nodes_committed` rows for the
  affected `AcceptanceUnitID`.
- **Repair queue growth**: `graph_projection_phase_repair` table grows
  without drain. Check `GraphProjectionPhaseRepairer` logs for
  `graph_projection_repair_publish_failed`; verify the phase publisher's
  Postgres connection is healthy.
- **Generation supersession flood**: `reducer_executions_total{status="superseded"}`
  rises. Investigate whether the ingester is emitting new generations faster
  than the reducer can drain old ones.
- **Heartbeat lease failure**: `lease_heartbeat_failure` in logs means the
  lease expired mid-execution; the intent will be re-claimed. Root cause is
  usually slow graph writes or Postgres saturation.
- **Slow `code_call_materialization` extraction**: if the completion log shows
  high `extract_duration_seconds` with low fact count, inspect large
  JavaScript `function_calls` arrays and run
  BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls before changing
  graph or queue code.

## Evidence notes

No-Regression Evidence (#4568 typed-payload decode): the AWS/IAM/security-group
reducer handlers now decode fact payloads through the `sdk/go/factschema` seam
instead of raw `payloadString` map lookups. The seam originally serialized every
fact twice (`json.Marshal(map)`+`json.Unmarshal(struct)`), which regressed the
hot join-index/node-projection path 2.9-4.3x. That was replaced with a
marshal-free reflection decoder (`decode_map.go`, cached per-type field plan), so
the typed path is within ~10% of the pre-migration raw-map baseline with fewer
allocations. Measured with the existing benchmarks (backend: in-memory extractor;
input: the benchmark corpus each already builds; darwin/arm64, `-count=3`):
`go test ./internal/reducer -run '^$' -bench 'BenchmarkExtractCloudResourceNodeRows|BenchmarkExtractAWSRelationshipEdgeRows|BenchmarkBuildCloudResourceJoinIndex' -benchmem`.
BEFORE (raw map, commit 7d313deb6) -> AFTER (typed decode):
BuildCloudResourceJoinIndex 22.4ms/330k allocs -> 24.5ms/310k allocs (+11% time,
-6% allocs); ExtractCloudResourceNodeRows 15.3ms/240k -> 15.6ms/220k (+2% time,
-8% allocs); ExtractAWSRelationshipEdgeRows 35.2ms/521k -> 38.5ms/497k (+10% time,
-5% allocs). The residual handler-time cost buys the accuracy guarantee (a fact
missing a required identity field dead-letters `input_invalid` instead of
producing an empty-string graph uid) and is a bounded, measured cost within the
diagnostic-rigor ~10% band, not an unmeasured regression. Result class:
Correctness win with a bounded, measured handler cost.

No-Observability-Change (#4568): the typed-decode migration adds no route, graph
query shape, queue table, worker, lease, runtime knob, metric instrument, metric
label, or log key. A malformed fact surfaces through the EXISTING dead-letter
path — `WorkSink.Fail` -> `deadLetterTriageMetadata` -> the durable
`fact_work_items.failure_class=input_invalid` row and the reducer execution
counters/spans operators already use. The decode error self-classifies via the
existing `FailureClass()`/`Retryable()` reducer error interface.

### Namespace environment-alias binding (#5434)

No-Regression Evidence: `DomainKubernetesNamespaceMaterialization`
(`kubernetes_namespace_materialization.go`) is a NEW additive domain, gated on
`FactLoader`+`KubernetesNamespaceNodeWriter` exactly like
`kubernetesWorkloadMaterializationDomainDefinition`; it registers and runs only
when the reducer binary explicitly wires the new writer
(`cmd/reducer/canonical_graph_writers.go`/`wiring_handlers.go`), so no existing
domain's registration order, intent shape, or handler behavior changes. No
existing fact kind, decode seam, or writer is touched. `go test
./internal/reducer/... ./internal/storage/cypher/... ./internal/payloadusage/...
./internal/facts/... ./cmd/reducer/... -count=1 -race` passes byte-identical for
every pre-#5434 path (4336 passed). The write path is a bounded batched `UNWIND`
+ `MERGE` on the collector-emitted `object_id` uid (mirrors
`KubernetesWorkloadNodeWriter`'s proven shape, same NornicDB schema-backed uid
lookup), split into at most two statements per batch (bound vs. unbound rows) --
no new lock, transaction, or shared-state path; each row's own `environment`
property purely local-decides its Cypher variant, so there is no cross-row or
cross-namespace contention.

No-Observability-Change: no new metric instrument, metric label, span, status
field, queue domain, worker count, batch size, or runtime knob. The domain
reuses the EXISTING `instruments.ReducerInputInvalidFacts` counter (via
`recordQuarantinedFacts`) for a malformed `kubernetes_live.namespace` fact --
same instrument, new `domain`/`fact_kind` attribute VALUES on the existing
closed low-cardinality label set, not a new label or instrument. Completion is
logged via the existing `slog` domain-completion pattern
(`kubernetes namespace materialization completed`), mirroring
`logKubernetesWorkloadMaterializationCompleted`'s fields with no new log key
family. Deliberately does NOT reuse `KubernetesWorkloadNodes` for the
node-materialized count (that would corrupt an existing counter's semantics
by mixing two distinct node kinds under one instrument), and does not add a
replacement counter in this change -- see `README.md`'s Telemetry section.

No-Regression Evidence: `go test ./internal/reducer -run 'TestSecurityAlertReconciliationFactIdentitySurvivesProviderOnlyToMatched|TestSecurityAlertReconciliationFactIdentitySurvivesMatchedToStale' -count=1` failed before reducer fact identity ignored mutable repository/source-fact fields, then passed. `go test ./internal/query -run 'TestPostgresSecurityAlertReconciliationQueryShape|TestSecurityAlertReconciliationAggregateQueriesUseCurrentProviderAlertRows' -count=1` failed before default list/count/inventory reads ranked one current provider-alert row before status and state filters, then passed.

No-Observability-Change: the change only adjusts reducer fact replacement identity and the existing Postgres read-model selection for security-alert reconciliations. It adds no route, graph query, queue, worker, runtime knob, metric instrument, or metric label; operators still diagnose the path through existing reducer run spans, reducer execution counters, durable `reducer_security_alert_reconciliation` payloads, query handler spans, and Postgres query duration metrics.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSecurityAlertReconciliations(ClassifiesProviderAlertStates|DoesNotCopyProviderVersionIntoObservedVersion|ReportsMissingAndMalformedObservedVersions)' -count=1`, `go test ./internal/query -run 'Test(SupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState|DecodeSecurityAlertReconciliationRowPreservesOwnedPackageEvidence|OpenAPISpecIncludesSecurityAlertReconciliations)' -count=1`, `go test ./internal/mcp -run 'Test(SecurityAlertReconciliationToolAdvertisesOwnedObservedVersion|ResolveRouteMapsSecurityAlertReconciliationsToBoundedQuery)' -count=1`, and `scripts/test-verify-remote-e2e-target-story.sh` failed before security-alert reconciliation rows exposed Eshu-owned installed-version evidence and the target-story verifier accepted installed/observed version expectations, then passed after the row contract added `eshu_package.observed_version`.

No-Observability-Change: the observed-version change only extends reducer-owned `reducer_security_alert_reconciliation` payloads and the existing HTTP/MCP read model. It adds no route, graph query, queue domain, worker, lease, runtime knob, metric instrument, or metric label; operators still diagnose the path through existing reducer run spans and execution counters, persisted reconciliation payloads, `query.supply_chain_security_alerts` spans, provider-source coverage, and Postgres query duration metrics.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildCodeCallRefreshIntentsUseVersionedDeltaPartitionKey|TestBuildCodeCallSharedIntentRowsCarriesDeltaPartitionForSourceFile|TestBuildCodeCallRefreshIntentsCarriesDeltaFileScope|TestCodeCallMaterializationHandlerAlignsDeltaEdgePartitions' -count=1` failed before CALLS delta edge intents carried source-file-scoped delta payloads and durable file partition keys, then passed. `go test ./internal/reducer -count=1` also passed after the shared-intent identity override preserved same-file edges with distinct relationship types while repo-refresh rows kept the full delta file set for safe retraction.

No-Observability-Change: the CALLS delta partition change only alters reducer intent construction for accepted code-call materialization rows. It adds no graph query, queue table, worker, lease, runtime knob, metric instrument, or metric label; operators still diagnose the path through existing `code_call_materialization` completion logs, code-call projection runner timing, reducer execution counters, and shared-intent backlog/status queries.

No-Regression Evidence: `go test ./internal/reducer -run 'TestCodeCallProjectionRunner(FileRefreshBlocksCoveredFilePartitions|SkipsRetractAfterCompletedCoveringRefresh)' -count=1` failed before file-scoped `repo_refresh` rows fenced later file partitions and completed current-run refresh rows suppressed redundant first-retracts, then passed. `go test ./internal/storage/postgres -run 'TestSharedIntentStoreHasCompletedAcceptanceUnitSourceRun(Partition|Refresh)DomainIntents' -count=1` proves the refresh-history lookup stays bounded to one acceptance/source run, completed `repo_refresh` rows, and selected file paths.

No-Observability-Change: the code-call refresh fence change only adjusts shared-intent selection and current-run history lookup for existing code-call projection rows. It adds no route, graph query shape, queue table, worker, lease, runtime knob, metric instrument, or metric label; operators still diagnose the path through existing code-call projection cycle logs, shared-intent backlog/status queries, partition lease rows, reducer execution counters, and Postgres query instrumentation.

No-Regression Evidence: remote full-corpus proof on #2626 showed completed
refresh rows no longer blocked code-call progress, but selected file partitions
still loaded unrelated pending rows from the same acceptance unit and collapsed
to single-digit completed code-call intents per minute after collectors stopped
enqueueing. `go test ./internal/reducer -run
'TestCodeCallProjectionRunnerLoadsSelectedPartitionDirectly|TestCodeCallProjectionRunnerLoadAllAcceptanceUnitIntents'
-count=1` failed before the runner used the partition-bounded store method,
then passed after selected partitions loaded only their own uncompleted rows
while keeping the fallback acceptance-unit scan for compatible stores.
`go test ./internal/storage/postgres -run
TestSharedIntentStoreListPendingAcceptanceUnitPartitionIntents -count=1`
proves the Postgres read stays keyed by scope, acceptance unit, source run,
domain, partition key, completion state, and deterministic creation ordering.

No-Observability-Change: the direct partition read adds one bounded Postgres
query method but no route, graph query shape, queue table, worker, lease,
runtime knob, metric instrument, or metric label. Operators still diagnose the
path through existing code-call projection cycle logs, shared-intent
backlog/status queries, partition lease rows, reducer execution counters, and
Postgres query instrumentation.

No-Regression Evidence: remote full-corpus proof on #2631 showed typed
`INSTANTIATES` graph writes and selected partition loads were no longer the
dominant cost, but code-call cycles still spent roughly 0.14-0.92s in
`selection_duration_seconds` while `write_duration_seconds` stayed around
1-6ms. `go test ./internal/reducer -run
'TestCodeCallProjectionRunner(SelectsPartitionCandidatesWithoutDomainScan|ReadsUnhashedPendingRowsWithoutDomainScan|EmptyPartitionDoesNotFallbackToDomainScan)'
-count=1` failed before the runner used the partition-candidate reader, then
passed after candidate selection preferred rows already hashed into the leased
partition and read pre-hash legacy rows through a bounded unhashed-reader path
without re-entering the global domain scan for normal empty partitions. `go test
./internal/reducer -run
'TestCodeCallProjectionRunner(Selects|Scans|Skips|Uses|Loads|FileRefresh|LoadAll|Processes|Marks|Retries|Keeps|DoesNot|Suppresses|Partitions|Runs)'
-count=1` proves readiness gating, refresh fencing, direct partition loading,
completion, retry, and fallback behavior still converge.

No-Observability-Change: the partition-candidate selector adds no graph query,
queue table, worker, lease, runtime knob, metric instrument, or metric label.
Operators still diagnose the path through existing code-call projection cycle
logs (`selection_duration_seconds`, `write_duration_seconds`,
`lease_claim_duration_seconds`, intent wait fields), shared-intent backlog and
status queries, partition lease rows, reducer execution counters, and
instrumented Postgres query spans/duration metrics.

No-Regression Evidence: `go test ./internal/reducer -run
TestSupplyChainImpactHandlerStopsActiveEvidenceExpansionConservatively -count=1`
failed before supply-chain impact returned a conservative finding at the active
evidence expansion ceiling, then passed after the handler kept the bounded
filtered reads, wrote the available impact finding, and marked
`active_evidence_truncated=true`.

Observability Evidence: supply-chain impact expansion truncation is surfaced in
the reducer result evidence summary and in each persisted finding's
`missing_evidence` payload. The change adds no route, graph query, queue table,
worker, lease, runtime knob, metric instrument, or metric label; operators still
diagnose the path through reducer run spans, reducer execution counters,
durable `reducer_supply_chain_impact_finding` payloads, and existing Postgres
query instrumentation.

No-Regression Evidence: #2637 follows the #2633 full-corpus finding that
partition-candidate selection removed the broad domain scan but still left
readiness- and refresh-fence-heavy selector outliers. `go test
./internal/reducer -run
'TestCodeCallProjectionRunnerReuses(Readiness|RefreshFence)RowsAcrossWidenedCandidateWindows'
-count=1` failed before one selector call re-prefetched the same blocked
readiness key and reloaded the same acceptance-unit refresh-fence rows across
widened candidate pages, then passed after the selector cached accepted
generations, readiness results, and refresh-fence acceptance rows for the
current selector invocation only. The cache does not change partition leases,
worker count, completion marking, graph writes, or retry semantics; a stale
same-invocation cache can only defer work to the next poll, preserving the
canonical-node readiness gate and refresh-fence truth.

Observability Evidence: #2637 adds no route, graph query, queue table, worker,
lease, runtime knob, metric instrument, or metric label. Code-call projection
cycle logs now split selector time into candidate load, accepted-generation
prefetch, readiness prefetch, and refresh-fence checks alongside the existing
total `selection_duration_seconds`, readiness blocked counts, and intent wait
fields. Operators still use shared-intent backlog/status queries, partition
lease rows, reducer execution counters, and instrumented Postgres query
spans/duration metrics to connect those selector phases to queue progress.

No-Regression Evidence: #2809 adds a property-keyed `(repo_id, path)` Endpoint
presence gate for the `handles_route` shared-projection domain so the
`Function-[:HANDLES_ROUTE]->Endpoint` edge no longer drains and silently drops on
the first generation before its target Endpoint commits. A phase-ready row whose
endpoint is ABSENT is TERMINAL (drained with no edge, retracted repo-scoped to
clear any stale edge), NEVER deferred — a route-only repo, whose endpoint will
never materialize, therefore cannot stall the shared-projection backlog forever
(the original gate's liveness bug). The presence and phase gates are independently
wired so the handles_route gate is toggled solely by
`ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED`, never by the secrets/IAM flag,
and the secrets/IAM uid presence writer is never handed to the cloud/Kubernetes
materializers via this path. `go test ./internal/reducer -run
'TestFilterRowsByReadinessHandlesRouteTerminatesAbsentEndpoint|TestFilterRowsByReadinessHandlesRoutePhaseBlockedStaysDeferred|TestFilterRowsByReadinessHandlesRouteProjectsWhenPresent|TestFilterRowsByReadinessHandlesRouteNilPresenceIsTodaysBehavior|TestFilterRowsByReadinessNonHandlesRouteIgnoresPresence|TestProcessPartitionOnceHandlesRouteDrainsAbsentEndpoint|TestProcessPartitionOnceHandlesRouteAllTerminalRepoDrainsAndRetracts|TestProcessPartitionOnceHandlesRouteNilPresenceProjectsAll|TestWorkloadMaterializationHandlerPublishesEndpointRepoPathPresence|TestWorkloadMaterializationHandlerNilPresenceWriterNoOp|TestNewDefaultRegistryWiresHandlesRoutePresenceWriterIndependently'
-count=1` failed before the terminal path and the independent wiring existed, then
passed. `go test ./internal/reducer ./internal/storage/cypher ./internal/runtime
./cmd/reducer -count=1` stays green, proving byte-identical behavior for every
other domain: the gate is keyed strictly on `domain == DomainHandlesRoute`, runs
ONE bounded `MissingUIDs` lookup over the distinct `(repo_id, path)` keys (no N+1
probe), and a nil presence lookup/writer is a no-op so the un-wired path matches
today's exactly. The presence rows reuse the existing `graph_endpoint_presence`
table under the `api_endpoint_repo_path` keyspace with no schema change.

Observability Evidence: #2809 reuses the existing endpoint-presence store and the
shared-projection readiness path and adds NO metric instrument, metric label,
span, route, graph query shape, or queue table. It adds one operator-facing log
key: terminal handles_route rows (route handlers with no endpoint) are reported
through the new `shared projection drained handles_route intents with no endpoint`
log line and the `terminal_no_endpoint` field on the existing shared-projection
cycle log and `PartitionProcessResult` — distinct from the readiness-blocked
counters, because terminal rows are complete, not waiting. Operators still
diagnose deferral through the existing `blocked_readiness`/
`MaxBlockedIntentWaitSeconds` counters and cycle logs, plus the existing workload
materialization completion logs and Postgres query instrumentation.

No-Regression Evidence: #2809 `publishAPIEndpointRepoPathPresence` deduplicates
its `(repo_id, path)` presence rows by synthesized uid before the upsert, because
a multi-workload repo emits one `APIEndpointRow` per workload sharing the same
route path (the endpoint id embeds the workload id; the presence uid does not),
and the batched `INSERT ... ON CONFLICT (keyspace, uid) DO UPDATE` would
otherwise carry the same conflict key twice — which Postgres rejects, making the
workload materialization intent retry forever after its graph write already
succeeded. `go test ./internal/reducer -run
'TestPublishAPIEndpointRepoPathPresenceDeduplicatesRepoPath|TestPublishAPIEndpointRepoPathPresenceUpsertsRepoPathKeys'
-count=1` fails on the duplicate-key batch before the dedupe and passes after; the
dedupe is O(rows) with one map and changes no graph write.

No-Observability-Change: the presence dedupe only collapses redundant rows in an
existing upsert batch on the workload materialization commit path. It adds no
route, graph query shape, queue table, worker, lease, runtime knob, metric
instrument, metric label, or log key; operators still diagnose the path through
the existing workload materialization completion logs and Postgres query
instrumentation.

No-Regression Evidence: #2855 extends the #2809 terminal-presence gate to the
`runs_in` domain, which had the identical cross-acceptance-unit silent-drop gap:
its `Function-[:RUNS_IN]->Workload` intent is built in the code stage (repo AU)
but its target `:Workload` commits in workload materialization, so on a cold first
generation the both-MATCH MERGE could run before the Workload committed and drop
the edge. The workload materializer now publishes repo-keyed `:Workload` presence
(new `GraphProjectionKeyspaceRepoWorkloadPresence` keyspace, deduped by repo_id,
same EndpointPresence store/writer as #2809), and the second readiness gate
(`filterRowsByTargetPresence` via `symbolRuntimePresenceGate`) terminally drains a
phase-ready `runs_in` row whose repo has no committed Workload — never deferred,
mirroring handles_route. `go test ./internal/reducer -run
'TestFilterRowsByReadinessRunsInTerminatesAbsentWorkload|TestFilterRowsByReadinessRunsInProjectsWhenWorkloadPresent|TestFilterRowsByReadinessRunsInNilPresenceIsTodaysBehavior|TestProcessPartitionOnceRunsInDrainsAbsentWorkload|TestPublishRepoWorkloadPresenceDeduplicatesByRepo|TestWorkloadMaterializationHandlerPublishesRepoWorkloadPresence'
-count=1` fails before the gate covered `runs_in`, then passes; the existing
handles_route and all other-domain tests stay green (nil-presence parity, the gate
is keyed on `symbolRuntimePresenceGate` returning gated only for the two
symbol→runtime domains).

Observability Evidence: #2855 adds no new metric or log key. It REUSES the #2809
`terminal_no_endpoint` count and the shared-projection terminal log line, whose
message is now domain-generic and carries the originating `domain` attribute
(`runs_in` vs `handles_route`) so an operator can tell which gate drained a row.
The same `ESHU_REDUCER_HANDLES_ROUTE_PRESENCE_GATE_ENABLED` flag gates both, since
both presence concerns are published by the workload materializer and share one
store; a nil presence lookup/writer keeps both domains byte-identical.

No-Regression Evidence: #2842 retracts stale `api_endpoint_repo_path` /
`repo_workload` presence rows when a repo re-materializes, so a removed or
re-pathed route/workload stops being reported present (the uid is a #2844 hash and
no longer carries the repo, so the store gained `repo_id` + `source_generation`
columns and a `RetractStaleRepoGenerations(keyspace, scope, generation, repoIDs)`
method). It is RACE-FREE under concurrent materializer workers: it deletes only
the listed repos' rows whose `source_generation <> current`, never the current
generation's rows a sibling intent may have just upserted (overlapping deployable
units share repos), and deleting an already-removed older row is idempotent — no
worker-count or batch-size reduction is used. `go test ./internal/reducer
./internal/storage/postgres -run
'TestGraphEndpointPresenceStoreRetractStaleRepoGenerations|TestPublishAPIEndpointRepoPathPresenceUpsertsRepoPathKeys|TestPublishRepoWorkloadPresenceDeduplicatesByRepo|TestGraphEndpointPresenceStoreUpsertIsIdempotent'
-count=1` fails before the columns/retraction exist (stale rows linger; the upsert
arg layout omits provenance), then passes. The schema change is additive and
idempotent (`ADD COLUMN IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`), validated
against real Postgres by the CI bootstrap that applies the data-plane schema;
new columns store NUL-free values so the #2844 NUL-in-text class cannot recur.

No-Observability-Change: #2842 only prunes stale presence rows and adds two
provenance columns to an existing table. It adds no route, graph query shape,
queue domain, worker, lease, runtime knob, metric instrument, metric label, or
log key; operators still diagnose the path through the existing workload
materialization completion logs, the shared-projection terminal/blocked counters,
and Postgres query instrumentation.

No-Regression Evidence: #2889 wires the already-registered `DomainCodeTaintEvidence`
materialization domain at runtime — a Postgres `CodeTaintEvidenceLoader`, the
canonical graph writer, and the domain registration in `knownDomains` — so
value-flow taint findings project into graph evidence nodes attached to their
Function. It is ADDITIVE: a new domain following the existing reducer
retract-by-evidence-source + canonical-write pattern (evidence source
`reducer/code-taint`), with no change to any existing domain's selection, write,
or readiness path. The domain is marked `CrossSource`/`CrossScope` to match how its
findings span files. `go test ./internal/reducer -run 'CodeTaintEvidence' -count=1`,
`go test ./internal/storage/postgres -run 'CodeTaintEvidenceLoader' -count=1`, and
`go test ./internal/projector -run 'CodeTaintEvidenceIntents' -count=1` cover the
loader, the handler, and intent emission; `go test ./cmd/reducer -count=1` proves
the runtime wiring registers the loader/writer without disturbing the other
domains.

No-Observability-Change: the new domain reuses the generic reducer
claim/execute/ack instrumentation — its materialization is diagnosed through the
existing reducer run spans, per-domain execution counters (the `domain` attribute
gains the `code_taint_evidence` value, an existing label, not a new instrument),
durable queue/status rows, and Postgres query duration metrics. It adds no route,
graph query shape, new metric instrument, new metric label key, span, lease,
runtime knob, or log key.

No-Regression Evidence: #2906 wires the already-registered
`DomainCodeInterprocEvidence` materialization domain at runtime — a Postgres
`CodeInterprocEvidenceLoader`, the canonical graph writer, and the domain
registration in `knownDomains` — so cross-function value-flow findings project
into `TAINT_FLOWS_TO` edges between their source and sink Function nodes. It is
ADDITIVE and mirrors the `code_taint_evidence` wiring above: a new domain
following the existing reducer retract-by-evidence-source + canonical-write
pattern (evidence source `reducer/code-interproc`), with no change to any
existing domain's selection, write, or readiness path, and no new schema (the
edge reuses the existing `iam_can_assume`/`handles_route` reducer-owned-edge
shape). `go test ./internal/reducer -run 'CodeInterproc' -count=1`,
`go test ./internal/storage/postgres -run 'CodeInterproc' -count=1`, and
`go test ./internal/projector -run 'CodeInterproc' -count=1` cover the loader,
the handler, and intent emission; `go test ./cmd/reducer -count=1` proves the
runtime wiring registers the loader/writer without disturbing the other domains.
The `configureReducerQueue` extraction in `main_helpers.go` is a pure move of the
existing work-queue setup (no behavior change) to keep `main.go` within the
file-size budget.

No-Observability-Change: the interproc domain reuses the same generic reducer
claim/execute/ack instrumentation — diagnosed through existing reducer run spans,
per-domain execution counters (the `domain` attribute gains the
`code_interproc_evidence` value, an existing label, not a new instrument),
durable queue/status rows, and Postgres query duration metrics. It adds no route,
graph query shape, new metric instrument, new metric label key, span, lease,
runtime knob, or log key.

No-Regression Evidence: #2931 registers and wires `DomainCodeFunctionSummary` — a
Postgres `CodeFunctionSummaryLoader` (rebuilds `summary.Effects` from
`code_function_summary` facts) and a `CodeFunctionSummaryWriter` (the merged
`FunctionSummaryStore.UpsertSnapshot`). The handler loads one generation's
Effects, recomputes content versions through an in-memory `summary.Store`, and
upserts the snapshot, idempotent on `FunctionID`. It is ADDITIVE: a new domain
gated on its loader+writer (so it never registers without a handler), following
the existing claim/execute/ack path, with no change to any existing domain's
selection, write, or readiness path. Unlike the evidence domains it persists to a
durable Postgres table (`function_summaries`) rather than the graph, so it adds no
Cypher. `go test ./internal/reducer -run 'CodeFunctionSummary' -count=1`,
`go test ./internal/storage/postgres -run 'CodeFunctionSummary' -count=1`, and
`go test ./internal/projector -run 'CodeFunctionSummary' -count=1` cover the
handler (versioned-snapshot persistence, wrong-domain reject, registration gate),
the JSONB-coerced Effects loader, and intent emission; `go test ./cmd/reducer
-count=1` proves the runtime wiring registers the loader/writer without disturbing
the other domains.

No-Observability-Change: the summary domain reuses the same generic reducer
claim/execute/ack instrumentation — diagnosed through existing reducer run spans,
per-domain execution counters (the `domain` attribute gains the
`code_function_summary` value, an existing label, not a new instrument), durable
queue/status rows, and Postgres query duration metrics. It adds no route, graph
query shape, new metric instrument, new metric label key, span, lease, runtime
knob, or log key.

No-Regression Evidence: #2931 also persists the durable `FunctionID`->graph-uid
map the cross-repo fixpoint needs to project findings by uid. The same
`code_function_summary` handler gains an OPTIONAL graph-id loader/writer (a
`CodeFunctionGraphIDLoader` reading the `graph_uid` the collector resolved onto
each `code_function_summary` fact, and a `CodeFunctionGraphIDWriter` =
`FunctionGraphIDStore.UpsertGraphIDs`). When wired it upserts the map alongside
the summaries and sources, idempotent on `FunctionID`, skipping unresolved
(empty) uids. It is additive and behind the same off-by-default value-flow gate;
no new Cypher (a durable `function_graph_ids` Postgres table), graph write,
worker, queue, or batch. `go test ./internal/reducer -run 'CodeFunctionSummary'
-count=1` and `go test ./internal/storage/postgres -run 'FunctionGraphID|Bootstrap'
-count=1` cover the handler graph-id persistence and the store/ordered bootstrap
schema; `go test ./cmd/reducer -count=1` proves the wiring.

No-Observability-Change: the graph-id persistence reuses the same generic reducer
claim/execute/ack instrumentation as the summary domain; it adds no metric
instrument, metric label, span, worker, queue domain, lease, runtime knob, or log
key beyond the existing per-domain counters.

No-Regression Evidence: #2964 composes durable function summaries, param
sources, and the FunctionID->uid map through `ValueFlowFixpointEvidenceLoader`
after `DomainCodeFunctionSummary` persists those stores. The post-persist
`ValueFlowFixpointEvidenceProjector` reuses the `TAINT_FLOWS_TO` writer but uses
the distinct `reducer/code-interproc-fixpoint` evidence source and UID namespace,
so existing fact-based `code_interproc_evidence` inputs stay isolated.

No-Regression Evidence: #2969 adds one Function.uid-bounded graph read that
joins INVOKES_CLOUD_ACTION to CAN_PERFORM cloud permission targets only after a
single exact RUNS_IN workload fan-out. `go test ./internal/reducer -run
'TestGraphValueFlowCloudSinkTargetLoaderLoadsCloudActionPermissions'
-count=1` failed before the loader returned permission-backed sinks, then
passed with ambiguous workload fan-out still empty.

No-Observability-Change: the cloud-action permission read uses the existing
value-flow fixpoint load path, graph query instrumentation, reducer
spans/counters, and CodeInterprocEvidence writer summaries; it adds no worker,
queue domain, metric instrument, metric label, runtime knob, or graph writer.

No-Regression Evidence: Rust trait-bound receiver resolution registers a
language resolver before weak repository-wide fallback and indexes only unique
trait declaration methods. The focused parser test failed before trait
declaration methods carried `trait_context` and direct parameter calls carried
`inferred_obj_type`; the focused reducer test failed before `shape.area`
resolved through `T: Area`. They pass after the resolver returns the unique
trait method and preserves ambiguity for bounds such as `T: Area + Surface`.

No-Observability-Change: Rust trait-bound receiver resolution only changes the
in-memory code-call resolver branch used before row emission. It adds no graph
query, queue, worker, lease, batch, runtime knob, metric instrument, metric
label key, span, route, status field, or log key; operators still inspect
existing code-call intent rows and materialization completion logs.

No-Regression Evidence: Python code-call resolver registration resolves
declared-base classmethod calls using parser-emitted `bases` and class-scoped
methods before weak repository-wide fallback. The focused reducer test for
constructors, qualified class receivers, ambiguous inherited methods, and
self-method calls failed before the resolver when an unrelated `from_dict` made
the inherited method ambiguous, then passed after the resolver returned the
unique declared-base method and preserved multiple-base ambiguity. The
resolutionparity package test stayed green with no golden update.

No-Observability-Change: Python declared-base resolver registration only changes
the resolver branch chosen before code-call row emission. It adds no graph
query, queue, worker, lease, batch, runtime knob, metric instrument, metric
label key, span, route, status field, or log key; operators still inspect
existing code-call intent rows and materialization completion logs.

No-Regression Evidence: Java code-call resolver registration moves receiver and
argument type evidence ahead of the weak repository-wide fallback without
changing edge identity. `go test ./internal/reducer -run
'TestResolveGenericCalleeUsesJavaReceiverTypeBeforeRepoUniqueName|TestExtractCodeCallRowsResolvesJava'
-count=1` fails before the Java resolver because the edge is classified as
`repo_unique_name`, then passes with `type_inferred`. `go test
./internal/resolutionparity -count=1` proves the intentional Java tier shift from
9 `repo_unique_name` rows to 5 `repo_unique_name` and 4 `type_inferred` rows.

No-Observability-Change: Java resolver registration only reclassifies the
existing code-call provenance method before row emission. It adds no graph query,
queue, worker, lease, batch, runtime knob, metric instrument, metric label key,
span, route, status field, or log key; operators still inspect the existing
code-call intent rows and materialization completion logs.

No-Regression Evidence (Wave 4a incident typed-payload decode): the
incident-routing materialization handler now decodes incident.record and
incident_routing.* fact payloads through the `sdk/go/factschema` seam
(`buildIncidentRoutingEvidenceInputs`) instead of the storage layer's raw
`payloadString`/`payloadBool` map lookups. The `IncidentRoutingEvidenceLoader`
seam was shrunk to return raw `[]facts.Envelope`
(`FactStore.LoadIncidentRoutingRawEvidence`); the four `*FromEnvelope` mappers
moved into the reducer as typed decodes routed through `partitionDecodeFailures`.
This is a cold, once-per-scope-generation projection path (not a hot per-edge
loop), so the typed-decode cost is measured for a no-regression bound rather than
a tight microbench. Measured with `go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildIncidentRoutingEvidenceInputs' -benchmem -count=3` (darwin/arm64;
input: 500 incidents x {incident, applied, observed, warning} = 2000 fact
envelopes): 2.42-2.57 ms/op, 1.89 MB/op, 14047 allocs/op — roughly 1.2 µs and 7
allocs per fact through the marshal-free reflection decoder (`decode_map.go`),
which is well inside the diagnostic-rigor band for a per-scope-generation
materialization pass. The residual cost buys the accuracy guarantee: an
incident_routing.applied_pagerduty_resource fact missing its required
resource_class key (or an incident.record missing provider_incident_id)
dead-letters as a per-fact input_invalid quarantine
(`TestIncidentRoutingMaterializationQuarantinesMissingRequiredField`) instead of
being silently dropped by the pre-typing `payloadString(...) != "service"`
compare. Valid facts produce byte-identical graph rows
(`TestIncidentRoutingMaterializationHandlerWritesAndRetracts` still writes 3
rows), so only malformed→dead-letter is new behavior. Result class: correctness
win with a bounded, measured cold-path cost.

No-Observability-Change (Wave 4a incident typed-payload decode): the migration
adds no route, graph query shape, queue table, worker, lease, or runtime knob. A
malformed incident/routing fact surfaces through the EXISTING per-fact
input_invalid apparatus the AWS/IAM family established —
`recordQuarantinedFacts` -> the `eshu_dp_reducer_input_invalid_facts_total`
counter (existing instrument; the `domain` label gains the
`incident_routing_materialization` value and the `fact_kind` label gains the
incident kinds, both existing label keys) plus one structured error log per
quarantined fact and the `input_invalid_facts` count on the existing
per-intent `Result.SubSignals` and completion log. The decode error
self-classifies via the existing `FailureClass()`/`Retryable()` reducer error
interface. Operators still diagnose the path through the existing incident
routing materialization completion log, reducer run spans, and reducer execution
counters.
No-Regression Evidence (Wave 4a, GCP family typed-payload decode): the GCP
cloud-resource and cloud-relationship reducer handlers
(`gcp_resource_materialization.go`, `gcp_relationship_join.go`,
`gcp_relationship_materialization.go`) now decode `gcp_cloud_resource` and
`gcp_cloud_relationship` fact payloads through the `sdk/go/factschema` seam
(`decodeGCPCloudResource`, `decodeGCPCloudRelationship` in
`factschema_decode.go`) instead of raw `payloadString` map lookups, mirroring
the AWS/IAM/security-group migration (#4568). `ExtractGCPCloudResourceNodeRows`
and `buildGCPCloudResourceJoinIndex`/`ExtractGCPRelationshipEdgeRows` now return
a `[]quarantinedFact` alongside rows: a fact missing a required identity field
(`full_resource_name`/`asset_type` for resources;
`source_full_resource_name`/`target_full_resource_name`/`relationship_type` for
relationships) is quarantined per-fact via `partitionDecodeFailures` rather than
silently producing an empty-string `CloudResource` uid, while every valid fact
in the same batch still projects. `go test ./internal/reducer -run
'TestGCPResourceMaterializationQuarantinesMissingFullResourceName|TestGCPRelationshipMaterializationQuarantinesMissingRelationshipType'
-count=1 -v` failed before the conversion (quarantine count 0, malformed fact
silently dropped with no operator signal), then passed after: the malformed
fact is recorded on `input_invalid_facts` and the valid sibling still
materializes its node/edge, with no uid ever computed from the empty-string
identity segment. Measured with the existing GCP benchmarks (in-memory
extractor; 5,000-resource/-relationship corpus each already builds; darwin/
arm64, `-count=3`): `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractGCPCloudResourceNodeRows|BenchmarkExtractGCPRelationshipEdgeRows'
-benchmem`. BEFORE (raw map, commit 9c8d2b655) -> AFTER (typed decode):
`ExtractGCPCloudResourceNodeRows` 15.13ms/235,058 allocs -> 16.60ms/235,047
allocs (+9.7% time, -0.005% allocs); `ExtractGCPRelationshipEdgeRows`
43.50ms/605,125 allocs -> 44.46ms/600,123 allocs (+2.2% time, -0.8% allocs).
Both deltas are within the diagnostic-rigor ~10% band; the residual cost buys
the same accuracy guarantee the AWS migration did. Result class: Correctness
win with a bounded, measured handler cost.

No-Observability-Change (Wave 4a, GCP family): the typed-decode migration adds
no route, graph query shape, queue table, worker, lease, runtime knob, metric
instrument, or metric label. A malformed `gcp_cloud_resource`/
`gcp_cloud_relationship` fact surfaces through the EXISTING dead-letter path —
the same `recordQuarantinedFacts`/`inputInvalidSubSignals` seam the AWS
migration wired — incrementing the existing
`eshu_dp_reducer_input_invalid_facts_total` counter (labeled `domain` =
`gcp_resource_materialization` / `gcp_relationship_materialization`, an
existing label value set, not a new instrument) and logging the existing
"reducer input_invalid fact quarantined" structured log line. The
`payload-usage-manifest` gate (`go/internal/payloadusage`) required an
additive-only extension: two new `factKindSchemaFile` entries
(`FactKindGCPCloudResource`, `FactKindGCPCloudRelationship`) and a new
`GCPStructDir` (`sdk/go/factschema/gcp/v1`) parsed alongside the existing AWS/
IAM struct dirs in `Load`; this only widens which decode seams the gate covers
and adds no new gate mechanism.
No-Regression Evidence (Wave 4a typed-payload decode, Contract System v1
#4566): the AZURE cloud-inventory reducer handlers (`azure_resource_materialization.go`,
`azure_relationship_materialization.go`, `azure_relationship_join.go`) now decode
`azure_cloud_resource`/`azure_cloud_relationship` fact payloads through the
`sdk/go/factschema` seam (`factschema.DecodeAzureCloudResource`/
`DecodeAzureCloudRelationship`, wrapped by `decodeAzureCloudResource`/
`decodeAzureCloudRelationship` in `factschema_decode_azure.go`) instead of raw
`payloadString(env.Payload, "key")` map lookups, mirroring the #4568 AWS
migration. Only the two WIRED azure kinds are typed this wave; the four whose
sole consumer is a shared cross-provider surface or an unconverted
Azure-specific storage loader (`azure_tag_observation`,
`azure_identity_observation`, `azure_resource_change`, `azure_image_reference`)
are deferred to the change that converts their read path, matching how #4568
left `aws_tag_observation`/`aws_image_reference` untyped. The wired decode
wrappers live in a per-family `factschema_decode_azure.go` file; the Contract
System v1 §6 gate-2 payload-usage manifest (go/internal/payloadusage) globs the
reducer dir's `factschema_decode*.go` files for decode seams and scans
`sdk/go/factschema/azure/v1` for struct shapes, so a per-family file is
discovered and gated the same as the base file. `azureCloudResourceNodeRow` now returns its resolved join identity
(`resourceID`) alongside the row and uid so `buildAzureCloudResourceJoinIndex`
never re-decodes an already-decoded resource fact to recover its join key — a
double-decode that a first benchmark pass caught as an avoidable regression
before this fix landed. Measured with `go test ./internal/reducer -run '^$'
-bench 'BenchmarkExtractAzureCloudResourceNodeRows|BenchmarkExtractAzureRelationshipEdgeRows'
-benchmem -count=5` (darwin/arm64, Apple M1 Max; backend: in-memory extractor;
input: 5,000 synthetic `azure_cloud_resource` facts / 2,500 resources +
1,250 `azure_cloud_relationship` facts). BEFORE (raw map, commit 9c8d2b655)
-> AFTER (typed decode): `ExtractAzureCloudResourceNodeRows` 15.5ms/225,058
allocs -> 15.5ms/220,044 allocs (~0% time, -2.2% allocs);
`ExtractAzureRelationshipEdgeRows` 9.97ms/144,866 allocs -> 10.2ms/141,103
allocs (+2.3% time, -2.6% allocs). Both are within the diagnostic-rigor ~10%
band and allocations went down, not up — the typed path buys the accuracy
guarantee (a fact missing a required identity field — `arm_resource_id`,
`resource_type`, `subscription_id`, `location` for `azure_cloud_resource`;
`relationship_type`, `source_arm_resource_id`, `target_arm_resource_id` for
`azure_cloud_relationship` — dead-letters `input_invalid` via
`partitionDecodeFailures`/`recordQuarantinedFacts` instead of producing an
empty-string graph uid) at effectively no measured handler cost. Result
class: Correctness win with no measured handler regression.
`TestAzureRelationshipMaterializationQuarantinesMissingResourceType` (new
regression test mirroring `TestAWSRelationshipMaterializationQuarantines-
MissingAccountID`) failed before `ExtractAzureCloudResourceNodeRows` routed a
missing-`resource_type` fact through the decode seam (it decoded to a
zero-value string and produced a materializable-looking row under an
empty-type-segment uid), then passed after the fact quarantined per-fact
while a valid sibling resource in the same batch still projected. Converting
`azure_relationship_join.go`'s required-field set surfaced a pre-existing gap
in `azure_relationship_materialization_test.go`'s skip-matrix fixtures
(`cross_subscription_target`, `invalid_type`, `unsupported_type`,
`self_loop`): those synthetic payloads set only `source_normalized_resource_id`/
`target_normalized_resource_id` and omitted `source_arm_resource_id`/
`target_arm_resource_id` entirely, which the pre-typing `payloadString` lookup
silently tolerated (returns `""` for an absent key) but the collector emitter
(`azurecloud.NewRelationshipEnvelope`, `go/internal/collector/azurecloud/
relationship.go:69-75`) always validates both non-empty before emission — the
fixtures were never realistic collector output. The four fixtures were
corrected to also set the two ARM id fields the real collector always emits,
preserving each subtest's intended skip-tally assertion
(`unresolved`/`invalid_type`/`unsupported`/`self_loop`) rather than a
now-incorrect `input_invalid` quarantine path.

No-Observability-Change: the AZURE typed-decode migration adds no route,
graph query shape, queue table, worker, lease, runtime knob, metric
instrument, metric label, or log key. A malformed `azure_cloud_resource`/
`azure_cloud_relationship` fact surfaces through the EXISTING dead-letter path
— `partitionDecodeFailures` -> `recordQuarantinedFacts` ->
`eshu_dp_reducer_input_invalid_facts_total` (labeled `domain`=
`azure_resource_materialization`/`azure_relationship_materialization`,
`fact_kind`) plus the existing structured `reducer input_invalid fact
quarantined` error log and `Result.SubSignals["input_invalid_facts"]` — the
same instruments and log key the #4568 AWS migration already wired; both
`AzureResourceMaterializationHandler` and `AzureRelationshipMaterialization-
Handler` gained an `Instruments *telemetry.Instruments` field wired from
`DefaultHandlers.Instruments` in `defaults_additive_domains_azure.go`,
mirroring the AWS handlers' existing field.

No-Regression Evidence (Wave 4b, kubernetes_live family typed-payload decode,
Contract System v1 #4566): the live Kubernetes workload materialization
handler (`kubernetes_workload_materialization.go`) and the correlation index
builder (`kubernetes_correlation_index.go`) now decode
`kubernetes_live.pod_template`/`kubernetes_live.relationship`/
`kubernetes_live.warning` fact payloads through the `sdk/go/factschema` seam
(`decodeKubernetesLivePodTemplate`/`decodeKubernetesLiveRelationship`/
`decodeKubernetesLiveWarning` in `factschema_decode_kuberneteslive.go`)
instead of raw `payloadString`/`payloadStrings` map lookups, mirroring the
#4568 AWS migration. `ExtractKubernetesWorkloadNodeRows` and
`buildKubernetesCorrelationIndex` now return a `[]quarantinedFact` alongside
their result: a pod-template fact missing its required `object_id` identity
field is quarantined per-fact via `partitionDecodeFailures` rather than
silently producing an empty-string `KubernetesWorkload` node uid, while every
valid fact in the same batch still projects.
`TestKubernetesWorkloadMaterializationQuarantinesMissingObjectID` (new
regression test mirroring
`TestGCPResourceMaterializationQuarantinesMissingFullResourceName`) failed
before the conversion (the old `payloadString` lookup returned `""` for the
absent key and the pre-typing code silently dropped the fact with no operator
signal — `SubSignals["input_invalid_facts"]` stayed `0`), then passed after:
the malformed fact is recorded on `input_invalid_facts` and the valid sibling
still materializes its node. The public, table-test-covered
`BuildKubernetesCorrelationDecisions` keeps its existing error-free signature
(`[]facts.Envelope -> []KubernetesCorrelationDecision`) and delegates to a new
quarantine-aware `buildKubernetesCorrelationDecisionsWithQuarantine` that
`KubernetesCorrelationHandler.Handle` calls directly, so the reducer intent
path reports quarantines without changing the pure classifier's signature the
existing table tests assert against.

First benchmark pass (`BenchmarkExtractKubernetesWorkloadNodeRows`,
5,000-pod-template corpus including a populated `selector` label map, the
realistic Deployment shape) measured a regression exceeding the
diagnostic-rigor ~10% band: BEFORE (raw map, commit 7df868370) 7.76ms/9.34MB/
150,025 allocs -> naive typed decode 12.34ms/12.74MB/215,026 allocs (+59%
time, +36% allocs). Root-caused to `sdk/go/factschema/decode_map.go`'s
`assignField`, whose `map[string]string`/`*map[string]string` fields (the
pod-template `Selector`/`Labels`, and AWS's pre-existing `Tags
*map[string]string`) fell through to the bounded `jsonRoundTripValue`
marshal/unmarshal fallback on every fact — the same fallback path the AWS
migration's own benchmark never exercised (its benchmark payload never
populates `tags`). Proven with a throwaway microbenchmark isolating
`jsonRoundTripValue`'s marshal/unmarshal round trip against a direct
type-assert/coerce path on the same map shape: 15.3x speedup
(1.42µs/op -> 92ns/op), with an explicit equivalence check (0 symmetric diff)
proving the fast path returns identical output to the fallback for every
value it accepts. Applied the theory as a genuine fix rather than a
kubernetes_live-local workaround: `decode_map.go` gained a `map[string]string`
fast path (`anyToStringMap`, reached from both the `Ptr`-to-map and plain
`Map` cases in `assignField`) that shares the exact type-assert/coerce shape
the existing `map[string]any` fast path already used one branch above it, so
every family decoding through `decode_map.go` (AWS, IAM, incident, GCP,
Azure, kubernetes_live) benefits, not only this wave's kind. Re-measured after
the fix: 8.27ms/9.54MB/150,019 allocs (+6.6% time, +2.1% memory, -6 allocs
versus the pre-migration baseline) — within the diagnostic-rigor band.
Re-ran the AWS/GCP/Azure/incident family benchmarks
(`BenchmarkExtractCloudResourceNodeRows`,
`BenchmarkExtractAWSRelationshipEdgeRows`,
`BenchmarkBuildCloudResourceJoinIndex`,
`BenchmarkExtractGCPCloudResourceNodeRows`,
`BenchmarkExtractGCPRelationshipEdgeRows`,
`BenchmarkExtractAzureCloudResourceNodeRows`,
`BenchmarkExtractAzureRelationshipEdgeRows`,
`BenchmarkBuildIncidentRoutingEvidenceInputs`) after the shared `decode_map.go`
change: all stayed within their previously documented bands (for example
`ExtractCloudResourceNodeRows` 15.3-15.6ms before this wave -> 16.6ms after,
`ExtractGCPCloudResourceNodeRows` 16.6ms -> 16.75ms), confirming the fast path
is additive (new accept paths only; every other shape still falls through to
the identical `jsonRoundTripValue` fallback) and introduces no regression for
the families that do not exercise `map[string]string` fields on their hot
path. Measured `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractKubernetesWorkloadNodeRows' -benchmem -count=3` (darwin/
arm64, Apple M1 Max; backend: in-memory extractor; input: 5,000 synthetic
`kubernetes_live.pod_template` facts each carrying a one-entry `selector`
label map). Result class: Correctness win with a bounded, measured handler
cost (well within the diagnostic-rigor band after the shared decode-path
fix), plus a reusable performance improvement for every already-migrated
family.
`TestAnyToStringMap_CoercesJSONBNativeShape` and
`TestDecodeMapInto_MapStringStringFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) lock the fast path's accept/
reject/equivalence contract: a string-valued `map[string]any` and an
already-typed `map[string]string` both decode correctly (including the
empty-but-present map case staying non-nil), while a map carrying a
non-string value or a non-map input falls back to `jsonRoundTripValue` rather
than silently coercing or dropping data.

No-Observability-Change (Wave 4b, kubernetes_live family): the typed-decode
migration and the shared `decode_map.go` fast-path addition add no route,
graph query shape, queue table, worker, lease, runtime knob, metric
instrument, or metric label. A malformed `kubernetes_live.*` fact surfaces
through the EXISTING dead-letter path — the same
`partitionDecodeFailures`/`recordQuarantinedFacts` seam every prior family
wired — incrementing the existing `eshu_dp_reducer_input_invalid_facts_total`
counter (labeled `domain` = `kubernetes_workload_materialization` /
`kubernetes_correlation`, an existing label value set, not a new instrument)
and logging the existing "reducer input_invalid fact quarantined" structured
log line, plus the existing `Result.SubSignals["input_invalid_facts"]` field.
`KubernetesWorkloadMaterializationHandler` and `KubernetesCorrelationHandler`
already carried an `Instruments *telemetry.Instruments` field before this
wave; no new wiring was needed. The `decode_map.go` fast path changes only the
internal path taken by `assignField` for a `map[string]string`-shaped field;
it adds no telemetry surface of its own and is invisible to every existing
operator-facing signal.

No-Regression Evidence (Wave 4c, sbom_attestation family typed-payload
decode, Contract System v1 #4566/#4582): `sbom_attestation_attachment_index.go`'s
`buildSBOMAttachmentIndex` and its document/statement classifiers now decode
`sbom.document`, `sbom.component`, `sbom.warning`,
`attestation.statement`, and `attestation.signature_verification` fact
payloads through the `sdk/go/factschema` seam
(`decodeSBOMDocument`/`decodeSBOMComponent`/`decodeSBOMWarning`/
`decodeAttestationStatement`/`decodeAttestationSignatureVerification` in
`factschema_decode_sbom.go`) instead of raw `payloadString`/`payloadStrings`
map lookups, mirroring the AWS/GCP/Azure/kubernetes_live migrations. Only the
five WIRED kinds were typed this wave; `sbom.dependency_relationship` and
`sbom.external_reference` were left typed-but-deferred in
`sdk/go/factschema/sbom/v1` at the time — matching how prior waves left an
unconsumed kind's struct in place without a reducer decode call — and were
wired to `buildSBOMAttachmentIndex` in a later change (#5370); the family's
last typed-but-deferred kind, `attestation.slsa_provenance`, was wired in
#5371 (SBOM runtime collector emitter plus reducer decode/join by
`statement_id`). `buildSBOMAttachmentIndex`
now returns a `[]quarantinedFact` alongside the index: a `sbom.document`,
`sbom.component`, `attestation.statement`, or
`attestation.signature_verification` fact missing its required identity
field (`document_id` / `statement_id`) is quarantined per-fact via
`partitionDecodeFailures` rather than being indexed under a wrong or missing
identity, while every valid sibling fact in the same batch still indexes and
produces an attachment decision.
`TestSBOMAttestationAttachmentQuarantinesMissingDocumentID` and
`TestSBOMAttestationAttachmentComponentQuarantinesMissingDocumentID` (new,
mirroring `TestGCPResourceMaterializationQuarantinesMissingFullResourceName`)
failed before the conversion, then passed after. The two prior-behavior paths
differ: the old `sbom.document`/`attestation.statement` classifiers keyed the
document by `firstNonBlank(payloadString(document_id|statement_id),
envelope.FactID)`, so an absent identity fell back to the fact's own id and
produced a real but WRONG-identity attachment decision — one that could write
bad graph identity downstream — with no operator signal; while
`sbom.component`/`sbom.warning`/`attestation.signature_verification` had no
FactID fallback, so an absent key returned `""` and `index.components[""]` /
the warning/verification join keys silently collapsed every malformed fact
into one empty-key slot. Both silent paths now dead-letter: the malformed
fact is recorded on `input_invalid_facts` and the valid sibling still produces
its attachment decision, with no decision ever keyed on a wrong or empty
document identity.
`sbom.warning`'s typed struct (`sbomv1.Warning`) has ZERO required fields by
design: the SBOM-document collector path always sets `document_id` and never
`statement_id`, while the attestation-runtime collector path always sets
`statement_id` and never `document_id` — the same fact kind, two
mutually-exclusive identity keys, so neither can be required without
dead-lettering half of this kind's real traffic; the reducer's
`firstNonBlank(document_id, statement_id)` fallback is preserved unchanged.
`sbomAttestationAttachmentFactKind` (`reducer_sbom_attestation_attachment`,
`sbom_attestation_attachment_writer.go`) is the reducer's OWN re-emitted
synthetic evidence fact, not one of the eight collector-emitted wire kinds
this wave types, and stays out of scope; `oci_registry.image_referrer` inside
this same index is a different family's kind whose reducer decode wrapper
lives in the projector package, so it also stays on raw `payloadString`
reads here, matching the scope-discipline precedent of the GCP/Azure waves
leaving a shared cross-family surface's raw reads alone.
`supplyChainSBOMComponentFromEnvelope` (`supply_chain_impact_match.go`) also
reads `sbom.component` raw: it is a different reducer domain
(`supply_chain_impact`) with zero existing quarantine plumbing of its own
across ANY of its many vulnerability/OS-package/deployment-context kinds, so
converting only its `sbom.component` read in isolation would be a hollow,
half-typed contract rather than a real accuracy fix — this is deferred to
the `supply_chain_impact` family's own future migration, matching how the
GCP wave deferred `gcp_image_reference`/`gcp_tag_observation` to their shared
cross-provider consumer's own conversion.
`facts_active_sbom_attestation_attachment.go`
(`go/internal/storage/postgres`) is a bounded index-filter SQL query (a
`WHERE fact.payload->>'x' = ANY($1)` digest/document-id lookup), not a
field-projecting loader like the incident family's raw-SQL-JSONB readers; it
returns full envelopes for the reducer to decode through the normal typed
seam, so it needs no schema-declared-field lock test.
`sbomAttachmentActiveKeys` (`sbom_attestation_attachment.go`) is a third,
pre-existing raw-`payloadString`/`payloadStrings` read site left untyped this
wave: it only extracts best-effort string keys to widen the bounded active-
evidence digest/document-id lookup above, never forms identity or feeds the
attachment-decision math, so a missing/malformed key there degrades to fewer
query keys (the same tolerant behavior it had pre-typing), not a wrong
decision — it is not a decode site requiring quarantine.
Measured with `go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildSBOMAttestationAttachmentDecisions' -benchmem -count=3`
(darwin/arm64, Apple M1 Max; backend: in-memory extractor; input: 5,000
synthetic sbom.document facts, each paired with one sbom.component, one
sbom.warning, one oci_registry.image_referrer, and one
attestation.signature_verification fact — 25,000 fact envelopes total).
BEFORE (raw map, commit bfbdd0a0b) -> AFTER (typed decode): 24.08ms/17.87MB/
185,248 allocs -> 26.27ms/17.40MB/215,254 allocs (+9.1% time, -2.6% memory,
+16.2% allocs). The time delta is within the diagnostic-rigor ~10% band;
memory usage actually improved. The residual allocation increase is the
reflection-based `decodeMapInto` path every prior wave's typed-decode
migration accepted as the bounded cost of the accuracy guarantee (a fact
missing a required identity field dead-letters `input_invalid` instead of
indexing under a wrong-identity FactID fallback for document/statement, or an
empty-string key for component/warning/verification). No double-decode was
introduced: `buildSBOMAttachmentIndex` decodes each component/warning/
verification exactly ONCE and stores the decoded fields in a small
per-kind evidence struct (`sbomAttachmentComponentEvidence`,
`sbomAttachmentWarningEvidence`, `sbomAttachmentVerificationEvidence`), so
`classifySBOMAttachmentDocument`/`componentEvidenceRows`/
`warningSummaryRollup` consume the pre-decoded evidence rather than
re-decoding the raw envelope a second time — mirroring the GCP relationship
join index's row-reuse fix. Result class: Correctness win with a bounded,
measured handler cost.
`TestSBOMAttestationAttachmentQuarantineReplayIsIdempotent` (new) proves
replaying the identical batch (including the quarantined fact) through
`Handle` twice converges on the same `input_invalid_facts` count, the same
`CanonicalWrites`, and `ResultStatusSucceeded` both times — the decode
outcome for a given payload is pure, so the quarantine never becomes
intermittent or escalates into a whole-intent failure across replays.

No-Observability-Change (Wave 4c, sbom_attestation family): the typed-decode
migration adds no route, graph query shape, queue table, worker, lease,
runtime knob, metric instrument, or metric label. A malformed sbom/attestation
fact surfaces through the EXISTING dead-letter path —
`partitionDecodeFailures` -> `recordQuarantinedFacts` ->
`eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`sbom_attestation_attachment`, an existing label value, `fact_kind` gaining
the sbom/attestation kinds) plus the existing structured "reducer
input_invalid fact quarantined" error log and
`Result.SubSignals["input_invalid_facts"]` — the same instruments and log key
every prior family wired. `SBOMAttestationAttachmentHandler` already carried
an `Instruments *telemetry.Instruments` field before this wave; no new
wiring was needed. `BuildSBOMAttestationAttachmentDecisions` keeps its
pre-typing public signature (no error return) because it is the entry point
existing table tests and the black-box `sbom_attestation_runtime_attachment_test.go`
exercise directly; it delegates to a new quarantine-aware
`buildSBOMAttestationAttachmentDecisionsWithQuarantine` that `Handle` calls
directly, mirroring the kubernetes_live wave's
`buildKubernetesCorrelationDecisionsWithQuarantine` pattern. The
`payload-usage-manifest` gate (`go/internal/payloadusage`) required an
additive-only extension: five new `factKindSchemaFile` entries
(`FactKindSBOMDocument`, `FactKindSBOMComponent`, `FactKindSBOMWarning`,
`FactKindAttestationStatement`, `FactKindAttestationSignatureVerification`)
and a new `SBOMStructDir` (`sdk/go/factschema/sbom/v1`) parsed alongside the
existing struct dirs in `Load`; this only widens which decode seams the gate
covers and adds no new gate mechanism.
No-Regression Evidence (Wave 4c, vulnerability_intelligence family typed-payload
decode, Contract System v1 #4566/#4582): `buildSupplyChainImpactIndexWithQuarantine`
(supply_chain_impact_index_build.go) now decodes `vulnerability.cve`,
`.affected_package`, `.affected_product`, `.os_package`, `.epss_score`, and
`.known_exploited` fact payloads through the `sdk/go/factschema` seam
(`supplyChainCVEFromEnvelope`, `supplyChainAffectedPackageFromEnvelope`,
`supplyChainAffectedProductFromEnvelope`, `supplyChainOSPackageFromEnvelope` in
`supply_chain_impact_typed_decode.go`; `decodeVulnerabilityEPSSScore`/
`decodeVulnerabilityKnownExploited` inline) instead of raw `payloadStr`/
`payloadStrings`/`payloadBool` map lookups, mirroring the #4568 AWS migration.
`vulnerability.go_module_evidence`/`.go_call_reachability` extraction
(`go_vulnerability_reachability_extract.go`) and `ClassifyGoVulnerabilityReachability`
convert the same way, gaining `extractGoModuleEvidenceRowsWithQuarantine`/
`extractGovulncheckReachabilityRowsWithQuarantine`/
`classifyGoVulnerabilityReachabilityWithQuarantine` counterparts. Only the eight
WIRED kinds are typed this wave. `vulnerability.reference` and `.source_snapshot`
have no reducer decode call, but their fields are read by the query layer's
raw-SQL-JSONB path, so they are typed-but-deferred to #4717 (which types those
query reads and adds their SQL-schema lockstep tests, mirroring the os_package
one here). `vulnerability.warning` has no read-side consumer at all and is
deferred, matching how prior waves left an unconsumed kind untyped.
`vulnerability.suppression` belongs to the separate `vulnerability_suppression`
registry family and is untouched.

First benchmark pass (`BenchmarkBuildSupplyChainImpactIndexWithQuarantine`,
2,000-advisory corpus each emitting one `vulnerability.cve` +
`vulnerability.affected_package` + `vulnerability.os_package` fact) measured a
regression exceeding the diagnostic-rigor ~10% band: BEFORE (raw map, commit
321e4aada1be3d418ab0f0a4d2628e6e68df02c9) 4.98ms/3.53MB/70,095 allocs -> naive
typed decode 9.7-15.4ms/4.82MB/78,097 allocs (roughly +100-200% time, +11.5%
allocs). Root-caused to `sdk/go/factschema/decode_map.go`'s `assignField`
having NO fast path for `float64`/`*float64` fields — every prior migrated
family (AWS, IAM, incident, GCP, Azure, kubernetes_live) has zero float fields,
so this gap was invisible until `vulnerability.cve`'s `CVSSScore *float64`
became the first float field decoded through this seam. Every `*float64` value
fell through `assignField`'s `Ptr` case to the `default: jsonRoundTripValue`
branch (a `json.Marshal`+`json.Unmarshal` round trip per field), the exact
shape of gap the kubernetes_live wave found and fixed for `map[string]string`.
Proven with a throwaway microbenchmark isolating a single-value JSON round trip
against a direct `raw.(float64)` type assertion on the same input: ~250x
speedup (500ns/op, 3 allocs -> ~2ns/op, 0 allocs). Applied the theory as a
genuine fix rather than a vulnerability-local workaround: `decode_map.go`
gained a `float64`/`*float64` fast path (`jsonNumberToFloat64`, reached from
both the plain `Float64` case and the new `Ptr`-to-`Float64` case in
`assignField`), mirroring the existing `Int32`/`Int64` pointer cases, so every
family decoding a float field in the future benefits, not only this wave's
`vulnerability.cve`. Re-measured after the fix (darwin/arm64, Apple M1 Max,
`-count=5`, alternating with the baseline to control for machine load):
BEFORE 4.76-5.27ms/3.53MB/70,095 allocs (median ~4.98ms) -> AFTER
5.6-6.7ms/4.50MB/72,093 allocs (median ~6.3ms) — roughly +27% time, +2.8%
allocs. `go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildSupplyChainImpactIndexWithQuarantine' -benchmem -count=5` (this
branch) versus the same command against commit
321e4aada1be3d418ab0f0a4d2628e6e68df02c9's `BenchmarkBuildSupplyChainImpactIndex`
(added identically on that commit for the comparison). The residual +27% is
NOT a new decode_map.go gap: a follow-up CPU profile (`-cpuprofile`) after the
fix shows no `jsonRoundTripValue` frame in the top 25 nodes; the remaining cost
is `assignField`/`decodeAndValidate` reflection dispatch scaling with
`vulnerability.affected_package`'s ten struct fields (nine named plus
Attributes-free — this kind has no untyped pass-through) versus the old raw
string-keyed map lookups, incurred TWICE per Go-ecosystem advisory fact: once
in the main index loop and once in `extractGoAffectedPackages`
(`go_vulnerability_reachability_extract.go`), a double-decode structure that
predates this migration byte-for-byte (the pre-migration `extractGoAffectedPackages`
already independently re-read the same envelopes' `payloadStr` fields a second
time for Go-specific filtering). The typed path buys the accuracy guarantee (a
`vulnerability.cve`/`.affected_package`/`.os_package` fact missing a required
identity field — `advisory_id` for the first two;
`distro`/`distro_version`/`package_manager`/`name`/`arch`/`installed_version_raw`
for `os_package` — dead-letters `input_invalid` via
`partitionDecodeFailures`/`recordQuarantinedFacts` instead of silently
producing a blank-identity index row) at a bounded, root-caused, measured cost;
the pre-existing double-decode of `affected_package` for Go-ecosystem
filtering is flagged as a follow-up consolidation opportunity, not blocking
this migration. Result class: Correctness win with a bounded, measured handler
cost (the real perf gap — the float64 fallback — found and fixed; the residual
is architectural reflection overhead, not an unmeasured regression).
`TestBuildSupplyChainImpactFindingsQuarantinesOSPackageMissingInstalledVersion`
(new flagship regression test, `vulnerability_input_invalid_test.go`) failed
before `buildSupplyChainImpactIndexWithQuarantine` routed a
missing-`installed_version_raw` `vulnerability.os_package` fact through the
decode seam (the old `payloadStr` lookup returned `""` for the absent key and
silently produced a row with no operator signal), then passed after: the
malformed fact is recorded on one `input_invalid` quarantine naming the field,
and a valid sibling CVE/affected_package/os_package trio in the same batch
still produces its finding.
`TestBuildSupplyChainImpactFindingsOSPackagePresentButEmptyVendorAdvisorySourceDecodes`
proves the absent-vs-empty distinction holds for `vulnerability.os_package`
specifically: `RepositoryClass`/`VendorAdvisorySource` are optional on the
typed struct, so a present-but-empty `vendor_advisory_source` (the collector's
own "ambiguous/unknown vendor origin" fail-closed observation) is a VALID
decode, not a quarantine — the pre-existing `osPackageMatchesAffectedPackage`
matcher still simply does not match it, byte-identical to pre-typing
behavior. `TestDecodeMapInto_Float64FastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks the float64 fast path's
accept/reject contract: a plain `float64` field, a present `*float64` field,
and an absent `*float64` field (stays nil) all decode correctly, while a
non-numeric value fails closed with an error rather than silently zeroing the
field.

`osPackageMatchesAffectedPackage` (supply_chain_impact_match.go) is UNCHANGED
by this migration: it still decides `vulnerability.os_package` impact purely
by `RepositoryClass=="vendor"` plus a `VendorAdvisorySource` string match
against the affected package's classified vendor source
(`classifyAffectedPackageAdvisorySource`), never by comparing
`InstalledVersion` (installed_version_raw, preserved verbatim through the
typed struct) against any upstream/fixed version — the exact vendor-backport
accuracy contract the migration's caveat exists to protect. No version
comparison was added or removed anywhere in this diff.

No-Observability-Change (Wave 4c, vulnerability_intelligence family): the
typed-decode migration and the shared `decode_map.go` float64 fast-path
addition add no route, graph query shape, queue table, worker, lease, runtime
knob, metric instrument, or metric label. A malformed `vulnerability.*` fact
surfaces through the EXISTING dead-letter path —
`partitionDecodeFailures` -> `recordQuarantinedFacts` ->
`eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`supply_chain_impact`, an existing label value, `fact_kind` gains the eight
vulnerability kinds) plus the existing structured "reducer input_invalid fact
quarantined" error log and `Result.SubSignals["input_invalid_facts"]`.
`SupplyChainImpactHandler` already carried an `Instruments
*telemetry.Instruments` field (wired from `DefaultHandlers.Instruments` in
`defaults_additive_domains_supply_chain.go`, shared with the security-alert
reconciliation and observability/kubernetes correlation domains in the same
file); no new wiring was needed. The `decode_map.go` float64 fast path changes
only the internal path taken by `assignField` for a `float64`/`*float64`
field; it adds no telemetry surface of its own and is invisible to every
existing operator-facing signal. The SQL-JSONB loader
`installed_advisory_targets.go` (`listOSPackageAdvisoryTargetsQuery`) is
unchanged; its `vendor_advisory_source`/`distro`/`name`/`installed_version_raw`/
`package_manager`/`repository_class`/`purl` reads are now locked to the
`vulnerability.os_package.v1` schema by
`TestOSPackageAdvisoryTargetsSQLProjectedFieldsAreSchemaDeclared`
(go/internal/storage/postgres), mirroring the incident family's SQL-schema
lockstep test, so a future contracts change that drops one of those fields
fails this build instead of silently breaking the SQL read.

No-Regression Evidence (Wave 4d, ci_cd_run family typed-payload decode,
Contract System v1 #4566): the ci_cd_run reducer handler
(`ci_cd_run_correlation.go`, `ci_cd_run_correlation_decode.go`,
`ci_cd_run_correlation_workflow_image.go` — the decode/quarantine-building
core was split into its own file to keep `ci_cd_run_correlation.go` under the
500-line cap) now decodes `ci.run`/`ci.artifact`/`ci.environment_observation`/
`ci.trigger_edge`/`ci.step`/`ci.workflow_image_evidence` fact payloads through the
`sdk/go/factschema` seam (`decodeCICDRun`/`decodeCICDArtifact`/
`decodeCICDEnvironmentObservation`/`decodeCICDTriggerEdge`/`decodeCICDStep`/
`decodeCICDWorkflowImageEvidence` in `factschema_decode_cicdrun.go`) instead
of raw `payloadString(env.Payload, "key")` map lookups, mirroring the
sbom_attestation and vulnerability_intelligence Wave 4c migrations.
`BuildCICDRunCorrelationDecisions` keeps its existing error-free signature
(`[]facts.Envelope -> []CICDRunCorrelationDecision`) and delegates to a new
quarantine-aware `buildCICDRunCorrelationDecisionsWithQuarantine` that
`CICDRunCorrelationHandler.Handle` calls directly, so the reducer intent path
reports quarantines without changing the pure classifier's signature the
existing table tests assert against (mirroring the kubernetes_live Wave 4b
pattern). A fact missing its required run-join-key field (`provider`/`run_id`
for the five run-scoped kinds) or `repository_id` (for
`ci.workflow_image_evidence`, the sole join key `attachWorkflowImagesToRuns`
uses) is quarantined per-fact via `partitionDecodeFailures` rather than
silently joining under an empty-string key, while every valid fact in the
same batch still projects.
`TestCICDRunCorrelationHandlerQuarantinesRunMissingRunID` and
`TestCICDRunCorrelationHandlerQuarantinesWorkflowImageEvidenceMissingRepositoryID`
(new regression tests mirroring
`TestSBOMAttestationAttachmentQuarantinesMissingDocumentID`) failed before the
conversion (the old `payloadString` lookup returned `""` for the absent key
and the pre-typing code silently either joined the fact onto an empty-string
run key or attached it to zero runs with no operator signal —
`SubSignals["input_invalid_facts"]` stayed unset/`0`), then passed after: the
malformed fact is recorded on `input_invalid_facts` and the valid sibling
still produces its correlation decision.
`TestCICDRunCorrelationHandlerQuarantineReplayIsIdempotent` proves replaying
the same batch (including the quarantined fact) converges on the same
quarantine count and decision each time. The pre-existing table tests
(`TestBuildCICDRunCorrelationDecisions*`, `TestPostgresCICDRunCorrelationWriter*`,
`TestCICDRunCorrelationHandlerLoadsActiveImageIdentityFacts`,
`TestCICDRunCorrelationFactIDIsStableAcrossOutcomeChanges`,
`TestWriteCICDRunCorrelationsBoundedExecCount`) stay green with byte-identical
outcomes (the five correlation outcomes — exact/derived/ambiguous/unresolved/
rejected — are unchanged for every valid-fact fixture), and
`go test ./internal/query -run 'CICD' -count=1` (the HTTP/MCP read-surface
tests) stays green with no query-shape drift. `cicdRunKey(payload
map[string]any)` is UNCHANGED and stays raw-payload: it is shared with the
SEPARATE `container_image_identity` reducer domain
(`container_image_identity_evidence.go`, `DomainContainerImageIdentity`),
which also reads `ci.run`/`ci.artifact`/`ci.workflow_image_evidence` payloads
but is OUT OF SCOPE for this migration (a distinct reducer domain, not part of
`ci_cd_run_correlation`); a new `cicdRunKeyFromParts` builds the same join key
from typed decoded fields for this file's own use. Measured with a realistic
5,000-run corpus (run + artifact + environment + trigger + step +
workflow-image-evidence per run — 30,000 facts total; darwin/arm64, Apple M1
Max, `-count=3`): `go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildCICDRunCorrelationDecisions' -benchmem`. BEFORE (raw map,
commit 93eb0582f) -> AFTER (typed decode): ~2.85-2.97s/25,160,180 allocs ->
~0.52-1.42s/265,068 allocs (roughly 2-5x FASTER, ~95x fewer allocations) —
the OLD code's `attachWorkflowImagesToRuns` ran an O(runs × workflow_images)
nested loop calling `payloadString` (a `map[string]any` lookup) on every pair;
the typed path decodes each workflow-image envelope once and compares against
a cached `*string` dereference (`ev.runDecoded.RepositoryID`) inside the inner
loop, removing the map-lookup cost from the hot O(n²) path. Unlike every
other Contract System v1 wave, this migration is a measured PERFORMANCE WIN,
not a bounded cost — the typed path is faster while also adding the accuracy
guarantee. Result class: correctness win with a measured performance
improvement (no regression to budget against).

No-Observability-Change (Wave 4d, ci_cd_run family): the typed-decode
migration adds no route, graph query shape, queue table, worker, lease,
runtime knob, metric instrument, or metric label. A malformed `ci.run`/
`ci.artifact`/`ci.environment_observation`/`ci.trigger_edge`/`ci.step`/
`ci.workflow_image_evidence` fact surfaces through the EXISTING dead-letter
path — `partitionDecodeFailures` -> `recordQuarantinedFacts` ->
`eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`ci_cd_run_correlation`, `fact_kind` = the six migrated kinds, both existing
label keys) plus the existing structured `reducer input_invalid fact
quarantined` error log and `Result.SubSignals["input_invalid_facts"]` — the
same instruments and log key every prior wave already wired. The #4573
payload-usage-manifest gate required an additive-only extension: six new
`factKindSchemaFile` entries (`FactKindCICDRun`, `FactKindCICDArtifact`,
`FactKindCICDEnvironmentObservation`, `FactKindCICDTriggerEdge`,
`FactKindCICDStep`, `FactKindCICDWorkflowImageEvidence`) and a new
`CICDRunStructDir` (`sdk/go/factschema/cicdrun/v1`) parsed alongside the
existing struct dirs in `Load`; this only widens which decode seams the gate
covers and adds no new gate mechanism. `ci.job`, `ci.pipeline_definition`,
and `ci.warning` are emitted by the collector but have no reducer decode call
today, so they are intentionally NOT typed this wave (cicdrun/v1 AGENTS.md),
matching how prior waves left an emitted-but-unread kind typed only when its
consumer lands.

No-Regression Evidence (Wave 4d, ci_cd_run family — PR #4724 review fixes):
two review findings were addressed. (1) codex P2 accuracy regression: the
pre-migration reducer read every ci.* payload field through `payloadString`,
which did `strings.TrimSpace(fmt.Sprint(value))` on every read
(`package_correlation_writer.go`). The typed decode seam preserves the raw
collector string, so a `ci.run` with `commit_sha:"   "` no longer trimmed to
`""` (skipping the unresolved-anchor check) and `run_id:" run-1 "` joined under
a padded key instead of the clean `run-1` — a byte-drift for padded/whitespace
inputs the clean-fixture tests missed. Fixed by trimming every ci.* identity/
anchor field at the point of use in the correlation logic via `trimmedCICDField`
(required non-pointer fields) / `trimmedCICDPtr` (optional pointer fields) in
`ci_cd_run_correlation_decode.go`, applied in `cicdRunKeyFromParts`,
`classifyCICDRunEvidence` (provider/run_id/run_attempt/repository_id/commit_sha/
environment/artifact_digest), `ciArtifactDigests`, `ciWorkflowImageRefs`, the
step `deployment_hint_source` compare, and `attachWorkflowImagesToRuns`/
`classifyCICDWorkflowImageEvidence` (repository_id/evidence_class/image_ref) —
the typed structs still carry the raw collector value; only the correlation
logic trims. `TestBuildCICDRunCorrelationDecisionsTrimsIdentityFieldsLikePayloadString`
and `TestBuildCICDRunCorrelationDecisionsTrimsArtifactAndWorkflowImageEvidence`
(new) failed on the raw-typed HEAD (padded provider/environment surfaced
untrimmed, a padded artifact_digest/evidence_class produced `derived` instead
of `exact`, a whitespace-only commit_sha was not `unresolved`), then passed
after the trim restore. (2) copilot perf: `classifyCICDWorkflowImageEvidence`
re-decoded each `ci.workflow_image_evidence` envelope once per run in a repo
(O(runs x workflow_images) typed decodes, since `attachWorkflowImagesToRuns`
fans the same evidence to every run). Fixed by decoding each workflow-image
envelope exactly once during the build phase (`decodedCICDWorkflowImage` pairs
the envelope with its decoded typed value) and having both attach and classify
read the cached struct through a `*decodedCICDWorkflowImage` (pointer so the
attach fan-out copies a pointer, not the fat decoded value). Proven with a new
shared-repo benchmark (`BenchmarkBuildCICDRunCorrelationDecisionsSharedRepoWorkflowImages`,
500 runs x 50 shared workflow images — the copilot scenario the original
unique-repo-per-run benchmark did not exercise; darwin/arm64, `-count=3`):
BEFORE (re-decode per run) ~4.35 ms/op, 19.67 MB/op, 11,427 allocs/op ->
AFTER (once-decode pointer cache) ~1.39 ms/op, 1.21 MB/op, 9,274 allocs/op
(~3.1x faster, ~16x less memory). The original unique-repo benchmark stayed
flat (~557-568 ms/op, 230,064 allocs/op, no regression). `go test
./internal/reducer -run CICD -count=1` and the golden-corpus gate stay green
(valid-fact correlation output byte-identical), so the trim restore only
corrects padded/whitespace drift and the decode cache is output-preserving.

No-Observability-Change (Wave 4d, ci_cd_run — PR #4724 review fixes): the trim
restore and the workflow-image decode cache change no route, graph query shape,
queue table, worker, lease, runtime knob, metric instrument, metric label, or
log key; both are pure in-process correlation-logic changes diagnosed through
the same existing reducer run spans, execution counters, and
`eshu_dp_reducer_input_invalid_facts_total` the migration already wired.

No-Regression Evidence (Wave 4d, secrets_iam family typed-payload decode,
Contract System v1 #4566/#4582 -- VAULT + K8S lanes only): `buildSecretsIAMIndex`
(secrets_iam_trust_chain_build.go) now decodes `vault_auth_role`,
`vault_acl_policy`, `vault_kv_metadata`, `k8s_service_account`,
`k8s_workload_identity_use`, `eks_irsa_annotation`, and
`eks_pod_identity_association` fact payloads through the `sdk/go/factschema`
seam (`decodeVaultAuthRole`/`decodeVaultACLPolicy`/`decodeVaultKVMetadata`/
`decodeKubernetesServiceAccount`/`decodeKubernetesWorkloadIdentityUse`/
`decodeEKSIRSAAnnotation`/`decodeEKSPodIdentityAssociation` in
`factschema_decode_secretsiam.go`) instead of raw `payloadString`/
`payloadStrings`/`payloadBool` map lookups, mirroring the #4568 AWS IAM
migration this same file already carries for `aws_iam_principal`. The AWS IAM
lane (`aws_iam_principal`, `aws_iam_trust_policy`) stays exactly as #4568 left
it -- untouched by this wave. The GCP IAM lane (`gcp_iam_principal`,
`gcp_iam_trust_policy`, `gcp_iam_permission_policy`) is explicitly OUT OF
SCOPE and deferred to its own future wave; every raw read site for those three
kinds carries a `// deferred: gcp_iam lane, Wave 4d types vault/k8s only`
comment. `k8s_gcp_workload_identity_binding` is an IN-SCOPE K8S-lane kind:
`secretsIAMIndex.gcpK8sBindings` now holds it decoded
(`decodeKubernetesGCPWorkloadIdentityBinding`, the `secretsIAMGCPBinding` pair
type mirroring every other VAULT/K8S kind's decode-once-at-index-build
pattern), and `secretsIAMGCPExactChainsForServiceAccount` reads
`binding.decoded.GCPServiceAccountEmailDigest`/
`GCPWorkloadIdentitySubjectFingerprint` rather than raw `payloadString`. Only
the downstream join against the deferred `gcp_iam_trust_policy` raw envelope
(`exactGCPWorkloadIdentityTrusts`, `index.gcpPrincipals`,
`index.gcpPermissions`) stays on raw reads -- that half of the join belongs to
the GCP IAM lane's own future wave, mirroring how #4715 (sbom_attestation)
left `sbom.component`'s `supply_chain_impact` consumer raw because that
reducer domain had zero existing quarantine plumbing of its own.

`buildSecretsIAMIndex` now returns a `[]quarantinedFact` alongside the index (as
it already did for `aws_iam_principal`): a `vault_auth_role` fact missing its
required `role_join_key`, or a `k8s_service_account`/
`k8s_workload_identity_use`/`eks_irsa_annotation`/
`eks_pod_identity_association` fact missing its required
`service_account_join_key` (or `role_arn` for the two EKS kinds), is quarantined
per-fact via `partitionDecodeFailures` rather than silently vanishing from
`index.vaultRoles`/`index.serviceAccounts`/`index.workloads`/`index.irsa`
under `addByKey`'s pre-typing blank-key guard -- while every valid sibling
fact's exact chain still resolves. `TestBuildSecretsIAMTrustChainReadModelsQuarantinesVaultAuthRoleMissingRoleJoinKey`
and `TestBuildSecretsIAMTrustChainReadModelsQuarantinesK8sServiceAccountMissingJoinKey`
(new, `secrets_iam_input_invalid_test.go`, mirroring
`TestGCPResourceMaterializationQuarantinesMissingFullResourceName`) failed
before the conversion (quarantine count 0; the old `payloadString`/
`payloadStrings` lookups returned `""`/`nil` for the absent key and
`addByKey`'s blank-key guard silently dropped the fact from the index with no
operator signal, not even a posture gap), then passed after: the malformed
fact is recorded on `input_invalid_facts` and the valid sibling workload-to-
vault-secret chain in the same batch still resolves to `SecretsIAMTrustChainStateExact`
with its full secret access path.

The VAULT/K8S facts are decoded exactly ONCE at index-build time and the
decoded struct is stored alongside the source envelope in a small per-kind
pair type (`secretsIAMServiceAccount`, `secretsIAMWorkload`, `secretsIAMIRSA`,
`secretsIAMVaultRole`, `secretsIAMVaultPolicy`, `secretsIAMVaultKV`,
`secretsIAMGCPBinding`), mirroring the existing `secretsIAMPrincipal` pattern
#4568 established for `aws_iam_principal` -- every downstream trust-chain
function (`secretsIAMExactChains`, `secretsIAMChain`, `secretsIAMVaultPaths`,
`exactIAMRoleTrust`, `secretsIAMWildcardVaultAuthRoleObservations`,
`vaultPolicyRules`) reads the pre-decoded struct fields rather than re-decoding
the envelope, so there is no double-decode of any VAULT/K8S fact.
`VaultACLPolicy.Rules` is a fully typed `[]secretsiamv1.VaultACLPolicyRule`
(not a `map[string]any` pass-through), matching the collector emitter's
closed `{path_fingerprint, path_depth, capabilities}` shape per rule.

First benchmark pass (`BenchmarkBuildSecretsIAMTrustChainReadModels`, new,
2,000-service-account corpus, each producing one full exact
workload-to-vault-secret chain with a 2-rule `vault_acl_policy`) surfaced a CPU
profile with a small (~2-4% cumulative) but real `jsonRoundTripValue` fallback
hit, isolated via `go tool pprof -peek` to `assignField`'s `Ptr` case for a
`*int` field -- `VaultACLPolicyRule.PathDepth` and `VaultKVMetadata.PathDepth`
were the first `*int`-shaped payload fields any migrated family decoded through
this seam (every prior family's optional integer fields used `*int32`/
`*int64`/`*float64`, each already fast-pathed by Wave 4a/4c). This is the same
gap class the kubernetes_live wave (`map[string]string`) and the
vulnerability_intelligence wave (`float64`) found and fixed for their own
first-occurrence field shapes. Proven with a throwaway microbenchmark isolating
a single `*int` JSON round trip against a direct `float64`/`int` type-switch
coercion on the same input: ~500x speedup (166ns/op, 2 allocs -> 0.32ns/op, 0
allocs). Applied the theory as a genuine fix: `decode_map.go` gained a
`jsonNumberToInt` helper and an `Int` case in `assignField`'s `Ptr` switch,
mirroring the existing `Int32`/`Int64`/`Float64` pointer cases, so every family
decoding an optional platform-`int` field in the future benefits, not only this
wave's `PathDepth` fields. Re-measured after the fix
(`go test ./internal/reducer -run '^$' -bench
'BenchmarkBuildSecretsIAMTrustChainReadModels' -benchmem -count=7 -benchtime=2s`,
darwin/arm64, Apple M1 Max; backend: in-memory extractor; input: 2,000
synthetic service accounts x {k8s_service_account,
k8s_workload_identity_use, eks_irsa_annotation, aws_iam_principal,
aws_iam_trust_policy, vault_auth_role, vault_acl_policy (2 rules),
vault_kv_metadata} = 16,000 fact envelopes). BEFORE (raw map, commit
b12a335eb) -> AFTER (typed decode + `*int` fast path), both measured
alternately on the same machine to control for ambient load: median 29.5ms /
320,331-320,335 allocs / 32.96MB/op -> median 29.0ms / 314,329-314,332 allocs /
34.19MB/op (~0% time delta within noise, -1.9% allocs, +3.7% memory). The
allocation count went DOWN, not up, and the small memory increase is the typed
struct fields themselves costing more bytes than an untyped `map[string]any`
entry -- both well inside the diagnostic-rigor ~10% band. The repo's existing
GATED perfcontract benchmark for this handler,
`BenchmarkSecretsIAMGCPGrantObservations` (`handler_budget_secrets_iam_gcp_grant_observations`,
ceiling 8,300,000 ns/op, `testdata/benchmarks/reducer-handler-budgets.txt`),
builds its index purely from `gcp_iam_principal`/`gcp_iam_permission_policy`
facts -- the deferred GCP IAM lane this wave leaves untouched -- so it never
exercises the converted VAULT/K8S switch arms; measured before/after on this
branch it stayed flat (median ~8.5ms both before and after, allocs unchanged
at 116,039-116,043), confirming this wave's decode-path change is invisible to
that specific gated benchmark, as expected. Every prior migrated family's own
benchmark (`BenchmarkExtractCloudResourceNodeRows`,
`BenchmarkExtractAWSRelationshipEdgeRows`,
`BenchmarkExtractGCPCloudResourceNodeRows`,
`BenchmarkExtractGCPRelationshipEdgeRows`,
`BenchmarkExtractAzureCloudResourceNodeRows`,
`BenchmarkExtractAzureRelationshipEdgeRows`,
`BenchmarkExtractKubernetesWorkloadNodeRows`,
`BenchmarkBuildIncidentRoutingEvidenceInputs`,
`BenchmarkBuildSupplyChainImpactIndexWithQuarantine`,
`BenchmarkBuildSBOMAttestationAttachmentDecisions`) was re-run after the
shared `decode_map.go` change and stayed within its previously documented
allocation counts, confirming the new `*int` fast path is additive-only (a new
accept path only; every other shape still falls through to the identical
`jsonRoundTripValue` fallback). `TestDecodeMapInto_IntFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks the fast path's accept/reject
contract: a plain `int` field, a present `*int` field, and an absent `*int`
field (stays nil) all decode correctly, while a non-integral float and a
non-numeric value both fail closed with an error rather than silently
truncating or zeroing the field. Result class: Correctness win with no
measured handler regression (the real perf gap -- the missing `*int` fast
path -- found and fixed before it could regress this or any future family).

No-Observability-Change (Wave 4d, secrets_iam family, VAULT + K8S lanes): the
typed-decode migration and the shared `decode_map.go` `*int` fast-path
addition add no route, graph query shape, queue table, worker, lease, runtime
knob, metric instrument, or metric label. A malformed `vault_auth_role`/
`vault_acl_policy`/`vault_kv_metadata`/`k8s_service_account`/
`k8s_workload_identity_use`/`eks_irsa_annotation`/
`eks_pod_identity_association`/`k8s_gcp_workload_identity_binding` fact
surfaces through the EXISTING dead-letter path -- the same
`partitionDecodeFailures`/`recordQuarantinedFacts` seam the `aws_iam_principal`
conversion (#4568) already wired in this same file -- incrementing the
existing `eshu_dp_reducer_input_invalid_facts_total` counter (labeled `domain`
= `secrets_iam_trust_chain`, an existing label value, `fact_kind` gaining the
eight VAULT/K8S kinds) plus the existing structured "reducer
input_invalid fact quarantined" error log and
`Result.SubSignals["input_invalid_facts"]`. `SecretsIAMTrustChainHandler`
already carried an `Instruments *telemetry.Instruments` field before this
wave; no new wiring was needed. The `payload-usage-manifest` gate
(`go/internal/payloadusage`) required an additive-only extension: eight new
`factKindSchemaFile` entries (`FactKindVaultAuthRole`, `FactKindVaultACLPolicy`,
`FactKindVaultKVMetadata`, `FactKindKubernetesServiceAccount`,
`FactKindKubernetesWorkloadIdentityUse`, `FactKindEKSIRSAAnnotation`,
`FactKindEKSPodIdentityAssociation`, `FactKindKubernetesGCPWorkloadIdentityBinding`)
and a new `SecretsIAMStructDir` (`sdk/go/factschema/secretsiam/v1`) parsed
alongside the existing struct dirs in `Load`; this only widens which decode
seams the gate covers and adds no new gate mechanism. The `decode_map.go`
`*int` fast path changes only the internal path taken by `assignField` for a
`*int`-shaped field; it adds no telemetry surface of its own and is invisible
to every existing operator-facing signal.

No-Regression Evidence (Wave 4e, security_alert family typed-payload decode,
Contract System v1 #4566/#4582): the SINGLE decode site for the
`security_alert.repository_alert` kind (`extractProviderSecurityAlerts`,
`security_alert_reconciliation.go`) now decodes through the `sdk/go/factschema`
seam (`decodeSecurityAlertRepositoryAlert` in
`factschema_decode_securityalert.go`, converting the typed
`securityalertv1.RepositoryAlert` into `providerSecurityAlert` via
`providerSecurityAlertFromDecoded`) instead of raw `payloadStr`/`payloadStrings`/
`securityAlertMap`/`securityAlertStringMap`/`securityAlertStringMapSlice`/
`securityAlertInt64` map lookups (all of which were DELETED). This kind is
delicate because its ONE decode site feeds TWO consumers:
`BuildSecurityAlertReconciliations` (the reconciliation read surface) and
`appendSecurityAlertImpactFindings` (the `supply_chain_impact` seeder, a
CanonicalWrites path). Both were converted in lockstep — a malformed fact is
quarantined per-fact on BOTH via `partitionDecodeFailures`/
`recordQuarantinedFacts`, and `SupplyChainImpactHandler.Handle` merges the
security-alert quarantines with its existing vulnerability quarantines so a
poisoned security_alert fact never aborts the supply_chain_impact generation.
`repository_id` is the only required field (the repository/provider anchor both
consumers key on; `securityAlertCanSeedImpact` already required it non-empty for
impact seeding). The typed struct mirrors the wire payload EXACTLY (no field
added/renamed/narrowed/widened), and the reducer re-applies the same trim/
drop-empty container normalization after decode, so valid facts produce
byte-identical reconciliation decisions AND byte-identical impact findings — no
supply_chain_impact node/edge/finding changes for any fact that decodes; only a
`repository_id`-less fact's outcome changes, from a silent blank-repository row/
finding to a visible input_invalid dead-letter.
`TestBuildSecurityAlertReconciliationsQuarantinesMissingRepositoryID` and
`TestSecurityAlertReconciliationHandlerQuarantinesMissingRepositoryID` (new)
failed before the conversion (quarantine count 0, the malformed fact silently
accepted with an empty RepositoryID), then passed after; the supply_chain_impact
equivalence + isolation is locked by
`TestSupplyChainImpactSecurityAlertSeededFindingsUnchangedByTypedDecode` (valid
→ byte-identical finding) and
`TestSupplyChainImpactQuarantinesMalformedSecurityAlertWithoutPoisoningGeneration`
(malformed → per-fact quarantine, findings reflect.DeepEqual the baseline, no
empty-identity finding, generation still succeeds), and replay purity by
`TestSecurityAlertReconciliationQuarantineReplayIsIdempotent`.

First benchmark pass (`BenchmarkExtractProviderSecurityAlerts`, 5,000-alert
corpus carrying the full Dependabot field set including a `cwes`
`[]map[string]string` list and `epss` `map[string]string` object) measured a
regression far exceeding the diagnostic-rigor ~10% band: BEFORE (raw map)
~14.3ms/25.6MB/210,014 allocs -> naive typed decode ~25ms/32.9MB/325,028 allocs
(roughly +75% time, +55% allocs). Root-caused via CPU + alloc profile to
`sdk/go/factschema/decode_map.go`'s `assignField` having NO fast path for a
`[]map[string]string` field — every prior migrated family's slice fields are
`[]string` or `[]Struct`, so `security_alert.repository_alert`'s `CWEs
[]map[string]string` (shared shape with `gcpv1.IAMPolicyObservation.Members`)
became the first slice-of-map field decoded through this seam and every element
fell through to the `default: jsonRoundTripValue` marshal/unmarshal branch —
the exact gap class the kubernetes_live (`map[string]string`) and vulnerability
(`float64`) waves closed for their own first-occurrence shapes. Applied the fix
as a genuine shared decode-path improvement, not a security-alert-local
workaround: `decode_map.go` gained a `[]map[string]string` fast path
(`anyToStringMapSlice`, reached from the new `reflect.Map` case in the
`reflect.Slice` switch), mirroring the existing `map[string]string` map fast
path, so every family decoding a slice-of-string-map field benefits (gcp
Members included). `TestDecodeMapInto_StringMapSliceFastPath` (new,
`sdk/go/factschema/decode_map_test.go`) locks its accept/reject/equivalence
contract. A second alloc profile then showed the EPSS/CWEs string maps were
double-allocated (decode allocates the map, then the normalizer cloned it), so
the reducer normalizers (`normalizeSecurityAlertStringMap`/
`normalizeSecurityAlertStringMapSlice`) now trim/drop-empty IN PLACE on the
solely-owned decode result (`normalizeSecurityAlertStringMapInPlace`, re-keying
a padded key after the range to stay iteration-safe;
`TestNormalizeSecurityAlertStringMapInPlace` locks the contract). After both
fixes: ~27.0MB/225,017 allocs versus the raw-map baseline's 25.6MB/210,014 —
+5.5% bytes, +7.1% allocs, within the diagnostic-rigor ~10% band (allocations/
bytes are the load-independent proxy; wall-clock ns/op tracked the same ratio
when measured un-contended, ~17ms typed vs ~14ms raw floor). This is a COLD
per-scope-generation reconciliation projection path (once per reconciliation
intent over the loaded alert facts, not a per-edge hot loop), and the residual
buys the accuracy guarantee (a `repository_id`-less alert dead-letters instead
of seeding a blank-repository reconciliation row or empty-identity impact
finding). Result class: Correctness win with a bounded, measured handler cost
(the real perf gap — the `[]map[string]string` fallback — found and fixed; the
double-alloc removed).

Review fixes (Wave 4e, PR #4735): (1) the shared `decode_map.go`
`anyToStringMap`/`anyToStringMapSlice` fast paths now defensively CLONE their
already-typed `map[string]string` / `[]map[string]string` input branches, not
just the JSONB `map[string]any` / `[]any` branches, so a decode result is always
a fresh owned value. The reducer normalizes epss/cwes in place, so aliasing an
in-memory caller's `env.Payload` would have mutated the original payload; the
JSONB decode path always allocated (that branch is not hit in production), so the
clone is free in prod and keeps the decode side-effect-free for every input shape
(`TestDecodeMapInto_TypedMapInputNotMutated` locks non-mutation of an
already-typed input). (2) `extractProviderSecurityAlerts` — the LENIENT
pre-filter/scoping wrapper — no longer drops a `repository_id`-less alert; it
reconstructs it best-effort from the raw payload
(`providerSecurityAlertFromRawPayload`) so the security-alert evidence-scoping
fence (`supplyChainImpactUsesSecurityAlertScope` /
`scopeSupplyChainImpactEvidenceToSecurityAlerts`) still narrows by the alert's
package/ecosystem identity when every alert in a security-alert-triggered
`supply_chain_impact` intent is malformed, exactly as pre-typing. Without this,
all-malformed alerts skipped scoping and unrelated active dependency/vulnerability
facts (loaded earlier from the malformed alert's package/CVE hints) could publish
unscoped impact findings. The DURABLE reconciliation and impact-seeding paths keep
using the strict `extractProviderSecurityAlertsWithQuarantine`, so the malformed
fact still dead-letters as `input_invalid`; only the non-durable scoping signal is
preserved (`TestSupplyChainImpactSecurityAlertScopingSurvivesAllMalformedAlerts`).

No-Observability-Change (Wave 4e, security_alert family): the typed-decode
migration and the shared `decode_map.go` `[]map[string]string` fast-path
addition add no route, graph query shape, queue table, worker, lease, runtime
knob, metric instrument, or metric label. A malformed
`security_alert.repository_alert` fact surfaces through the EXISTING dead-letter
path — `partitionDecodeFailures` -> `recordQuarantinedFacts` ->
`eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`security_alert_reconciliation` on the reconciliation handler and
`supply_chain_impact` on the impact handler, both existing label values;
`fact_kind` gains `security_alert.repository_alert`) plus the existing
structured "reducer input_invalid fact quarantined" error log and
`Result.SubSignals["input_invalid_facts"]` — the same instruments and log key
every prior family wired. `SecurityAlertReconciliationHandler` and
`SupplyChainImpactHandler` already carried an `Instruments
*telemetry.Instruments` field before this wave; no new wiring was needed. The
`payload-usage-manifest` gate (`go/internal/payloadusage`) required an
additive-only extension: one new `factKindSchemaFile` entry
(`FactKindSecurityAlertRepositoryAlert`) and a new `SecurityAlertStructDir`
(`sdk/go/factschema/securityalert/v1`) parsed alongside the existing struct
dirs; this only widens which decode seams the gate covers and adds no new gate
mechanism.
No-Regression Evidence (Wave 4e, observability family typed-payload decode,
Contract System v1 #4566/#4582): the observability coverage-metadata classifier
(`observability_coverage_metadata.go`, `observabilityMetadataEvidenceFromEnvelope`)
now decodes the seventeen declared/applied/observed observability fact kinds
through the `sdk/go/factschema` seam (`decodeObservabilityMetadataView` in
`observability_coverage_metadata_decode.go`, per-kind `decode<Kind>` wrappers in
`factschema_decode_observability.go`) instead of raw
`payloadString(env.Payload, "…")` map lookups. `source_instance_id` is required
on every kind (the one identity field every collector injects in both lanes);
`provider_object_uid` is additionally required on the four observed kinds whose
sole emitter always writes it (`observed_dashboard`/`observed_target`/
`observed_log_signal`/`observed_trace_signal`), and stays optional on
`observed_rule` (the Grafana emitter uses `alert_rule_uid`). Decode failures
route through the shared `partitionDecodeFailures` classifier, threaded up
through `classifyObservabilityMetadataEvidence` ->
`BuildObservabilityCoverageDecisions` to both the correlation and materialization
handlers, so a fact missing `source_instance_id` is a per-fact input_invalid
quarantine (counter + structured error log + `Result.SubSignals`), skipped while
every valid sibling still classifies.
`TestObservabilityCoverageMetadataQuarantinesMissingSourceInstanceID` (new
regression test) failed before the conversion — a missing anchor read `""` and
still produced a source-less coverage decision keyed on the StableFactKey
fallback (a silent projection) — then passed after: the malformed fact
quarantines input_invalid (quarantine count 0 -> 1) and produces no decision,
while the valid sibling still projects. This is a COLD, once-per-scope-generation
projection path (the correlation/materialization handlers each call
`BuildObservabilityCoverageDecisions` once per intent, not a hot per-edge loop),
so the typed-decode cost is measured for a no-regression bound rather than a
tight microbench. Measured with `go test ./internal/reducer -run '^$' -bench
'BenchmarkObservabilityMetadata(TypedDecode|RawMap)$' -benchmem -benchtime=2s
-count=5` (darwin/arm64; batch: 17 kinds x 64 = 1088 facts): best-of typed
~1.60ms/op, 1.50MB/op, 12114 allocs/op vs raw-map baseline ~1.12ms/op, 1.16MB/op,
10195 allocs/op — roughly +0.5us and +2 allocs per fact through the marshal-free
reflection decoder (`decode_map.go`) plus the typed metadata view. The residual
per-scope-generation cost buys the accuracy guarantee (a missing
`source_instance_id` dead-letters instead of projecting a source-less
correlation row) and is a bounded, measured cold-path cost of the same class the
Wave 4a incident family recorded, not a hot-path regression. Valid facts produce
byte-identical coverage/drift correlation output (the object-ref 20-key fallback
chain and StableFactKey terminal fallback are preserved field-for-field via the
typed view). Result class: correctness win with a bounded, measured cold-path
cost.

No-Observability-Change (Wave 4e, observability family): the typed-decode
migration adds no route, graph query shape, queue table, worker, lease, or
runtime knob. A malformed observability fact surfaces through the EXISTING
per-fact input_invalid apparatus every prior wave established —
`recordQuarantinedFacts` -> the `eshu_dp_reducer_input_invalid_facts_total`
counter (existing instrument; the `domain` label gains the
`observability_coverage_correlation`/`observability_coverage_materialization`
values and the `fact_kind` label gains the observability kinds, both existing
label keys) plus one structured error log per quarantined fact and the
`input_invalid_facts` count on the existing per-intent `Result.SubSignals` and
completion log. The `ObservabilityCoverageCorrelationHandler` and
`ObservabilityCoverageMaterializationHandler` already carried an
`Instruments *telemetry.Instruments` field before this wave; no new wiring was
needed. The `payload-usage-manifest` gate required an additive-only extension:
seventeen new `factKindSchemaFile` entries and a new `ObservabilityStructDir`
(`sdk/go/factschema/observability/v1`) parsed alongside the existing struct dirs
in `Load` (`observability.source_instance` is intentionally absent — the
classifier skips that kind, so it has no reducer decode seam, mirroring how the
sbom family leaves its unconsumed kinds unmapped); this only widens which decode
No-Regression Evidence (Wave 4e, documentation family typed-payload decode,
Contract System v1 #4566/#4582): the documentation edge materialization
handler (`documentation_edge_materialization.go`,
`documentation_edge_delta_scope.go`) now decodes `documentation_document` and
`documentation_entity_mention` fact payloads through the `sdk/go/factschema`
seam (`decodeDocumentationDocument`/`decodeDocumentationEntityMention` in
`factschema_decode_documentation.go`) instead of raw
`semanticPayloadString`/`payloadStr`/`mapSlice`/`sourceMetadataString` map
lookups, mirroring the AWS/GCP/Azure/kubernetes_live/sbom_attestation
migrations. Only these two of the family's eight kinds are typed AND wired
this wave; `documentation_source`, `documentation_section`,
`documentation_link`, and `documentation_claim_candidate` have no reducer or
storage-loader field-level read at all (the query read model and
`go/internal/storage/postgres` filter on them only by `fact_kind` column or
JSONB containment, never a decoded field), and `documentation_finding`/
`documentation_evidence_packet` are emitted by `go/internal/doctruth` (a
different owning package) and read only by the query layer's raw
`fact_records.payload->>'field'` SQL — out of scope for this migration,
matching the incident family's SQL-projected-fields precedent
(`TestDocumentationFindingSQLProjectedFieldsAreSchemaDeclared`,
`go/internal/query`, locks that those SQL-read fields stay schema-declared
even though the query layer itself is not converted).
`documentation_section` carries its OWN schema version
(`facts.DocumentationSectionFactSchemaVersion`, `"1.1.0"`), preserved via the
existing `schema_version_overrides: {documentation_section: "1.1.0"}` entry
in `specs/fact-kind-registry.v1.yaml`; the decode seam still dispatches on
the schema-version major only (`"1"`), mirroring `gcp_cloud_resource`'s
identical one-minor-ahead precedent — no reducer or decode-dispatch change
was needed for this. `ExtractDocumentationEdgeRows`/`buildDocumentationDeltaScope`
keep their pre-typing error-free signatures (existing table tests exercise
them directly) and delegate to new quarantine-aware
`ExtractDocumentationEdgeRowsWithQuarantine`/`buildDocumentationDeltaScopeWithQuarantine`
functions that `Handle` calls directly, mirroring the kubernetes_live wave's
`buildKubernetesCorrelationDecisionsWithQuarantine` pattern. A
`documentation_entity_mention` fact missing its required `document_id`,
`section_id`, or `resolution_status` field, or a `documentation_document`
fact missing its required `document_id` field, is quarantined per-fact via
`partitionDecodeFailures` rather than the pre-typing behavior of silently
skipping the fact (both helpers returned `""` for an absent key
indistinguishably from a present-but-empty one, and neither produced any
operator signal).
`TestExtractDocumentationEdgeRowsQuarantinesMissingDocumentID` and
`TestBuildDocumentationDeltaScopeWithQuarantineQuarantinesMissingDocumentID`
(new, mirroring `TestKubernetesWorkloadMaterializationQuarantinesMissingObjectID`)
failed before the conversion (a missing `document_id` silently dropped with
no operator signal, or degraded to an empty-string join key), then passed
after: the malformed fact is recorded on `input_invalid_facts` and a valid
sibling fact in the same batch still produces its DOCUMENTS edge or
contributes to the delta scope.
`TestDocumentationMaterializationHandlerRecordsQuarantinedMentionInputInvalid`
proves the full `Handle` path surfaces the quarantine through
`Result.SubSignals["input_invalid_facts"]`.

Measured with `go test ./internal/reducer -run '^$' -bench
'BenchmarkExtractDocumentationEdgeRowsWithQuarantine|BenchmarkBuildDocumentationDeltaScopeWithQuarantine'
-benchmem -count=5` (darwin/arm64, Apple M1 Max; backend: in-memory
extractor; input: 5,000 synthetic `documentation_entity_mention` facts each
carrying one candidate ref, and one repository-delta fact plus 5,000 matching
`documentation_document` facts) against the equivalent pre-typing benchmark
added identically on commit 8749b7c1d
(`BenchmarkExtractDocumentationEdgeRows`/`BenchmarkBuildDocumentationDeltaScope`).
BEFORE (raw map, commit 8749b7c1d) -> AFTER (typed decode):
`ExtractDocumentationEdgeRows` 3.28-4.31ms/3.71MB/75,064 allocs (median
~3.66ms) -> `ExtractDocumentationEdgeRowsWithQuarantine` 5.21-7.98ms/4.83MB/
95,062 allocs (median ~5.34ms), roughly +46% time, +30% allocs;
`buildDocumentationDeltaScope` 3.15-3.51ms/2.98MB/5,261 allocs (median
~3.21ms) -> `buildDocumentationDeltaScopeWithQuarantine` 5.77-6.07ms/5.54MB/
20,261 allocs (median ~5.84ms), roughly +82% time, +285% allocs. Both deltas
exceed the diagnostic-rigor ~10% band by percentage, so this was investigated
as a candidate `decode_map.go` fast-path gap the way kubernetes_live's
`map[string]string` and vulnerability's `float64` gaps were: a CPU profile
(`-cpuprofile`) of `BenchmarkExtractDocumentationEdgeRowsWithQuarantine` shows
NO `jsonRoundTripValue` frame in the top 20 nodes — every field shape this
family's structs use (`*string`, `*bool`, `*ACLSummary` as a pointer-to-struct,
`[]EvidenceRef`/`[]OwnerRef` as a slice-of-struct, `map[string]string`) already
has a `decode_map.go` fast path from a prior wave. The cost is `decodeMapInto`
iterating its full per-type field plan (9 fields for `EntityMention`, versus
the 4-5 keys the pre-typing code read via direct map indexing) plus one nested
`assignStructSlice`/`decodeMapInto` recursion per candidate ref — genuine,
proportional reflection and allocation cost for a richer struct shape, not an
unhandled gap; profiling shows GC/allocation overhead (`runtime.mallocgc`,
`runtime.mapassign_faststr`) as the dominant cost alongside `decodeMapInto`
itself, not a marshal/unmarshal round trip. `DomainDocumentationMaterialization`
is a per-scope-generation materialization domain (registered like
`DomainInheritanceMaterialization` in `registry.go`), not a shared-projection
hot-per-edge-loop domain, so this is a COLD path — the same "measured for a
no-regression bound rather than a tight microbench" classification the
incident family's Wave 4a evidence used for its own once-per-scope-generation
cost (2.42-2.57ms for 500 incidents x 4 fact kinds = 2,000 fact envelopes on
that wave's benchmark corpus, ~= 1.2 microseconds/fact). This wave's
benchmark corpus is 5,000 facts per function (matching the kubernetes_live/
sbom scale), so the fact-normalized cost is directly comparable:
`ExtractDocumentationEdgeRowsWithQuarantine` costs ~5.34ms / 5,000 facts ~=
1.07 microseconds/fact, and `buildDocumentationDeltaScopeWithQuarantine`
costs ~5.84ms / 5,000 facts ~= 1.17 microseconds/fact — both landing within
the same ~1.1-1.2 microseconds/fact band the incident precedent's own
2.42ms/2,000-fact corpus already accepted as a cold-path cost, confirming
the percentage delta (+46%/+82%) is a magnitude artifact of this family's
faster absolute baseline (this benchmark's raw-map baseline ran in
3.2-3.7ms, versus incident's un-migrated baseline being a slower starting
point), not a growing or unbounded per-fact cost.

WASTED-DECODE CHECK (coordinator-directed, before accepting the cold-path
bound): both functions were audited to confirm the typed decode call sits
behind the cheapest available pre-filter, not merely after some filter.
`ExtractDocumentationEdgeRowsWithQuarantine` checks `env.FactKind !=
facts.DocumentationEntityMentionFactKind || env.IsTombstone` (a struct-field
read, no decode) BEFORE calling `decodeDocumentationEntityMention`; there is
no cheaper pre-filter available before checking `ResolutionStatus ==
"exact"` because `ResolutionStatus` is itself a required struct field that
does not exist until the payload decodes (the pre-typing code paid the
identical cost via `payloadStr(env.Payload, "resolution_status")`, a raw map
lookup of the same key, not a skip). `buildDocumentationDeltaScopeWithQuarantine`
checks `scope.hasDelta` BEFORE entering the `documentation_document` loop at
all (no repository delta means zero document facts are decoded), and within
the loop checks `env.FactKind != facts.DocumentationDocumentFactKind ||
env.IsTombstone` before calling `decodeDocumentationDocument`; there is no
cheaper pre-filter for `document_id`/`source_metadata.path` because both are
struct fields the pre-typing code also had to read via
`semanticPayloadString`/`sourceMetadataString` for every document fact in
the batch, an identical per-fact read count to the typed path. Neither
function decodes a fact whose result is then discarded by a filter that
could have run first; the ordering is already optimal, so the regression is
irreducible reflection/allocation cost for this family's field count and
nested-slice shape, not a wasted-work bug.

The residual cost buys the accuracy guarantee (a fact missing a required
identity field dead-letters `input_invalid` instead of silently producing an
empty-identity edge or being dropped from delta scope with no operator
signal). Result class: Correctness win with a bounded, measured, root-caused
cold-path cost (no `decode_map.go` gap found, no wasted-decode ordering bug
found; the higher-than-prior-waves percentage reflects this family's richer
per-kind field count and nested-slice shape against a fast absolute
baseline, not a missed fast path or an avoidable extra decode).

No-Observability-Change (Wave 4e, documentation family): the typed-decode
migration adds no route, graph query shape, queue table, worker, lease,
runtime knob, metric instrument, or metric label. A malformed
`documentation_document`/`documentation_entity_mention` fact surfaces through
the EXISTING dead-letter path — `partitionDecodeFailures` ->
`recordQuarantinedFacts` -> `eshu_dp_reducer_input_invalid_facts_total`
(labeled `domain` = `documentation_materialization`, an existing label value,
`fact_kind` gaining the two documentation kinds) plus the existing structured
"reducer input_invalid fact quarantined" error log and
`Result.SubSignals["input_invalid_facts"]` — the same instruments and log key
every prior family wired. `DocumentationEdgeMaterializationHandler` gained an
`Instruments *telemetry.Instruments` field (wired from
`DefaultHandlers.Instruments` in `defaults_domain_catalog.go`, mirroring
every other migrated handler's field), a new but purely additive wiring
change. The `payload-usage-manifest` gate (`go/internal/payloadusage`)
required an additive-only extension: two new `factKindSchemaFile` entries
(`FactKindDocumentationDocument`, `FactKindDocumentationEntityMention`) and a
new `DocumentationStructDir` (`sdk/go/factschema/documentation/v1`) parsed
alongside the existing struct dirs in `Load`; this only widens which decode
seams the gate covers and adds no new gate mechanism.

No-Regression Evidence (Wave 4f S1, code family typed-payload decode, Contract
System v1 #4566/#4749): the code-graph-core reducer read sites
(`code_call_materialization_extract.go`, `code_call_materialization_intents.go`,
`code_import_repo_edge.go`, `code_import_repo_edge_retract.go`) now decode the
`file`/`repository` fact OUTER envelope through the `sdk/go/factschema` seam
(`decodeCodegraphFile`/`decodeCodegraphRepository` in
`factschema_decode_codegraph.go`) instead of raw `payloadStr(env.Payload, ...)`
lookups. `parsed_file_data` stays an opaque validated `map[string]any`
passthrough (required-present + must-be-map; inner-AST typing deferred to
#4750). `extractCodeCallRowsWithIndex` now returns a `[]quarantinedFact`: a
`file` fact missing `repo_id` or `relative_path` is quarantined per-fact via
`partitionDecodeFailures` rather than silently producing an empty-string graph
identity, while every valid `file` fact in the same batch still projects.
`TestExtractCodeCallRowsQuarantinesFileMissingRepoID` /
`...MissingRelativePath` failed before the conversion (no quarantinedFact was
ever recorded — the missing field decoded to `""` and the fact was silently
skipped by the `repositoryID == ""` guard), then passed after. Measured with the
existing hot-path benchmark (in-memory extractor; the 500-source large-JS
dynamic-call corpus the benchmark builds; darwin/arm64, `-count=5`): `go test
./internal/reducer -run '^$' -bench
'BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls' -benchmem`. BEFORE
(raw `payloadStr`, `origin/main`) -> AFTER (typed decode):
8.79ms/1653571 B/30209 allocs -> 8.77ms/1655558 B/30208 allocs (~0% time, ~0%
allocs — the per-file outer-envelope decode is amortized against the per-call
resolution work that dominates this path). The typed path buys the accuracy
guarantee at no measured handler cost. Result class: correctness win with no
measured regression. The `file`/`repository` fact kinds are NOT registered in
the fact-kind registry this wave (registry + schema-version admission deferred
to #4752).

SCHEMA-VERSION NORMALIZATION (corpus-gate P0, PR #4753): the git collector
emits `file`/`repository` with NO `SchemaVersion`, but the Postgres persist
layer stamps a version-less fact as the sentinel `"0.0.0"`
(`go/internal/storage/postgres/facts.go`, `facts_streaming.go`:
`emptyToDefault(SchemaVersion, "0.0.0")`). A fact LOADED for reduction
therefore carries `SchemaVersion="0.0.0"`, not `""`. `decodeLatestMajor`
accepts only `major=="1"` and `major("0.0.0")=="0"`, so before the fix EVERY
real `file`/`repository` fact dead-lettered as an unsupported major and the
whole code graph collapsed (12 golden-corpus rc gates to zero: rc-2/8/10/11/12/
13/15/23). `factschemaEnvelope` now normalizes BOTH version-less spellings —
`""` and the persisted `"0.0.0"` sentinel — to the latest major, so a real
loaded code-graph fact decodes. `"0.0.0"` is never a real emitted schema
version (it is exclusively the persist-layer's empty marker), so this is safe
for every other family, all of which stamp a concrete `"1.0.0"`. The fix does
NOT weaken accuracy: a genuine unsupported major (`"2.0.0"`) still dead-letters,
and a fact missing `repo_id`/`relative_path` still dead-letters `input_invalid`.
`TestDecodeCodegraphAcceptsPersistedVersionlessSchemaVersion` and
`TestExtractCodeCallRowsProducesRowsForPersistedVersionlessFacts` reproduce the
corpus P0 at unit scale (a `file` fact with `SchemaVersion="0.0.0"` must decode
and produce >=1 code-call row);
`TestDecodeCodegraphTreatsAbsentSchemaVersionAsLatestMajor` covers the empty
spelling. The unit tests that used ABSENT (`""`) version passed while the corpus
broke because they never exercised the persisted `"0.0.0"` a real loaded fact
carries — the coverage gap this regression pair closes.

No-Observability-Change (Wave 4f S1, code family): the typed-decode migration
adds no route, graph query shape, queue table, worker, lease, runtime knob,
metric instrument, or metric label. A malformed `file` fact surfaces through the
EXISTING dead-letter path — `partitionDecodeFailures` -> `recordQuarantinedFacts`
-> `eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`code_call_materialization`, an existing label value, `fact_kind` gaining the
`file` kind) plus the existing structured "reducer input_invalid fact
quarantined" error log and `Result.SubSignals["input_invalid_facts"]` — the same
instruments and log key every prior family wired.
`CodeCallMaterializationHandler` gained an `Instruments *telemetry.Instruments`
field (wired from `DefaultHandlers.Instruments` in `defaults_domain_catalog.go`,
mirroring every other migrated handler). The code-import repo-edge builders
surface a malformed `file` fact through the existing per-run
`codeImportEdgeCounts` telemetry (new `skippedMalformedFile` tally) rather than a
new instrument. The `payload-usage-manifest` gate required an additive-only
extension: two new `factKindSchemaFile` entries (`FactKindCodegraphFile`,
`FactKindCodegraphRepository`) and a new `CodegraphStructDir`
(`sdk/go/factschema/codegraph/v1`) parsed alongside the existing struct dirs in
`Load` — this widens gate coverage (manifest count 88 -> 90) with no new gate
mechanism and no dependency on the deferred registry registration.

No-Regression Evidence (Wave 4f S2, git dataflow family typed-payload decode,
Contract System v1, issue #4754): types the six git-collector value-flow fact
kinds — `code_dataflow_scanned`, `code_dataflow_function`,
`code_function_summary`, `code_function_source`, `code_taint_evidence`,
`code_interproc_evidence` — into `sdk/go/factschema/codedataflow/v1`. The
postgres loaders (`code_taint_evidence_loader.go`,
`code_interproc_evidence_loader.go`, `code_function_summary_loader.go`,
`code_function_source_loader.go`) now return the RAW fact envelopes and the
reducer handlers decode them through the typed contracts seam via the
`*WithQuarantine` extractors (`ExtractCodeTaintEvidenceRowsWithQuarantine`,
`ExtractCodeInterprocEvidenceRowsWithQuarantine`,
`ExtractCodeFunctionSummaryEffectsWithQuarantine`,
`ExtractCodeFunctionGraphIDsWithQuarantine`,
`ExtractCodeFunctionSourcesWithQuarantine`), matching the Wave 4f S1
(`code_call_materialization_extract.go`) and Wave 4e documentation
(`ExtractDocumentationEdgeRowsWithQuarantine`) precedent: the storage adapter
owns the SQL fetch, the reducer owns the typed decode AND the input_invalid
dead-letter. Also converts `shell_exec_materialization.go`'s and
`sql_relationship_delta_scope.go`'s `file`/`repository` identity reads to
Wave 4f S1's `decodeCodegraphFile`/`decodeCodegraphRepository`.

Dead-letter Evidence (epic #4566 §1, the migration's core deliverable): a fact
missing a required identity field — `function_uid` (taint),
`source_function_uid`/`sink_function_uid` (interproc), `function_id` (summary),
`function_id`/`kind` (source) — is routed through `partitionDecodeFailures` to
a visible `quarantinedFact` the handler records via `recordQuarantinedFacts`,
feeding `eshu_dp_reducer_input_invalid_facts_total` (labeled `domain` =
`code_taint_evidence`/`code_interproc_evidence`/`code_function_summary`,
existing label values), the structured "reducer input_invalid fact
quarantined" error log, and `Result.SubSignals["input_invalid_facts"]`. This
REPLACES the initial (rejected-in-review) swallow-to-zero-value behavior; the
production-path proof is `TestCodeTaintEvidenceHandlerQuarantinesMalformedFact`,
`TestCodeInterprocEvidenceHandlerQuarantinesMalformedFact`,
`TestCodeFunctionSummaryHandlerQuarantinesMalformedFact`, and
`TestCodeFunctionSummaryHandlerQuarantinesMalformedSourceFact` — each feeds a
malformed fact through the ACTUAL loader -> handler path and asserts
`SubSignals["input_invalid_facts"] == 1` while a valid sibling still projects.
A P0 was ruled out: `ExtractCodeTaintEvidenceRows`/`ExtractCodeInterprocEvidenceRows`
already `continue` on an empty uid, so a decode-failed fact never wrote an
empty-key graph node even before this dead-letter wiring landed. The interproc
handler reads a NEW `CodeInterprocEvidenceFactLoader` (envelopes) distinct from
the fixpoint projector's `CodeInterprocEvidenceLoader` (typed inputs from an
in-memory solve, no raw decode) so the projector path is untouched. The
graph-id view reads the SAME `code_function_summary` facts the summary-effects
view already quarantines, so its quarantines are discarded to avoid
double-counting one malformed fact.

Benchmark Evidence: `go test ./internal/reducer -run xxx -bench
'BenchmarkDecodeCodeTaintEvidenceInput|BenchmarkDecodeCodeInterprocEvidenceInput|BenchmarkExtractCodeTaintEvidenceRows|BenchmarkExtractCodeInterprocEvidenceRows|BenchmarkExtractShellExecRows'
-benchtime=3x -count=3`. The typed-decode path costs ~12 allocs/fact
(`TaintEvidence`, 11 optional pointer fields) and ~59 allocs/fact
(`InterprocEvidence`, 8 optional pointer fields plus a `WhyTrail`
`[]map[string]any`), versus 0 allocs/fact for the removed ad hoc
`payloadString`/`payloadInt`/`payloadFloat` reads — measured against the
already-merged Wave 4f S1 baseline (`decodeCodegraphFile`/
`decodeCodegraphRepository`, ~1 alloc per populated optional field) on a
same-shaped synthetic payload, this migration's cost scales identically with
optional-field count, not a regression this migration introduces. Result
class: correctness win (typed, schema-validated decode; a malformed identity
field now dead-letters as input_invalid instead of silently joining under a
blank identity) at the same per-field allocation cost every prior Contract
System v1 wave already accepted.

No-Regression Evidence (review-fix pass, same PR): four accuracy gaps found in
review and closed before merge, each with a regression test:
(1) `buildSQLRelationshipDeltaScope` reused Wave 4f S1's
`codeCallDeltaRelativePathsFromRepository`, which returns each
`delta_relative_paths` JSON element RAW (no trim/drop-empty), unlike the
removed `semanticPayloadStringSlice`; a whitespace-only entry could qualify
into a bogus `"<repoPath>/  "` path via `path.Clean` — fixed with an explicit
`TrimSpace`+skip-empty step
(`TestBuildSQLRelationshipDeltaScopeSkipsWhitespaceOnlyRelativePath`).
(2) the function-summary/source decode accepted a present-but-blank
`function_id`/`kind` as valid identity — fixed with an explicit post-decode
`TrimSpace`-and-skip (a blank identity is a silent drop, distinct from a
missing-field dead-letter), mirroring Wave 4f S1's `buildCodeCallProjectionContexts`
(`TestCodeFunctionSummaryEffectsBlankFunctionIDReturnsNotOKNoError`,
`TestCodeFunctionSourceBlankFieldsReturnNotOKNoError`).
(3) the taint/interproc decode did not trim string fields, while the removed
`payloadString` helper trimmed universally; a padded `function_uid` would have
flowed untrimmed into the graph node key — fixed by trimming every field
(`TestDecodeCodeTaintEvidenceInputTrimsWhitespace`,
`TestDecodeCodeInterprocEvidenceInputTrimsWhitespace`).
(4) [Codex] `param_to_call_arg[].callee` was stored verbatim, but the old
loader TrimSpace'd it via `payloadString`; a padded callee would point the
durable summary at a FunctionID the value-flow fixpoint cannot match its
summary/graph-id maps against — fixed by trimming the callee before it becomes
a `summary.FunctionID` (`TestCodeFunctionSummaryEffectsTrimsCallee`).

No-Observability-Change (Wave 4f S2, git dataflow family): the typed-decode
migration adds no route, graph query shape, queue table, worker, lease,
runtime knob, or NEW metric instrument. The three consuming domains reuse the
EXISTING `eshu_dp_reducer_input_invalid_facts_total` counter for the new
per-fact dead-letter (Dead-letter Evidence above), plus the generic
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`
every reducer domain gets from `service.go` — the same instruments and log key
every prior Contract System v1 family wired. The three
`code_*_typed_decode.go` / `factschema_decode_codedataflow.go` seam files are
covered by rows in `docs/public/observability/telemetry-coverage.md`. The
`payload-usage-manifest` gate required an additive-only extension: six new
`factKindSchemaFile` entries and a new `CodedataflowStructDir`
(`sdk/go/factschema/codedataflow/v1`) parsed alongside the existing struct
dirs in `Load` — widens gate coverage (manifest count 90 -> 96) with no new
gate mechanism.

No-Regression Evidence (#4750 S1, parsed_file_data inner-key typing): the
code-graph-core reducer now reads two closed-shape parsed_file_data inner keys
through typed factschema accessors instead of raw map lookups —
`dead_code_file_root_kinds` (`resolveFileRootCodeCallCallerID`) and
`gomod_state.module_path` (`goModuleDeclaredPath`), wrapped in
`parsed_file_data_typed.go`. `File.ParsedFileData` stays an OPEN
`map[string]any` (the aws_resource.Attributes open-object precedent), so the
`file.v1.schema.json` wire schema is unchanged (no major bump) and the graph
rows for valid facts stay byte-identical. Byte-identity is proven three ways:
(a) accessor/raw-read equivalence tests
(`go test ./internal/reducer -run 'TestParsedFileData|TestResolveFileRootCallerIDTypedByteIdentity' -count=1`);
(b) hot-path ns/op no-regression on the same JavaScript code-call benchmark the
prior typed-decode waves used —
`go test ./internal/reducer -run '^$' -bench 'BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls' -benchmem -count=5`
went 8.82ms/1.66MB/30,212allocs (BEFORE) -> 8.82ms/1.66MB/30,211allocs (AFTER),
0% delta (darwin/arm64, Apple M1 Max); (c) the B-7/B-12 golden-corpus gate
byte-identical. The two migrated read sites are cold relative to the SCIP and
generic per-edge inner loops, and the SCIP consumer
(`extractSCIPCodeCallRows`) was left reading raw on purpose: its output rows
copy raw edge values verbatim with present/absent semantics
(`copyOptionalCodeCallField`) an `omitempty` typed struct cannot reproduce
byte-identically, so per the byte-identity-non-negotiable guardrail it keeps its
raw read while the typed `codegraphv1.SCIPFunctionCall` struct is delivered as
the authoritative contract shape (round-trip-proven in
`go/internal/parser/scip_parsed_file_data_contract_test.go`). Result class:
Correctness/contract win (typed inner-key seam) with no measured handler
regression.

No-Observability-Change (#4750 S1): the inner-key typing adds no route, graph
query shape, queue table, worker, lease, runtime knob, metric instrument, metric
label, or log key. The migrated reads keep the reducer's pre-typing tolerant
semantics (a malformed or absent inner sub-object reads as empty, exactly as the
raw map read did) — the graph-truth accuracy anchor for a "file" fact remains
its OUTER envelope, already dead-lettered by `partitionCodegraphFileFacts` (Wave
4f S1). Operators diagnose the path through the same code-call materialization
completion logs, reducer run spans, and execution counters as before.

## Anti-patterns

- Do not add `if backend == nornicdb` (or equivalent) logic inside domain
  handlers. Backend differences belong in `storage/cypher` narrow seams.
- Do not skip `GraphProjectionPhaseRepairQueue.Enqueue` on publish failure.
  Swallowing the error hides missing readiness rows.
- Do not build a new domain that writes edges before confirming the
  appropriate readiness phase is published. Edge writes without a readiness
  gate produce silent partial graph truth.
- Do not use `ResultStatusFailed` for superseded intents. Use
  `ResultStatusSuperseded`; it avoids incrementing the retry counter.
- Do not change the default fields of `SharedProjectionIntentInput` used by
  `stableIntentID` without auditing all in-flight intents in the Postgres
  shared-intent table. Use `IdentityKey` only when the stored `PartitionKey`
  must group multiple durable rows and tests prove those rows keep distinct
  `intent_id`s.

## What NOT to change without an ADR

- The `deployment_mapping` Phase 3 reopen requirement.
- The domain `OwnershipShape` invariants (cross-source, cross-scope,
  durable canonical write or bounded counter emission).
- The heartbeat / lease / retry contract in `service.go`.
- The `BuildSharedProjectionIntent` SHA256 identity function.
- The `GraphProjectionPhaseRepairQueue` contract (removing it breaks
  the non-atomic write/publish recovery path).
- The ordering of phases in `sharedProjectionReadinessPhase`
  (`shared_projection.go:91–99`).

## #4771 — docker-compose runtime signal repair (evidence)

`extractArtifactSignals` (`candidate_loader.go`) now detects the docker-compose
workload signal off `artifact_type == "docker_compose"` (the classification
`templated_detection.go` already emits) instead of the never-produced
`docker_compose_services` key, and drops three other never-produced key reads
(`github_actions_workflow_triggers`, `github_actions_reusable_workflow_refs`,
`jenkins_pipeline_calls`) whose signals already fire via `artifact_type`, the
`Jenkinsfile` path, and groovy `pipeline_calls`.

No-Regression Evidence: this runs once per file during workload-candidate
loading, off the code-call hot path. It swaps one slice-length check for one
string compare and removes three slice-length checks, so per-file work is
unchanged or slightly lower. No hot-path Cypher, graph write, lease, or queue
behavior is touched; the git-collector E2E candidate-loading baseline is
unaffected (no measurable per-file cost change on darwin/arm64).

No-Observability-Change: no metric, span, log, or status field is added,
removed, or renamed. The `eshu_dp_reducer_*` candidate-loading surface and the
workload-signal confidence values are identical; docker-compose files now carry
the `docker_compose_runtime` provenance they always should have, off the same
`SignalDockerComposeRuntime` confidence. Only which `parsed_file_data` key is
read changed, not the emitted signal shape or any telemetry surface.

## #4885 — search-vector build sweep hot-loop fix (evidence)

Two coupled defects made the reducer's search-vector build sweep hot-loop
forever (~every 2.7s), pinning ~2 Postgres cores 24/7 with zero useful output
on a drained full-corpus stack.

Defect B (root cause): the pending lister derived the document id via
`fact.payload->'document'->>'id'`, but `searchdocs.Document` has no JSON tags,
so its `ID` field marshals as the capitalized key `"ID"` and the lowercase
nested read returned NULL. The terminal-metadata `NOT EXISTS` join then never
matched, so every active repository scope was returned as pending on every
sweep regardless of whether its vectors were already built. Fixed by reading
the top-level `payload->>'document_id'` key the writer already emits
(`eshuSearchDocumentPayload`, `eshu_search_document_writer.go:427`).

Defect A (defense-in-depth): `SearchVectorBuildRunner.Run` only backed off when
`PendingScopes==0`; a sweep with pending scopes but no durable output re-looped
immediately. Now a no-progress sweep (`DocumentCount==0 && VectorCount==0 &&
DisabledCount==0`) applies the poll-interval backoff.

Performance Evidence: backend Postgres 18-alpine (local Compose, `:15432`),
Eshu at origin/main 508f8e964. The behavior regression
`TestEshuSearchVectorPendingBoundedPlanLive` (`go test
./internal/storage/postgres
-run TestEshuSearchVectorPendingBoundedPlanLive -count=1` with
`ESHU_SEARCH_VECTOR_PENDING_PLAN_LIVE=1` + `ESHU_POSTGRES_DSN`) FAILS on the
old nested-key SQL ("scopeB (all-ready) returned; expected excluded" — the
perpetual-pending bug) and PASSES on the fixed top-level-key SQL. Input shape:
scope A (mixed pending/ready docs, cases 1-7), scope B (all `ready`+`disabled`,
must be excluded), scope C (duplicate document_id dedup); the payload is seeded
in the production shape (`{"document_id":…,"document":{"ID":…},…}`).
`EXPLAIN (ANALYZE, BUFFERS)` on the fixed query shows a bounded Nested-Loop
Anti-Join over `eshu_search_vector_metadata` (Execution Time 0.128 ms, no
top-level Unique/Sort over the metadata table). Before: the sweep never drained
and hot-looped (~2 Postgres cores, 24/7); after: an all-built scope leaves the
pending set so the sweep reaches zero pending and rests on the 30s poll.
No-Regression Evidence: `TestSearchVectorBuildRunnerBacksOffOnNoProgressSweep`
(`go test ./internal/reducer -run SearchVector -count=1`) FAILS before the
backoff (0 Wait calls, hot-loops the full 2s ctx) and PASSES after (exactly 1
Wait call, exactly 1 build before backoff). The shape guard
`TestEshuSearchVectorPendingStoreListsScopes` locks the SQL to
`payload->>'document_id'` and rejects the NULL-yielding nested key in CI.

Observability Evidence: no new metric instrument. The stall is diagnosable via
the existing `eshu_dp_search_vector_build_phase_seconds` histogram, the existing
"search vector build sweep completed" completion log (non-zero `pending_scopes`
with zero `document_count`/`vector_count` is the telltale), and a new WARN
structured log "search vector build sweep made no progress; backing off"
(`stall_reason=no_durable_output`). The sweep logging/metric emitters moved to
`search_vector_build_runner_log.go` for the 500-line cap; that stage file is
covered in `docs/public/observability/telemetry-coverage.md`.

Live-corpus confirmation (drained `e2e3586persist` full-corpus stack, 831 active
search-document scopes over 2,595,922 facts): the OLD `payload->'document'->>'id'`
key produced 0 non-NULL document_ids out of 2,595,922 (the nested `id` is always
NULL because `searchdocs.Document.ID` marshals as `"ID"`), so the pending query
selected 831/831 scopes — 100% of active scopes, permanently, which is the
never-draining set that pinned ~2 Postgres cores 24/7. The NEW
`payload->>'document_id'` key produced 2,595,922 non-NULL document_ids and the
pending query selected 556 scopes: the 275 fully-built scopes correctly drop out
and the remaining 556 are genuinely unbuilt work that leaves the set as it
builds, so the sweep reaches `pending=0` and rests on the 30s poll.

Remote runtime proof (`eshu-remote-validation`, live `e2e3586persist` stack):
a musl-built fixed `eshu-reducer` was swapped into the running resolution-engine
and compared against the shipped (buggy) binary. BEFORE: 9 sweeps/30s, each
`document_count=0`/`vector_count=0` — the sweep looped on the already-built
low-`scope_id` batch (`ORDER BY scope_id LIMIT 100`) the broken lister kept
returning, so it never reached the genuine unbuilt backlog and built ZERO
vectors while Postgres sat at 154-280%. AFTER: sweeps build ~30k vectors each
(`document_count`/`vector_count` = 31276, 29370, 31032), no no-progress
backoffs (real work exists), Postgres doing productive write work draining the
backlog toward idle. So the fix is not only a spin fix — it restores
search-vector building for the unbuilt scope backlog that the NULL join key had
silently starved.

## #4893 — TAINT_FLOWS_TO edge + CodeTaintEvidence node retract anchored by uid via ledgers (evidence)

The reducer's value-flow retracts scanned the whole graph on NornicDB. The
TAINT_FLOWS_TO **edge** retract used an unanchored `(:Function)-[rel]->(:Function)
WHERE rel.<prop>` shape that NornicDB plans as `traverseGraphParallel`/`findPaths`
over the whole Function adjacency; the CodeTaintEvidence **node** retract used
`MATCH (n:CodeTaintEvidence) WHERE n.scope_id ... WITH n LIMIT k DETACH DELETE n`,
which the NornicDB delete-with-limit hot path (`collectDeleteWithLimitCandidates`)
serves via `GetNodesByLabel` — decoding the whole node population when no
`scope_id` property index exists. Both are fired per-scope by
`CodeValueFlowStaleCleanupRunner` (~896 scopes) and per-intent by the
materialization/fixpoint paths.

Fix: durable `code_interproc_projected_edge` and `code_taint_evidence_projected_node`
ledgers record the source-Function uid / node uid of every projected artifact,
written BEFORE the graph write so each ledger is a superset of the graph. Retract
enumerates uids from the ledger and anchors the delete by the indexed uid
(`UNWIND $uids MATCH (s:Function {uid})-[rel:TAINT_FLOWS_TO]->() WHERE <pred> DELETE rel`
and `UNWIND $uids MATCH (n:CodeTaintEvidence {uid}) WHERE <pred> DETACH DELETE n`),
then prunes the ledger. An empty ledger enumeration is the existence guard: the
reducer sends no graph delete when a scope has no projected artifacts, so a
zero-taint corpus does zero graph work (this also side-steps the
`collectDeleteWithLimitCandidates` fail-open, which broad-scans on a 0-result
`WITH n LIMIT` delete — the reason a naive `scope_id` property index would not
have fixed the zero-taint case; NornicDB hot-path cookbook §8.5 recommends
indexed key-list deletes). A one-time, count-guarded startup backfill seeds each
ledger from existing graph artifacts so pre-deploy edges/nodes remain retractable.

Performance Evidence: backend NornicDB v1.1.10 `d97f02c1` (~980k nodes / 1.6M
edges; 511,825 Function nodes; 0 TAINT_FLOWS_TO edges; 0 CodeTaintEvidence nodes
on the live `e2e3586persist` stack). BEFORE: the stale-cleanup log showed one
100-scope cycle at `duration_seconds=13055.6` (3.6 h, `cursor_exhausted=false`),
pinning NornicDB CPU 150–509% continuously; a read-shaped reproduction of the
edge retract (`MATCH (:Function)-[rel:TAINT_FLOWS_TO]->(:Function) WHERE
rel.evidence_source=… RETURN count`) measured 18.57 s to return 0. AFTER
(rebuilt reducer hot-swapped on the same stack): stale-cleanup cycles ran at
`duration_seconds=0.03–0.09` and all 896 scopes drained (`cursor_exhausted=true`)
in ~0.5 s; NornicDB CPU fell to 0.55–3.17% idle; a live goroutine dump showed
zero `traverseGraphParallel`/`findPaths` and zero `collectDeleteWithLimitCandidates`
frames. Anchored retract-read timing: 0.03 s for 100 uids, 1.6 s for 2000 uids
vs 18.57 s unanchored. Result class: Wall-clock win (continuous 150–509% pin →
idle).

No-Regression Evidence: result-set equivalence proven on the live backend with a
uniquely-scoped seeded edge set — the old whole-scan delete set and the
ledger-anchored delete set were identical for both the scoped retract (all four
seeded edges) and the stale retract (the two prior-generation edges); seed
removed after. Focused gates: `go test ./internal/storage/postgres
./internal/storage/cypher ./internal/reducer ./cmd/reducer -count=1` and
`golangci-lint run` on those packages pass. Store/writer/handler/runner/backfill
tests cover record-before-write ordering, ledger-enumerated anchored delete,
prune, first-generation skip, empty-ledger existence-guard no-op, and count-guard
backfill.

Observability Evidence: the win is diagnosable through the EXISTING reducer
`code value-flow stale cleanup cycle completed` log (`scopes_scanned`,
`taint_sweeps`, `interproc_sweeps`, `cursor_exhausted`, `duration_seconds` — the
13055.6 → 0.05 drop is directly visible there), the existing reducer run
spans/execution counters, and the `InstrumentedDB` Postgres query
spans/`eshu_dp_postgres_query_duration_seconds` covering the new ledger reads and
writes. The change adds no new metric instrument, metric label, worker, queue
domain, lease, runtime knob, or graph-write route; the two new ledger tables are
reached through the existing Postgres store instrumentation.

No-Regression Evidence (typed-payload decode, issue #4632, completing the
AWS/IAM family Contract System v1 migration #4568 left out of scope): the two
remaining AWS extractors that still read their OWN fact kind's payload via raw
`payloadString` map lookups — `rdsPostureRow` (`rds_posture_rows.go`) for
`rds_instance_posture` and `ExtractS3ExternalPrincipalGrantRows`
(`s3_external_principal_grant_rows.go`) for `s3_external_principal_grant` — now
decode through the `sdk/go/factschema` seam (`decodeRDSInstancePosture`,
`decodeS3ExternalPrincipalGrant` in `factschema_decode.go`), which wrap the
already-committed `factschema.DecodeRDSInstancePosture`/
`DecodeS3ExternalPrincipalGrant` functions and `awsv1.RDSInstancePosture`/
`awsv1.S3ExternalPrincipalGrant` structs (both landed on `main` ahead of this
reducer-side wiring). `ExtractRDSPostureRows` and
`ExtractS3ExternalPrincipalGrantRows` now return `[]quarantinedFact` populated
from the posture/grant decode failures (previously only the joined
`aws_resource` side populated it): a fact missing a required identity field
(`account_id`/`region`, always stamped by the emitting collector) is quarantined
per-fact via `partitionDecodeFailures` rather than fabricating a `CloudResource`
uid or `GRANTS_ACCESS_TO` edge from an empty account/region, while every valid
fact in the same batch still projects. `go test ./internal/reducer -run
'TestExtractRDSPostureRowsQuarantinesMissingRequiredField|TestExtractS3ExternalPrincipalGrantRowsQuarantinesMissingRequiredField'
-count=1 -v` failed before the conversion (quarantine count 0, the malformed
fact's empty account_id silently reached the uid/edge derivation instead), then
passed after. Row shape and every existing `TestExtractRDSPostureRows*`/
`TestExtractS3ExternalPrincipalGrantRows*` assertion (numeric widening,
security-parameter/parameter-group ordering, principal skip tallies, dedup/sort
order) are unchanged — the typed decode preserves the pre-typing raw-payload
derivation byte-for-byte. Result class: Correctness win (per-fact isolation
closes the last two AWS raw-payload reducer sites) with no measured behavior
change for valid facts.

No-Observability-Change (issue #4632): the typed-decode migration adds no
route, graph query shape, queue table, worker, lease, runtime knob, metric
instrument, or metric label. A malformed `rds_instance_posture`/
`s3_external_principal_grant` fact surfaces through the EXISTING dead-letter
path (`recordQuarantinedFacts`/`inputInvalidSubSignals`), incrementing the
existing `eshu_dp_reducer_input_invalid_facts_total` counter (labeled `domain` =
`rds_posture_materialization` / `s3_external_principal_grant_materialization`,
an existing label value set) and logging the existing "reducer input_invalid
fact quarantined" structured log line. The `payload-usage-manifest` gate
(`go/internal/payloadusage`) required an additive-only extension: two new
`factKindSchemaFile` entries (`FactKindRDSInstancePosture`,
`FactKindS3ExternalPrincipalGrant`) in `go/internal/payloadusage/schema.go`
(both schema files already existed under `sdk/go/factschema/schema/`); this
only widens which decode seams the gate covers (106 -> 108 manifest kinds,
`TestLoadAgainstRealReducer`/`TestGateAgainstRealReducerAndSchemas` in
`go/internal/payloadusage/load_test.go`) and adds no new gate mechanism.

### CRI-resolved digest for live-workload image correlation (#5432)

No-Regression Evidence: `go test ./internal/collector/kuberneteslive/... ./internal/reducer/... -count=1` passes with byte-identical behavior for all pre-existing correlation paths. The CRI-digest path is additive — when no resolved digest exists (Deployments, ReplicaSets, pending pods), behavior stays byte-identical to before. Five new regression tests (`kubernetes_correlation_cri_digest_test.go`) prove: (1) tag-form ref with CRI digest + matching source → exact, edge-eligible; (2) tag without CRI digest stays derived/provenance-only; (3) CRI digest without source observation → unresolved, never tag-derived; (4) CRI-digest-promoted workload produces a RUNS_IMAGE edge; (5) tag without CRI digest produces no edge. The existing correlation test suite (edge rows, handler loading, writer idempotency, determinism) stays green with byte-identical outcomes for digest-pinned, tag-only, ambiguous, stale, tombstoned, and owner-reference paths. Digest-join cardinality shim (`TestDigestJoinCardinalityShim`, coherent 6-ref fixture using the same repository for all refs and source manifests): 33% edge-eligible before (2 digest-pinned refs) → 50% edge-eligible after (2 digest-pinned + 1 CRI-digest-promoted tag ref). The B-7 golden-corpus gate unit tests pass (`test-verify-golden-corpus-gate.sh`, `go test ./cmd/golden-corpus-gate/`). Cassette updated with tag-referenced Pod + resolved digest (only on the Pod; Deployment/ReplicaSet entries carry no resolved_image_digest); B-12 snapshot gains rc-153 (RUNS_IMAGE min ≥ 3, non-vacuous). Full Docker B-7 gate run on this branch head: `scripts/verify-golden-corpus-gate.sh` PASS (433 pass, 0 required-fail, 0 advisory-warn; rc-153 RUNS_IMAGE count=3 ≥ 3, KubernetesWorkload nodes=3).
No-Observability-Change: No new metric instrument, metric label, span, structured log field, status field, queue domain, worker count, batch size, or runtime knob. The `resolved_image_digest` payload field is a new optional key on `kubernetes_live.pod_template` containers — malformed values surface through the existing `input_invalid` dead-letter path. The CRI-digest promotion to exact reuses the existing `materialized[digest]` tally and `kubernetes correlation materialization completed` log. The `eshu_dp_kubernetes_correlation_edges_total` counter and `eshu_dp_reducer_executions_total` counter diagnose the path through their existing `resolution_mode` and `domain` labels.
