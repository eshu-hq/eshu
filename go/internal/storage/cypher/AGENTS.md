# Cypher Storage Agent Rules

These rules are mandatory for changes under `go/internal/storage/cypher`.

## Read First

1. `go/internal/storage/cypher/README.md`
2. `go/internal/storage/cypher/writer.go`
3. `go/internal/storage/cypher/canonical_node_writer.go`
4. `go/internal/storage/cypher/retrying_executor.go`
5. `docs/public/reference/cypher-performance.md`
6. `docs/public/reference/nornicdb-pitfalls.md`
7. `go/internal/telemetry/contract.go`

## Invariants

- Graph writes MUST be idempotent. Use `MERGE` or `ON CONFLICT` semantics;
  never use unconditional `CREATE` for canonical identity.
- `CanonicalNodeWriter.Write` phase order is a correctness contract:
  retractions, repository cleanup, repository, directories, files, entities,
  entity retractions, containment, Terraform state, OCI registry, package
  registry, modules, then structural edges.
- The backend seam is `Executor`. This package MUST NOT call Neo4j or NornicDB
  drivers directly.
- `ExecuteGroup` retry behavior MUST stay limited to MERGE-shaped groups where
  retrying commit-time UNIQUE conflicts is idempotent.
- Source-local and canonical operations MUST stay separate:
  `OperationCanonicalUpsert` for canonical domain nodes, source-local
  operations for source-local records.
- OCI digest identity and package registry identity are source-local evidence.
  Mutable tags and source hints MUST NOT create ownership or publication truth.
- Repository cleanup MUST stay before repository MERGE and in a separate phase
  group for non-first-generation scopes.
- Entity cleanup MUST stay label-anchored and after current entity upserts.
- File and directory nodes update in place. Do not restore broad current-node
  `DETACH DELETE` cleanup.
- Metric labels MUST NOT include paths, symbols, fact IDs, raw queries, or
  backend error text.

## Change Rules

- New canonical node type: add builder and retract statements, add focused
  tests, and verify graph schema support.
- New shared projection domain: add reducer domain wiring, row mapping, edge
  writer tests, and backend proof for active backends.
- SQL relationship change: update write and retract paths together; preserve
  trigger-to-function `EXECUTES` reachability.
- New executor wrapper: implement `Executor`, optionally group interfaces, add
  tests, and wire in `cmd/` only.
- Backend batch tuning: use writer options in command wiring; do not hard-code
  backend-specific batch sizes in canonical writers.
- Hot-path Cypher change: follow the Cypher performance page, capture
  before/after or no-regression evidence, and update versioned evidence notes.

## Failure Checks

- Deadlock retry metric rising: inspect concurrent MERGE on shared nodes and
  retry classification before changing workers.
- Slow retract phase: inspect stale node volume and schema state before
  increasing timeouts.
- Graph write timeout: inspect `TimeoutHint`, canonical phase metrics, and
  statement shape before changing global budgets.
- Atomic fallback count above zero: confirm the wired executor implements
  `GroupExecutor`.
- NornicDB UNIQUE conflict not retried: verify the error shape and MERGE
  classification in `retrying_executor.go`.

## Forbidden Without Architecture-Owner Approval

- `Executor`, `GroupExecutor`, or `PhaseGroupExecutor` interface shape.
- `CanonicalNodeWriter` phase order.
- Retraction label sets.
- `RetryingExecutor` conflict classification.
- Backend brand branches in writers, reducers, projectors, or MCP/query code.
- Serializing workers or shrinking batch sizes as a concurrency fix.
