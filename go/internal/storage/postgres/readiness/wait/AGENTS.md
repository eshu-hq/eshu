# AGENTS.md — internal/storage/postgres/readiness/wait

Scoped instructions for this package. The root `AGENTS.md` and `CLAUDE.md`
still apply.

- Every statement stays a single-row primary-key read, upsert, or delete on
  `reducer_readiness_waits`. Do not join `fact_work_items` or add a claim-path
  subquery; the ledger exists so the hot queue table and its claim query stay
  unchanged.
- The upsert must never lower `first_deferred_at` unless the caller passes
  `resetAnchor`. `TestReadinessWaitConcurrentUpsertsKeepEarliestAnchorLive`
  fails when `LEAST` is replaced by the new value.
- Return an error, never a missing row, when the database cannot answer. A
  missing row reads as "no wait yet" and restarts the bound.
- Decision logic belongs in `crossscope.DecideWait`, not here.
- This package imports `reducer/contract`, `reducer/crossscope`, and
  `postgres/db` only. Its live test may import the parent `postgres` package
  for `ApplyBootstrap`; production code must not.
