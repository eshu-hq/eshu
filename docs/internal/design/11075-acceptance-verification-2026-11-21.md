# #11075 acceptance verification against origin/main (2026-11-21)

Refs #11075. Part of #10860. Parent: #10839 (Gate B leg-(b)).

Verify lane `drive/11075-verify` at `origin/main` `8a3665ae27` (#11091).
Scope: sweep every acceptance criterion and Expected constraint against the
merged tree, not the issue text alone. No production code change in this lane;
#11089 (marker-loss observability, lane `drive/11089-marker-loss`) is not
duplicated here.

## Flow checked (entry, ordering, data, re-trigger)

- Entry: advisory-group `Handler.Handle` calls `awaitWinnersWatermark`
  before any projection write; the reducer claim loop delivers the intent.
- Ordering: claim (failure evidence read back) -> watermark verdict ->
  project or `Fail` (park / retry / dead-letter).
- Data dependencies: canonical winners watermark `R`, the intent's
  enqueue/cycle-start bound, and the prior cycle's first-deferral marker in
  failure details (written by the prior `Fail`, read by both claim paths).
- Re-trigger: a covering watermark advance wakes the parked row in the
  maintainer's commit transaction; otherwise the waiting backstop expiry
  re-claims it. A reopen restarts the deferral clock.

## Criterion-by-criterion verdict

| # | Criterion (issue + Expected) | Verdict on `8a3665ae27` | Evidence |
|---|---|---|---|
| AC1 | Design record answering Q1/Q2 with per-candidate dispositions, merged or attached to the closing PR | **MET** (record merged; arbiter trail partly open, see G1) | `docs/internal/design/11075-winners-watermark-deferral-clock.md` via #11086 (`ae142dcb77`): Q1 DEFECT candidate (a) adopted, (b) viable-not-adopted, (c) insufficient-alone; Q2 explicitly not closed by the fix. Anchor-conflict + superseded ledger rulings merged via #11090 (`33c0087608`), citation fix #11091 (`8a3665ae27`). |
| AC2 | Regression evidence red->green for the chosen behavior | **MET** | Handler tests on main: `BornStaleUnderBacklogDefersNotReady`, `RecentDeferralWithOldWatermarkDefersNotReady`, `SustainedDeferralPastBoundCountsAttempts`, `StaleMarkerFromPriorCycleStartsFresh`, `WatermarkWaitEventuallyCountsAttempts` (rewritten to same-cycle marker), `WatermarkWaitTelemetrySplitsPreAndPostClaim`. Live full loop `TestReducerQueueClaimCarriesFailureEvidenceLive`: 3 counting stale `Fail`s -> observed `dead_letter`. This lane re-ran the handler set at `8a3665ae27`: PASS (see Proof). |
| AC3 | Fresh A2-identical 60-min read-only packet, full trailing 60 min: first-claim lag max <30 min; zero first stale classifications; resolutions >= first-classifications with non-rising retrying + dead-letter stocks; wall-R lag closed or demonstrably closing; sustained inflow with NVD/GHSA split measured | **NOT RUN — the closing gate** | No post-deploy packet exists on #11075. Last QA readings (2026-11-21T00:39Z comment) predate the fix (deployed `664cd7533d`): watermark age 5.1 h, 3,418 s resweep statement holding the advisory lock, 111 skips. Anchor-conflict ruling Decision 6 keeps #11075 open on AC3 with unchanged thresholds; Step B stays unauthorized. Packet shape is defined below. |
| E1 | Q1: dead maintainer still surfaces in bounded attempts and dead-letters visibly | **MET in code + live proof; live QA confirmation folds into AC3** | `awaitWinnersWatermark`: stale iff `now - deferralStart > winnersWatermarkMaxWait` (strict >, 30 min unchanged at `handler.go` const), `deferralStart` = same-cycle marker via `resolveDeferralStart`/`classifyDeferralMarker`, else now. Stale counts toward the attempt budget; not_ready does not. |
| E2 | Q2: separate maintainer throughput from intake over paired spans; state whether catch-up is reachable by waiting, throughput work, or not at all | **PARTLY MET — measurement yes, closure no** | Phase-1 paired spans (comment 7052476882): R frozen 0.000x for 10 min after A2's 0.860x, staging +24,063 with log flat — waiting refuted at those rates. Design record states satisfaction stays zero until R closes wall-R lag. Stall-vs-ceiling + throughput work lives on #10706 (OPEN; 00:39Z stall addendum posted 2026-11-21T01:48Z per ruling Decision 12). AC3's R-closing bar is the joint proof. |
| E3 | Prove the theory first with bounded measurement before any production change | **MET** | Phase-1 bounded measurement (6 pulls, 120 s cadence, SELECT-only vs QA replica) preceded the phase-2 fix; A2 packet (31 pulls) preceded that. |
| E4 | Telemetry distinguishing pre-claim wait, post-claim deferral, maintainer stall, maintainer slow-but-advancing, in the same change | **MET, one residual gap split out** | #11086 shipped `first_claim_lag_seconds` (documented as latest-claim lag on retries), `post_claim_deferral_seconds`, `watermark_age_seconds`, `watermark_gap_seconds`, `watermark_waits_total{domain,failure_class,attempt}`. Residual: marker missing/unparsable/future-dated falls back silently — that is #11089 (OPEN, parallel lane, includes the exact-30-min boundary tests absent on main today). Not a #11075 AC3 blocker per ruling Decision 10. |
| E5 | No silent change to bound value, clock start, or worker concurrency outside the record; no serialization fix | **MET** | Bound still 30 min; satisfaction gate, failure-class enrollment, worker/batch/claim-order/lease untouched (claim adds output columns only). Clock-start change is the record's chosen answer, arbiter-routed. |
| E6 | NVD/GHSA inflow gap as a deliverable or a linked sibling filed the same turn | **MET as sibling; instrument not yet landed** | #11078 OPEN carries the full tally spec from the design record. No `ghsa_proxy_rows` column/predicate on main yet; spec's migration "325" is stale (325 is now `325_projector_admission_wait_attempts.sql`) — take next free at implementation. Until #11078 lands, AC3 measures the split read-only by `advisory_id` prefix (ruling Decision 6 allows this). |
| E7 | Clock-semantics change routes to the arbiter before implementation | **MET in substance; one record file still uncommitted (G1)** | Design ruling approved-with-conditions and anchor-conflict ruling both recorded; semantics on main match the surviving ruling (marker anchor). |

## Gaps that are not AC boxes but are still owed

- **G1 (audit trail):** ruling Decision 11 ordered three files; two landed
  (#11090). The phase-2 lane's `11075-deferral-clock-design-ruling-2026-11-20.md`,
  cited as #11086's arbiter basis, still does not exist on `origin/main`
  (verified in `docs/internal/arbiter/` at `8a3665ae27`). Until it lands,
  #11086's arbiter basis is uncommitted. Owner: the lane holding that text;
  coordinator recovers it verbatim or records its loss — this verify lane
  cannot reconstruct it and must not fabricate it.
- **G2 (merge hygiene):** ruling Decision 8(iii) requires retroactive
  dispositions for #11086's 3 red checks at merge (`Package coverage floors`,
  `static-mirror (ledger and runner probes)`, `scheduled-setup`). Still
  failing on the PR with no disposition comment (only the Cloudflare bot
  comment). Coordinator-owned; blocks calling #11086 clean, not AC3 itself.
- **G3 (dangling pointer):** design record cites an uncommitted phase-1
  scratch draft (`docs/scratch/11075-...`, absent on main). The same evidence
  is on #11075 comment 7052476882, so AC1 evidence survives; the pointer
  itself is dead.
- **G4 (boundary tests):** no exact-30-min (not stale) / 30-min+epsilon
  (stale) test on main — nearest are `MaxWait + 1m` and 45-min cases.
  Owned by #11089 acceptance; do not duplicate here.

## What remains code-actionable in this lane

Nothing. Every code-shaped remainder has an owner: #11089 (marker-loss
signal + boundary tests), #11078 (GHSA tally instrument), #10706 (Q2
stall/throughput). The only unowned items are coordinator acts: deploy
`ae142dcb77`-or-later to QA, run the AC3 packet below, post G2 dispositions,
and land or formally write off G1. No new issue is filed from this lane —
G1/G2 are already ordered by the merged anchor-conflict ruling (Decisions
8(iii) and 11), so they ride the goal ledger via #11075 itself.

