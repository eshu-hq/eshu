# #11053 main-red: cross-scope producer wake + sidecar count arms

Refs #11053. The readiness, contention, and scheduled gates went red on
every main wave for a day after #11008 parked deferred reducer rows and
#10971/#10878/#10877 added sidecar tables without updating the proofs.
This record covers the one runtime change in the fix (the cross-scope
producer-terminal wake); the rest is fixture, pin, and count-arm repair.

## What changed

`wakeProducerTerminalAfterAck` (reducer single Ack and AckBatch) wakes one
more waits set per acked narrowed producer domain: scope-bound
`producer_domain_terminal` waits naming that domain in any scope
(`wakeProducerTerminalCrossScopeQuery`). Before, a catalog correlation
parked on a repository-scope workload could never be satisfied (the
satisfaction probe looks in the waiter's scope) nor woken (the narrowed
wake names the acked scope, the domain wake matches domain-only keys), so
it slept to the 10-minute backstop on every cycle.

## No-Regression Evidence:

No-regression claim: one extra wake statement per acked narrowed producer
domain, index-driven over the parked set only.

- Backend: PostgreSQL 18.6 (Debian 18.6-1.pgdg12+2, `ci-postgres-bookworm`
  digest `afc7e2d4`), disposable database, shipped migration-316 DDL.
- Input shape: one parked scope-bound producer-terminal wait (the
  contention-proof shape), then the empty parked set (the steady state).
- `EXPLAIN (ANALYZE, BUFFERS)` of the wake query (same predicate and join
  shape as the shipped statement, equivalent table DDL): 24 shared buffers
  hit with one row parked (Index Scan on
  `reducer_waiting_items_wake_idx` for the kind, then PK probes for the
  reopen and the clear), ~20 shared buffers hit with nothing parked.
  Sub-millisecond both ways; cost follows the parked set, never the fleet.
- End to end on the same backend: the concurrent-workers contention proof
  went from a 60 s timeout to 4.3 s, and the full `TestReducerContentionGate`
  filter (79 top-level proofs) plus the retention, projector, fan-in, and
  memo suites pass (286 green, 0 red locally).
- The projector Ack journal cost gate still passes with its bounds (WAL +6,
  blocks +40): the reducer-ack wake does not run on the projector path.

## Observability Evidence:

No new metric. The wake reports through the existing waiting-wake
telemetry (`recordWaitingWake` with site `in_tx` and outcome
`reopened`/`noop`/`error`), the same series the narrow and domain wakes
already emit, so the cross-scope leg is visible in the same dashboard as
the wakes it completes. A missed cross-scope wake still surfaces through
the audit's `missed` counter and the 10-minute backstop spin.
