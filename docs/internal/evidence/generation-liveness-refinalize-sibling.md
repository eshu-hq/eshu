# Generation Liveness Refinalize Sibling Evidence

Issue #7086: after `recover-generations` runs its refinalize prelude, the
generation-liveness sweep failed on every cycle with SQLSTATE 21000 ("more than
one row returned by a subquery used as an expression"). The budget gate in
`recoverWedgedActiveGenerationsQuery` read `liveness_recovery_attempts` through a
scalar subquery keyed on (scope, generation, `projector`, `source_local`). The
refinalize prelude adds a `refinalize_<scope>_<gen>` row beside the canonical
`projector_<scope>_<gen>` row, and `fact_work_items` has no uniqueness on that
tuple. The fix reads the counter with `MAX(...)`.

Performance Evidence: baseline and after were both read-only runs on ops-qa
Postgres (image `sha-763c65e`, Neo4j graph backend, 2026-09-24 ~22:10Z, during a
post-cutover reprojection drain). Input shape: 12,844 (scope, generation) groups
with one projector/source_local row and 809 with two, 806 of them on active
generations. Baseline: the wedged-candidate CTE as a SELECT fails with SQLSTATE
21000 and returns no rows. After: the same CTE with the fix runs `EXPLAIN
(ANALYZE, BUFFERS)` in 102 ms execution plus 12 ms planning and returns 0 wedged
rows. The budget lookup is an `Aggregate` over `Index Scan using
fact_work_items_scope_generation_idx`, 806 loops of 2 rows each, about 10.5k
shared buffer hits, with no sequential scan on `fact_work_items`. The predicate
and index path are identical to the old scalar read; only the row-count
contract changed. The change is safe because the counter is written only on the
canonical row. `MAX` returns that value, yields exactly one row whatever the
sibling count, and treats the budget as spent if any sibling reports it spent.
The in-flight `NOT EXISTS` gate, the reducer-backlog gate and the `ON CONFLICT`
upsert are unchanged.

No-Regression Evidence: `TestGenerationLivenessRefinalizeDuplicateProjectorRow`
(gated by `ESHU_GENERATION_LIVENESS_PROOF_DSN`, postgres:16-alpine) covers four
cases. On base, all four fail with SQLSTATE 21000; on head, all four pass. The
existing `TestGenerationLivenessIntegration` (7 subtests) and the repo-dependency
ownership proof also pass.

No-Observability-Change: this adds no worker, lease, queue, knob, metric, label,
span, or route. Operators see the fix through the existing
`generation_liveness_error` failure class, which stops firing, and through the
existing liveness logs and the stuck-generation gauge
(`countActiveGenerationsByAgeQuery`), which uses the same in-flight predicate.
