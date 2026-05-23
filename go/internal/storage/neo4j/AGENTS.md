# AGENTS.md — storage/neo4j guidance for LLM assistants

## Read first

1. `go/internal/storage/neo4j/README.md` — current state, what is here and
   what is not, and the planned adapter boundary
2. `go/internal/storage/cypher/README.md` — the `cypher.Executor` seam and all
   backend-neutral write contracts that future adapters in this package will
   implement
3. `go/internal/storage/cypher/writer.go` — the `cypher.Executor`,
   `cypher.GroupExecutor`, and `cypher.PhaseGroupExecutor` interfaces and
   `cypher.Statement`; understand these before adding adapter code here
4. `go/cmd/ingester/wiring_neo4j_executor.go` and
   `go/cmd/reducer/neo4j_wiring.go` — the current cmd/-local adapter
   implementations that will eventually move here

## Invariants this package enforces

- **No exported symbols today** — the package has only `doc.go`. Do not add
  new exports here without a plan to move existing `cmd/` adapters here at the
  same time; partial migration creates two adapter sites.
- **Adapter-only boundary** — this package will implement the `cypher.Executor`
  interface and optionally `cypher.GroupExecutor` / `cypher.PhaseGroupExecutor`.
  It must not contain statement builders, write plans, or canonical writer logic
  — those belong in `internal/storage/cypher`.
- **No imports from `internal/` callers** — `internal/projector`,
  `internal/reducer`, `internal/query`, and other `internal/` packages must
  never import this package. Only `cmd/` wiring imports Neo4j driver adapters.
- **Backend-neutral contract preserved** — any adapter here must implement the
  same `cypher.Executor` interface that NornicDB adapters implement. Do not add
  Neo4j-specific method shapes to the interface.

## Common changes and how to scope them

- **Move `cmd/ingester/wiring_neo4j_executor.go` here** → create an exported
  struct implementing `cypher.Executor` and `cypher.GroupExecutor`; move the
  Bolt session logic; update `cmd/ingester/wiring.go` to construct the adapter
  from this package; add unit tests for the Execute and ExecuteGroup methods.

- **Add a PhaseGroupExecutor adapter** → implement the
  `cypher.PhaseGroupExecutor` interface on the adapter; wire in `cmd/` to
  enable phase-grouped writes; verify against Neo4j deadlock behavior documented
  in the ADR.

- **Add connection pool configuration** → expose as constructor options on the
  adapter struct; document as env-var-backed knobs per the
  ESHU_GRAPH_BACKEND=neo4j configuration surface.

## Failure modes and how to debug

Since the package currently has no code, failures today are in `cmd/` wiring:

- Symptom: Neo4j connection failure at startup → check ESHU_NEO4J_URI,
  ESHU_NEO4J_USERNAME (legacy NEO4J_USERNAME also accepted), ESHU_NEO4J_PASSWORD
  (legacy NEO4J_PASSWORD also accepted) in environment; check that the
  Neo4j service is healthy in Docker Compose or Kubernetes.

- Symptom: `eshu_dp_neo4j_deadlock_retries_total` rising → MERGE contention on
  shared nodes; `cypher.RetryingExecutor` handles this transparently; check
  worker concurrency in projector/reducer.

- Symptom: `failure_class=graph_write_timeout` in queue rows → the
  `cypher.TimeoutExecutor` deadline was exceeded; check the TimeoutHint field
  in the error for the env var to adjust.

## Anti-patterns

- **Do not import this package from `internal/` packages**. Concrete driver
  adapters are wiring concerns; `internal/` packages talk to backends only
  through the `cypher.Executor` seam.
- **Do not add statement builders here**. All BuildCanonical... and BuildRetract...
  functions belong in `internal/storage/cypher`.
- **Do not duplicate the executor wrapper chain** (InstrumentedExecutor,
  RetryingExecutor, TimeoutExecutor) here. Those wrappers live in
  `internal/storage/cypher` and are composed in `cmd/` wiring. Adapters in
  this package are the innermost layer.

## What NOT to change without an ADR

- `cypher.Executor` interface shape — adding methods here or in `cmd/` wiring
  without updating all backends (Neo4j and NornicDB) breaks the backend-neutral
  contract; see `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- Bolt session transaction model — session read/write mode and transaction
  timeout behavior are correctness constraints; see
  `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`.
