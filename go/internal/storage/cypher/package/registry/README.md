# Package Registry Parts

## Purpose

This package builds package-registry graph writes as statement-free parts:
Cypher templates, UNWIND row maps, phase names, and the package-identity lock
set for `Package`, `PackageVersion`, `PackageDependency`, `PackageArtifact`,
and `RegistryEvent` nodes plus their `HAS_VERSION`, `DECLARES_DEPENDENCY`,
`DEPENDS_ON_PACKAGE`, `HAS_ARTIFACT`, and `HAS_REGISTRY_EVENT` edges.

## Ownership boundary

The leaf owns templates, row shaping, phase names, lock mechanics, and part
ordering. The parent `cypher` package owns executable `Statement` wrapping
(batching via `BuildBatchedStatements`, phase/label/scope/generation/summary
metadata), the `CanonicalNodeWriter` API, and phase dispatch — including the
deferred second `ExecuteGroup` for the edge phases. The leaf never imports the
parent.

## Exported surface

- `Part` — statement-free Cypher plus rows, label, and phase
- `BuildPackagePart`, `BuildVersionPart`, `BuildDependencyPart`,
  `BuildDependencyTargetPart`, `BuildArtifactPart`, `BuildArtifactEdgePart`,
  `BuildVersionEdgePart`, `BuildDependencyEdgePart`, `BuildEventPart`,
  `BuildEventEdgePart` — one part per statement family
- `Phase*` — the ten canonical phase names (values unchanged)
- `ArtifactRows`, `EventRows` — shared node/edge row builders
- `IdentityLocks`, `NewIdentityLocks`, `LockKeys`,
  `UniqueSortedIdentityKeys`, `Lease` — the package-identity lock set

See `doc.go` for the package contract.

## Dependencies

- `internal/projector/canonical` — materialization row contracts

## Telemetry

None. The parent writer executes parts through its existing instrumentation,
including the `package_registry_identity_lock_*` span attributes and the slow
lock log.

## Gotchas / invariants

- Preserve every Cypher template byte-identically unless a change explicitly
  carries backend and performance evidence.
- `MERGE` stays anchored on `uid` for `Package`, `PackageVersion`, and
  `PackageDependency`. Do not add `Repository` matches or ownership edges;
  source hints need reducer admission first.
- The edge parts MUST run after the node phases commit (deferred second
  `ExecuteGroup`): NornicDB does not make multi-label nodes visible to a
  same-transaction UNWIND-driven MATCH.
- Artifact `hashes` is a sorted `algorithm:digest` string list, never a map;
  the escape/split pair must change together (round-trip test pins it).
- Registry events gate on `version_id`, not just event identity: an event
  with no version to anchor on is dropped, never written as an orphan node.
- Lock acquisition order is the sorted key order (deadlock-free); the lease
  must unlock even when the write fails.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
