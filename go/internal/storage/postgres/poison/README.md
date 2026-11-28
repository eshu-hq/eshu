# Poison

## Purpose

The poison package owns the dead-letter/poison class in `fact_work_items`
(#4740): rows with `status = 'dead_letter'` whose scope has no strictly-newer
`scope_generations` row. The generation-liveness sweep never reaches this
class — `dead_letter` is terminal and unclaimable, and the wedge-detection
`NOT EXISTS` guard treats same-generation `dead_letter` reducer work as still
in progress. Without this arm, such a scope can never self-heal.

## Ownership boundary

Detection (`CountDeadLetters`) and bounded recovery (`RecoverDeadLetters`)
over the poison class only. It does not own claiming, retry budgets for
non-terminal rows, or the runner loop — the sweep cadence and the auto-retry
flag live in `reducer/maintenance/poison` and `cmd/reducer`. It does not own
the generation-liveness sweep for wedged `active` generations.

## Exported surface

- `Store`, `NewStore` — Postgres-backed detector and bounded recoverer
- `Policy`, `Normalize` — re-drive budget (`MaxRecoverAttempts`, default 1)
  and per-sweep cap (`BatchLimit`, default 200)
- `StuckCounts`, `RecoveryResult` — gauge input and sweep summary
- `CountDeadLettersQuery`, `RecoverDeadLettersQuery` — shipped statements,
  exported as one contract (the parent package pins the recovery statement in
  its container-image capability latch)

See `doc.go` for the full contract.

## Dependencies

- `storage/postgres/db` — `ExecQueryer` surface the store runs against
- `storage/postgres/queue` (`queuestore`) — `ReplayRetryAllowanceAssignments`,
  the replay retry-allowance SET fragment the recovery statement appends
  (#11217). That package imports only `projector/failure` and pure helpers, so
  the import adds no cycle.

Non-test files must never import the parent `postgres` package: the parent's
tests and `cmd/reducer` import this one, so the reverse import is a cycle.

## Telemetry

None emitted here. The gauges (`eshu_dp_poison_dead_letter_*`,
`eshu_dp_poison_liveness_*`) are registered by `cmd/reducer` from the counts
this package returns, and the runner in `reducer/maintenance/poison` logs the
sweep outcome.

## Gotchas / invariants

- The recovery UPDATE's `AND target.status = 'dead_letter'` is a write-time
  re-verify, not a redundant predicate: under Read Committed a row a
  concurrent worker reclaimed between the candidate snapshot and the row lock
  must be skipped (EvalPlanQual recheck). Keep the row-self predicate on the
  locked relation.
- Recovery locks its selected rows first, in `work_item_id COLLATE "C"`
  order (`poison_locked`, blocking `FOR NO KEY UPDATE`), the order the
  recovery and admin replays lock in (#8261). The selection's own
  `ORDER BY dead.work_item_id` uses the database's default collation; on an
  `en_US.utf8` database, ordering locks by it deadlocked (40P01) against a
  replay over work ids whose two orders disagree. The lock step also
  rechecks the attempt budget, so a row re-driven and dead-lettered again
  after the snapshot is not re-driven twice in one sweep.
- The attempt budget lives in the row's JSONB payload and is capped with
  `LEAST`; there is no schema column. A row at or past the ceiling is
  excluded by the candidate CTE, never retried.
- A recovered row gets a new row's retry allowance (#11217): the recovery
  SET list appends `queuestore.ReplayRetryAllowanceAssignments` (the shared
  fragment of the three operator replays, never a copy) and raises a
  projector row's `attempt_count` to at least 1. `replay_attempt_baseline`
  takes the row's `attempt_count` and `admission_wait_attempts` goes to 0, so
  the row gets 3 projection claims and 48 admission waits again;
  `attempt_count` is never lowered (it is the claim fence). Reducer rows keep
  `attempt_count` and their baseline until #11184. The per-row bound is still
  `poison_recovery_attempts`: with a budget of M a row gets at most
  3 x (1 + M) counting projection claims and 48 x (1 + M) admission waits.
- `DefaultBatchLimit` (200) deliberately matches the generation-liveness sweep
  batch default; the parent package pins the equality in
  `batch_default_parity_test.go`, so changing one without the other
  fails loudly.
- `"Strictly newer"` is `(ingested_at, generation_id)` compared
  lexicographically, mirroring the generation supersede comparator. A scope
  with any newer generation has moved on: its dead-letter row is historical,
  not live poison.

## Related docs

- `go/internal/reducer/recovery-runners.md` (poison dead-letter section)
- `docs/internal/design/6693-postgres-target-tree/control-plane.md`
