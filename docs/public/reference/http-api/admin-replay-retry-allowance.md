# Admin Replay Retry Allowance

A replayed projector row gets the same retry allowance as a new row. This
applies to `POST /api/v0/admin/replay` and to the runtime recovery replay
(`POST /admin/replay`, including the dead-letter backlog drain) (#11182). The
reducer's poison-liveness auto-retry sweep gives a recovered row the same
allowance (#11217).

## What a replay changes on the row

A replay moves a `dead_letter` or `failed` row back to `pending` and changes
three counters:

| Column | After the replay | Why |
| --- | --- | --- |
| `attempt_count` | kept (`GREATEST(attempt_count, 1)`), never lowered | It is the claim fence. All projector workers share one lease owner, so Ack, Fail and heartbeat match on `attempt_count`. A lowered count would let a worker that still holds an old claim match a new claim at the same attempt. |
| `replay_attempt_baseline` | set to the kept `attempt_count` (projector rows only) | The projection budget counts claims after this point. |
| `admission_wait_attempts` | reset to `0` | The admission budget starts fresh. |

The next claim takes `attempt_count + 1`, so the first claim after a replay
counts as claim 1 of the new allowance.

## The budget after a replay

`ProjectorQueue.Fail` sorts each retryable failure into one of two budgets:

- **Projection failures** (anything that is not an admission timeout) retry
  while `attempt_count - admission_wait_attempts - replay_attempt_baseline` is
  below the projection budget (default 3). After a replay the row gets 3
  projection failures, and the third one dead-letters it again.
- **Admission timeouts** retry while `admission_wait_attempts + 1` is below the
  admission budget (`ESHU_REDUCER_ADMISSION_MAX_ATTEMPTS`, default 48). After a
  replay the row gets 48 admission waits, and the 48th one dead-letters it
  again.

The backoff of a projection retry uses `attempt_count - replay_attempt_baseline`,
so the first failure after a replay waits as long as a new row's first
failure does. The backoff of an admission retry uses `admission_wait_attempts`,
as before.

Each of the three replay paths is an operator act. It grants one bounded
allowance, and the row ends in `dead_letter` again if the cause persists. A
duplicate request with the same `idempotency_key` returns the recorded outcome
from the `admin_replay_requests` ledger and changes nothing, so a retried
request never grants a second allowance.

## The poison-liveness sweep

A fourth writer moves `dead_letter` rows back to `pending` on a timer: the
reducer's poison-liveness auto-retry sweep, when its enable setting in the
[service runtimes](../../deployment/service-runtimes.md) page is on (default
off). Since #11217 it writes the same three counters as a replay, so a
recovered projector row gets a new row's allowance: 3 projection failures and
48 admission waits.

The four writers that carry the allowance are the admin replay, the recovery
replay, the bounded dead-letter drain and the poison-liveness sweep. They share
one SET-list fragment, so a change to it reaches all four.

A projector row can be a dead letter at `attempt_count = 0`: the operator skip
dead-letters `pending` rows that were never claimed. Every carrier raises such
a row to attempt 1 with baseline 1, so its first claim counts as claim 1 and
it gets exactly 3 projection claims, not 4.

The sweep's own budget, `poison_recovery_attempts` in the row's JSONB payload,
still bounds how many times it recovers a row (its per-item recovery budget,
default 1, set next to the enable setting). Each recovery grants
one allowance, so with a budget of M a row gets at most 3 x (1 + M) counting
projection claims and 48 x (1 + M) admission waits before it stays
`dead_letter`. A non-retryable failure still dead-letters the row on its first
claim. Reducer rows keep `attempt_count` and `replay_attempt_baseline` until
#11184, so a recovered reducer row resumes the budget it had left.

The sweep writes no replay event. To tell a sweep-set baseline from a replay's,
read `payload ->> 'poison_recovery_attempts'` on the row: a sweep-recovered
row has it, and `POST /api/v0/admin/replay-events/query` shows no event for
that recovery.

Before #11182 a replay kept `attempt_count` but no baseline. A row
dead-lettered at attempt 3 claimed at attempt 4, so its first projection
failure dead-lettered it at once.

Writers that lower a projector row's `attempt_count` also reset
`replay_attempt_baseline` to `0` in the same statement: the reconcile
re-projection of a terminal row and the refinalize re-enqueue. A row that was
never replayed has baseline `0`, which is the rule from before #11182.

Reducer rows keep `replay_attempt_baseline` unchanged on replay, so it stays
`0` on every reducer row: no writer sets it for them. The reducer budget does
not read it yet. Reducer writers that reopen rows with `attempt_count = 0`
(the supply-chain fanout, the service-catalog retrigger, the scope completion
fanout, the dead-letter reopen of #11147) rely on that `0`. When #11184 gives
reducer rows a baseline, each of those writers must reset the baseline in the
same statement. Otherwise `attempt_count - replay_attempt_baseline` goes
negative and the row retries past its budget.

## Where to read it

- `POST /api/v0/admin/replay` and `POST /api/v0/admin/work-items/query` return
  `replay_attempt_baseline` on each item.
- `POST /api/v0/admin/replay-events/query` returns, per replay event,
  `replay_attempt_baseline` (the baseline the replay set) and
  `prior_admission_wait_attempts` (the admission waits the row had spent before
  the replay reset them). Both are `null` for reducer rows and for events
  written before migration 334.
- The `admin_replay_requests` ledger row is unchanged. It is one row per
  request with a flat `work_item_ids` list.
- The runtime recovery replay and the poison-liveness sweep write no replay
  events. Read the work row to see the baseline.

To see how much allowance a replayed row has left, read the row:

```sql
SELECT status, attempt_count, admission_wait_attempts, replay_attempt_baseline,
       attempt_count - admission_wait_attempts - replay_attempt_baseline AS projection_claims_used,
       payload ->> 'poison_recovery_attempts' AS poison_recoveries
FROM fact_work_items
WHERE work_item_id = '<work_item_id>';
```
