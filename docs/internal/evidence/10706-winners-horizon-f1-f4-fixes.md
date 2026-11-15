# #10706 review fixes F1–F4: stamp ordering, committed-watermark result, docs, evidence

Refs #10706. Follow-up to the horizon lane
(`fd78d21ca2`, "Winners keyed resweep: watermark horizon plus immediate
re-tick on Bounded"). Design ruling:
`docs/internal/arbiter/9805-9975-drain-blocked-ruling-2026-11-15.md`.
Parent evidence: `9805-winners-keyed-resweep.md` and
`9805-winners-keyed-resweep-scale.md`.

## What changed

The Deep review of the horizon lane returned four findings (F1–F4) plus four
mismatched test doc-comments (P3). This note tracks the fixes, all on branch
`drive-10706-horizon`:

- F1: the tick reads its DB-clock stamp S exactly once, before admission, and
  every branch that stamps S stamps that same read — the idle tick, the
  drained keyed apply, and the over-cap full fallback. A row that commits
  mid-tick carries a later `captured_at`, so the stamp never covers a capture
  the tick did not see. Before, the idle, drained, and fallback branches each
  re-read the clock after settle, and a capture committed mid-tick could land
  under the stamp while still queued.
- F4: the over-cap fallback re-reads the committed watermark after the rebuild
  instead of echoing the stamp input, the same contract every other branch
  keeps; the bounded-gauge unit fake now carries the committed `Watermark` so
  the gauge test tracks the committed horizon instead of the Go-clock
  fallback; bounded, drained, and idle live tests assert
  `result.Watermark == DB watermark`.
- F3: the operator reference
  (`docs/public/reference/telemetry/supply-chain-impact-winners.md`) now
  describes horizon stamping and the immediate re-tick on Bounded, and the
  climb-means-drain guidance is inverted: a climbing watermark age during a
  falling-depth drain is a stall (ticks not committing), not the drain.
- F2: this note.
- P3: four test doc-comments cited test names that did not match their
  functions (missing `Horizon` infix or transposed words); all four now cite
  the function they document.

No SQL statement changed shape: the tick-clock read moved from three
post-settle sites to one pre-admission site, and the fallback gained one
re-read of the already-committed watermark. The phase sets a tick records are
unchanged.

## Concurrency

Conflict domain: the winners table, the dirty log, and the watermark
singleton (`supply_chain_impact_winners_materialization`).

- The single pre-admission stamp read runs inside the tick transaction under
  the resweep single-flight lock; no new lock, no new transaction, no
  reordered write. Admission, settle, and the stamp keep their order.
- The accepted one-tick residual is unchanged and now has executable form:
  a row whose INSERT executes before the stamp but commits after it is
  covered by that stamp while still queued, and the very next tick applies
  it (`HorizonDrainedLateCommitLive`). The idle path cannot cover a capture
  that commits after its stamp (`HorizonIdleLateCommitLive`).
- No serialization was added: the immediate re-tick on Bounded still skips
  the 30s wait while rows are queued.

## Before figures (arbiter ruling, QA 2026-11-15)

- Keyed p95 in (20,30]s vs the 15s bar: FAIL.
- Watermark age ~6.9h vs the 60s peak bar: FAIL by ~400x.
- Inflow ~1.2–1.5k rows/min; consumption ~271 identifiers/min; tick-busy duty
  ~26% (30s idle wait per ~42s period) in front of a ~1.03M-row backlog,
  oldest ~57.5h and aging at wall rate.

## After figures (local live suite, this change)

- Full `Winners|TestFleetAcceptanceGate` filter in
  `go/internal/storage/postgres` against disposable Postgres 18 (local,
  ubuntu superuser, TCP): PASS, 46.2s wall, zero failures. A first run over
  the unix socket failed only
  `TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive` on peer
  auth for its test-created `publish_proof_worker` role (environmental: that
  proof swaps a password role into the DSN, which needs password auth); the
  TCP re-run passes it.
- Focused re-run of the horizon/chunked/cap/scale/fleet-gate live proofs:
  PASS, 20.2s, including the three new live tests
  (`HorizonIdleLateCommitLive`, `HorizonDrainedLateCommitLive`,
  `FullFallbackReportsCommittedWatermarkLive`).
- `go/internal/reducer/supplychain/core` and
  `go/internal/reducer/advisory/...`: PASS.
- Failing-first proof: `TestResweepWinnersKeyedReadsTickClockBeforeAdmission`
  failed on all three paths pre-fix (clock read after admission) and passes
  post-fix; `TestResweepWinnersKeyedReportsCommittedWatermark` failed on the
  over-cap fallback pre-fix (result echoed the stamp input) and passes
  post-fix. The drained/idle subcases passed before and after: they are
  contract guards.
- Scale: `TestSupplyChainImpactWinnersResweepScaleProofLive` PASS (1.08s).
  No per-statement plan changed, so no EXPLAIN delta is claimed at fixture
  scale; the under-load numbers below are still the ones to beat.

Performance Evidence: the local live suite above (suite PASS 46.2s, focused
proofs PASS 20.2s, scale proof PASS): the reorder adds one pre-admission
clock read and one fallback watermark re-read to phases that already read
both statements, and the recorded phase sets are byte-identical
(`claim, consume, build_idents, build_active_facts, apply, commit`;
`claim, consume, stamp, commit`; `claim, consume, build_idents, full_rebuild,
commit`).

No-Regression Evidence: the winners table still equals a fresh full recompute
after every drain (`assertParity` in each live proof, including the three new
ones); the phase-shape unit tests are unchanged and green; the full-rebuild
statement is untouched.

Observability Evidence: `eshu_dp_supply_chain_impact_winners_watermark_age_seconds`
now drops on every committed tick including bounded ones (the gauge is fed the
committed `result.Watermark`); `..._chunked_ticks_total` still counts bounded
ticks; `..._tick_phase_duration_seconds{phase="consume"}` now includes the
pre-admission stamp read; the commit log still carries `bounded`,
`remaining`, `chunk_identifiers`, `chunk_kept_rows`, and
`previous_watermark_age_seconds`.

## QA re-read pending

The >=2h under-load re-read at >= observed inflow is NOT run here: same bars
as ruled (keyed p95 <= 15s, peak watermark age <= 60s, bounded
oldest-dirty-age, zero new `winners_watermark_stale`), on the QA topology.
A reader of this note must not take the local fixture suite as that proof.
