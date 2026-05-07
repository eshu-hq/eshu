# AGENTS.md — storage/cypher guidance for LLM assistants

## Read first

1. `go/internal/storage/cypher/README.md` — pipeline position, executor chain,
   exported surface, and operational notes
2. `go/internal/storage/cypher/writer.go` — `Executor` interface, `Statement`,
   `Plan`, `GroupExecutor`, `PhaseGroupExecutor`, `Adapter`; the full contract
   before touching anything else
3. `go/internal/storage/cypher/canonical_node_writer.go` — `CanonicalNodeWriter.Write`,
   phase dispatch, and the GroupExecutor/PhaseGroupExecutor/sequential paths
4. `go/internal/storage/cypher/retrying_executor.go` — NornicDB MERGE unique
   conflict handling and why `ExecuteGroup` does not retry
5. `go/internal/telemetry/instruments.go` and `contract.go` — metric names and
   span constants before adding new telemetry

## Invariants this package enforces

- **Idempotency** — every write path uses MERGE or ON CONFLICT semantics; no
  unconditional CREATE. `doc.go` states this as a package invariant.
- **Phase order** — `CanonicalNodeWriter.Write` phases run strictly in order:
  retract → repository_cleanup → repository → directories → files → entities →
  entity_retract → entity_containment → modules → structural_edges. Parent
  nodes must exist before child MATCH statements run, repository cleanup must
  commit before the repository MERGE, and stale entity cleanup must run after
  current entity upserts so it can avoid giant `uid IN` exclusion filters.
- **No GraphWrite type** — this package does not export a GraphWrite port.
  The backend seam is `Executor`. Every caller in `internal/projector` and
  `internal/reducer` uses the projector CanonicalWriter or
  SharedProjectionEdgeWriter interfaces that are backed by `CanonicalNodeWriter`
  and `EdgeWriter`.
- **No direct driver calls in this package** — the concrete Neo4j and NornicDB
  driver sessions live in `cmd/` wiring. This package only defines contracts.
- **RetryingExecutor.ExecuteGroup skips the retry loop** — the driver's own
  `GroupExecutor` implementation already retries transient errors for the group
  path; adding a retry loop in `RetryingExecutor` would double-retry
  (`retrying_executor.go:87`).
- **OperationCanonicalUpsert vs. OperationUpsertNode** — canonical domain nodes
  use `OperationCanonicalUpsert`; source-local `SourceLocalRecord` writes use
  `OperationUpsertNode`/`OperationDeleteNode`. Do not mix them.
- **Identity cleanup** — repository upserts must keep cleanup before MERGE and
  in a separate phase group. Directory and File writers must not restore
  current-directory or current-file `DETACH DELETE` cleanup.
- **Entity cleanup anchors** — stale entity retractions and current-generation
  `Class`/`Function` containment cleanup must use label-specific anchors. Do
  not collapse those statements back into unlabelled `MATCH (n)` or UID MATCH
  shapes; they are correct Cypher but can force broad graph scans on local
  NornicDB. Stale entity retractions belong in the `entity_retract` phase after
  current entity upserts, not in the pre-upsert `retract` phase.
- **Directory and File nodes update in place** — do not replace current
  `Directory` or `File` nodes just to avoid stale edges. Local NornicDB pays
  heavily for `DETACH DELETE` on those identities. File paths update with
  `MATCH (f:File {path: row.path})`; missing files use a `WHERE NOT EXISTS`
  guard before MERGE so existing `File.path` rows avoid the MERGE
  unique-conflict path.

## Common changes and how to scope them

- **Add a new canonical domain node type** → add a BuildCanonical...Upsert
  function (follow the pattern of `BuildCanonicalWorkloadUpsert`) in
  `canonical.go` or a new file; add a BuildRetract... companion following
  `BuildRetractRepoDependencyEdges` in `canonical_retract.go`; add unit tests in
  `canonical_test.go`. No writer changes needed — callers build the `Statement`
  and pass it to any `Executor`.

