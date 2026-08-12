# #5709 attempt_count-freeze theory-proof

Enrolling `cross_scope_producer_not_ready` in
`nonCountingReducerRetryFailureClasses` changes the reducer claim UPDATE's
`attempt_count` assignment (`reducerClaimAttemptCountCaseSQL`). This proves the
intended behavior change: a **retrying** row in the new class keeps its
attempt_count (so it is deferred, not dead-lettered, while its producer scope
activates), while counting classes still increment and non-retrying rows still
increment.

## Setup

Ephemeral `postgres:16-alpine`. A minimal `fact_work_items(work_item_id, status,
failure_class, attempt_count)` and the exact assignment
`reducerClaimAttemptCountCaseSQL()` renders (aliased `work`), with the new class
present in the exempt disjunction. Three rows exercise the branches. Shim:
`docs/internal/evidence/5709-attempt-count-freeze.sql`.

## Result — the new class freezes only when retrying

```
    work_item_id     |  status  |         failure_class          | attempt_count | matches_expected
---------------------+----------+--------------------------------+---------------+------------------
 retrying-counting   | retrying | graph_write_timeout            |             3 | t
 retrying-crossscope | retrying | cross_scope_producer_not_ready |             2 | t
 running-crossscope  | running  | cross_scope_producer_not_ready |             3 | t
```

- `retrying-crossscope` (retrying + new class): attempt_count **frozen at 2** —
  the deferred consumer keeps its budget.
- `retrying-counting` (retrying + `graph_write_timeout`, a counting class):
  incremented to 3 — enrolling the new class does not exempt unrelated classes.
- `running-crossscope` (running + new class): incremented to 3 — the
  `status = 'retrying'` guard holds; only retrying rows freeze.

All three rows matched the expected value (`matches_expected = t`).

## Verdict

Confirmed. Enrolling `cross_scope_producer_not_ready` freezes the attempt budget
for exactly the deferred-retrying case and nothing else, so a cross-scope
consumer waiting on its producer's activation is deferred rather than
dead-lettered. Lockstep is guarded by `TestReducerQueueClaimDoesNotCountCrossScopeReadinessDefers`
(SQL) and `TestCrossScopeProducerNotReadyIsNonCounting` (Go).

When this proof was written the enrollment was inert — nothing returned
`crossScopeProducerNotReadyError`. That is no longer true, and the freeze
measured here is now load-bearing rather than hypothetical:
`ci_cd_run_correlation` produces the class through its readiness floor
(`go/internal/reducer/cross_scope_readiness_floor.go`), which defers when the
cross-scope identity load resolved nothing and the declared producer scopes have
not activated. Because the class never counts against the retry budget, that
floor carries its own elapsed-time bound so a producer scope that never activates
still converges instead of deferring forever.
