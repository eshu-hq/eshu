# AGENTS.md - internal/storage/nornicdb

## Read first

1. `README.md`
2. `go/internal/storage/cypher/README.md`
3. `docs/public/reference/cypher-performance.md`
4. `docs/public/reference/nornicdb-tuning.md`
5. `docs/public/reference/nornicdb-pitfalls.md`

## Invariants

- Expose `PhaseGroupExecutor` without `GroupExecutor`.
- Keep dependency phases ordered and entity fan-out bounded to disjoint keys.
- Keep retracts on sequential autocommit or bounded-drain routes.
- Fail closed when the inner executor is absent.
- Keep drivers, env parsing, timeouts, retries, and process gates in commands.
- Preserve exact graph output and idempotent partial-phase replay.

## Common changes

- Batch/default tuning: change the shared constant/config here, keep ingester
  and projector aliases on it, then provide same-data timing and exactness.
- Phase dispatch: add a failing adapter test plus offline real-Nornic replay.
- Concurrency: prove admission, ordered error return, race freedom, and the
  inner gate ceiling above one.
- Retracts: prove empty, mixed, all-retract, bounded drain, and replay cases.

## Failure modes

- Zero graph output with success: missing inner executor or lost phase-group
  capability; both must fail tests.
- Later phases miss nodes: phase order or transaction boundary regressed.
- Backend in-flight exceeds configured cap: gate wrapped the outer adapter.
- Retract stalls or no-ops: wrong autocommit/drain route or unsanitized params.

## Anti-patterns

- Do not add a command-local copy of this executor.
- Do not implement `ExecuteGroup` on the outer adapter.
- Do not serialize entity fan-out as a reliability workaround.
- Do not swallow invalid config or graph errors.
- Do not add repository/entity values to metric labels.

## ADR boundary

Changing dependency phase order, restoring whole-materialization atomic writes,
or changing the ownership of graph-write concurrency requires an ADR and
backend conformance proof. Default tuning changes require representative
performance evidence but not an ADR when transaction semantics are unchanged.

## Verification

Run focused, race, offline real-Nornic replay, and B-7 golden-corpus gates as
listed in `README.md`. Runtime-affecting changes also require built-binary
same-data before/after and exact graph proof.
