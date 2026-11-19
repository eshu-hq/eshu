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
