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
  (2000), which leaves 704 bytes under Postgres's 2704-byte btree limit.
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
prefix, a subset of the full set. Backend: Postgres 18 (throwaway container),
production bootstrap schema. Input shapes and results from
`go test ./internal/storage/postgres -run 'TestNarrowedWait.*Live'` (exit 0, 49.9 s):

| Proof | Shape | Result |
|---|---|---|
| `TestNarrowedWaitLargeFenceParksThroughFailLive` | 600 random-id holders, `ReducerQueue.Fail` | 1 wait row under the cap, queue row `retrying`, `visible_at` at the 10-minute backstop; a repeated Fail keeps 1 row |
| `TestNarrowedWaitWorstCaseKeyFitsLive` | 300 incompressible ids, 156-byte work_item_id, then 3000-byte work_item_id | realistic id parks; the extreme id parks nothing and stays on the backstop |
| `TestNarrowedWaitPrefixWakeSelectivityLive` | 200 holders | an outside-prefix activation leaves the row parked; a prefix activation reopens it |
| `TestNarrowedWaitAdversarialOrderConvergesLive` | 90 holders, non-prefix cleared first | converged in 28 re-parks (at most 90 members); each re-park unsatisfied at park time |
| `TestNarrowedWaitAuditReopensClearedPrefixLive` | 120 holders, prefix cleared with no activation | audit reopens 1; the next park carries the next prefix |
| `TestNarrowedWaitCutKeyRacesActivationLive` | 80 holders, cut park races 26 prefix activations | wait row gone, queue row `retrying` attempt 1 |

RED on the base commit: a 600-scope `Fail` on `origin/main` (live Postgres 18) returns
`park waiting reducer work: ERROR: index row requires 27192 bytes, maximum size is 8191 (SQLSTATE 54000)`. Longest work_item_id measured
here is the constructed 156-byte shape from the QA partition key; the real
fleet maximum is NOT_CHECKED.

## Observability Evidence:

An operator sees a bounded key on `eshu_dp_reducer_waiting_key_bounded_total` by
`reason`. A rejected park logs ERROR `reducer waiting park rejected as an oversized index row; the queue backstop retries the item`
with `intent_id`, `waits_for_kind`, `key_bytes`, `work_item_id_bytes`, and
`failure_class=waiting_index_row_rejected`. A rising `cut` count means a wide
fence, not a failure. Any `index_row_rejected` count means the cap is wrong for the
data and needs a look. `unrepresentable` means a work_item_id or scope id is
implausibly long.
