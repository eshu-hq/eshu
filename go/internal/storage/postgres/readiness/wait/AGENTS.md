# AGENTS.md — internal/storage/postgres/readiness/wait

Scoped instructions for this package. The root `AGENTS.md` and `CLAUDE.md`
still apply.

- Every statement stays a single-row primary-key read, upsert, or update on
  `reducer_readiness_waits`. Do not join `fact_work_items` or add a claim-path
  subquery; the ledger exists so the hot queue table and its claim query stay
  unchanged.
- The upsert must never lower `first_deferred_at` at an equal
  `anchor_epoch`. `TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive`
  fails when `LEAST` is replaced by the new value.
- Keep the `anchor_epoch` fence on both writes: the upsert's
  `ON CONFLICT ... WHERE EXCLUDED.anchor_epoch >= wait.anchor_epoch` and the
  clear's `WHERE anchor_epoch = $read`. `store_fence_live_test.go` fails when
  either is removed. Do not turn the clear back into a `DELETE`: the tombstone
  is what fences a straggler's re-insert.
- Return an error, never a missing row, when the database cannot answer. A
  missing row reads as "no wait yet" and restarts the bound.
- Decision logic belongs in `crossscope.DecideWait`, not here.
- This package imports `reducer/contract`, `reducer/crossscope`, and
  `postgres/db` only. Its live test may import the parent `postgres` package
  for `ApplyBootstrap`; production code must not.
