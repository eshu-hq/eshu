# Evidence: package_registry canonical edges deferred to a second write group (rc-9)

Scope: `package_registry_canonical_writer.go`, `package_registry_edge_writer.go`,
`canonical_node_writer.go`. Fixes a NornicDB read-your-writes defect that left
`PackageVersion` / `HAS_VERSION` / `PackageDependency` / `DECLARES_DEPENDENCY` /
`DEPENDS_ON_PACKAGE` unmaterialized on the default graph backend (#3978).

## Root cause (reproduced on NornicDB 1.1.6 via Bolt)

In a single transaction, a node MERGE'd with **multiple labels**
(`:Package:PackageRegistryPackage`) is invisible to a later same-transaction
statement's `UNWIND $rows AS row MATCH (p:Package {uid: row.field})`. Matrix:
single-label + UNWIND-param MATCH works; multi-label + literal MATCH works;
multi-label + UNWIND-param MATCH returns 0; the same MATCH works once the node
is committed in a prior transaction. The canonical writer runs all phases in one
atomic `ExecuteGroup` for the projector, so the version/dependency cypher MATCHed
the package/version/target nodes MERGE'd earlier in the same transaction and
found nothing — the writer reported `statements=4` with non-zero row counts while
no version/dependency nodes or edges persisted.

## Change

Split package_registry into node creation (Package, PackageVersion, target
Package, PackageDependency — main atomic group) and edge attachment
(`HAS_VERSION`, `DECLARES_DEPENDENCY`, `DEPENDS_ON_PACKAGE`) in a deferred second
`ExecuteGroup` that MATCHes the now-committed, per-label-indexed nodes. Label-less
MATCH was rejected (per-label indexes only → full scan); dropping the secondary
labels was rejected (they carry uid uniqueness constraints and back read
aggregates).

## No-Regression Evidence

- Baseline (before): rc-9 `DEPENDS_ON_PACKAGE` count = 0 on every run; the
  package_registry canonical edges never materialized on NornicDB.
- After: B-7 golden corpus gate green in 37s wall-clock (budget ceiling 1800s),
  3 consecutive deterministic runs, all 23 required correlations pass including
  rc-9 `(PackageDependency)-[:DEPENDS_ON_PACKAGE]->(Package) count=1`.
- Backend / version: NornicDB 1.1.6 (default), Bolt, database `nornic`.
- Input shape: 10-repo B-7 corpus + 9 cassette collectors; package_registry
  supply-chain-demo cassette carries 1 package + 1 version + 1 dependency for
  `github.com/acme/lib-common@1.0.0 -> github.com/acme/synthetic-dep`.
- Cost: the deferred edge group adds exactly one extra round-trip per
  package_registry scope projection, bounded by the version+dependency row count
  (small per scope). Git and all other canonical writes are unchanged — the
  second group runs only when package_registry edge statements exist; the main
  atomic group still carries every node phase.
- Terminal queue: projector + reducer queues drain to terminal; the
  package_registry projector work item ends `succeeded` with the edges present.
- Convergence: the only non-converging window is a group-1 success followed by a
  terminal (non-retryable) group-2 failure, which is implausible because both
  groups share the same static cypher and the same driver/error classification
  path — a terminal classification on group 2 would have already failed group 1
  identically; transient group-2 failures converge via idempotent retry of both
  MERGE-only groups (the work item requeues and reruns from the node group).

## No-Observability-Change

The existing `canonical atomic write completed` log line (and the projector
`canonical_write` runtime-stage telemetry with the per-row counts) already cover
both write groups; the second group is logged with its own `edge_statements`
count via the same `slog`/metric path. No new metric, span, or status field is
introduced and none is removed; existing operator signals (projector
`canonical_write` stage duration + the package_registry node/edge counts) remain
the diagnostic surface.
