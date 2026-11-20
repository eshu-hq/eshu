# #10876 implementation: waiting set-aside with in-transaction wakes

Refs #10876. Part of #10860 (phase 2, step 2). Implements prior results
1–3 (design, census, theory proof).

## What changed

One way of waiting. A claimed intent whose handler reports a non-counting
readiness miss is parked in `reducer_waiting_items` (migration 316) keyed by
waits_for (kind + JSON key: scope, generation, and producer domain or fence
domains where they narrow the wake). The queue row stays `retrying` in its
existing class with `visible_at` at the 10-minute backstop, so the claim
ranking never sees a new status and the attempt budget never moves.

- `go/internal/storage/postgres/reducer_waiting.go`: closed `waits_for_kind`
  enum (7 kinds), `WaitingDeferralMap` — one row per non-counting class plus
  own-gate counting classes (never parked) and write-superseded classes
  (never a wait) — with the providing save per row (criterion 1). The
  completeness tests fail on any unmapped class.
- `reducer_waiting_store.go`: set-aside insert, shared satisfaction
  predicate, shared in-transaction wake, winners range wake, per-reason
  counts.
- `reducer_waiting_audit.go`: 5-minute `ReducerWaitingAuditor` (obligation
  pattern), `AuditWaitingItems` with missed-wake counting and reconciliation.
- Wakes wired into the providing saves: relationship activation
  (`activateWithConsumerWake`), phase publish
  (`GraphProjectionPhaseStateStore.Upsert`), projector Ack activation,
  winners resweep, and the two narrowed producer-terminal reducer ACKs.
- `go/internal/telemetry/instruments_reducer_waiting.go`:
  `eshu_dp_reducer_waiting_wake_total{site,outcome}`,
  `eshu_dp_reducer_waiting_missed_wake_total`,
  `eshu_dp_reducer_waiting_items{reason}`,
  `eshu_dp_reducer_waiting_oldest_age_seconds{reason}`; coverage row added.
- `go/cmd/reducer/waiting_auditor.go`: audit loop started beside the stale
  sweeper.

## Criterion evidence (all live-Postgres, disposable DB)

- Set-aside: `TestWaitingSetAsideParksWithBackstopLive` — parked row is
  `retrying` in `generation_activation_not_ready`, attempt 1, visible_at
  now+10m (not the 30s backoff), exactly one wait row; 5 Fail cycles past
  `MaxAttempts: 3` stay retrying at attempt 1.
- Both orders: `TestWaitingBothOrdersEquivalenceLive` — waiter-first
  (claim, park, activate, claim, succeed) and input-first (activate, claim,
  succeed) converge on byte-equal terminal rows at attempt_count 1; the
  woken row is claimable at once, not after the backstop.
- Audit: `TestWaitingAuditFindsMissedWakeLive` — two planted satisfied waits
  (one hour-old, one fresh) reopen in one pass; missed = 1; a second pass
  reopens nothing (no double-start).
- Concurrency: `TestWaitingConcurrentClaimAndWakeLive` (`-race`) — 4
  competing claimers on independent connections plus a mid-flight provider
  commit and a register-after-commit waiter: 8/8 succeed exactly once,
  attempt 1, zero lingering waits.
