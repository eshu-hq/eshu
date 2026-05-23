# internal/storage/neo4j Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `go/internal/storage/cypher/README.md` before adding adapter code.
3. Current command wiring in `cmd/ingester` and `cmd/reducer` before moving
   Neo4j Bolt session adapters here.
4. `docs/public/reference/cypher-performance.md` before changing hot-path
   Neo4j query behavior.

## Local Rules

- This package is currently reserved. It exports no runtime adapter yet.
- Backend-neutral statement building, planning, batching, retry wrappers, and
  instrumentation stay in `internal/storage/cypher`.
- Future code here may implement Neo4j-specific driver adapters for
  `cypher.Executor`, `cypher.GroupExecutor`, or `cypher.PhaseGroupExecutor`.
- Do not import this package from projector, reducer, query, or other internal
  packages. Backend selection stays in command/runtime wiring.
- Do not make Neo4j the implicit default. `ESHU_GRAPH_BACKEND=neo4j` must stay
  explicit; NornicDB remains the default graph backend.

## Change Gates

- Moving an adapter here requires focused tests, command wiring updates, and no
  change to the backend-neutral `cypher` contract unless that contract is
  reviewed separately.
- Hot-path query or transaction behavior requires before/after or
  no-regression evidence against the pinned Neo4j version.
- New telemetry must reuse `cypher.InstrumentedExecutor` patterns unless there
  is a measured reason to add a Neo4j-specific signal.

## Do Not Change Without Owner Review

- Backend selection semantics.
- `cypher` package ownership of statement builders and wrappers.
- Adapter boundaries that would make reducers or query handlers depend on a
  concrete Neo4j driver.
