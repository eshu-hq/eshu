# AGENTS — internal/reducer

This file guides LLM assistants working in `go/internal/reducer`. Read it
before touching any file in this directory.

## Read first

1. `CLAUDE.md` **entirely** — especially "Facts-First Bootstrap Ordering",
   "Correlation Truth Gates", "Concurrency Workflow", and "Golden Rules 1–4".
2. `docs/docs/architecture.md` — service boundaries and data flow.
3. `docs/docs/deployment/service-runtimes.md` — Resolution Engine section.
4. `docs/docs/reference/telemetry/index.md` — observability contract.
5. `go/internal/projector/README.md` — projector→reducer handoff and phase
   publication model.
6. `go/cmd/reducer/README.md` — runtime wiring context.

## Invariants (cite file:line)

- **Every domain must be cross-source, cross-scope, and canonical-write** —
  `registry.go:22–33` `OwnershipShape.Validate`; registration fails otherwise.
- **Intent lifecycle is fixed: pending → claimed → running → succeeded/failed** —
  `intent.go:51–61`; do not invent additional states.
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
  `shared_projection.go:59–66`; changing the identity fields listed breaks
  in-flight idempotency.
- **Edge domains gate on readiness phases** — `shared_projection.go:91–99`;
  `code_calls` gates on `canonical_nodes_committed`;
  `sql_relationships` and `inheritance_edges` gate on
  `semantic_nodes_committed`.
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

## Common changes

### Add a new reducer domain

1. Add a `Domain` constant to `domain.go` and `knownDomains` map.
2. Write the handler struct satisfying the `Handler` interface.
3. Add the handler to `implementedDefaultDomainDefinitions` in `defaults.go`.
4. Add a `DomainDefinition` (with `OwnershipShape` and `TruthContract`) to
   `DefaultDomainDefinitions` in `registry.go`.
5. Wire the backend adapters in `cmd/reducer/main.go` `DefaultHandlers`.
6. If the domain consumes `resolved_relationships`, add a post-Phase-3
   reopen in `bootstrap-index/main.go` after ReopenDeploymentMappingWorkItems.
7. Add telemetry: at minimum a `telemetry.SpanReducerRun` span and
   `eshu_dp_reducer_executions_total` counter.
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
- Do not change the fields of `SharedProjectionIntentInput` used by
  `stableIntentID` without auditing all in-flight intents in the Postgres
  shared-intent table.

## What NOT to change without an ADR

- The `deployment_mapping` Phase 3 reopen requirement.
- The domain `OwnershipShape` invariants (cross-source, cross-scope,
  canonical-write).
- The heartbeat / lease / retry contract in `service.go`.
- The `BuildSharedProjectionIntent` SHA256 identity function.
- The `GraphProjectionPhaseRepairQueue` contract (removing it breaks
  the non-atomic write/publish recovery path).
- The ordering of phases in `sharedProjectionReadinessPhase`
  (`shared_projection.go:91–99`).
