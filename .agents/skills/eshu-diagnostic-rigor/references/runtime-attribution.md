# Runtime Attribution

## Diagnostic Model

Separate these before proposing an optimization:

- queue wait
- handler duration
- actual graph/backend write time
- fact/input load time
- shared projection wait and processing time
- conflict blocking or readiness wait
- CPU idle, IO wait, and disk idle
- ambient backend work such as embeddings, background indexing, or non-Eshu
  runtime features
- stale image, wrong branch, missing schema/bootstrap, or mismatched backend
  build

If CPU and disk are idle, suspect serialization, queue fences, query shape,
backend lookup/validation behavior, or data shape before adding workers.

Timeout-shaped failures are only evidence, not diagnosis. Classify the failure
as timeout budget, query shape, missing schema/index, backend fallback,
transaction validation, retry/idempotency behavior, stale image, or ambient
backend work before patching.

## Common Eshu Reducer Lessons

- Queue wait alone is not proof that more concurrency helps.
- Broad conflict keys may be correct and still not be the current bottleneck.
- Full fact loads can dominate handlers even when graph writes are cheap.
- Shared projection wall time must be split into wait, selection, lease claim,
  processing, graph write, and completion/ack before tuning workers.
- NornicDB performance depends on exact Cypher shape, label/index lookup,
  relationship-existence checks, transaction validation, and commit behavior.
- A shim or `EXPLAIN (ANALYZE, BUFFERS)` proves query SHAPE; only a re-drain of
  the built binary against the real worst-case backlog proves WALL-CLOCK. A
  small-N EXPLAIN can pass while the live drain exposes a missing `AS
  MATERIALIZED` (CTE re-inlined per reference) or a residual correlated subquery
  (O(N^2) tail). End the proof ladder on the binary, not the EXPLAIN.
- A row-set equivalence differential (bidirectional `EXCEPT` / set+order 0/0)
  proves the candidate SET, not locking or lease behavior — it drops `FOR
  UPDATE`. Any claim/lock/lease/queue rewrite needs a separate concurrency proof
  (contention / EvalPlanQual recheck / lease-safety).