- **Add a new shared projection domain (EdgeWriter)** → add the domain constant
  in `internal/reducer`; add a `batchCypherForDomain` case and a `buildRowMap`
  case in `edge_writer.go`; add tests in `edge_writer_test.go`. Verify the new
  UNWIND Cypher template against both Neo4j and NornicDB if both backends are
  active.

- **Add a new executor wrapper** → implement `Executor`; optionally implement
  `GroupExecutor` and/or `PhaseGroupExecutor`; add unit tests. Wire in `cmd/`
  only, not here.

- **Tune batch sizes for a backend** → use `CanonicalNodeWriter` builder
  options (`WithEntityBatchSize`, `WithEntityLabelBatchSize`, etc.) in `cmd/`
  wiring. Do not hard-code backend-specific values inside `canonical_node_writer.go`.

- **Add telemetry for a new metric** → add the instrument to
  `go/internal/telemetry/instruments.go`; add the metric name string to the
  compile-time list in `go/internal/telemetry/contract.go`; record via
  `Instruments.*` in the write path.

## Failure modes and how to debug

- Symptom: `eshu_dp_neo4j_deadlock_retries_total` rising → likely cause:
  concurrent MERGE on shared nodes (Repository, Directory) → check worker
  concurrency in projector/reducer; `RetryingExecutor.MaxRetries` is 3 by
  default; raising it extends recovery time.

- Symptom: `eshu_dp_canonical_phase_duration_seconds{phase="retract"}` elevated
  → likely cause: large stale node set or missing index on `repo_id +
  evidence_source + generation_id` → check graph schema; retract Cypher uses
  DETACH DELETE which is proportional to edge volume.

- Symptom: `failure_class=graph_write_timeout` in queue rows → likely cause:
  `TimeoutExecutor.Timeout` too short for current write volume → check
  `TimeoutHint` in the error for the env var to adjust; check
  `eshu_dp_canonical_phase_duration_seconds` for the slow phase. If the slow
  statement is source-local entity cleanup, verify it is anchored to the
  concrete entity label before raising timeouts.

- Symptom: `eshu_dp_canonical_atomic_fallbacks_total` > 0 → the executor does
  not implement `GroupExecutor`; writes are sequential; investigate whether
  the wired executor is missing `ExecuteGroup`.

- Symptom: NornicDB MERGE unique constraint violation not retried → check
  `isNornicDBMergeUniqueConflict` in `retrying_executor.go:129`; the cypher
  string must contain MERGE and the error must match the expected message shape.

## Anti-patterns

- **Do not add `if backend == "nornicdb"` branches** in writers, statement
  builders, or callers. Backend dialect differences belong only in documented
  narrow seams (schema adapters, `cmd/` wiring, executor constructors).
- **Do not call Neo4j or NornicDB drivers directly** from inside this package.
  Concrete driver sessions belong in `cmd/` wiring.
- **Do not change `Executor` interface signature** without coordinating all
  `cmd/` wiring sites and the projector CanonicalWriter contract.
- **Do not use CREATE instead of MERGE** in canonical Cypher templates. CREATE
  breaks idempotency and will cause duplicate-node errors on retries.
- **Do not add `GroupExecutor` to `ExecuteOnlyExecutor`**. It intentionally
  hides the group path so callers during concurrent ingestion do not hold large
  atomic transactions.

## What NOT to change without an ADR

- `Executor` interface shape — changes break every `cmd/` wiring site and the
  projector CanonicalWriter contract; see
  `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- `CanonicalNodeWriter` phase order — phase ordering is a correctness constraint
  because later phases MATCH nodes created by earlier phases; see
  `docs/docs/adrs/2026-04-17-neo4j-deadlock-elimination-batch-isolation.md`.
- Retraction Cypher label sets — adding or removing node labels from retract
  queries requires coordinated graph schema migration.
- `RetryingExecutor` retry classification logic — NornicDB compatibility
  behavior is documented in `retrying_executor.go`; changes must be evidence-
  backed per the NornicDB maintainer patch bar.
