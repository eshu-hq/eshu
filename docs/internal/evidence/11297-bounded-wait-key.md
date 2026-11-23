# #11297: bounded narrowed wait key

Issue: #11297. Arbiter ruling: option A (sorted-prefix cut) with four conditions.

## Cause

The general reducer on the QA environment exited with
`fail reducer work: park waiting reducer work: ... index row size 4448 exceeds btree version 4 maximum 2704 for index "reducer_waiting_items_pkey" (SQLSTATE 54000)`.
The `Retryable()` deferral itself was handled: the queue row was already logged
`reducer execution deferred`. The park INSERT that follows it failed because a
narrowed corpus wait keyed on the whole blocking scope set, and the batch worker
treats a broken `WorkSink.Fail` as fatal.

## Design

- `narrowedWaitingKey` keeps a sorted prefix of the blocking set (at most 1200
  JSON bytes) and the read set (at most 400). The whole index row (work_item_id,
  waits_for_kind, rendered key) must stay under `waitingIndexRowBudgetBytes`
  (2000). The cap counts JSON text length; Postgres stores the key as JSONB,
  which costs one more byte per array element. The independent reviewer
  measured the worst case (a set of 2-character ids) at a 2328-byte index row
  for a 2000-byte counted row, so the real headroom under Postgres's 2704-byte
  btree limit is 376 bytes.
- `work_item_id` is unbounded by construction: `reducerWorkItemID`
  (`reducer_queue_helpers.go`) is `"reducer_"` plus scope, generation, domain
  and entity key with no length cap. The code is safe at any length because
  the budget subtracts `len(work_item_id)` before it sizes the key sets. The queue
  row's own primary key (`fact_work_items_pkey`) already refuses an
  incompressible id near 2.7 KB at enqueue (`index row size 3016 exceeds btree
  version 4 maximum 2704`, observed while building the live worst-case test),
  so the wait row's `work_item_id` is below that in practice.
- A set whose first id does not fit is unrepresentable. Nothing parks and the
  queue row keeps its 10-minute backstop. The coarse `{scope_id, generation_id}`
  key is never used for a narrowing-carrying deferral: the own generation is
  already active when a narrowed deferral is built, so the coarse wait is
  satisfied at park time and would reopen the waiter in a tight loop.
- `failIntent` survives a park INSERT rejected with SQLSTATE 54000 only: ERROR log,
  counter, return nil. The retry UPDATE already committed. Every other park
  error still reaches the caller.
- Counter `eshu_dp_reducer_waiting_key_bounded_total{reason}` with the closed set
  `cut`, `unrepresentable`, `index_row_rejected`.

## No-Regression Evidence:

Wakes per waiter are at most the full-key count: the wake matches only the retained
prefix, a subset of the full set. Backend: Postgres 18.6 (throwaway container),
production bootstrap schema. Input shapes and results from the five new live
proofs, run with `go test ./internal/storage/postgres -run '<name>'` on the final head:

| Proof | Shape | Result |
|---|---|---|
| `TestNarrowedWaitLargeFenceParksThroughFailLive` | 600 random-id holders, `ReducerQueue.Fail` | 1 wait row under the cap, queue row `retrying`, `visible_at` at the 10-minute backstop; a repeated Fail keeps 1 row |
| `TestNarrowedWaitWorstCaseKeyFitsLive` | 300 incompressible ids, 156-byte work_item_id, then a random 2000-byte work_item_id | realistic id parks; the extreme id parks nothing, counts `unrepresentable` (never `index_row_rejected`) and stays on the backstop |
| `TestNarrowedWaitPrefixWakeSelectivityLive` | 200 holders | an outside-prefix activation leaves the row parked; a prefix activation reopens it |
| `TestNarrowedWaitAdversarialOrderConvergesLive` | 90 holders, non-prefix cleared first | converged in 28 re-parks (at most 90 members); each re-park unsatisfied at park time |
| `TestNarrowedWaitAuditReopensClearedPrefixLive` | 120 holders, prefix cleared with no activation | audit reopens 1; the next park carries the next prefix |

Not claimed here: a cut-key park racing a prefix activation. The park INSERT and
its satisfied check run outside the activation transaction while the activation
wake runs inside it under READ COMMITTED, so a wake can be lost. That is the open
base defect #11256. It is not caused by the bound: the existing
`TestNarrowedWaitConcurrentConvergenceLive` fails the same way on `origin/main`
(3 of 5 runs), and a cut-key race test written for this PR flaked about 3 in 8
runs for the same reason, so the test was removed rather than enrolled flaky. The
race proof belongs to #11256's fix. `TestNarrowedWaitConcurrentConvergenceLive` is
therefore excluded from this PR's pass claim; the other existing narrowed live
tests are listed in the PR body with their results.

RED on the base commit: a 600-scope `Fail` on `origin/main` (live Postgres 18) returns
`park waiting reducer work: ERROR: index row requires 27192 bytes, maximum size is 8191 (SQLSTATE 54000)`. Longest work_item_id measured
here is the constructed 156-byte shape from the QA partition key; the real
fleet maximum is NOT_CHECKED (the code does not depend on it: see above).

## Observability Evidence:

An operator sees a bounded key on `eshu_dp_reducer_waiting_key_bounded_total` by
`reason`. A rejected park logs ERROR `reducer waiting park rejected as an oversized index row; the queue backstop retries the item`
with `intent_id`, `waits_for_kind`, `key_bytes`, `work_item_id_bytes`, and
`failure_class=waiting_index_row_rejected`. A rising `cut` count means a wide
fence, not a failure. Any `index_row_rejected` count means the cap is wrong for the
data and needs a look. `unrepresentable` means a work_item_id or scope id is
implausibly long.
