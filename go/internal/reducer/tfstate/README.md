# internal/reducer/tfstate

`reducer/tfstate` records the accepted scaffold for the Terraform
state-derived canonical reducer family. The package names the projector
components and the readiness checkpoints that future implementations must
publish. No live projection logic exists here yet.

## Where this fits in the pipeline

```mermaid
flowchart LR
  TFStateCollector["Terraform state\ncollector (future)"] --> FactStore["Postgres\nfact store"]
  FactStore --> ReducerQ["Reducer queue"]
  ReducerQ --> TFStateReducer["reducer/tfstate\n(scaffold — not yet wired)"]
  TFStateReducer --> PhaseRows["terraform_resource_uid\nterraform_module_uid\ncanonical_nodes_committed"]
  PhaseRows --> DownstreamEdges["DSL evaluator\nedge domains"]
```

## Purpose

Pin the `RuntimeContract` (component list and readiness checkpoints) for
Terraform state canonical projection so ADRs, test fixtures, and future
reducer wiring share one source of truth.

## Ownership boundary

- Owns: the Terraform state scaffold contract (`RuntimeContract`,
  `PublishedCheckpoint`) and its `Validate` shape.
- Does not own: live Terraform state collection, materialization, or graph
  writes. None of those exist in this package today.

## Exported surface

- `PublishedCheckpoint{Keyspace, Phase}` — `contract.go:13`.
- `RuntimeContract{Components, Checkpoints}` — `contract.go:20`.
- `RuntimeContract.Validate` — `contract.go:55`.
- `DefaultRuntimeContract()` — `contract.go:43` — defensive copy.
- `RuntimeContractTemplate()` — `contract.go:50` — alias for
  `DefaultRuntimeContract`.

The accepted scaffold:

- Components: `resource_projector`, `module_projector`, `output_projector`.
- Checkpoints:

| Keyspace | Phase |
| --- | --- |
| `terraform_resource_uid` | `canonical_nodes_committed` |
| `terraform_module_uid` | `canonical_nodes_committed` |

## Dependencies

- `go/internal/reducer` — `GraphProjectionKeyspace` and
  `GraphProjectionPhase` constants only.

## Telemetry

None. Scaffold types only; runtime telemetry will be defined when the
projector family is implemented.

## Gotchas / invariants

- This is a scaffold. It does not produce facts, does not enqueue work, and
  does not publish phase rows at runtime.
- Both accepted checkpoints are Phase 1 (`canonical_nodes_committed`)
  publications. Downstream domains that consume `resolved_relationships`
  derived from Terraform state canonical rows still require the post-Phase-3
  reopen mechanism described in CLAUDE.md "Facts-First Bootstrap Ordering".
  That reopen lives outside this package.
- `Validate` enforces non-blank components and checkpoint fields; it does
  not check that the listed component names map to any concrete
  implementation.
- `DefaultRuntimeContract` and `RuntimeContractTemplate` both use
  `slices.Clone`; do not take pointers to the internal default and mutate it.

## Related docs

- `docs/docs/architecture.md`
- `go/internal/reducer/README.md`
- `go/internal/reducer/aws/README.md`
- `go/internal/reducer/dsl/README.md`