- Defer share: `TestWaitingDeferShareImprovementLive` — 12 waiters:
  BEFORE = 36 wasted re-runs over 3 timer horizons (every re-run parked
  again); AFTER = 12 runs, 12 successes, 0 re-parks, attempt frozen.
  Production before = 7,965/24,982 = 31.9% deferred (#10876 issue figure,
  #10862 baseline method); production after needs a live QA window to
  re-measure and is coordinator scope.
- Counts: `WaitingCounts` pinned on the parked set (1 row, count 12) and
  empty after the drain; the auditor records the gauges per cycle.

## Deliberate deviations from the design notes

- Wake SET keeps `status = 'retrying'` instead of the ruled `pending`:
  pending costs one attempt on the next claim (claim CASE only freezes
  retrying non-counting rows), so a pending-wake burns a budget unit per
  park cycle and an elapsed-bounded wait dead-letters after maxAttempts
  parks. Proven by the both-orders test (pending ends at attempt 2,
  retrying at attempt 1). Ack clears the class on success; a re-Fail
  overwrites it. The replay-reset lockstep does not apply (different
  lifecycle: recovery reset vs wait reopen).
- Foreign-target waits (`target_committed` without a same-scope provider)
  resolve through the audit terminal-progress fallback plus the 10-minute
  backstop, not an in-transaction wake: no single providing save exists in
  this repo for a foreign-scope evidence commit, and the ACK ordering is
  unproven there. Same for the fence/event kinds, which keep the existing
  completion-event fanout (not re-wired: the fanout query is pinned by
  shape tests and owned by another lane).

## No-Regression Evidence (#10876):

Harness: disposable postgres:18-alpine (Docker loopback), production
bootstrap schema with migration 316 applied, `go test -race`. Input shape:
8–12 parked reducer waits (`generation_activation_not_ready`) per test, 4
competing claimers on independent connections, one mid-flight provider
commit (projector Ack). Baseline (timer path on the same seed): 36 wasted
re-runs over 3 backstop horizons for 12 waiters, 0 successes. After
(waiting set-aside + in-transaction wakes): 12 runs, 12 successes, 0
re-parks; both claim orders converge on byte-equal terminal rows at
attempt_count 1; 8/8 succeed exactly once under `-race` with zero lingering
wait rows; the audit reopens 2 planted satisfied waits (missed = 1) and a
second pass reopens nothing. Claim-path cost is unchanged by construction
(no claim predicate changed; theory proof measured claim cost neutral to
within noise across set-aside shapes, and the wake is scope-pinned at ~0.1
ms). Terminal counts: the waiting table is empty after every drain, queue
rows succeeded. The change is safe because every reopen re-checks the
provider predicate (the claim gate and the handler re-verify exact
readiness), the 10-minute backstop bounds any missed wake, and
elapsed-bounded classes keep their bounds (the retrying-preserving wake
never burns the attempt budget, unlike a pending-wake).

Observability Evidence (#10876):
`eshu_dp_reducer_waiting_wake_total{site,outcome}`,
`eshu_dp_reducer_waiting_missed_wake_total`,
`eshu_dp_reducer_waiting_items{reason}`,
`eshu_dp_reducer_waiting_oldest_age_seconds{reason}`; the auditor logs one
INFO line per reopening cycle and records the per-reason gauges each cycle;
coverage row added to `telemetry-coverage-reducer-core.md` and
`scripts/verify-telemetry-coverage.sh` agrees with no untracked stages.

## Verification

- `go test ./internal/storage/postgres/ -run 'TestWaiting|...'` (touched
  surfaces incl. enrollment, activation wake, projector Ack, phase store,
  manifest): PASS.
- Concurrency test with `-race`: PASS.
- `go test ./internal/telemetry/`: PASS.
- `scripts/verify-telemetry-coverage.sh`: agree, no untracked stages.
- `git diff --check`: clean (run before commit).
- NOT run: `make pre-push` / `make pre-pr` (coordinator owns the floor),
  full postgres package suite, production QA re-measurement.

## #11000: cross-scope producer-terminal waits re-keyed to producer domain

Refs #11000. Part of #10860. The cross_scope_producer_not_ready class
parked scope-keyed waits (`{scope_id, generation_id}`) that no producer
commit in its own scope could match, and the audit predicate checked the
consumer scope for a producer terminal row, so it could never be
satisfied either: only the 10-minute backstop converged these waits. The
golden-corpus gate drain left 2 residual
`ci_cd_run_correlation/retrying/cross_scope_producer_not_ready` rows
waiting on `container_image_identity` (issue evidence; the same gate on
bare base drains to zero).

### What changed

- Park: `failIntent` extracts per-domain sets via `errors.As` on
  `crossscope.ProducerNotReadyError` with the CompletionEdges
  catalog-derivation fallback for the consumer; neither yielding a domain
  keeps the existing timer path (no un-wakeable wait is parked).
  `setAsideWaiting` parks one domain-only wait row (`{"producer_domain"}`)
  per domain; the row keeps the waiter's own scope/generation columns for
  observability.
- Wake (reducer Ack): `wakeProducerTerminalAfterAck` keeps the narrowed
  same-scope wakes byte-identical and adds domain-only wakes for acked
  intents whose domain is a CompletionEdges producer, regardless of the
  acked scope. Synchronous post-commit, the established #10876 reducer-Ack
  pattern (ReducerQueue holds no transaction).
- Wake (projector Ack): the generation activation transaction also wakes
  the domain-only waits for the producer domains mapped to the acked
  scope's collector kind, alongside the generation_active wake. This wake
  is load-bearing: the producer reducer row commits before its generation
  activates.
- Audit and set-aside re-check: domain-only producer rows evaluate the new
  `waitingSatisfiedQuery` branch, which replicates the handler quiescence
  rule for the mapped kind (zero registered scopes, or at least one
  quiescent-active scope, reads satisfied; unmapped domain counts as
  satisfied) through the one new exported accessor
  `crossscope.CollectorKindForProducerDomain`. Scope-keyed narrowed rows
  keep the legacy branch byte-identical.
- The producer-to-collector-kind map moved from
  `scope/completion` to the `crossscope` leaf as the single source for
  the floor, the park, the audit, and both Ack-path wakes; the floor
  reads it through the same accessor, so the halves cannot drift.
- `WaitingDeferralMap`: the class row moves to WakeSite in_tx, provided
  by the producer reducer Ack plus the projector-Ack generation
  activation (audit quiescence fallback + backstop). Closed enum and table
  shape unchanged; no migration; 5-minute audit and 10-minute backstop
  unchanged; attempt-count freeze untouched (class stays non-counting).

### Criterion evidence (all live-Postgres, disposable DB)

- Park shape: `TestCrossScopeProducerWaitParksDomainOnlyLive`.
- Acceptance with the audit disabled: `...ReopensOnReducerAckLive`
  (reducer terminal commit) and `...ReopensOnProjectorActivationLive`
  (generation activation).
- Audit satisfiability by a real producer commit (busy then quiescent):
  `TestCrossScopeProducerAuditSatisfiesQuiescentWaitLive`.
- Both orders converge on the same terminal shape:
  `TestCrossScopeProducerBothOrdersLive`.
- Per-domain independence across the two declared producers:
  `TestCrossScopeProducerMultiProducerLive` (batch Ack releases one,
  activation releases the other).
- Non-interference with the byte-identical narrowed rows:
  `TestCrossScopeProducerWakeNonInterferenceLive`.
- Concurrency: `TestCrossScopeProducerConcurrentWakeRaceLive` (`-race`):
  4 parkers on independent connections plus racing reducer Ack and
  projector activation; all reopen exactly once, attempts frozen, zero
  lingering waits.
- Unit: park-domain extraction (direct/wrapped/fallback/empty),
  domain-only key bytes, CompletionEdges producer universe, deferral-map
  row pin, kind-map pins against the scope constants.

### Theory proof (disposable PG, before implementation)

- Domain-only reopen rides `reducer_waiting_items_wake_idx` (index
  condition on kind + key): no new index.
- Mapped audit branch probes `ingestion_scopes` with the quiescent
  anti-join (same shape as the measured 0.30 ms #5709 probe: registered
  scopes of the kind, active generation, no live projector work).
- Unmapped domain answers through the index-backed empty-kind guard; its
  fact_work_items leg never executes.

### Gate drain (B-7 golden-corpus gate, `ESHU_GRAPH_BACKEND=neo4j`)

- BEFORE (issue evidence, #10876 lane): 2 residual
  `ci_cd_run_correlation/retrying/cross_scope_producer_not_ready` rows on
  `container_image_identity`; bare base drains to zero.
- AFTER (this lane, single run, elapsed 380 s): 451 pass, 0 required-fail,
  2 advisory-warn (phase_bootstrap 11 s and phase_first_drain 304 s timing
  bands only — bootstrap runs pure DDL with no DDL change here, so both
  are loaded-machine variance, not a code regression).
  `fact_work_items_residual: residual=0` on all four drains (first plus
  three maintenance), dead_letter=0; the first drain reached terminal zero
  inside ~5m04s, within the 10-minute bound, with the 5-minute audit
  unchanged. The waiting auditor reopened nothing in the run (zero
  audit-pass lines): convergence came through the pipeline, not the audit
  or the backstop. Zero `cross_scope_producer_not_ready` residuals.

## No-Regression Evidence (#11000):

Harness: disposable postgres:18-alpine (Docker loopback), production
bootstrap schema with migration 316 applied. Focused suites, all green:
cross-scope park/acceptance/audit/orders/multi/non-interference plus
`-race` concurrency; `crossscope` and `scope/completion` packages;
waiting/cross-scope/winners/deferral-map suites; Ack/Fail suites
(single, batch, projector Ack, journal, lock order, contention probe);
`cmd/reducer`; consumer handler suites. Attempt budget frozen on every
parked row (attempt_count stays 1 across re-parks); the narrowed
same-scope waits keep byte-identical keys and wakes. Claim-path cost is
unchanged by construction (no claim predicate changed; the added audit
branch runs only for parked domain waits and reuses the measured probe
shape; reducer-Ack wakes add one index-probed wake per acked producer
domain). Terminal counts: the waiting table is empty after every drain,
queue rows succeeded. The change is safe because every reopen re-checks
the provider predicate (the claim gate and the handler re-verify exact
readiness), the 10-minute backstop bounds any missed wake, and the audit
replicates the floor's own quiescence rule through the shared map.

Performance Evidence (#11000): no new hot-path query shape: the Ack-path
wakes are equality probes on the existing `reducer_waiting_items_wake_idx`;
the audit branch reuses the measured quiescence probe shape (0.30 ms in
the #5709 evidence) and runs only over parked domain waits; the
projector Ack adds zero queries for unmapped scope kinds and one bounded
wake per mapped producer domain. `scripts/verify-performance-evidence.sh`
agrees; `scripts/test-verify-performance-evidence.sh` passes. Full
before/after wall-time comparison is the golden-corpus gate drain below.

Observability Evidence (#11000):
`eshu_dp_reducer_waiting_wake_total{site="in_tx",outcome}`,
`eshu_dp_reducer_waiting_missed_wake_total`,
`eshu_dp_reducer_waiting_items{reason}`,
`eshu_dp_reducer_waiting_oldest_age_seconds{reason}` — the same four
signals as #10876, no new metric; the two saves report under the existing
`in_tx` site label and the audit under `audit`.
`scripts/verify-telemetry-coverage.sh` agrees with no untracked stages.

## Missed wake in the winners resweep path (golden-corpus gate rerun)

Root-Cause Evidence: advisory waits parked at 01:58:01 against watermark
01:57:31 while keyed resweeps committed every 30 s without waking them;
the 5-minute audit reopened all 4 at 02:03:01 (reopened=4, missed_wake=0)
yet all 4 were still residual at the drain deadline. Two defects combined:

1. The keyed tick (`ResweepWinnersKeyed`, the 30 s production cadence)
   stamped the watermark every tick but never called `wakeWinnersWatermark`;
   only the rarely-taken full `ResweepWinners` woke. Every gate resweep was
   `path=keyed`, so no wake ever fired.
2. The woken row's bound ratcheted past the stamp that woke it: the wake
   set `reopened_at` to the wake wall time, the claim derives the handler
   bound from it, and the stamp predates it by definition, so the next
   claim re-parked with a newer key. The audit's equality reopen does the
   same, which is why the audit-reopened rows were still residual: they
   were reclaimed and re-parked before any new stamp covered the audit
   time, and the next tick (no wake) left them parked to the deadline.

Fix: the tick wakes in-transaction before commit with the re-read committed
watermark (`wakeWinnersWaits`, `lock_timeout=2s`, 55P03/40P01 to the
retryable class-less `WinnersWakeContentionError` per #7597); the winners
range wake aligns `reopened_at` to the covering stamp so the next claim's
handler recheck passes; the `waitingSatisfied` winners branch uses `>=` to
agree with the handler (`!watermark.Before(required)`).

Proof: `TestWinnersKeyedTickWakesParkedAdvisoryWaitLive` (RED before: wait
rows survive the tick; GREEN after: woken in-tx, bound covered) and
`TestWinnersAuditReopenedWaitConvergesOnNextTickLive` (replays the gate's
audit-reopen-then-residual path to convergence on the next tick).
