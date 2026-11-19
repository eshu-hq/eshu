# #10869: idle projector workers back off instead of claiming every second

## Problem

Idle projector workers ran the full hand-out claim and then waited a fixed
one second, every second, with no cheap check and no wake-up signal. On QA
the claim ran 16,674,481 times in 31.8 days, returned 51,581 rows (99.7% of
calls empty), averaged 52.1 ms, and read 20,436 buffers per call (issue
evidence, `pg_stat_statements` 31.8-day window). A 60-second sample on
2026-11-17 averaged 307 ms and was 17% of all database time. The claim's
first step selects projector items in every state; at the time of the
measurement all 22,210 were finished.

## Fix

Per-worker exponential idle backoff in `projector.Service`, mirroring the
shared-projection runner (`go/internal/reducer/intents/shared/worker/runner.go`):
after each consecutive empty poll the wait doubles from `PollInterval`
(base, 2x, 4x) and caps at `maxProjectorPollInterval` (5 s); the cap is
`max(5 s, base)` so an operator `PollInterval` above 5 s is never shortened.
At the default 1 s wiring the idle waits run 1 s, 2 s, 4 s, 5 s, 5 s, ...
Each worker tracks its own counter in both `runSequential` and
`runConcurrent` (one shared helper, no divergent copies). The counter
increments only on `(ok=false, err=nil)` and resets to zero before
`processWork` on a claimed item, so productive cycles still re-poll
immediately. `recoverClaimConflict` still waits exactly one base interval
and never touches the counter: contention is not emptiness. Fatal claim
errors are unchanged. `PollInterval` still defaults to 1 s and the claim SQL
is byte-identical.

Pickup bound, by construction: an item queued (or reaching `visible_at`)
just after an empty poll at maximum backoff waits at most
`max(5 s, PollInterval)` plus one claim duration. At production wiring that
is about 5 s plus the claim (52 ms mean over 31.8 d, 307 ms in the sampled
minute). Workers do not improve the bound; synchronized waits are assumed.

The cheap `EXISTS` pre-check leg (ruling decision 2) was NOT shipped: the
blocking theory shim failed its own proceed gate (see Evidence). Shipping
only the backoff cuts the idle full-claim rate about 5x (8 workers: ~8/s to
~1.6/s at the default wiring) without changing what the claim takes or
sweeps.

## The stopped cheap-check leg

The candidate probe was the ruling's shape exactly:

```sql
SELECT EXISTS(
    SELECT 1
    FROM fact_work_items AS work
    WHERE work.stage = 'projector'
      AND (work.status IN ('pending', 'retrying', 'failed', 'dead_letter')
           OR (work.status IN ('claimed', 'running')
               AND (work.claim_until IS NULL OR work.claim_until <= $1)))
      AND ($2 = '' OR EXISTS (
          SELECT 1 FROM ingestion_scopes AS scope
          WHERE scope.scope_id = work.scope_id
            AND scope.source_system = $2))
)
```

Measured locally on PostgreSQL 18 (this box) against a disposable clone of
the bootstrap schema holding 22,000 terminal projector rows (the QA worst
case from the issue) plus 200,000 filler `reducer` rows so the table is
large overall, after `ANALYZE`, `EXPLAIN (ANALYZE, BUFFERS)` x5:

- TRUE path (2,200 `failed` rows present): 2-3 shared buffers, 0.004-0.04 ms.
  A `failed` row sits early in heap order, so the probe answers almost
  immediately -- but `true` means the full claim still runs.
- FALSE path (all 22,000 rows `succeeded`/`superseded`, the true idle steady
  state): full `Seq Scan`, 222,001 rows removed by filter, ~5,500 shared
  buffers, ~15 ms per call, all five runs. No index from the gate's list
  (`fact_work_items_stage_domain_status_idx`,
  `fact_work_items_status_idx`) is used: the top-level OR over two status
  groups plus the `claim_until` arm defeats both leading columns, and the
  `domain` column that would complete the stage-led index cannot be added to
  the probe (the claim does not filter on it, so filtering would risk false
  negatives against rows other writers enqueue).