## Reconciliation 2026-11-27 (arbiter NO-GO triplet; docs-only)

The verdict table above is the 2026-11-21 record at `8a3665ae27` and is
kept as written. Standing statements it contains are superseded as follows
(gh-verified 2026-11-27):

- E4/G4 (#11089): CLOSED 2026-11-21. The marker-loss counter and the
  exact-30-min / 30-min+1ns boundary pair are on main
  (`handler_marker_loss_test.go`); no residual gap remains in this lane.
- E6 (#11078): CLOSED 2026-11-21 via #11130/migration 328
  (`328_supply_chain_impact_winners_dirty_tally_ghsa.sql`,
  `ghsa_proxy_rows`). The under-backlog tally-zero is documented behavior
  (2h-recency filter), not a defect; T-inflow uses the read-only
  `advisory_id` prefix method named in the packet header. Step B stays
  closed solely on AC3, no longer on GHSA NOT_CHECKED.
- AC3: still NOT MET. The 2026-11-27 start-gate triplet (pin
  `sha-e755aa3a`, #11075 comment 7224501755) is NO-GO: lag ~26x the
  30-min bound on all three reads, R stalling then freezing, dirty log
  rising net. Full ruling:
  `docs/internal/arbiter/11075-decision9-gate-nogo-ruling-2026-11-27.md`.
  The packet window stays unspent; Step B stays refused. Next gate is a
  fresh 3x5-min triplet only after lag approaches 30 min, then the
  one-shot Decision-9 packet, then Step B only by fresh arbiter ruling.
- Dead-letter +500 growth (slow-queue probe window) stays a carrier
  observation on #11075 pending the marker-split census
  (first-classification vs sustained stale, window + denominator); it
  proves nothing about the clock and blocks no gate.

## A2 evidence packet shape (AC3, post-deploy, read-only)

Run only after the deployed QA commit includes `ae142dcb77` (#11086) and
the backlog-drain state is recorded. SELECT-only against the QA read
replica with `statement_timeout` set; no writes, no deploys from the packet.

### Preconditions recorded in the packet header

- Deployed commit (full SHA) and image digest per reducer deployment.
- Replica liveness: `pg_last_xact_replay_timestamp()` lag at start/end.
- Backlog state at start: pending counts by work kind (global queue mix),
  advisory pending age (mean/max), retrying + dead-letter stocks by
  watermark failure class, waiting-table depth for the winners wait.

### Sampling

- 31 pulls at exact 120 s cadence over 60 min (A2-identical), launch skew
  recorded per pull; thresholds judged on the full trailing 60 min only.
- Per pull: `R(t)` (watermark), wall clock; first evaluations in the
  trailing interval with per-row first-claim lag (cycle-start numerator,
  never `created_at` alone) and verdict class; resolutions; retrying and
  dead-letter stocks; dirty inflow by `advisory_id` prefix
  (`GHSA-%` / `CVE-%` / other) in log + staging until #11078 lands, or the
  tally instrument after; the five #11086 telemetry signals
  (claim lag, post-claim deferral, watermark age/gap, waits by class+attempt).
- Marker-freshness leg (new since #11086): among attempt>=2 watermark-wait
  rows, count rows carrying a parsable same-cycle marker vs missing/bad
  (the #11089 signal when landed; otherwise failure-details inspection).
  Report counts only — a nonzero missing count does not fail AC3, but an
  unmeasured one does.
- Residency leg (the 30-min bound under backlog): every attempt-1 verdict
  in-window must be `not_ready` regardless of pre-claim lag; list the max
  observed pre-claim lag and the max post-claim deferral before first stale.

### Pass bars (unchanged; any one failure fails the packet)

| Bar | Threshold |
|---|---|
| First-claim lag | max < 30 min over the full trailing 60 min |
| First stale classifications | zero (attempt-1 stale count = 0) |
| Resolutions vs first-classifications | resolutions >= first-classifications |
| Stocks | retrying and dead-letter stocks non-rising across the window |
| Watermark pace | wall-R lag closed, or demonstrably closing (paired-span dR/dwall > 1 with lag shrinking; report every paired span) |
| Satisfaction | at least one evaluated row satisfied by the watermark (E-R <= 0) — zero satisfaction fails per the issue's Q1-only refusal |
| Inflow | sustained inflow present with the NVD/GHSA split measured and named (instrument or prefix method) |

### Packet query definitions (2026-11-22)

These define how the bars above are measured. They change no threshold. The
runnable queries are in [11075-a2-packet.sql](11075-a2-packet.sql).

- **First classification.** A row whose failure details carried no
  `winners-watermark-deferral:` marker when the handler evaluated it. In the
  packet it is a work item that first appears in a pull's id snapshot (Q4b).
  The `attempt_count = 1` filter is withdrawn: a `not_ready` park freezes
  `attempt_count` at 1, so a later stale re-evaluation also reads 1. On QA at
  2026-11-22 00:01Z, 28 rows with `attempt_count = 1` and the stale class were
  re-evaluations, not first stale classifications. Where the bars above say
  "attempt-1", read "first classification".
- **First-claim lag numerator.** The marker clock: `first-deferred-at` minus
  `cycle-started-at`, which is the handler's own measure. The
  `COALESCE(reopened_at, created_at)` figure is reported beside it, labelled
  `legacy`, and the two are never mixed in one number. On those 28 rows the
  two cycle starts differ by about 1.5 h (19:26:18Z against 20:57:23Z).
- **Resolutions and satisfaction.** Set differences of the Q4b id snapshots
  across consecutive pulls. A resolution is an id that left the winners class.
  It is satisfied when its row now carries no failure. Summed 120 s slices are
  not used, because launch skew leaves holes between them.

### Posting

Post the packet verbatim on #11075 as closing evidence; cross-link from
#11080 and #10839 (their own bars are judged separately). A failed packet
keeps #11075 open with the failing bars named — no threshold edits, no
third sampling lane under new bars.

## Proof run by this lane

| Check | Result at `8a3665ae27` |
|---|---|
| `go test ./internal/reducer/advisory/group/ -run 'TestAdvisoryGroupHandler(BornStale\|RecentDeferral\|SustainedDeferral\|StaleMarker\|WatermarkWait)' -count=1` | PASS (6 tests incl. telemetry split) |
| Source sweep: clock rule, marker parse/fallback, bound value, telemetry instruments | Present as tabled above |
| `gh` sweep: #11075 (9 comments + claim), #11089, #11080, #11078, #10706, #11086 checks | As tabled above |

NOT_CHECKED by this lane at `0ec2abfd24`: live QA state (deploy status of
`ae142dcb77`, current R(t), stall cause), the live-Postgres full-loop test
re-run (needs disposable PG; taken as merged evidence), #11089 lane progress
beyond its claim comment, claim-benchmark delta for the +3 claim columns
(ruling Decision 3 rests on the structural bound).

## Addendum 2026-11-21T03:00Z: read-only QA readings (no deploy)

SELECT-only vs `eshu-postgresql-read` (namespace `eshu`), read-only
transaction + 30 s statement timeout, replay lag 4.4 ms at 02:57:00Z.
Runnable form of every query: [11075-a2-packet.sql](11075-a2-packet.sql).

- Deploy status: `failure_details` carries the `winners-watermark-deferral:`
  marker on 0 of 524 attempt>=2 watermark-wait rows (02:57:48Z). QA still
  runs pre-fix code; AC3's precondition (deployed commit >= `ae142dcb77`)
  is NOT met, so no AC3 bars are judged here.
- R(t) (the handler's own singleton watermark): 2026-11-20 23:39:12Z, wall
  lag 3 h 19 m at 02:58:41Z — down from 5.1 h at 00:39Z, so dR/dwall ~1.8x
  over the window: R is closing, not stalled, at read time.
- Stocks: retrying stale 874 + not_ready 28, dead_letter stale 509 (up from
  ~232-250 in earlier windows: stale keeps accumulating pre-fix, as
  expected), claimed stale 4. Waiting: winners_watermark 142 rows, anchors
  00:39Z-02:50Z (`first_deferred_at` pre-exists via migrations 110/316).
- Backlog: 551 advisory pending, all stage=reducer, mean age 7.5 d, max
  8.3 d — AC3's first-claim-lag bar cannot pass until this drains.
- Inflow split (prefix method): staging GHSA 458,495 / CVE 171,659 / other
  278; log GHSA 34,684 / CVE 10,865 / other 41 — GHSA ~73%, method works.
  Tally still 0 rows: #11078 instrument gap persists (E6 unchanged).
- Q4 slice 02:59:46Z: 16 attempt-1 not_ready first evaluations in the
  trailing 120 s, max pre-claim lag 17 min, zero stale in-window (single
  slice, not an AC3 judgment). The `attempt_count = 1` filter behind this
  slice was withdrawn on 2026-11-22; see "Packet query definitions".
- G1/G2 re-verified live: the `11075-deferral-clock-design-ruling-2026-11-20.md`
  file is still absent (only the two 2026-11-21 ruling files exist); #11086
  carries only the Cloudflare bot comment, no retroactive dispositions.
- G1 disposition (coordinator, post-addendum): the file never existed — absent
  from origin/main, the phase-2 branch, and every worktree, with no write record
  in the phase-2 session log. Written off without fabrication; its basis is covered
  by the merged design record plus the anchor-conflict ruling (#11090).
- G4 re-verified: nearest bound tests are `MaxWait + 1m` and 45-min cases.
- Proof re-run: handler watermark subset (BornStale, RecentDeferral,
  SustainedDeferral, StaleMarker, WatermarkWait incl. telemetry split,
  LagStartsAtCycleBound): PASS at lane HEAD.
- Claim comment: the verify-lane claim for `drive/11075-verify` already
  stands on #11075 (posted with the prior result); no duplicate posted.

Observability: no code or telemetry change in this lane (docs-only
verification record + read-only packet); no-observability-change is exact.
