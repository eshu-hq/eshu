# AGENTS — internal/reducer

This file guides LLM assistants working in `go/internal/reducer`. Read it
before touching any file in this directory.

## Read first

- This package's tests parse shell files under
  `scripts/lib/ifa_family_registry/`, and its Ifá CI gate runs the whole
  package with no `-run` filter. Both are recorded under "Gotchas /
  invariants" in `README.md`; read them before renaming that shell directory or
  adding a slow test here.

1. Root `AGENTS.md` "Golden Rules", `docs/internal/agent-guide.md` "Bootstrap
   And Correlation Truth", `eshu-correlation-truth`, `concurrency-deadlock-rigor`.
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
- **Generation supersession short-circuits execution** — `runtime.go` checks `GenerationCheck` before dispatching to `Handler.Handle`; return `ResultStatusSuperseded` rather than projecting stale truth.
- **One rebind exception (#10106)** — the scope-keyed Crossplane SATISFIED_BY domain rebinds to the active generation in `generationCurrent` when `ActiveGeneration` is wired; never widen it to a domain whose intent or handler reads generation-scoped facts.
- **Heartbeat stops before Ack** — `service.go:337` calls `stopHeartbeat()`
  before `WorkSink.Ack`; do not reorder this or you risk lease extension
  after the transaction has committed.
- **`deployment_mapping` blocks on `resolved_relationships`** —
  `DomainDeploymentMapping` handler consumes relationships that do not exist
  until Phase 3 of the bootstrap pipeline. Any domain added as a consumer of
  `resolved_relationships` must have a post-Phase-3 reopen mechanism. See
  `bootstrap-index/main.go:273`. Its consumers' own gate (`crossrepo.EvaluateOwnGate`, #7597) fails a dead-lettered producer in the counting `*_producer_dead_lettered` classes and a missing one in `*_producer_missing` (never enroll them as non-counting); the producer's relationship-generation activation reopens those consumers in the same transaction.
- **Phase publications and graph writes are not atomic** — if a write
  commits but the publication fails, `gpphase.PhaseRepairQueue`
  captures the retry. Do not skip enqueueing to the repair queue when a
  publish fails.
- **Shared projection intent IDs are stable SHA256 hashes** —
  `sharedintent.Build` (`sharedintent/intent.go`, root spelling `BuildSharedProjectionIntent`); changing the default identity fields breaks
  in-flight idempotency. `IdentityKey` is a narrow override for domains that
  must store several rows under one durable `PartitionKey` without collapsing
  their `intent_id`s; audit every caller before using it.
- **Edge domains gate on readiness phases** — `worker.ReadinessPhase` (`intents/shared/worker/domains.go`);
  `code_calls`, `inheritance_edges`, `sql_relationships`, and `rationale_edges`
  gate on `canonical_nodes_committed` because their targets are canonical or
  created inline. `semantic_nodes_committed` can stall them forever (#2867-#2869).
- **Promoted edge domains keep their handler's evidence source** — a domain moved
  onto the shared-projection runner must keep its original `evidence_source`, not
  the runner's global source: inheritance, SQL, and rationale keep
  `reducer/inheritance`, `reducer/sql-relationship`, and `reducer/rationale`.
- **SQL trigger functions materialize as `EXECUTES` edges** —
  `ExtractSQLRelationshipRows` reads `function_name` from `SqlTrigger`
  metadata and writes trigger-to-`SqlFunction` `EXECUTES` rows
  (`sqlrelationship/sql_relationship_materialization.go:347`). Code dead-code
  uses those rows as incoming reachability for stored routines.
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
  migrate). `DROP TABLE` targets are parsed as `operation: "drop"` metadata;
  `MIGRATES` remains one adjacency/provenance edge per migration-target pair and
  never implies the target is absent from head state.
- **All canonical graph writes go through `internal/storage/cypher`** — no
  handler may call a Neo4j or NornicDB driver directly.
- **`JavaScript` dynamic-call alias parsing is indexed once per function** —
  `BuildEntityIndex` caches static alias metadata
  (`code/call/shared/javascript_aliases.go`) and
  `javascript.ResolveDynamicCallee` reuses that cache
  (`code/call/javascript/dynamic.go`). Do not move that
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
  `securityalert/reconciliation_writer.go` must key reducer facts by provider,
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
  `WorkSink.Ack`/`Fail` is a concurrency change. Follow the
  `concurrency-deadlock-rigor` skill fully before writing code.
- Prove idempotency: a duplicate claim or partial failure must converge on
  the same graph truth, not produce duplicate or absent rows.

### Add a new graph projection phase or keyspace

1. Add the constant to `gpphase/keyspace.go` (keyspace) or `gpphase/phasekey.go` (phase) — see `gpphase/README.md`. Callers spell the single `gpphase` name; do not add root aliases (retired in #6626 batch 6 bucket 9).
2. Verify the new constant does not conflict with existing keyspace usage in
   `worker.ReadinessPhase` and `worker.ReadinessKeyspace` (`intents/shared/worker/domains.go`).
3. Update `internal/storage/postgres` schema DDL if a new readiness row
   shape is needed.
4. Update `worker.ReadinessPhase` in `intents/shared/worker/domains.go` if the
   new phase gates a shared-projection domain.

### Change shared projection runner config

- Env var parsing lives in `worker.LoadConfig`
  (`intents/shared/worker/config.go`; the root keeps the `LoadSharedProjectionConfig` spelling); constants live in `cmd/reducer/config.go`.
- Update both the runner config and the README config table in the same PR.

## Failure modes

- **Stuck `deployment_mapping`**: queue shows `deployment_mapping` items in
  `pending` or `failed` state long after bootstrap. Check whether
  ReopenDeploymentMappingWorkItems ran in the bootstrap pipeline;
  cross-reference `graph_projection_phase_state` for
  `backward_evidence_committed` rows.
- **Missing phase publication causing edge domain blocking**: shared
  projection logs "skipped intents until their prerequisite graph phase is committed"
  at high frequency. Check `graph_projection_phase_state` for
  `semantic_nodes_committed` or `canonical_nodes_committed` rows for the
  affected `AcceptanceUnitID`.
- **Repair queue growth**: `graph_projection_phase_repair` table grows
  without drain. Check `repair.Repairer` logs for
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
  BenchmarkExtractCodeCallRowsLargeJavaScriptDynamicCalls (package `code/call`) before changing
  graph or queue code.

## Anti-patterns

- Do not add `if backend == nornicdb` (or equivalent) logic inside domain
  handlers. Backend differences belong in `storage/cypher` narrow seams.
- Do not skip `gpphase.PhaseRepairQueue.Enqueue` on publish failure.
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
- The `gpphase.PhaseRepairQueue` contract (removing it breaks
  the non-atomic write/publish recovery path).
- The ordering of phases in `worker.ReadinessPhase`
  (`intents/shared/worker/domains.go`).
