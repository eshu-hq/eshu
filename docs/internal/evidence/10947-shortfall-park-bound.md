# Code-call shortfall park 30-minute bound (#10947)

The enforced-CALLS shortfall park held a batch open across cycles with no
elapsed cap: a deleted endpoint in a never-superseded generation parked one
partition forever. The bound stamps the first park durably
(`first_code_call_shortfall_at`, COALESCE-once, migration 313) and, past
`crossscope.ProducerReadinessMaxWait` (30 minutes elapsed), dead-letters one
`sharedintent.UnroutableRow` per active intent through the unroutable sink
before the shared completion path runs. Every other outcome keeps the batch
parked (non-shortfall error, no stamp port, stamp failure, no anchor yet,
bound not reached, no sink) or fails the cycle before anything completes
(dead-letter write failure). Two new fail-closed paths carry pinned tests.

## No-Regression Evidence: the bound adds one small statement per parked cycle, zero on success

`expireShortfallParkIfDue` runs only inside the `writeActiveRows` error
return (`runner.go`: the call sits under `if err != nil`), so a successful
visit executes zero added statements, zero added scans, and no new query
plan. The added work exists only while a batch is already parked:

- one `UPDATE shared_projection_intents SET first_code_call_shortfall_at =
  COALESCE(first_code_call_shortfall_at, $1) WHERE intent_id = ANY($2)
  RETURNING intent_id, first_code_call_shortfall_at` per parked cycle, plus
  an in-memory oldest-stamp scan over the active batch;
- past the bound, exactly one dead-letter write plus the normal completion
  the batch would have taken anyway, then the park ends permanently.

Measured on a disposable PostgreSQL 18 (`postgres:18-alpine`, scratch
container, shape-equivalent table with `intent_id text PRIMARY KEY` plus the
stamp column, 50,000 rows, exact shipped statement text, 12-intent batch):

| Pass | Plan | Execution |
| --- | --- | --- |
| first stamp | Bitmap Index Scan on `shared_projection_intents_pkey`, 11 index searches, ~35 execution buffers | 0.180 ms |
| anchored re-stamp (COALESCE no-op, the steady parked-cycle state) | same PK plan, ~35 execution buffers | 0.153 ms |

The statement touches only the batch's own PK rows: O(batch) buffers,
independent of table size. At the 500 ms poll a full 30-minute park issues
at most ~3,600 such sub-millisecond statements against a handful of rows
and then terminates; an unparked lane issues none. The in-memory scan is
O(active batch) pointer comparisons. No hot loop, lease, lock, batch-size,
or concurrency knob changes, so there is nothing to re-tune.

Why this is safe to ship: the worst case adds a bounded number of
sub-millisecond PK writes on a path that previously spun forever, and the
common path is byte-identical in I/O. The focused suites pin both new
fail-closed contracts (`TestCodeCallProjectionStampFailureKeepsParking`,
dead-letter-write failure fails the cycle) alongside the existing
projection, sharedintent, worker, postgres, and crossscope suites.

## Observability Evidence: new bounded reason on existing series, new coverage row

No new instrument: the expiry counts on
`eshu_dp_shared_edge_unroutable_rows_total` (`domain="code_calls"`,
`reason="code_call_shortfall_wait_exceeded"`) and
`eshu_dp_shared_projection_stale_intents_total` under the same bounded
reason via `AcceptanceTelemetry.RecordCodeCallShortfallWaitExceededIntents`,
plus the `code call projection shortfall park expired; dead-lettered the
batch` WARN (`dead_lettered_intents`, `elapsed`, `max_wait`,
`unroutable_reason`) and two fail-closed keep-parked WARNs. The dead
letters stay queryable in `shared_projection_unroutable_intents` under the
bounded reason. Coverage: the `projection (code-call shortfall-park
30-minute bound, #10947)` row in
`docs/public/observability/telemetry-coverage-reducer-core.md` names both
new stage files (`bound.go`, `shortfall_wait.go`) and every signal above;
`scripts/verify-telemetry-coverage.sh` agrees with
`go/internal/telemetry/instruments.go` with no new untracked stages.
