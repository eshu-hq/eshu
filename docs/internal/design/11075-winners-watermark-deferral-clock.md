# #11075 design record: winners-watermark stale clock is a defect — deferral measured from first deferral

Refs #11075. Part of #10860. Parent context: #10839 (Gate B leg-(b)).
Arbiter framing: `gateb-A2-packet-disposition-2026-11-20.md` Decision 3.
Phase-1 evidence: comment 7052476882 on #11075 (bounded R(t) + queue-composition measurement, SELECT-only vs QA replica) and the phase-1 draft `docs/scratch/11075-winners-watermark-phase1-design-draft.md` (uncommitted, worktree `drive/11075-winners-watermark-phase1`); this record supersedes that draft with the phase-2 decision and implementation.

**Routing note:** this answer changes clock semantics (not the bound value, not enrollment, not concurrency). Per Decision 3 it routes to the arbiter as a design ruling on this record before the implementation lands. Nothing here is pushed, PR'd, or deployed by the lane.

**Post-landing arbiter reconciliation (2026-11-23):** `docs/internal/arbiter/11075-design-record-ruling-2026-11-23.md` records the verdict Q1 = DEFECT with candidate (a) adopted as implemented in #11086, rejects (b)/(c), rules the #11151 stamped fast path counts against unchanged T-outcome, and keeps #11075 open on AC3 (one fresh A2-identical QA packet at unchanged bars).

## Q1 verdict: DEFECT, candidate (a)

Pre-claim queue residency consuming `winnersWatermarkMaxWait` is a defect. The stale class exists to assert "the maintainer is down" (`handler.go:26-36`: 60 missed 30 s ticks); phase 1 measured the maintainer advancing (A2 51.6 min, 0.860x wall) while every first evaluation classified stale and burned budget. An unclaimed row has performed zero deferrals, yet the row-age clock (`now − cycleStart`) bills it for queue wait. An intended-design answer would have to certify born-stale churn (148/154 attempt-1 rows) as correct while the bound's rationale names a different cause — the same contradiction the arbiter already found.

### Why the fix needs durable state (rejected simpler shapes)

A handler-only rule without new durable state cannot implement candidate (a) correctly. Proven from source in this lane:

- The non-counting freeze (`reducerClaimAttemptCountCaseSQL`) keeps `attempt_count = 1` across every not_ready deferral, so attempt-gating (`stale ⟺ attempt ≥ 2`) never fires under a dead maintainer — infinite honest wait, violating the dead-letter property.
- The claim maps `last_attempt_at → ClaimedAt`, overwritten on every claim, so a "clock from last claim" resets on every retry — same infinite-wait failure.
- A watermark-age gate (`stale ⟺ now − watermark > bound`) conflates slow-but-advancing with dead and fires stale at attempt 1 under the current 3 h wall−R lag, violating the acceptance's zero-first-stale outcome.

Therefore the deferral clock needs a durable first-deferral anchor. Two shapes carry it without a migration: the failure-details round-trip (chosen) vs a new queue column (rejected: migration + claim/update/scanner/contract surface for one timestamp, with live-DB proof the lane cannot fully close here).

### Chosen mechanism

- The wait error carries a machine-readable marker in its failure details: `winners-watermark-deferral: first-deferred-at=<RFC3339Nano> cycle-started-at=<RFC3339Nano>`, appended to the human-readable message the queue already stores. `QueueFailureMetadata` already propagates a cause's `FailureDetails()` into durable `failure_details` (bounded at 4096 bytes); no queue-write change was needed.
- Both claim paths (single + batch) now also select `failure_class/message/details` (COALESCEd non-NULL) and the scanner maps them onto `intent.Failure` (nil when empty). No predicate, lease, status, or ordering change; `FOR UPDATE SKIP LOCKED` and fencing untouched.
- The stale rule is `now − deferralStart > winnersWatermarkMaxWait` where `deferralStart` is the same-cycle marker, else now (first deferral). Markers from a prior cycle, unparsable markers, and future-dated markers all fall back to now: a reopen always restarts the clock (the replay already clears failure evidence; the cycle check is defense in depth). Value (30 min), enrollment (not_ready non-counting, stale counting), and the satisfaction gate (`winnersBoundStart`) are untouched.
- Dead maintainer, bounded and visible: attempt-1 defers not_ready (frozen attempt); each backstop re-entry keeps the same-cycle anchor; 30 min past the first deferral the verdict flips stale, which counts, so the budget (default 3) dead-letters visibly. No serialization: worker count, batch size, and claim ordering untouched.

