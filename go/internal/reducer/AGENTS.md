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
