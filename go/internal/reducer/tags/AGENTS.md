# internal/reducer/tags Agent Rules

These rules are mandatory for changes under `go/internal/reducer/tags`.

## Read first

1. `README.md` and `doc.go`.
2. `go/internal/reducer/README.md` and `go/internal/reducer/AGENTS.md`.
3. `docs/internal/agent-guide.md` section "Bootstrap And Correlation Truth"
   before changing readiness publications.

## Mandatory Invariants

- This package owns the normalization seam only. Do not add cloud-tag parsing
  or concrete normalization logic here.
- `PhaseStates` always produces `cloud_resource_uid` at
  `canonical_nodes_committed`. A different keyspace or phase requires an
  explicit contract change.
- `PhaseStates` deduplicates by `CanonicalResourceID`; this is a replay
  correctness property.
- Post-Phase-3 reopen is not owned here. Any domain deriving
  `resolved_relationships` from cloud canonical rows needs that reopen outside
  this package.
- `DefaultRuntimeContract` and `RuntimeContractTemplate` return defensive
  copies.

## Change Rules

- New canonical keyspace: update the runtime contract, phase-state mapping,
  README, and tests together.
- Concrete normalizer: implement it outside this package. It MUST satisfy
  `Normalizer` and produce `CanonicalResourceID` values that match canonical
  nodes already written to the graph.

## Failure modes

- **Missing `cloud_resource_uid` phase rows**: downstream DSL evaluator or
  edge domains that gate on `canonical_nodes_committed` for
  `cloud_resource_uid` will block. Verify the normalizer ran and
  `PublishNormalizationResult` was called with a non-nil publisher.
- **`NormalizationResult` with blank `CanonicalResourceID`**: validation
  returns an error and `PhaseStates` must not run. The error MUST propagate to
  the caller.

## Anti-Patterns

- Do not add cloud-tag normalization logic to this package.
- Do not publish a different phase or keyspace than
  `(cloud_resource_uid, canonical_nodes_committed)` from this package.
  If the substrate needs different readiness metadata, extend the contract
  through a new output kind or a separate `PhaseStates`-equivalent.

## Forbidden Without Architecture-Owner Approval

- The hardcoded `cloud_resource_uid` / `canonical_nodes_committed`
  mapping in `PhaseStates`. Changing it alters the Phase 1 readiness
  signal consumed by DSL evaluation and edge domains.
- The deduplication behavior in `PhaseStates`.