### Per-candidate dispositions

- (a) Deferral from first claim/first deferral: ADOPTED (this record). Matches the bound's documentation exactly; preserves dead-maintainer dead-lettering; pre-claim residency consumes nothing.
- (b) Stale gated on R(t)-stall evidence: VIABLE, NOT ADOPTED. Stronger fidelity to "maintainer down" per-row, but couples the handler to an R(t) series (new read path, new failure modes) and still satisfies nothing alone — it renames the starvation while wall−R lag grows. Rejected for this turn; a future stall-gated refinement can layer on the same marker.
- (c) Drain/claim-ordering with clock unchanged: INSUFFICIENT ALONE, still REQUIRED as precondition. Draining restores the regime where any clock is correct, but any future backlog deeper than 30 min re-arms born-stale churn with no code defense. Operational drain stays the precondition for the acceptance packet under any answer.

## Q2 verdict: pace unchanged by this fix; catch-up still open

Phase 1 measured R frozen for 10 min (0.000x, lag +10.0 min in 10.0 min wall) after A2's 0.860x, with E−R +87.1 min on fresh evaluations and intake accumulating in staging (+24063, log flat). This fix changes verdicts, not throughput: under a slow-but-advancing maintainer rows now wait honestly as not_ready instead of churning stale, but satisfaction still needs R to close wall−R lag.

One Q2-relevant mechanism surfaced in this lane's live proof: a not_ready Fail parks the row on the `winners_watermark` wait (10-minute backstop horizon, woken in-tx by the maintainer's resweep commit), and the wake sets `reopened_at` to the covering watermark — only an advance that covers the wait's enqueue reopens it and restarts the cycle clock; a partial advance leaves the wait parked and the clock running (`reducer_waiting_winners_wake.go:41`: the wake predicate is `enqueued_at <= new watermark`). Sustained same-cycle deferral (the stale path) is reached through backstop expiry, not through wakes. Catch-up reachability (waiting vs throughput work) remains open and belongs to the maintainer lane with the A2-identical packet.

## Telemetry shipped in the same change

`eshu_dp_advisory_group_projection_first_claim_lag_seconds` (cycle start → latest claim — `ClaimedAt` is `last_attempt_at`, overwritten per claim, so on retries this is the latest-claim lag, not the first claim's; the pre-claim half), `..._post_claim_deferral_seconds` (first deferral → verdict; the bound's numerator), `..._watermark_age_seconds` (stall signal: age growing past 1800 s), `..._watermark_gap_seconds` (per-row E−R at the verdict), and `..._watermark_waits_total{domain,failure_class,attempt}` (verdicts by class and closed attempt bucket 1/2/3plus). Stall vs slow-but-advancing reads as age+gaps series plus wait counts, not as a per-row verdict — a single evaluation cannot tell them apart, so the clock decides and telemetry distinguishes. Coverage doc row, histogram-bucket table, and the operator reference updated; the coverage verifier passes.

## NVD/GHSA inflow split

Phase-1 first-hand measurement: GHSA is ~76% of dirty inflow (log GHSA 9273 / CVE 2838; staging GHSA 32785 / CVE 12327) and entirely uncounted. Gap anatomy: (i) the tally only counts rows captured in the trailing 2 h (by design under backlog drain; table empty 31/31 pulls), and (ii) the tally predicates count NVD-proxy (`kind='fact' AND advisory_id LIKE 'CVE-%'`) and NVD-scope rows only — no GHSA predicate, bucket, or column exists (`supply_chain_impact_winners_keyed_chunk.go:217-218`, `winnersDirtyTallyCTEs`, migration 299 table). `source` cannot substitute (writer operation names).

