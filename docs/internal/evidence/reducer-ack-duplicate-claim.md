# Reducer Ack Duplicate-Claim Evidence

Issue #6162. The Ifá fault-injection cell `cell_expirelease` forces
`claim_until = now()` on every claimed reducer row without killing the handler,
so the claimer legitimately re-claims a work item whose first handler is still
in flight. Both handlers finish and the acker flushes one batch holding that
work item twice, under two claim identities.

The conflict domain is a single `fact_work_items` row, owned by at most one
current claim — `(lease_owner, last_attempt_at, container_image_identity_claim_epoch)`
with a live `claim_until` — but concurrently executable by two worker goroutines
in one reducer process after a lease expiry and reclaim. The transaction scope is
one ack statement per domain family; the retry scope is the claimer's poll loop.
The idempotency key is the work item id plus its claim identity: an ack carrying
a superseded claim identity matches zero rows.

`splitReducerAckBatchIntents` used to reject that batch with a hard error. Two
consequences, and the second is the stall: the whole batch was discarded,
including the surviving claim's ack, and the error does not wrap
`reducer.ErrExecutionClaimRejected`, so `Service.runBatchConcurrent`'s acker took
its fatal `appendErr`/`cancel` branch rather than its claim-rejected branch. The
reducer exited, the row stayed `claimed` with nothing alive to reclaim it once
`claim_until` lapsed, its dependent `gcp_relationship_materialization` stayed
`pending` behind the same partition key, and the drain gate ran out its bound
with `dead_letter=0`.

## Failure captured in CI

Run 35617012182, `fault-injection (shard 2/4)`, artifact
`ifa-fault-injection-shard-2-attempt-1-failure`. Last line of
`logs/reducer-expirelease.log`:

```
2026/09/21 15:11:51 ERROR reducer failed error="batch ack reducer work: batch ack
reducer work item \"reducer_gcp_project_supply-chain-demo-project_cassette-gcp-scd-gen1_gcp_resource_materialization_gcp_resource_materialization_gcp_project_supply-chain-demo-project\"
has conflicting claim epochs or domains"
```

Preceded by two `gcp resource materialization completed` lines for the same
scope, 2.309s and 1.694s, on workers 3 and 0 — the two concurrent handlers. That
ERROR is written through the plain `log` path rather than the structured one, so
a scan for `"severity_text":"ERROR"` reports zero ERROR lines while it sits in
the same file.

## Lease safety

Postgres already fenced correctly. `ackReducerWorkBatchQuery` locks on
`last_attempt_at = requested.claimed_at AND stage = 'reducer' AND lease_owner = $2
AND status IN ('claimed','running') AND claim_until > clock_timestamp()`, and the
container-image statement pairs `container_image_identity_claim_epoch` with
`last_attempt_at` per claim group. A superseded ack therefore matches zero rows by
construction. No claim, lease, or ack SQL changed; the split now keeps the newest
claim, drops the superseded one, and counts it, which surfaces as
`ErrReducerClaimRejected`.

`TestReducerQueueAckBatchFencesSupersededClaimLive` proves it against real
Postgres 16.15, driving the cell's own `UPDATE fact_work_items SET claim_until =
now() WHERE stage = 'reducer' AND status IN ('claimed','running')`:

- the superseded claim alone acks nothing and leaves the row `claimed` with its
  lease owner intact;
- the batch carrying both claims acks the survivor, ends the row `succeeded`
  with `lease_owner` cleared, and returns a claim rejection the batch acker
  survives;
- both pending orders resolve identically, which matters because the acker's
  pending slice order follows whichever worker goroutine finishes first.

Mutation-proven rather than asserted. Restoring the pre-fix guard turns the live
proof red in both orders and the two hermetic cases red, with `go vet` exit 0 on
the mutant so the red is behavioural and not a build failure. The two negative
controls — a same-id/different-domain pairing, and an identical duplicate that is
deduped without supersession — stay green under both, so they are not satisfied
by the change itself.

No serialization was used. Worker count, batch size, poll interval, lease
duration, and the drain bound are untouched; two workers still execute the same
intent concurrently under this fault, which is what the cell injects and what the
idempotent graph writes already tolerate.

Benchmark Evidence: `BenchmarkSplitReducerAckBatchIntents`, Go 1.27.1
darwin/arm64, Apple M1 Max, `-benchtime=2000x -count=6`, median of six, baseline
`origin/main` at `f6ce3e59a` in a separate worktree on the same machine, same
input generator, no duplicates (the hot path):

| batch | baseline ns/op | candidate ns/op | delta | baseline allocs/op | candidate allocs/op |
| --- | --- | --- | --- | --- | --- |
| 16 | 3,952 (3,653–4,253) | 3,258 (3,142–3,612) | −17.6% | 22 | 7 |
| 64 | 13,062 (12,890–14,571) | 9,825 (9,400–10,075) | −24.8% | 70 | 7 |
| 256 | 46,810 (46,328–47,386) | 34,812 (33,826–35,776) | −25.6% | 262 | 7 |

Ranges do not overlap at any size. The gain is incidental, not the point: the
previous map stored whole `reducer.Intent` values, so every distinct work item
copied the struct into the map; keeping an index into the retained slice removes
that copy and leaves allocations flat in batch size. Bytes per op are unchanged
within noise — the same intents are still retained.

Observability Evidence: a dropped superseded ack now reaches the existing
`reducer batch ack rejected stale claim` WARN, with
`failure_class=execution_claim_rejected`, `queue=reducer`,
`pipeline_phase=reduction` and `batch_size`, emitted by
`Service.logReducerAckClaimRejected`; each item in the batch is recorded through
`recordBatchAckOutcome` as `ack_outcome_unknown`. Before this change the same
condition produced one `reducer failed` line and process exit. No metric
instrument, label, span, route, queue table, or runtime knob was added or
renamed. At 3 AM the signal an operator reads is that WARN plus a reducer that is
still polling, in place of a dead worker pool and a row stuck `claimed`.
