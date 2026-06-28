# Evidence — reducer conflict-fence TOCTOU fix (#4137, completing #3558)

## What changed

The reducer claim path could let two **pending** siblings on one
`(conflict_domain, conflict_key)` both acquire a live lease when claimed by
genuinely simultaneous single-claim workers. The `NOT EXISTS` live-sibling fence
in `claimReducerWorkQuery` only defers a sibling once a holder's claim has
COMMITTED; under READ COMMITTED two concurrent claimers each select a different
pending sibling row before either commits, and `FOR UPDATE SKIP LOCKED` locks the
distinct rows, so both claim.

Fix: a partial unique index (`fact_work_items_reducer_live_lease_uniq`, migration
005) enforces at most one claimed/running reducer row per conflict key at the
database level. `ReducerQueue.Claim` / `ClaimBatch` translate the resulting
`unique_violation` (SQLSTATE 23505, matched by constraint name) into a deferred
no-op claim.

Because a partial index cannot use `now()` to mean "live only", the index keeps
one **claimed/running** row per key including an EXPIRED holder. The claim
selection therefore reclaims that holder before any pending sibling, so an
expired holder is never raced past (which would hit the index forever and wedge
the key):

- single claim: the conflict fence defers a pending/retrying candidate to ANY
  claimed/running holder on the key (not only a live one); the holder has no
  other claimed/running sibling, so it stays reclaimable;
- batch claim: the per-conflict-key representative prefers a claimed/running row
  over older pending siblings, so the batch reclaims the holder.

The hot claim `SELECT` cost is unchanged (the fence dropped one condition; the
representative gained one ordering term).

## Conflict domain, workers, leases

- Conflict domain: `fact_work_items` rows sharing `(conflict_domain,
  COALESCE(conflict_key, scope_id))` at `stage = 'reducer'`, `status IN
  ('claimed','running')`.
- Workers: the regression/gate tests run ≥4 (8) concurrent claimers, each on its
  own Postgres connection, over 40 rounds.
- Lease: 1h in the race rounds (so a claimed sibling stays live); short
  real-clock leases (250–300ms) in the fencing/reaping tests.

## No-Regression Evidence:

The fix adds no per-row work to the claim `SELECT` (the plan is identical to the
pre-fix query, confirmed via `EXPLAIN ANALYZE`); the only added cost is one
partial-index maintenance write when a row transitions to/from a live lease.

`BenchmarkReducerQueueClaimDeepQueue/depth_2000` (single `Claim`), backend
`postgres:18-alpine`, input shape depth 2000 over 1024 conflict scopes (~2
siblings per key), `benchstat` n=6, before = `origin/main` migration (no index),
after = this change:

- sec/op: 3.947m → 3.967m — `~ (p=0.240)` (no significant change)
- B/op: `~ (p=0.597)`
- allocs/op: 84 → 84 — `~ (p=1.000)`

(A rejected alternative — restricting the candidate to the per-conflict-key
representative row in the `SELECT` — regressed this benchmark 4.4× because the
correlated subquery is evaluated per scanned row; `EXPLAIN ANALYZE` showed
`loops=2000`. The partial unique index avoids that entirely.)

## No-Observability-Change:

No new metric or span. A fenced sibling stays `pending` and remains visible via
the existing `eshu_dp_queue_depth` gauge; the deferred claim returns the existing
"no claimable work" path (`ok=false`), so no failure class or telemetry contract
changes. The 23505 fence only fires under genuinely simultaneous same-key
single-claims (rare; the production concurrent path uses the batch claim, which
never trips it). A dedicated claim-contention counter is a reasonable future
addition but is not required for correctness.

## Safety

- Correctness: the regression test
  `TestReducerContentionGateConflictKeyMutualExclusionConcurrentPendingSiblings`
  fails pre-fix (both siblings claimed) and passes post-fix; the existing
  `TestReducerClaimFencesConcurrentClaimersOnSharedConflictKey` now passes
  deterministically. Both ran green `-race`, ×5–×10. Disjoint-key concurrency,
  committed-holder fencing, stale-lease reaping, and fenced-sibling-after-ack all
  still pass.
- Deadlock-free: the index never blocks two transactions in a cycle — the batch
  representative and the single-row-per-claim mean at most one transaction
  inserts a given key tuple at a time; the loser waits for the winner (acyclic)
  and receives 23505.
- Migration safety: migration 005 resets any pre-existing duplicate live leases
  (only reachable via the race this index closes) to pending before creating the
  unique index, so it is safe to apply to a live deployment and is idempotent.