Deliverable spec for the linked sibling issue (same-turn filing by the coordinator): migration 325 `ALTER TABLE supply_chain_impact_winners_dirty_tally ADD COLUMN ghsa_proxy_rows BIGINT NOT NULL DEFAULT 0 CHECK (ghsa_proxy_rows >= 0)`; extend `winnersDirtyTallyCTEs` with predicate `kind = 'fact' AND advisory_id LIKE 'GHSA-%'` tallied/upserted like the NVD-proxy leg; extend the trip sampler (`vulnerability/nvd_baseline_trip_sample.go`) to sum the new column; unit + live proof. The 2 h-recency filter stays (documented behavior under drain). Step B stays closed on NOT_CHECKED until the split is measured.

## Regression evidence RED→GREEN (this lane, in-worktree)

Failing-first, all in `go/internal/reducer/advisory/group/handler_test.go` (reopened-row shapes use `CycleStartedAt`, never `created_at`, as the lag numerator per the Decision-3 constraint):

- `...BornStaleUnderBacklogDefersNotReady` (attempt-1, 45 min pre-claim lag, behind watermark → not_ready): RED stale on old code, GREEN.
- `...RecentDeferralWithOldWatermarkDefersNotReady` (5 min post-claim, 85 min-old watermark → not_ready): RED, GREEN.
- `...SustainedDeferralPastBoundCountsAttempts` (45 min post-claim on stuck watermark → stale, retryable, counting): GREEN both (dead-maintainer property guard).
- `...StaleMarkerFromPriorCycleStartsFresh` (prior-cycle marker → fresh clock, not_ready): RED, GREEN.
- `...WatermarkWaitTelemetrySplitsPreAndPostClaim`: GREEN (split + verdict counts).
- `...WatermarkWaitEventuallyCountsAttempts` (pre-existing, pinned the defective row-age clock): updated to carry a same-cycle 31-minute-old marker; still pins counting past the bound.

Read path: `reducer_queue_failure_evidence_test.go` (hermetic scanner round-trip of the 14-column claim shape) RED→GREEN; ~27 hermetic claim-shape fakes moved to the 14-column shape via the `PadQueueFakeRow` convention plus two explicit row builders; telemetry registry/contract expectation extended for the `attempt` dimension.

Live proof (real Postgres, disposable bootstrap at tree migrations): `TestReducerQueueClaimCarriesFailureEvidenceLive` — enqueue → claim (Failure nil) → production handler defers not_ready → Fail parks → backstop time-travel → re-claim (Failure carries the marker, attempt frozen at 1) → handler not_ready at +1 min, stale at +31 min on the still-behind watermark → stale Fail retries (attempt 2, marker preserved) → stale Fail retries (attempt 3) → stale Fail dead-letters: exactly MaxAttempts=3 counting stale evaluations to observed `status='dead_letter'` (Decision 2 full loop; RED-first: asserting dead_letter after the first stale Fail fails with `retrying`). PASS.

Suites run: `go test ./internal/reducer/advisory/...`, `./internal/telemetry/`, `./internal/storage/postgres/ -run 'TestClaimBatch|TestReducerQueue|TestScanReducerIntent'`, `./internal/storage/postgres/fixtures/`, `verify-telemetry-coverage.sh` (agree), `git diff --check` (clean). Full `./internal/storage/postgres/` suite and `make pre-push`/`pre-pr` are coordinator promotion gates (live-DB legs run in CI).

## No-Regression Evidence (perf-evidence gate)

No-Regression Evidence: this is a correctness fix with no throughput, batching, concurrency, lock, or plan change. Baseline: origin/main claim SELECT (11 columns) and the row-age stale rule (`now − cycleStart > 30 min`). After: 14-column claim SELECT (3 additive output columns, same WHERE/ORDER BY/LIMIT/FOR UPDATE SKIP LOCKED, no new predicate or index) and the post-claim deferral rule. Backend: PostgreSQL 18.6 (Ubuntu 18.6-0ubuntu0.26.04.1, aarch64) disposable bootstrap at tree migrations (350 applied, ~1.3 s) plus Go 1.26.11 hermetic suites. Input shape: single reducer row cycled enqueue → claim → Fail(not_ready) → backstop time-travel → re-claim; hermetic 14-column claim rows for both claim paths. Terminal counts: claim returns the same single row; scanner maps 14 columns (hermetic RED→GREEN on count mismatch); live round trip PASS in ~4.4 s; handler, telemetry, fixture, and claim suites green with `-count=1`. Why safe: the success path executes zero new work (the wait split records only on the deferral branch, which already allocated and returned an error); the marker is one bounded string append inside the existing 4096-byte details cap; the claim adds output columns only. No benchmark delta is claimed because none is expected: the changed statements keep their plans.

