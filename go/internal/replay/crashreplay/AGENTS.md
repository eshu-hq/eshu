# replay/crashreplay — agent scope

## Owned surface

- `go/internal/replay/crashreplay/` — the R-14 Layer 3 crash-recovery gate.

## Non-negotiable invariants

- The gate asserts **crash recovery is correct and idempotent**: the recovered
  `Graph.Canonical()` snapshot MUST be byte-identical to the no-crash snapshot,
  and no work item may be completed more than once (`Report.DoubleAcks == 0`).
- It MUST drive the **real** `reducer.Service` loop. The crash is injected by
  decorating the work source / executor, never by reimplementing the claim,
  execute, or ack steps. Exercising the real claim/execute/ack path is the point.
- `durableStore` is the **durable Postgres analog** that survives the crash. A
  completed item MUST never be re-handed by `Claim` (the fencing guarantee); a
  lease-held item becomes claimable only once its lease lapses on the injected
  `clock.Clock`. Do not weaken either rule to make a test pass.
- Lease expiry MUST go through the injected clock (R-12), not `time.Now()`.
  Recovery advances the simulated clock to lapse the crashed item's lease; if you
  remove that advance, the dirty-window scenarios MUST stop draining (proven by
  mutation — keep it that way).
- The crash sentinel panic MUST be the only panic the run goroutine swallows.
  Any other panic MUST be re-raised so a real bug is never hidden.
- Crash runs stay **single-worker** for determinism. Do NOT add concurrent crash
  injection here to "also cover contention" — the real concurrent `SKIP LOCKED`
  path under Postgres contention is R-15's gate (design doc `4102` §10.1). Do not
  claim this gate subsumes it.
- The gate MUST keep its teeth: the non-idempotent-applier test MUST observe a
  snapshot divergence after a crash, and `RunWithCrash` MUST fail loudly when the
  crash point never fires. If a refactor makes either pass vacuously, fix the
  harness — do not delete the negative test.
- Inputs come from the committed offline-tier cassette via
  `schedulereplay.LoadWorkItems`; do not synthesize work items inline.
- It MUST stay credential-free: no Postgres, no graph backend, no Docker. The
  gate runs in the default `go test` pass.

## Skill routing

- `concurrency-deadlock-rigor` for the durable store, lease/fencing model, the
  reducer loop drive, and the crash/recovery phases.
- `eshu-golden-corpus-rigor` for the snapshot/cassette assertions.
- `golang-engineering` for Go edits and tests.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test -race ./internal/replay/crashreplay/ -count=1
```
