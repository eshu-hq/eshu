# internal/reducer/aws

`reducer/aws` records the accepted scaffold for the AWS cloud-resource
reducer family. The package names the projector components and the readiness
checkpoints that future implementations must publish. No live projection logic
exists here yet.

## Where this fits in the pipeline

```mermaid
flowchart LR
  AWSCollector["AWS collector\n(future)"] --> FactStore["Postgres\nfact store"]
  FactStore --> ReducerQ["Reducer queue"]
  ReducerQ --> AWSReducer["reducer/aws\n(scaffold — not yet wired)"]
  AWSReducer --> PhaseState["cloud_resource_uid\ncanonical_nodes_committed"]
  PhaseState --> DownstreamEdges["Edge domains\n(deployment_mapping, dsl)"]
```

## Purpose

Pin the `RuntimeContract` (component list and readiness checkpoints) for AWS
canonical projection so ADRs, test fixtures, and future reducer wiring share
one source of truth. The contract validates before any code runs via
`RuntimeContract.Validate`.

## Ownership boundary

- Owns: the AWS scaffold contract (`RuntimeContract`, `PublishedCheckpoint`)
  and its `Validate` shape.
- Does not own: live AWS collection, materialization, or graph writes. None
  of those exist in this package today.

## Exported surface

- `PublishedCheckpoint{Keyspace, Phase}` — `contract.go:13`.
- `RuntimeContract{Components, Checkpoints}` — `contract.go:19`.
- `RuntimeContract.Validate` — `contract.go:52`.
- `DefaultRuntimeContract()` — `contract.go:41` — defensive copy of the
  accepted scaffold.
- `RuntimeContractTemplate()` — `contract.go:48` — alias for
  `DefaultRuntimeContract`; used by ADR fixtures.

The accepted scaffold:

- Components: `resource_projector`, `relationship_projector`,
  `dns_projector`, `image_projector`.
- Checkpoint: `cloud_resource_uid` at `canonical_nodes_committed`.

## Dependencies

- `go/internal/reducer` — `GraphProjectionKeyspace` and
  `GraphProjectionPhase` constants only.

## Telemetry

None. Scaffold types only; runtime telemetry will be defined when the
projector family is implemented.

## Gotchas / invariants

- This is a scaffold. It does not produce facts, does not enqueue work, and
  does not publish phase rows at runtime. Treat it as a planning artifact,
  not a deployable behavior.
- The single accepted checkpoint is a Phase 1 canonical-nodes publication
  (`canonical_nodes_committed`). Downstream domains that consume
  `resolved_relationships` populated from AWS canonical nodes still require
  the standard post-Phase-3 reopen mechanism described in CLAUDE.md
  "Facts-First Bootstrap Ordering"; that reopen lives outside this package.
- `Validate` enforces non-blank components and checkpoint fields, but does
  not verify that the listed component names correspond to any concrete
  implementation.

## Related docs

- `docs/docs/architecture.md`
- `go/internal/reducer/README.md`
- `go/internal/reducer/dsl/README.md`
- `go/internal/reducer/tfstate/README.md`