Gate verdict: index scan required, `<=200` buffers required, mean `<=5 ms`
required. The FALSE path fails all three (seq scan, ~5.5k buffers, ~15 ms),
and it scales with total table size: at 10x the table it would cost more
than today's claim. Per the ruling's own stop rule, leg 2 stops here and no
index is added in this change.

Two QA-mix notes for the follow-up: if QA's finished rows include lone
`failed`/`dead_letter` rows with no newer sibling, the probe answers `true`
on every idle poll and the full claim keeps running (zero savings plus one
extra statement); if they are all `succeeded`/`superseded`, the probe
answers `false` at one full table scan per check. Either way the probe's
steady-state value on QA is bounded by the status mix, which only
post-deploy `pg_stat_statements` can show.

Proposal (reported, not filed): a partial composite index covering exactly
the probe's row set, e.g. on `(stage, status)` where the stage is
`projector` and the status is actionable, so the FALSE path becomes an
index-only absence proof of a few buffers; re-run this note's shim against
it before building the check. A second, independent proposal is the
`status` filter on `source_scoped_projector_work` to cut the claim's own
busy cost at the root.

## Tests

- `TestProjectorPollBackoffSequence`: helper returns 1 s, 2 s, 4 s, 5 s, 5 s,
  5 s at base 1 s. Fails before the helper exists.
- `TestProjectorPollBackoffNeverShortensLargeBase`: base 10 s stays 10 s at
  every depth (cap is `max(5 s, base)`).
- `TestProjectorPollBackoffDefaultsNonPositiveBase`: zero/negative base
  falls back to the default interval.
- `TestServiceRunSequentialBacksOffWhileIdle`: always-empty source records
  1 s, 2 s, 4 s, 5 s, 5 s. Fails on the old fixed 1 s waits.
- `TestServiceRunSequentialResetsBackoffAfterWork`: two empties, one claimed
  item, then empties again record 1 s, 2 s, 1 s, 2 s, 4 s.
- `TestServiceRunSequentialConflictWaitsBaseWithoutBackoff`: three claim
  conflicts then drain record four base-interval waits; contention never
  advances the counter.
- `TestServiceRunConcurrentBacksOffWithinBounds` (`Workers: 3`): every
  recorded wait is a valid backoff step, none exceeds 5 s, and each worker's
  first wait is the base interval. Run with `-race`.
- Full `go/internal/projector` package suite plus `go vet` (see Evidence).

Removing the increment, the reset, or the cap each fails at least one of
these tests; restoring the fixed `s.pollInterval()` wait fails the sequence
tests.

## Evidence

Performance Evidence: the service change adds no statement and no index; it
removes polls. Idle full-claim rate per worker falls from 1/s to at most
1/5 s at the default wiring (about 5x across 8 workers: ~8/s to ~1.6/s),
each avoided call saving the issue's 52.1 ms mean and 20,436 buffers, so
idle claim database time falls about 80% with no change to claim latency
itself. The pickup bound above is asserted by the wait-sequence tests, not
wall clock: no test sleeps a real backoff. Post-deploy, the issue's last two
checkboxes close by re-reading the same `pg_stat_statements` row for
`source_scoped_projector_work` over a >=1 h idle window and comparing calls,
mean time, and buffers per call against the 31.8-day baseline
(16,674,481 calls, 51,581 rows, 52.1 ms mean, 20,436 buffers/call).

No-Regression Evidence: the claim statement is unchanged (empty diff on both
claim SQL files), so the existing projector claim live suite
(`TestProjectorClaimMaintenanceSemantics`, the fence/full-guard/marked-guard
and deadlock proofs named in `go/internal/storage/postgres/AGENTS.md`)
covers the claim path unmodified; this change only spaces the polls. Busy
behavior is untouched: a claimed item resets the counter before processing
and workers re-poll immediately after productive cycles.

Observability Evidence: `QueueClaimDuration{queue="projector"}` is still
recorded around every `Claim()` in both loops, so the reduced poll rate is
directly visible in its count; the queue stage/status observer still shows
`pending > 0` with no pickups. No new metric or counter was added;
`recoverClaimConflict` keeps its existing WARN with no counter change.

NOT_CHECKED: re-measurement against the deployed database (unreachable from
this box); the shim above runs the same method locally. Post-deploy
`pg_stat_statements` delta over >=1 h idle (acceptance criterion 6,
second half).
