# #10879 queue-update census by cause (before side)

Read-only measurement, no schema change. Harness:
`go/internal/storage/postgres/reducer_queue_10879_census_test.go`,
`TestReducerQueue10879UpdateCensusByCause` (DSN-gated, disposable schema).

## Method

Full #10862 replay seed (8,713 rows, oldest >= 29 d tail), then one phase per
cause through the real `ReducerQueue` methods on `supply_chain_impact` rows
(plain ACK path). Per-phase `pg_stat_user_tables` deltas (after
`pg_stat_force_next_flush()`) attribute each heap update to exactly one
cause. Local Postgres 18.6, disposable schema, branch `drive/10879-queue-hot`.

## Result (175 statements issued)

| cause | statements | heap updates | HOT updates | HOT share |
|---|---|---|---|---|
| claim | 40 | 40 | 0 | 0% |
| renew (heartbeat x2) | 80 | 80 | 0 | 0% |
| finish (ack) | 15 | 15 | 0 | 0% |
| retry (fail retryable) | 15 | 15 | 0 | 0% |
| dead-letter (fail terminal) | 10 | 10 | 0 | 0% |
| reopen (succeeded to pending) | 15 | 15 | 0 | 0% |
| triggers (DB, extra rewrites) | 0 issued | 0 excess | — | — |
| total | 175 | 175 | 0 | 0% |

## What this proves

- Attribution is exact: heap delta equals issued statements, so DB triggers
  contribute zero extra `fact_work_items` rewrites. All six triggers on the
  table either rewrite `NEW` in a `BEFORE ROW` trigger (no extra heap
  update) or write another table (`cross_scope_completion_events`).
- HOT share is 0% for every cause today. Mechanism: every cause writes
  `updated_at`, and `updated_at` is indexed (fairness/order key), as are
  `status`, `claim_until`, and `visible_at`. This matches the QA reading
  (0.7% HOT overall).
- Verdict math for the arbiter thresholds: per lifecycle of 1 claim + H
  heartbeats + 1 terminal, heartbeat share is H/(2+H): H=1 gives 33.3%,
  H=2 gives 50%, H=5 gives 71.4%. The >= 30% bar to proceed with the lease
  split is met as soon as production averages >= 1 heartbeat per run
  lifecycle; below 10% (H ~= 0) the theory is dead. Production H is still
  NOT measured; it needs `pg_stat_statements` by queryid on QA.

## Not covered here (for follow-up lanes)

- The cheaper shims (drop `reducer_conflict_claim_idx`; heartbeat without
  the `updated_at` bump) — not run.
- Claim-path join cost and migration proof — not run.
- `TestReducerQueue10862ReplayShapeAndDrain` fails on this branch before
  its first ACK: its disposable schema lacks `reducer_waiting_items`, which
  the #10876 post-ACK wake reads. Filed as a follow-up issue (see lane
  handoff); the census carries its own copy of the table.