Observability Evidence: five new signals (`first_claim_lag_seconds` — latest-claim lag on retries, `ClaimedAt` = `last_attempt_at`; `post_claim_deferral_seconds`, `watermark_age_seconds`, `watermark_gap_seconds` histograms on the `advisory-group-watermark-wait-seconds` buckets, `watermark_waits_total{domain,failure_class,attempt}` counter) recorded once per deferral verdict; unit test asserts exact per-signal sums (2640/0/6300/3600 s on the fixture) and the `{not_ready, attempt=1}` count; `verify-telemetry-coverage.sh` agrees; the operator reference documents the split and the stall-vs-slow read.

## Acceptance mapping (arbiter Decision 3)

1. Design record: this file (routes for ruling before landing).
2. Regression RED→GREEN: above; fresh A2-identical 60-min read-only packet is the coordinator's post-drain run, with the NVD/GHSA split measured per the sibling issue.
3. Thresholds unchanged and not redefined by the lane: zero first-stale classifications and R-closing stand; the deferral clock only changes which evaluations may go stale (post-claim), never the packet bars.

## Marker-loss signal (#11089)

The deferral clock anchors on a first-deferral marker in the row's failure details. When a row whose prior failure class is a winners-watermark wait class (`advisory_group_winners_watermark_not_ready` or `_stale`) reaches the handler with that marker missing, unparsable, or future-dated, the clock restarts at the current evaluation. Before #11089 nothing told an operator that apart from a genuine first wait. Each such evaluation now increments `eshu_dp_advisory_group_projection_watermark_deferral_marker_missing_total{domain,failure_class,reason}` (reason: `missing`, `unparsable`, `future_dated`). A genuine first deferral (no failure evidence), a non-wait prior class, a healthy marker, and a parsable prior-cycle marker (a designed reopen restart) stay silent. The fallback clock start and every verdict are unchanged.

Operator story: a non-zero rate means markers are being lost between verdicts. Each counted evaluation delays dead-maintainer dead-lettering by one fresh 30-minute window, so alert on a sustained rate and then inspect `fact_work_items.failure_details` for the affected domain. A marker overwritten by a non-wait failure still costs a counting attempt, so that loss only delays the dead-letter; a marker stripped without an overwrite would restart the clock for free each time, no such mechanism is known, and this counter is the detector.

No-Regression Evidence: no throughput, batching, concurrency, lock, claim, or query change. Baseline: origin/main `awaitWinnersWatermark` computed the fallback clock start inline and recorded no marker-loss signal. After: the same parse moves to `watermark_deferral_marker.go`, returns the same start for every input, and adds one counter increment on the already-deferring path only (no new I/O, no new allocation beyond the existing string scan). Backend: none touched; Go 1.26.11 hermetic suite. Input shape: ten classified cases (missing after not_ready, missing after stale, three unparsable shapes, future-dated, genuine first, non-wait prior class, healthy marker, prior-cycle marker) plus the exact 30-minute and 30-minute-plus-1ns boundary pair. Terminal counts: each firing case records exactly one sample with the prior class and reason labels; every silent case records none; all verdicts stay `not_ready` (expected wait). Why safe: verdict, class, and clock start are asserted unchanged in every case.

Observability Evidence: new counter `eshu_dp_advisory_group_projection_watermark_deferral_marker_missing_total{domain,failure_class,reason}` with closed labels; registered in `instruments_advisory_group_projection.go`, documented in `docs/public/reference/telemetry/advisory-group-projection.md` and the reducer-domains coverage row; mutation probe (emission removed) fails six marker-loss subtests.
