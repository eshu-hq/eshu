# AGENTS.md — internal/graph/legacy/workload

Scoped instructions for this package. The root `AGENTS.md` still applies.

- This is a leaf package. It may import `go/internal/workloadid` and the
  standard library only. `go/internal/query` (including `query/entity`),
  `go/internal/runtime`, the reducer, and `go/cmd/reducer` import it, so it
  must never import any of them, nor `storage/cypher`.
- It performs no writes and emits no telemetry. Keep it that way: the writer
  and its counters live in `go/internal/reducer/workload/legacy/retire`.
- Keep the read statements single-clause and identity-anchored with a bound
  relationship variable. Do not add `EXISTS {}`, `OPTIONAL MATCH` with an
  aggregate, a pattern comprehension, or `w.id = <expression of w>` to a read:
  `docs/public/reference/nornicdb-pitfalls.md` records them as unreliable on
  NornicDB. Put the negative logic in
  `Evaluate`.
- Every list parameter must be bound as a non-nil list. `x IN null` is null
  and silently matches nothing.
- `safeRelationship` and the unsafe-relationship subquery in
  `DeleteNeo4jCypher` encode one rule. Change both in the same commit, and run
  the live test (`ESHU_CYPHER_BOLT_DSN`) against Neo4j.
- Never add a delete path that ignores incident relationship types, or one
  that UNWIND-batches the delete.
- Keep the lock-first shape of `DeleteNeo4jCypher`: the marker `SET`/`REMOVE`
  must come before the retirement clauses, or a writer that commits while the
  delete waits is deleted over. Run `TestLiveDeleteNeo4jRechecksAfterTheLockWait`
  after any change to it, and write label checks as `'L' IN labels(o)` (the
  label-predicate gate rejects `o:L` in a relationship `WHERE`).
