# #11075 Decision-9 gate NO-GO ruling (2026-11-27)

## Provenance

- Source: `arbiter-eshu` verdict delivered to the drive coordinator on
  2026-11-27 (drain-only wait at unchanged Decision-9 bars; gate triplet
  17:20/17:25/17:30Z, read-only).
- Recorded verbatim by the executor per the ruling's required-proof step 1.
  No code change is authorized or made by this ruling.
- Continues `11075-design-record-ruling-2026-11-23.md` (Decision 9) and the
  `11075-q2-throughput-addendum.md` paired-span record. Cross-linked from
  `docs/internal/design/11075-winners-watermark-deferral-clock.md`.
- Gate evidence: #11075 comment 7224501755 (2026-11-27T17:31:27Z), pin
  `sha-e755aa3a`, SELECT-only vs `eshu-postgresql-read-0`.

## Ruling

Gate NO-GO on 2026-11-27 triplet — drain-only wait at unchanged
Decision-9 bars, no clock change, packet window stays unspent, Step B
stays refused; #11075 stays open on AC3.

Numbered decisions:

1. (a) NO-GO confirmed. Raw observations (pin sha-e755aa3a, replica
   eshu-postgresql-read-0, SELECT-only): coordinator single 17:14Z
   R=2026-11-27T04:02:27Z lag 13:11:44 dirty 60032; triplet PULL1
   17:20:28Z R=04:27:11.80676Z lag 12:53:17 dirty 59638 staging 2;
   PULL2 17:25:29Z R=04:31:19.419191Z lag 12:54:10 dirty 60020 staging
   1351 dR/dwall ~0.82x; PULL3 17:30:30Z R=04:31:19.42126Z lag
   12:59:10 dirty 60395 staging 1510 dR/dwall ~0.000x frozen; replica
   replay lag 0.20s/0.95s/0.14s; eshu-0 restarts 0; contract v2
   8e761819; supports complete 2026-11-23T21:55:20Z; tally 0 all pulls.
   Start-gate (iii) FAIL 0/3 (absolute lag ~26x the 30-min bound on every
   read AND R not advancing across all 3); (iv) FAIL (dirty net +757
   rising, staging rising); (v) PASS; (i)/(ii) pin + contract present
   but insufficient — conjunction fails. Drain-only wait continues at
   unchanged bars. No clock change ordered: live watermark_stale under
   frozen/slow R is the designed sustained-deferral outcome (Decision 7),
   not proven born-stale; Q4 dry-run first-classifications were
   not_ready. No packet spend against frozen R. Recheck only after lag
   approaches 30 min.
2. (b) GO preconditions (for the future spend, stated now so the
   coordinator needs no new ruling to run the gate): packet spends ONLY
   when the full conjunction passes on a fresh 3x5-min triplet —
   (i) deployed SHA+digest proven to contain ae142dcb77 +
   #11130/migration 328 + #11332 (+ 62f92b0afd stamp-telemetry-only)
   recorded per deployment; (ii) no full resweep in flight, contract
   line present, keyed maintaining; (iii) R advancing AND lag <=30min on
   3 consecutive 5-min reads; (iv) dirty log not rising net;
   (v) supports complete re-confirmed. Then ONE Decision-9 packet at
   full fidelity (31 pulls, exact 120s cadence, full trailing 60min,
   SELECT-only with statement_timeout, Q4b/Q6 id-snapshot
   reconciliation, marker-clock first-claim lag with legacy figure
   beside it never mixed, stamped/sustained/not_ready split,
   marker-freshness counts, amendments 1-8 incl. dry-run Q4b/Q6 first,
   per-block judging, predeclared T-R spans, E-R/Q6 dual, named inflow
   method, stamp query, integrity rules). Bars unchanged, any one
   failure fails: T-lag max first-claim lag <30min; T-outcome zero first
   stale classifications; T-resolve resolutions>=first-classifications;
   T-stock retrying+dead-letter non-rising; T-R dR/dwall>1 lag shrinking
   every paired span; Satisfaction >=1 row E-R<=0; T-inflow sustained
   with NVD/GHSA split measured and named (measured zero is evidence,
   NOT_CHECKED fails). Step B re-application ONLY after a passing
   packet posted verbatim on #11075 (cross-linked #11080/#10839), and
   only by fresh arbiter ruling — a passing packet does not itself
   authorize Step B. A failed packet restarts no clock and authorizes no
   third lane under new bars.
