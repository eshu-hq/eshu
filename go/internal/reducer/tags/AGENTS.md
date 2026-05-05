# AGENTS — internal/reducer/tags

This file guides LLM assistants working in `go/internal/reducer/tags`. Read
it before touching any file in this directory.

## Read first

1. `go/internal/reducer/README.md` — full reducer context and the
   post-Phase-3 reopen requirement.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — Phase 1 canonical-nodes
   publications from this package feed downstream domains that may require
   Phase 3 reopen.

## Invariants (cite file:line)

- **Scaffold seam only** — `doc.go:1–9`; no normalization logic lives here.
  Do not add cloud-tag parsing or normalization code; create a separate
  concrete implementation satisfying `Normalizer`.
- **`PhaseStates` always produces `canonical_nodes_committed` for
  `cloud_resource_uid`** — `normalizer.go:86–97`; the keyspace and phase
  are hardcoded. If a normalizer substrate needs a different keyspace,
  define a separate seam or extend this contract in a new revision.
- **`PhaseStates` deduplicates by `CanonicalResourceID`** —
  `normalizer.go:79–85`; this is a correctness property for replay
  stability.
- **Post-Phase-3 reopen is not owned here** — the `canonical_nodes_committed`
  publication is Phase 1; any domain deriving `resolved_relationships` from
  AWS/cloud canonical rows needs the Phase 3 reopen from
  `bootstrap-index/main.go:273`.
- **Defensive copies from factory functions** — `contract.go:27–37`; both
  `DefaultRuntimeContract` and `RuntimeContractTemplate` use `slices.Clone`.

## Common changes

### Add a new canonical keyspace to the scaffold

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

## What NOT to change without an ADR

- The hardcoded `cloud_resource_uid` / `canonical_nodes_committed`
  mapping in `PhaseStates`. Changing it alters the Phase 1 readiness
  signal consumed by DSL evaluation and edge domains.
- The deduplication behavior in `PhaseStates`.
