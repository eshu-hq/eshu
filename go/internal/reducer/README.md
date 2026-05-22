# internal/reducer

## Purpose

`internal/reducer` owns cross-domain materialization after source-local facts
have been committed. It admits evidence into reducer truth, writes canonical
graph rows through storage ports, publishes reducer-owned facts, coordinates
shared projection, and repairs non-atomic graph-phase publication.

## Ownership boundary

This package decides graph and reducer-fact truth for cross-source domains. It
does not collect sources, parse files, commit intake facts, serve query routes,
or call graph drivers directly. Graph writes go through
`internal/storage/cypher`; durable fact, queue, phase, and repair storage is
wired by command packages through narrow interfaces.

Before changing a domain, trace the path from raw fact evidence to admitted
candidate, projected row, graph or fact write, phase publication, and API/MCP
read truth.

## Exported surface

The godoc contract in `doc.go` is the source of truth for exported types and
functions. The main surfaces are:

- `Service`, `Runtime`, `Registry`, `DomainDefinition`, and `Handler` for
  executing reducer intents.
- `Intent`, `Result`, retry helpers, generation checks, and graph phase types
  for queue and readiness coordination.
- Domain handlers and writer interfaces for workload identity, deployable-unit
  correlation, platform materialization, semantic entities, code-call intents,
  SQL relationships, inheritance, package correlation, AWS drift, SBOM
  attachment, service-catalog correlation, container-image identity, and
  supply-chain impact.
- Shared projection runners for code calls, dependencies, platform edges, and
  graph phase repair.

Use `DefaultDomainDefinitions`, `NewDefaultRegistry`, and `NewDefaultRuntime`
for the currently wired domain set instead of duplicating a domain catalog in
docs.

## Dependencies

Reducer code consumes fact envelopes and storage/query ports from
`internal/facts`, `internal/storage/postgres`, `internal/storage/cypher`, and
`internal/query`. Parser, collector, and projector packages feed the evidence
but are not owned here. Command packages provide concrete Postgres and graph
adapters.

## Telemetry

Reducer diagnostics depend on `SpanReducerRun`, `SpanCanonicalWrite`,
`SpanReducerDriftEvidenceLoad`, `SpanReducerAWSRuntimeDriftEvidenceLoad`,
queue wait/run metrics, shared projection metrics, graph phase repair metrics,
and domain-specific counters such as drift, package correlation, SBOM,
service-catalog, container-image, and supply-chain impact outcomes.

When a reducer path is slow, classify the cost as fact load, relationship
extraction, intent upsert, graph write, phase publication, shared projection,
or repair before changing worker counts or timeouts.

## Gotchas / invariants

- Generation supersession is a write barrier. Stale intents must not write
  graph rows or reducer facts.
- Graph writes and `graph_projection_phase_state` publication are not atomic;
  keep `GraphProjectionPhaseRepairer` wired.
- Shared projection only runs after the required readiness phase exists.
- Bootstrap facts-first ordering matters: domains that consume
  `resolved_relationships` need a reopen or re-trigger after deployment
  mapping is reopened.
- Do not serialize worker lanes to hide non-idempotent writes. Fix the conflict
  key, retry path, or write design.
- Keep ambiguous code, SQL, package, image, and cloud evidence as provenance
  until the domain can admit truth from explicit evidence.
- Do not call graph drivers directly from domain handlers. Canonical graph
  writes go through `internal/storage/cypher`.
- Do not remove graph projection phase repair; it is the recovery path for
  non-atomic graph-write plus phase-publication failures.
- Do not use serialization as a correctness fix for non-idempotent writes.
- Do not turn type/class references into `CALLS`; use `REFERENCES` when source
  text proves reachability without proving invocation.
- SQL trigger-to-function `EXECUTES` edges protect trigger-bound stored
  procedures from false dead-code cleanup candidates.
- Mutable container tags are not image identity unless exactly one projected
  digest observation proves the mapping.
- SBOM component evidence is not vulnerability impact by itself.
- Package source hints are provenance until reducer correlation admits
  ownership or consumption truth.
- Service catalog names and ownership labels are provenance until explicit
  repository evidence admits exact, derived, ambiguous, unresolved, stale, or
  rejected correlation facts.

## Focused tests

Run the smallest tests that cover the touched contract, then broaden as needed:

```bash
cd go
go test ./internal/reducer -run 'TestRuntimeExecute|Test.*Projection|Test.*Materialization|Test.*Correlation|Test.*Drift|Test.*Identity' -count=1
go test ./internal/reducer -count=1
go test ./cmd/reducer -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

No-Regression Evidence: `go test ./internal/reducer ./internal/storage/postgres ./cmd/reducer -run 'TestPlatformMaterializationHandlerLocksInfrastructurePlatformIDs|TestNewDefaultRegistryWiresPlatformGraphLocker|TestPlatformGraphLocker|TestPlatformGraphLockerForReducer|TestBuildReducerServiceWiresDefaultRuntimeAndQueue' -count=1` proves deployment_mapping platform writes acquire per-Platform.id locks without lowering worker concurrency and skip lock wiring when transactions are unavailable.

Observability Evidence: existing reducer queue conflict fields, fact-work retry
counters, deployment_mapping completion logs, graph-write retry WARNs, and
Postgres query errors expose blocked, retrying, failed, and completed platform
materialization work; no new metric label was needed.

## Related docs

- `docs/public/architecture.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/dead-code-reachability-spec.md`
- `docs/public/reference/relationship-mapping.md`
