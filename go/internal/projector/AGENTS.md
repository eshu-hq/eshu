# Projector Agent Rules

These rules are mandatory for changes under `go/internal/projector`.

## Read First

1. `go/internal/projector/README.md`
2. `go/internal/projector/service.go`
3. `go/internal/projector/runtime.go`
4. `go/internal/projector/canonical.go`
5. `go/internal/projector/canonical_builder.go`
6. Source-family files for the changed fact family.
7. `go/internal/telemetry/contract.go`

## Invariants

- Projection MUST be idempotent across retries, duplicate claims, and partial
  graph writes.
- Canonical graph phases MUST publish before Ack. If publication fails and a
  repair queue is wired, enqueue repair.
- `Module` and `Parameter` labels MUST stay out of the generic entity phase
  because they use different graph keys.
- File and entity paths MUST stay repo-qualified to avoid cross-repo MERGE
  collisions.
- Terraform-state, OCI registry, package-registry, AWS, and service-catalog
  evidence stay source-local here. Cross-source admission belongs in reducer
  domains.
- OCI image identity MUST stay digest-keyed. Tags are weak observations.
- Package source hints MUST NOT create repository ownership, publication, or
  consumption truth in the projector.
- AWS runtime drift admission belongs in the reducer; projector may enqueue the
  reducer intent but MUST NOT join AWS resources to Terraform state or config.
- Directory chains MUST sort parents before children.
- Reducer intents MUST keep stable sorted ordering before enqueue.
- Graph writes MUST go through `CanonicalWriter`; no direct Neo4j or NornicDB
  driver calls.
- Superseded work MUST stop without Ack or Fail once a newer same-scope
  generation exists.

## Change Rules

- New entity type: add it to `entityTypeLabelMap`, add graph schema support,
  and run projector tests.
- New projection stage: wire it in `Runtime.Project`, add stage telemetry,
  add tests, and update telemetry docs if the operator signal changes.
- Concurrency change: read `service.go`, `service_superseded.go`, shutdown
  tests, and large-generation semaphore behavior before editing.
- New reducer intent: add the reducer domain constant, build the intent in the
  owning helper, test parseability and enqueue behavior.

## Failure Checks

- Projection failures: inspect `failure_class` before retry or timeout changes.
- Slow canonical write: inspect projector stage metrics and Cypher write
  metrics before changing workers.
- Queue age growth: inspect worker pool activity and large-repo semaphore wait.
- Missing phase state: inspect publish errors and repair queue depth.
- Missing entities: inspect `entityTypeLabelMap`, schema support, and
  generation entity counts.

## Forbidden Without Architecture-Owner Approval

- `CanonicalWriter` interface shape.
- Graph projection phase publication semantics.
- Entity label names after schema support exists.
- Backend-specific branches in projector code.
- `ContentBeforeCanonical` outside local-profile degraded-backend wiring.
- New entity types without schema constraints and tests.
