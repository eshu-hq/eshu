# #11075 design-record ruling (2026-11-23)

## Provenance

- Source: `arbiter-eshu` verdict delivered to the #9209 drive coordinator on
  2026-11-23 (workflow `11075-arbiter-clock-semantics`, agent
  `01a205bd-9efb-73d3-8799-871bd5fa5b23`, 29 tool calls, read-only).
- Recorded verbatim by the coordinator per the ruling's required-proof step 1.
  No code change is authorized or made by this ruling.
- Supersedes nothing yet: `11075-winners-watermark-deferral-clock-ruling-2026-11-21.md`
  is the ledger anchor (marked superseded); `11075-anchor-conflict-ruling-2026-11-21.md`
  carries Decision A (marker is sole anchor) and Decisions 6-7 (#11075 reopens on
  AC3, #11080 Q1 disposed). Cross-linked from
  `docs/internal/design/11075-winners-watermark-deferral-clock.md`.
- Four cited ruling files are absent on main (verified `ls` by the arbiter):
  `gateb-entry-certification-2026-11-20.md`, the Step-A and
  `gateb-A2-packet-disposition-2026-11-20.md` files,
  `11075-deferral-clock-design-ruling-2026-11-20.md`, and
  `11075-ac3-packet-disposition-2026-11-21.md`. Their substance survives only as
  GitHub comments (Step A forensics 7050767319, A2 packet 7052159866, A2
  disposition 7052216088 on #10839), #11086's account, and an untracked copy
  reported in #11075 comment 7061029760. Do not fabricate them; land recovered
  text verbatim or record the loss (required-proof step 1, still owed).

## Ruling

**DEFECT** — pre-claim queue residency must not consume
`winnersWatermarkMaxWait`. Candidate (a) deferral-from-first-deferral governs
#11075 (30 min, satisfaction and enrollment unchanged). Candidates (b) R(t)-gated
stale and (c) drain-only are rejected as the fix. Step B stays refused and
#11075 stays open until one fresh A2-identical packet passes every unchanged bar.

## Verified findings (arbiter, inspected 2026-11-23)

- Issue eshu-hq/eshu#11075 OPEN (Refs #10839, Part of #10860), body + 28 comments
  read. Step A forensics (#10839 comment 7050767319): 39/39 rows stale on first
  evaluation, first-claim lag 80-94 min, H3 backlog-surfacing established.
  A2 packet (#10839 comment 7052159866, 2026-11-20T21:24-22:24Z, 31 pulls):
  first-claim lag 436.1-11675.7 min, 148/154 stale-set entries attempt-1
  first-touch, R 51.6 min/60.0 min wall = 0.860x, wall-R 175.2-183.6 min,
  E-R +65.5..+84.2 min on 148/148 rows, zero satisfied. A2 disposition (#10839
  comment 7052216088): Step B refused, no bar amended, no third sampling lane,
  Decision-3 fix-or-design issue ordered. Entry/digest certification (comment
  7043828262) and gray-escalation harvest (7050528468) were read; neither
  authorizes Step B.
- Pre-fix defect in source: at `ae142dcb77^1`,
  `go/internal/reducer/advisory/group/handler.go` `awaitWinnersWatermark` set
  `cycleStart = CycleStartedAt` falling back to `EnqueuedAt` and
  `stale: now.Sub(cycleStart) > winnersWatermarkMaxWait` (30 min). An unclaimed
  row therefore burned the whole bound in queue. The bound's own contract bounded
  "one repair cycle defers" (60 missed 30 s ticks) and the stale class asserted
  "the maintainer is down" — false in A2, where the maintainer advanced 51.6 min.
- Current source: `handler.go:34-39` (30 min, post-claim deferral from first
  deferral, #11075); `handler.go:230-235`
  `winnersBoundStart = max(EnqueuedAt, CycleStartedAt)`; `handler.go:257-267`
  satisfaction `!watermark.Before(required)` then
  `stale := now.Sub(deferralStart) > winnersWatermarkMaxWait`;
  `handler.go:380-385` counting stale vs non-counting not_ready;
  `watermark_deferral_marker.go:82-103` `resolveDeferralStart` (usable same-cycle
  marker, else now; #11089 loss reasons). Merged design record:
  `docs/internal/design/11075-winners-watermark-deferral-clock.md` (via PR #11086,
  merge `ae142dcb77`); acceptance sweep:
  `docs/internal/design/11075-acceptance-verification-2026-11-21.md`; packet SQL:
  `docs/internal/design/11075-a2-packet.sql` (first classification = no marker,
  per PR #11181).
- Re-ran by the arbiter, exit 0: `cd go && go test
  ./internal/reducer/advisory/group/ -count=1` ok; targeted watermark/marker/
  Decision-3 set PASS including the exact-30-min boundary
  (`handler_marker_loss_test.go:249,255`).
- Later narrowing: PR #11151 (`50430586c1`, under #11080 comment 7063348582
  ruling) added `handler.go:263-267` + `first_underlying_data.go:13-22,46-70`: a
  stamped row whose `first_underlying_data_at` is more than 30 min old reads stale
  on first evaluation. This post-dates the anchor ruling's Q1-a rejection and
  narrows candidate (a)'s "first evaluation always not_ready" guarantee to
  unstamped rows. Disposition in Decision 6 — it does not amend any #11075 bar.

## Numbered decisions

1. **Q1 = DEFECT, not intended design.** The bound measures deferral inside one
   repair cycle and the stale class asserts maintainer-down. Queue residency is
   neither: an unclaimed row has performed zero deferrals. A2 falsifies the
   assertion in production (maintainer +51.6 min; every first evaluation born
   stale). An intended-design answer would have to state that leg-(b) cannot pass
   under any backlog with more than 30 min first-claim lag and that drain alone
   is the precondition; that certifies a false cause and is refused. Accuracy
   first: a failure class asserting a false cause is a product failure at any
   backlog depth.

2. **Candidate (a) ADOPTED — deferral-from-first-deferral.** Stale iff
   `watermark < required` AND `now - firstDeferralOfCycle >
   winnersWatermarkMaxWait` (strict `>`, value unchanged at 30 min).
   Satisfaction unchanged: proceed iff `watermark >= required`,
   `required = max(EnqueuedAt, CycleStartedAt)`. Enrollment unchanged:
   `not_ready` non-counting, `stale` counting/retryable. First deferral (no
   same-cycle anchor) is `not_ready` regardless of pre-claim lag — subject only
   to Decision 6's stamped fast path. Reopen restarts the clock; partial
   watermark advances leave the wait parked with the clock running; sustained
   same-cycle deferral is reached via backstop expiry. Simpler no-new-state
   shapes are proven wrong in the merged record
   (`11075-winners-watermark-deferral-clock.md:13-21`): attempt-gating never fires
   under the non-counting freeze, last-claim resets every retry, watermark-age
   gating born-stales under multi-hour wall-R.

3. **Mechanism: the landed failure-details marker is the sole anchor.** Per
   anchor-conflict Decision A, the marker in #11086 governs; the waiting-ledger
   branch (`b21dceed2f`) stays retired and no second anchor ships. Marker loss is
   bounded (each overwriting non-wait Fail costs a counting attempt) and now
   observable via #11089
   (`watermark_deferral_marker_missing_total{reason=missing|unparsable|future_dated}`;
   #11089 CLOSED). No silent fallback to the row-age/`cycleStart` clock is
   permitted on any path.

4. **Candidate (b) REJECTED as a verdict input — stale gated on maintainer stall
   from R(t).** It needs a per-evaluation R(t)-series read on the hot path,
   conflates global maintainer pace with per-row deferral, can mask a dead
   maintainer behind historical advance, and satisfies zero rows alone. Stall vs
   slow-but-advancing is telemetry, not a clock input: `watermark_age_seconds`
   (now-R) + `watermark_gap_seconds` (required-R) + `watermark_waits_total`
   series, shipped in #11086. A future stall-gated refinement needs a fresh ruling
   and must still pass Decision 7.

5. **Candidate (c) REJECTED as the fix — backlog/claim-ordering repair with clock
   unchanged.** It keeps asserting "maintainer down" while the maintainer advances
   and re-arms born-stale on the next >30 min backlog. Drain and claim health
   remain an acceptance precondition (T-lag cannot pass while ancient rows surface
   days late), not a code answer. No bound-value widening, no worker/batch/
   claim-order change as a stale fix, no serialization anywhere in this lane.

6. **#11080/#11151 stamped fast path: tolerated as a later, separate
   clock-semantics change; no #11075 bar moves.** A first-classification stale
   fired by `first_underlying_data_at` counts against T-outcome exactly as written
   (zero first stale classifications). The packet must therefore report, per first
   classification, stamped-fast-path stale vs sustained-deferral stale vs
   not_ready (join the payload stamp; first classification = no marker at
   evaluation per #11181), but reporting is not exemption. If any lane wants
   fast-path stales exempted from T-outcome, that is a retroactive bar amendment
   and is refused here; it would need a new arbiter ruling on fresh evidence.
   Practical consequence: the packet passes only in a regime where the watermark
   actually covers `required` for surfacing rows — which is the point of Q2.

7. **Dead-maintainer bounded-surfacing proof obligation (blocking for any clock
   change, including (a) and any future (b)).** Required shape, discharged for (a)
   by #11086 and re-owed in full for any further clock change: (i) RED-first
   born-stale regression — attempt-1/no-marker, pre-claim lag >30 min measured
   from `CycleStartedAt` (never `created_at` alone), advancing-but-behind
   watermark → `not_ready`, never stale (`handler_test.go:184`
   `TestAdvisoryGroupHandlerBornStaleUnderBacklogDefersNotReady`); (ii)
   recent-deferral/old-watermark → not_ready; sustained deferral 45 min → stale
   counting (`handler_test.go:256`); exact 30 min → not stale, 30 min+eps → stale;
   prior-cycle/future/unparsable marker → fresh clock with #11089 signal;
   reopen-restart; read-error → visible failure, never stale-by-fallback;
   (iii) live Postgres full loop observing `fact_work_items.status` after every
   `Fail`: not_ready park (attempt frozen at 1) → backstop re-claims on stuck
   watermark → exactly `MaxAttempts=3` counting stale evaluations → observed
   `status='dead_letter'`
   (`go/internal/storage/postgres/reducer_queue_failure_evidence_live_test.go:50`,
   RED-first: dead_letter after first stale Fail must fail with `retrying`). No
   clock change lands on a handler-only matrix; the queue-observed dead-letter is
   the proof.

8. **Q2 (watermark pace / E-R closure): waiting is refuted; pace-the-watermark by
   throughput work.** Separating throughput from intake over paired spans: A2 R
   0.860x (halves 0.899/0.820, lag growing); phase-1 frozen 0.000x with staging
   +24,063 and log flat (#11075 comment 7052476882); AC3 packet
   2026-11-21T13:39-14:39Z (comment 7064272407) R 0.000x for 60 min against
   ~6.4k/min intake, lag 347.2-407.2 min; later reads (comments 7071519876,
   7084458803-7087729355) show keyed throughput ~2-3 identifiers/s against growing
   dirty log and multi-hour lag. Catch-up is not reachable by waiting at measured
   rates. Throughput ownership: #10706 CLOSED; live carriers are #11120
   (keyed-tick headroom) and #11117 (full-resweep on roll) under #10839/#10860.
   The #11080 ruling's drain target (sustained >=2.53x wall until wall-R <= 5 min)
   stands as a throughput goal for those lanes; it is not an AC3 bar and does not
   lower Decision 9. A Q1-only fix leaving satisfaction at zero is not
   acceptance — #11075 does not close on unit green.

9. **Acceptance = one fresh A2-identical QA packet at unchanged thresholds;
   anything else keeps #11075 open and Step B refused.** Run only when a cheap
   precondition read first shows R advancing on two consecutive reads, keyed ticks
   maintaining the table (not "during the full"), deployed commit >= the current
   fix set (`ae142dcb77` deferral clock + #11130 GHSA instrument/migration 328;
   record full SHA + image digest per deployment). Packet: 31 pulls, exact 120 s
   cadence, full trailing 60 min, SELECT-only vs `eshu-postgresql-read-0` with
   `statement_timeout`, replica replay lag recorded, entries/exits/net-stock
   reconciled from per-pull id snapshots (Q4b/Q6 in `11075-a2-packet.sql`),
   per-row first-claim lag on the marker clock (`first-deferred-at -
   cycle-started-at`, `legacy` COALESCE figure reported beside it, never mixed),
   Decision-6 stamped/sustained split, marker-freshness counts. Bars — any one
   failure fails the packet: **T-lag** max first-claim lag <30 min; **T-outcome**
   zero first stale classifications; **T-resolve** resolutions >=
   first-classifications; **T-stock** retrying and dead-letter stocks non-rising;
   **T-R** wall-R closed or demonstrably closing on every paired span (dR/dwall
   >1, lag shrinking); **Satisfaction** >=1 evaluated row with E-R <=0;
   **T-inflow** sustained inflow with NVD/GHSA split measured and named (shipped
   tally via #11078/#11130 CLOSED, or read-only `advisory_id` prefix; a measured
   zero is evidence, NOT_CHECKED is failure). Post the packet verbatim on #11075;
   cross-link #11080/#10839 (their bars judged separately). AC1 (this record +
   merged design record) and AC2 (RED→GREEN + live loop) are MET; AC3 is NOT MET
   — the 2026-11-21 packet failed T-lag/T-outcome/T-resolve/T-stock/T-R/
   satisfaction on zero maintainer throughput, and no packet has run since.
   MET-WITH-INTERPRETATION is refused.

10. **Telemetry shipped with any clock change, no exceptions** (present for (a),
    re-owed for any successor): `first_claim_lag_seconds` (documented
    latest-claim lag on retries), `post_claim_deferral_seconds` (the bound
    numerator), `watermark_age_seconds`, `watermark_gap_seconds`,
    `watermark_waits_total{domain,failure_class,attempt,origin}` series, shipped
    in #11086; marker-loss counter via #11089. A future stall-gated refinement
    needs a fresh ruling and must still pass Decision 7.

## Required proof (in order, coordinator-owned)

1. Record this ruling + recover-or-write-off the four missing ruling files named
   above; retroactive dispositions for #11086's merge-time red checks if still
   outstanding.
2. For any further clock edit: prove-the-theory-first bounded measurement, then
   Decision 7 RED→GREEN + live dead-letter loop + telemetry mutation probe,
   post-final-edit exits quoted.
3. Cheap precondition R reads (no packet spend against frozen R or "during the
   full"), then the Decision-9 packet once, at full fidelity. A failed packet
   does not restart any clock and does not authorize a third lane under new bars.
4. Step B re-application only after a passing packet, by fresh arbiter ruling.

## What the arbiter will not do, and why

Widen the 30-min bound or redefine any Step A/A2 threshold retroactively
(refused by every ruling in this chain); accept (b) or (c) as the #11075 fix
(Decisions 4-5); exempt #11151 fast-path stales from T-outcome (Decision 6);
replace the marker anchor or ship a second anchor (Decision 3); close #11075 on
AC1/AC2 or transfer AC3 to #11080/#10839/#11117/#11120 (carriers for the retake,
not owners of this bar); authorize Step B; edit files, push, merge, deploy, or
touch QA state (arbiter role — coordinator records and executes).

## Unverified / NOT_CHECKED

Live QA state on 2026-11-23 (current R(t), deployed SHA, whether the contract-v2
full finished and keyed ticks resumed) — no QA reads were taken by the arbiter;
last evidence is #11075 comment 7087729355 (full still running at 23:32Z
2026-11-22). Live-Postgres dead-letter test not re-run by the arbiter (needs
disposable PG; taken as merged evidence + package suite green). Whether #11086's
three merge-time red checks ever received the Decision-8(iii) dispositions. Text
of the uncommitted 2026-11-20 phase-2 ruling and the cited AC3-disposition file
beyond the accounts on the issues.
