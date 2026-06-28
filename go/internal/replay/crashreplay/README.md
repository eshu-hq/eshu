# replay/crashreplay — Layer 3 crash-point replay (R-14, #4123)

Crash-point replay is the recovery half of the deterministic replay framework
(design doc `4102` §10, Layer 3). It proves that an Eshu reducer run interrupted
by a crash recovers to **exactly** the graph truth it would have produced with no
crash, and that recovery never projects the same work item twice.

## What it does

For one scenario it runs two phases against shared durable state:

1. **Crash phase.** Recorded work items drain through the real `reducer.Service`
   loop until a scripted crash fires at a controlled checkpoint. The in-memory
   worker (the reducer service goroutine) is then thrown away.
2. **Recovery phase.** The simulated clock is advanced past the lease TTL so any
   lease the crashed worker still held lapses, then a fresh reducer loop replays
   the remainder from the durable store onto the same graph.

It then asserts the recovered `Graph.Canonical()` snapshot is byte-identical to
the snapshot the same scenario produces with no crash, and that no work item was
completed more than once.

## The model

| Real system | Here |
| --- | --- |
| Postgres `fact_work_items` + lease rows | `durableStore` (survives the crash) |
| `FOR UPDATE SKIP LOCKED` claim | `durableStore.Claim` (pending or lease-lapsed) |
| Per-claim attempt counter (fencing token) | `itemState.attempt`, surfaced on the claimed `Intent` |
| Lease expiry on the wall clock | lease lapse on the injected `clock.Simulated` (R-12) |
| Committed graph writes | the in-memory canonical `schedulereplay.Graph` (R-13) |
| Process death | a recovered panic from a claim/execute decorator |

Only the lease/claim path is on the simulated clock. The reducer still reads
`time.Now()` internally for its own claim-duration and queue-wait telemetry; that
is harmless here because the harness wires no `Instruments`, so nothing records
those nonsensical (multi-year) durations.

`durableStore` is the only state shared across the two phases — that is the whole
point. A completed item is never re-handed-out (the fencing guarantee), so a
clean-boundary crash never redoes finished work; a dirty-window crash leaves a
held lease that recovery reclaims under a strictly higher attempt count and
re-projects idempotently.

## Crash checkpoints

- `CrashBeforeClaim` — after `After` items are durably completed, the next claim
  crashes. Clean boundary, no lease held across the crash.
- `CrashAfterApply` — the `After`-th item (zero-based) is projected to the graph
  and then the worker dies before the ack, leaving a held lease. This is the
  post-lease-pre-complete window recovery must repair.

## Using it

```go
items, _ := schedulereplay.LoadWorkItems(cassettePath)

baseline, _ := crashreplay.RunToCompletion(ctx, crashreplay.Config{
    Items: schedulereplay.ScheduleInOrder(items),
})

out, _ := crashreplay.RunWithCrash(ctx,
    crashreplay.Config{Items: schedulereplay.ScheduleInOrder(items)},
    crashreplay.CrashPoint{Kind: crashreplay.CrashAfterApply, After: 2},
)
// out.Snapshot == baseline, out.Report.DoubleAcks == 0,
// out.Report.MaxAttempt >= 2 (the recovery reclaim).
```

`RunWithCrash` fails loudly if the crash point never fires (so a misconfigured
scenario cannot pass as a green no-crash run) or if recovery does not drain.

## Scope

Crash injection runs single-worker, so the simulated crash is deterministic and
the snapshot is byte-stable. The real concurrent `SKIP LOCKED` claim path under
genuine Postgres contention — lease expiry on the real wall clock, the attempt
counter at the database — is intentionally **not** modeled here. That is the
irreducible remainder owned by R-15's real-Postgres contention gate
(`go/internal/storage/postgres/reducer_queue_contention_gate_test.go`, design doc
`4102` §10.1). This package does not claim to subsume it.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/crashreplay/ -count=1
```

No Postgres, graph backend, or Docker required.

## Evidence

- **Conflict domain.** The reducer claim/lease queue: one `durableStore`
  conflict domain over `fact_work_items`-analog records, claimed through the real
  `reducer.Service` loop. Lease settings: single worker (`Workers: 1`), a
  1-minute simulated lease TTL, lease expiry driven by `clock.Simulated` (R-12),
  fencing via the per-item attempt count.

- No-Regression Evidence: this package is a new offline, credential-free test
  gate. It is imported only by tests/gates, never by `cmd/*` or runtime wiring,
  so it changes no production hot path. `go test -race
  ./internal/replay/crashreplay/ -count=1` is green; the crash/recovery
  invariant is proven by mutation testing — removing the recovery clock-advance
  makes the dirty-window scenarios fail to drain, and making the store re-hand
  completed items breaks convergence — so the lease/fencing logic is
  load-bearing, not decorative.

- No-Observability-Change: no operator-facing metric, span, log, or status
  surface is added or altered. The run's progress/contention signal is the
  in-test `Report` (PreCrashAcks, RecoveryAcks, ReclaimedAfterCrash, MaxAttempt,
  DoubleAcks) plus the byte-identical recovered-vs-no-crash canonical snapshot —
  assertions, not runtime telemetry.
