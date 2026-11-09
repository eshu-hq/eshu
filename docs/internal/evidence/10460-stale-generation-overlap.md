# #10460 Live Concurrent Overlap Proof: Stale-Commit-Last for the Stale-Generation Fence

`TestStaleGenerationPassOverlapsNewerCommitLive` drives the exact concurrent
interleave the sequential #10422 proof cannot: a stale generation's pass has
its bulk findings insert in flight (uncommitted) while the lifecycle advances
and the newer generation's pass records and commits; the stale pass then
records last, sees the newer generation, skips its per-scope pass-state
write, and still commits its own per-generation findings.

## Proven Interleave

Same scope, two generations, two passes, real Postgres:

1. `gen-1` active; a `gen-1` partial pass records the scope stuck.
2. A stale `gen-1` replay (partial, higher fencing token) starts on an
   instrumented transaction: its first `fact_records` Exec marks the bulk
   landed, and its pass-state record statement parks on a gate. Bulk rows
   are written but uncommitted.
3. The test advances the lifecycle (`gen-1` superseded, `gen-2` active with
   a later `ingested_at`) and runs the `gen-2` complete pass to commit: the
   scope row is cleared. The two passes take different per-`(scope,
   generation)` conflict-domain locks and never wait on each other, so the
   newer commit genuinely lands mid-stale-pass.
4. The gate releases; the stale record statement executes on the freshest
   READ COMMITTED snapshot, sees `gen-2`, and skips. The stale transaction
   commits its `gen-1` findings.

Assertions: the stale result reports `PassStateSkipped=true`, the scope row
stays recorded-clear (the `gen-2` clear untouched), and the stale `gen-1`
finding rows are committed (`fact_records` holds active `gen-1` rows).

## Determinism

No sleeps. The rendezvous is a transaction-statement gate inside the test's
own `Beginner` wrapper: the stale bulk completion closes `bulkDone`, and the
post-bulk record statement blocks until the test closes `release` after the
newer commit. Program order on the stale transaction makes the newer commit
strictly precede the stale record statement on every run. The wrapper
forwards `Commit`, `Rollback`, and the optional winners capturer, so the
parked pass captures exactly what the plain transaction would.

## No-Regression Evidence

- Backend: disposable PostgreSQL 18.6, schema migrated to the lane head
  (312 migrations).
- Input shape: synthetic overlap proof (one scope, `gen-1` superseded
  mid-pass, `gen-2` active with a later `ingested_at`, strict fencing
  tokens mirroring the production sequence).
- Terminal counts: `TestStaleGenerationPassOverlapsNewerCommitLive` PASS
  4.19s cold (fresh template clone) and 3/3 PASS under `-race`
  (4.77s + 0.18s + 0.19s); sibling sequential proof
  `TestStaleGenerationPassPreservesScopePassStateLive` PASS 4.13s in the
  same run.
- Mutation (acceptance criterion 1): with the record call temporarily moved
  back ahead of the bulk writes, the overlap proof FAILS at the skip
  assertion (`PassStateSkipped=false`: the early record ran before the
  activation and recorded instead of rendezvousing), then PASSES again
  after the restore (production file byte-identical: only the test file
  differs).
- Gate enrollment: the proof rides the reducer contention gate via the
  `-run` filter and `--select` verifier (exact test name),
  `scripts/lib/reducer_contention_inventory.json`, and
  `TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate` GREEN;
  `scripts/lib/reducer_contention_events.py` inventory check GREEN
  (83 files verified).

## Residual Window (acceptance criterion 4)

A commit landing between the record statement and this transaction's commit
can still interleave: the fence decides once per statement, so a newer
activation committing in that window is unseen and the stale upsert's token
guard (`existing <= EXCLUDED`) admits the higher-token stale pass. No bulk
work runs in that window (the record is the last statement before commit),
and the token guard additionally blocks a late committer whose token is
lower than the row's. This window is stated, not tested: closing it needs a
commit-time recheck, which is a further change, not this proof.
