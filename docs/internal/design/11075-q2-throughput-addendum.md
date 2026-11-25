# #11075 Q2 addendum: maintainer throughput vs intake on every span on record (2026-11-25)

Refs #11075. Part of #10860. Parent context: #10839 (Gate B leg-(b)).

This addendum closes only the design record's Q2 prose gap
(`11075-winners-watermark-deferral-clock.md`, "catch-up still open"): it states
measured maintainer throughput against intake, separated per span, and what
closes E-R in throughput terms. Docs-only: no code change, no bound, bar,
clock, or enrollment change. It never closes AC3 or the issue.

## Method (no new sampling)

Every figure below comes from existing records: `11075-a2-packet.sql` runs,
the design record, the arbiter rulings (2026-11-20/21/23/25), and the posted
QA reads. Nothing was re-measured for this addendum, and no bound, bar, or
clock is redefined retroactively.

- R(t) is the handler's own watermark:
  `supply_chain_impact_winners_materialization.materialized_at`.
- Pace is judged on paired spans: dR/dwall > 1 with wall-R shrinking.
- Inflow split methods, named per span: (a) the read-only `advisory_id`
  prefix split (`GHSA-%` vs `CVE-%`); (b) the shipped tally instrument
  (#11078/#11130, migration 328). The two are never mixed in one number.

## Paired spans: maintainer throughput vs intake

| Span (window, pulls, source) | dR/dwall | wall-R start → end | Keyed commits + identifiers/hr by lane/path | Dirty + staging net/hr | Inflow/hr, split method named |
|---|---|---|---|---|---|
| A2 2026-11-20 21:24–22:24Z, 31 pulls (#10839 comment 7052159866) | 0.860x (R +51.6 min / 60.0 min wall; halves 0.899/0.820) | 175.2 → 183.6 min (growing) | NOT_CHECKED (no keyed-tick commit/identifier counts by lane/path in the A2 read set) | NOT_CHECKED as net/hr; peaks only: dirty-log trailing-10-min peaks ~675/min, staging waves 0–7731 with zero troughs, combined peak ~966/min vs ~1.2k/min sustained bar | Rate measured, split NOT_CHECKED: tally 0 buckets on all 31 pulls; the log `source` column carries writer names, not advisory origin |
| Phase-1 2026-11-20 22:35:35–22:45:35Z, 6 pulls (#11075 comment 7052476882) | 0.000x (R frozen at 19:28:09.498803Z on all 6 pulls) | 187.4 → 197.4 min (+10.0 in 10.0 wall) | NOT_CHECKED | Union total 66247 → 90310 (+24,063, all into staging 20657 → 44720); log flat ~45590 (no drain); staging floor age 11246s → 11846s (ages with wall, nothing admitted) | ~2.4k/min into staging; split by prefix method (a): log GHSA 9273 / CVE 2838, staging GHSA 32785 / CVE 12327 → ~76% GHSA; tally 0\|0\|0 all pulls (by design under drain; no GHSA predicate — the gap #11078/#11130 later closed) |
| AC3 2026-11-21 13:39:18–14:39:19Z, 31 pulls (#11075 comment 7064272407) | 0.000x on 00→30, 00→15, 15→30 (R pinned at 07:52:05.817015Z on all 31 pulls) | 347.2 → 407.2 min (+60.0 over 60.0 wall) | 0 commits/hr, 0 identifiers/hr (dirty log byte-static at 48,481; zero tally drains in-window) | Staging +382,943 over 60 min (~6.4k/min; 2068013 → 2450956; ~6.2k/min first half, ~6.6k/min second half); log static | ~6.4k/min sustained; split by both methods: prefix (a) ~76% GHSA / ~24% CVE on staging; tally (b) read 31/31 with 0 buckets / 0 rows — a measured zero, not NOT_CHECKED (the maintainer performed zero drains in-window) |
| Probe 2026-11-25 20:12:26 / 20:14:38 / 20:18:13Z, 3 reads (#11075 comment 2026-11-25T20:28Z; sha-cb4a799b both lanes) | 0.000x over ~5.8 min (R frozen at 2026-11-25 05:36:07Z on all three reads) | 14h42m (52,926s) vs the 30-min bound, zero passing reads | 47 commits/hr, 30,675 identifiers/hr, all path=keyed on the vulnerability lane, 0 on general | Dirty log 54616 → 54474 → 54344 (net −47/min, negligible against stock); staging 977 → 5252 → 237 (bursty, not steady); commit-remaining 54649 → 59488 (+~7,950/hr: inflow exceeds drain) | Inflow exceeds drain per commit-remaining; NVD/GHSA split NOT_CHECKED for this probe (no split method run in the read set); supports backfill COMPLETE since 2026-11-23 21:55:20Z |

Reading across spans: every span fails dR/dwall > 1 with lag shrinking. The
best observed pace is A2 at 0.860x, then three consecutive 0.000x regimes
(phase-1, AC3, 2026-11-25 probe). Catch-up is not reachable by waiting at any
measured rate.

## What closes E-R in throughput terms

E-R closes only when keyed-tick consumption sustainably exceeds inflow and
reducer rolls stop discarding progress through full resweeps. The carriers are
#11120 (keyed-tick headroom: ~6 keys/s at ~100% duty with the payload
re-detoasted per extracted field) and #11117 (a roll re-running the full
winners resweep; drain the dirty log a full covers), under #10839 (capacity
shortfall: keyed consumption below sustained inflow) and #10860.

The target stays the #11080 ruling's drain goal: sustained >= 2.53x wall
until wall-R <= 5 min. That is a goal for the carrier lanes, not an AC3 bar:
it does not lower Decision 9 (T-R passes only on dR/dwall > 1 with lag
shrinking on every paired span, at unchanged thresholds). The 2026-11-25
packet NO-GO stands: no Decision-9 packet spends while R is frozen; the
ruling's start conditions (i)–(v) hold unchanged.

## Decision-6 reporting split, updated for current code

The 2026-11-23 ruling (Decision 6) tolerates the #11151 stamped fast path as a
later, separate clock change: any first-classification stale counts against
T-outcome exactly as written, and the packet reports stamped-fast-path stale
vs sustained-deferral stale vs not_ready (payload stamp join; first
classification = no marker at evaluation per #11181). Reporting is not
exemption.

Current-code update: `62f92b0afd` ("Remove first-data fast-path stale
verdict; stamp stays telemetry-only (#11080)") is in HEAD. Per
`handler.go:246-248` and `first_underlying_data.go:13-22`, the
producer-stamped first-underlying-data time is deliberately ignored by the
verdict and stays telemetry/provenance plus packet discrimination only
(`advisoryOrigin` nvd/ghsa/other). Every row, stamped or not, reads the same
post-claim deferral clock.

The packet reporting shape is retained: keep the stamped/sustained/not_ready
split via the stamp join and origin attribution, but the verdict classes are
now sustained-deferral stale vs not_ready only. No exemption from T-outcome:
any first-evaluation stale, stamped or not, fails T-outcome as written. No bar
is amended here.

## NOT_CHECKED (not measured, never speculated)

- What event advances R (which tick commit stamps the singleton; whether the
  keyed path stamps it at all).
- Why R held frozen under keyed commits on 2026-11-25 (47 commits/hr with
  dR = 0.000x). The mechanism is not measured; no hypothesis is asserted here.
- A2 keyed commits/identifiers by lane/path; A2 dirty/staging net/hr; A2
  inflow split; phase-1 keyed counts; probe inflow NVD/GHSA split; stall cause;
  claim topology.

## Standing (unchanged by this addendum)

AC1 (design record) and AC2 (RED→GREEN) stay MET; AC3 stays NOT MET on the
failing regime bars, Step B stays refused, and #11075 stays open on the one
fresh A2-identical packet at unchanged thresholds. This addendum is evidence
for Q2 only.
