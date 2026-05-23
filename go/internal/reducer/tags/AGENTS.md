# AGENTS - internal/reducer/tags

Read this before touching `go/internal/reducer/tags`.

## Read first

1. `go/internal/reducer/README.md` — full reducer context and the
   post-Phase-3 reopen requirement.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — Phase 1 canonical-nodes
   publications from this package feed downstream domains that may require
   Phase 3 reopen.

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

## Common changes

### Add a new canonical keyspace to the contract

1. Append to `defaultRuntimeContract.CanonicalKeyspaces` in `contract.go`.
2. Update `PhaseStates` in `normalizer.go` to produce a row for the new
   keyspace if it needs a different phase or keyspace mapping.
3. Update this README.

### Implement a concrete `Normalizer`

- The normalizer belongs in a separate package. It must satisfy
  `Normalizer` (`normalizer.go:14`) and produce a `NormalizationResult`
  with `CanonicalResourceID` values that match canonical nodes already
  written to the graph.

## Failure modes

- **Missing `cloud_resource_uid` phase rows**: downstream DSL evaluator or
  edge domains that gate on `canonical_nodes_committed` for
  `cloud_resource_uid` will block. Verify the normalizer ran and
  `PublishNormalizationResult` was called with a non-nil publisher.
- **`NormalizationResult` with blank `CanonicalResourceID`**: `Validate`
  (`normalizer.go:47`) returns an error, and `PhaseStates` will not be
  called. The error must propagate to the caller, not be swallowed.

## Anti-patterns

- Do not add cloud-tag normalization logic to this package.
- Do not publish a different phase or keyspace than
  `(cloud_resource_uid, canonical_nodes_committed)` from this package.
  If the substrate needs different readiness metadata, extend the contract
  through a new output kind or a separate `PhaseStates`-equivalent.

## What MUST NOT change without architecture-owner approval

- The hardcoded `cloud_resource_uid` / `canonical_nodes_committed`
  mapping in `PhaseStates`. Changing it alters the Phase 1 readiness
  signal consumed by DSL evaluation and edge domains.
- The deduplication behavior in `PhaseStates`.