3. (c-1) Deferral clock: CLOSED, no open design question. Q1=DEFECT,
   candidate (a) deferral-from-first-deferral governs (design record
   :11, ruling 2026-11-23 Decisions 1-3); (b) R(t)-gated stale rejected,
   (c) drain-only rejected as fix (retained as packet precondition
   only). Code verified: 30min strict > from same-cycle marker else now
   (handler.go:39,259-260; watermark_deferral_marker.go:82),
   exact-30min/30min+1ns boundary pinned
   (handler_marker_loss_test.go:249,255), marker sole anchor, ledger
   branch retired, #11151 stamp now telemetry-only (62f92b0afd) with
   every row on the same clock, #11089 marker-loss counter CLOSED. Any
   further clock edit re-owes Decision 7 in full (RED-first born-stale
   + live dead-letter loop + telemetry) and needs a fresh ruling.
4. (c-2) GHSA coverage: INSTRUMENT CLOSED, coverage gap under backlog is
   documented behavior, not a defect. #11078 CLOSED via #11130/migration
   328 (ghsa_proxy_rows, verified migrations/328...sql:18 on main +
   deployed-ancestor). Tally reads 0 rows under backlog by design
   (2h-recency filter, design record :50-52; AC3 span read 31/31 with 0
   buckets as measured zero, addendum row AC3). Verdict: T-inflow in the
   Decision-9 packet uses the read-only advisory_id prefix method
   (GHSA-%/CVE-%/other, ~73-76% GHSA measured phase-1/AC3) named in the
   packet header; tally figures reported beside it if nonzero, never
   mixed into one number. Step B no longer closed on GHSA NOT_CHECKED —
   it stays closed solely on AC3/Decision 2. No new GHSA filing.
5. (c-3) Dead-letter growth carrier: STAYS on #11075, does not block the
   gate, proves nothing about the clock yet. Raw observation: advisory
   watermark_stale dead letters +500 in latest slow-queue probe window
   while sci drains (window/denominator NOT_CHECKED, no census). Under
   frozen R the designed path dead-letters sustained deferrals after 3
   counting stale Fails (Decision 7) — growth is the expected visible
   surfacing, not born-stale churn, until a census splits
   first-classification stale (no marker) from sustained stale (marker
   age >30min). Required before any clock inference: one read-only
   census of the growth window (dead_letter watermark rows by failure
   class, marker first-deferred-at age at final Fail,
   stamped/sustained split, window + denominator). T-stock
   (non-rising dead-letter stock over the packet window) remains the
   binding judge at packet time; no separate bar, no new issue unless
   the census shows first-classification stale >0 (that would be a
   born-stale regression filing, not a bar change).
6. Housekeeping verified, no action: dangling tips 78ab32a0/b94f20d4
   stale/superseded, nothing to re-land; missing 2026-11-20 ruling file
   written off without fabrication (acceptance doc G1 disposition
   :190-193); Q2 addendum closes prose gap only, never AC3 (addendum
   :88-93). Throughput ownership stays #11120/#11117 under
   #10839/#10860 (all OPEN); >=2.53x-until-wall-R<=5min stays a carrier
   goal, not an AC3 bar.

Required proof (coordinator records in goal file + #11075): (1) this
ruling verbatim; (2) dead-letter census in decision 5 before next gate
recheck; (3) next gate = full 3x5-min triplet only after lag approaches
30min, judged on (i)-(v) conjunction; (4) on GO, Decision-9 packet
verbatim on #11075; (5) Step B only via fresh ruling after passing
packet.

What the arbiter will not do and why: widen the 30-min bound or any
packet bar (refused every ruling in chain); spend the packet against
frozen R (burns the one window on a certain fail); order a clock change
from dead-letter growth without the marker-split census (growth is
designed surfacing until proven born-stale); exempt stamped rows from
T-outcome; close #11075 on AC1/AC2 or transfer AC3 to carriers;
authorize Step B; edit/push/deploy/touch QA (arbiter role).

Unverified / NOT_CHECKED: live QA R(t) after 2026-11-27T17:30:30Z (last
read = triplet above, taken by prior lane, corroborated by issue
comment 7224501755, not re-read by the arbiter); what event advances R
/ why R froze pull2->pull3 under keyed commits (addendum NOT_CHECKED
:80-86, #10972 warming packet's question); dead-letter +500
window/denominator/census (decision 5); deployed image digest ancestry
re-proven by prior result 1, accepted; live dead-letter test not re-run
by the arbiter (merged evidence). No-observability-change: read-only
ruling, no code/telemetry change.
