# AGENTS.md - internal/parser/summary guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: content-versioning and termination guarantees
3. `summary.go` - `FunctionID`, `Effects`, `Summary`, structural hashing, content
   versioning
4. `store.go` - the incremental `Store`, `Upsert`, dirty propagation, recompute
5. `scc.go` - Tarjan condensation that makes the version fixpoint terminate
6. `snapshot.go` - the durable `Snapshot`/`Load` form
7. `store_test.go`, `snapshot_test.go` - delta, recursion, identity, and reload
   proofs

## Invariants this package enforces

- Pure and language neutral: standard library only; no storage, graph, parser,
  or telemetry imports. A language analysis supplies `Effects`.
- The content version excludes same-SCC callee versions, so recursion converges.
  Never hash an intra-SCC callee's version.
- `FunctionID` is generation-independent. Never derive it from a commit or
  generation.
- `Upsert` recomputes only structurally-changed functions and their transitive
  callers; unrelated functions are untouched.
- Deterministic: sorted iteration everywhere that feeds a hash or output;
  `Snapshot` ordered by ID; reload reproduces versions exactly.
- The `Store` is not concurrency-safe.

## Common changes and how to scope them

- Change the summary model (`Effects`): update `summary.go`, keep
  `structuralHash` total and order-independent (sort every field), and add a
  delta-test case in `store_test.go` first.
- Change versioning: edit `contentVersion`; keep the SCC exclusion or the
  fixpoint may not terminate. Prove with `TestRecursionTerminates`.
- Add persistence backends: implement against `Snapshot`/`Load`; do not couple
  the in-memory `Store` to a database.

## Failure modes and how to debug

- `Upsert` hangs or recomputes forever: a version is hashing an intra-SCC callee
  version. Check `externalCalleeVersions` excludes `sccID[callee] == sccID[fn]`.
- Over-recomputation (whole store churns): identity carries a generation, or
  `structuralHash` is not order-independent, or dirty propagation seeds too much.
- Reload mismatch: `Snapshot` ordering or `Load` reverse-edge wiring drifted.
  Re-upserting unchanged effects on a reloaded store must recompute nothing.

## Do not change without review

- The SCC-exclusion rule in `contentVersion`/`externalCalleeVersions` (it is the
  termination guarantee) or the generation-independence of `FunctionID`.
