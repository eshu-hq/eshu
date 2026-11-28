# #9209 C3 acceptance query and pin-stamped result

Refs eshu-hq/eshu#9209. Arbiter ruling (2026-11-28, #11613): C3
denominator is CVE-keyed findings (option a). This note commits the
decision queries as [`9209-c3-acceptance.sql`](9209-c3-acceptance.sql) and
records their pin-stamped output. It closes nothing: C3 still needs a
re-measure after the winners unblock (P4 on #11613).

**Placement.** This artifact lives under `docs/internal/design/`, not
`docs/internal/evidence/`: new files there are banned by
`scripts/verify-no-ephemeral-docs.sh` (purge in #11661). The committed
SQL precedent is `docs/internal/design/11075-a2-packet.sql`.

## Decision queries

- **Denominator (C3):** effective-visible rows of
  `supply_chain_impact_canonical_winners` whose `cve_id` matches
  `^CVE-[0-9]{4}-[0-9]{4,}$` (case-insensitive, per
  `epss_finding_derivation.go:35`; stored ids are uppercase so `~`/`~*`
  agree here — verified, 0 lowercase). Row-weighted: one count per
  winners row, no DISTINCT.
- **Numerator (C3):** denominator rows with EPSS present, as stored on
  the winners payload: `COALESCE(payload->>'epss_probability','') <> ''`
  (top-level `epss_percentile` gives the identical split, 76,613/76,613
  on the 16:11Z rerun corpus).
- **Effective-visible:** expiry-aware suppression state outside
  `not_affected, accepted_risk, false_positive, ignored,
  advisory_retracted` (the `evaluation.go:83-91` hidden set, ported from
  `advisoryGroupWinnerEffectiveStateSQL` for typed TIMESTAMPTZ columns).
- **Fact-join variant (Q2):** denominator rows with an active
  `vulnerability.epss_score` fact for the same `cve_id`
  (`is_tombstone = false`). The gap Q2 − Q1 is stale payload (fact
  exists, payload not yet refreshed); the floor is FIRST catalog misses.
- **All-active beside (Q1/Q2b):** the same counts without the CVE
  filter, and the ceiling `(payload-present + stale) / all-active`.

## Pin-stamped result (QA read replica, 2026-11-28 15:36:58Z)

Read-only (`BEGIN READ ONLY`, `pg_is_in_recovery() = t`); output of the
committed file, verbatim:

```text
Q1_c3_payload|76926|76633|99.619|81083|76633|94.512|4450|0
Q2_c3_factjoin|76926|76742|99.761|109|184|99.761
Q2b_ceiling_allactive|81079|76739|94.647|77026|287
Q3d_distinct|1986|1976|1982|99.496|99.799
Q3e_pin|2026-11-28 15:36:58.990032+00|t|2026-11-28 06:12:17.040594+00|81286|35772|1985
```

Q1 column order: CVE denom, CVE present, CVE %, all-active denom,
all-active present, all-active %, all-active missing, blank `cve_id`.
Q2 columns: CVE denom, fact-join present, fact-join %, stale, catalog,
CVE ceiling %. Q2b columns: all-active denom, ceiling numerator,
ceiling %, rows needed for 95%, shortfall. Q3e: transaction wall time,
recovery, winners watermark max, winners rows, EPSS facts, EPSS
distinct CVEs.

READ COMMITTED per-statement snapshots drift under concurrent writers:
Q2b reads 4 fewer all-active rows than Q1 in the same transaction
(81,079 vs 81,083). Percentages below use the Q1+Q2 snapshot where a
count is shared; Q2b shortfall (287) is identical on either snapshot.

| Measure | Count | % | Verdict vs 95% |
|---|---|---|---|
| C3 payload (row-weighted CVE-keyed) | 76,633 / 76,926 | **99.619%** | PASS |
| C3 fact-join (row-weighted CVE-keyed) | 76,742 / 76,926 | **99.761%** | PASS |
| All-active payload | 76,633 / 81,083 | 94.512% | FAIL |
| All-active ceiling | 76,742 / 81,083 | 94.646% | FAIL, short 287 |

Distinct (not the bar): 1,976/1,986 payload = 99.496%, 1,982/1,986
fact-join = 99.799%. EPSS catalog: 35,772 facts over 1,985 CVEs.

## Restatement against Wave 1 (corpus moved)

Wave 1 pin 2026-11-28 07:52Z: payload 99.732% (77,714/77,923),
fact-join 99.764%, all-active 94.667% (77,714/82,092), ceiling 94.694%
(77,739/82,092). Neither Wave 1 percentage reproduces; the cause is
corpus movement, not a definition change:

- Denominators shrank by 997 (CVE-keyed) and 1,009 (all-active); the
  winners row total is 81,286–81,290 at the pin.
- Stale rows grew 25 → 109 (+84), concentrated in CVE-2026-73646
  (85 npm rows, EPSS fact `score_date 2026-11-27 03:14Z`, payload still
  empty at 15:36Z) plus 24 rows across 8 other CVEs. The winners
  watermark max is 06:12Z (~9h24m old at pin): fresh EPSS facts are
  present but not yet reflected in winners payloads.
- Catalog misses are unchanged at 184 rows / 4 CVEs, and the fact-join
  rate moved only −0.003pp (99.764% → 99.761%). Distinct payload is
  unchanged at 1,976/1,986.

## The 4 FIRST catalog-miss CVEs

184 rows, 0 present, 0 active `epss_score` facts (no active fact found
for them in Q2 — distinct arithmetic: 1,986 winner CVEs minus 1,982
fact-join CVEs is exactly these 4; a tombstoned fact would look identical,
so the tombstone-inclusive scan stays owed for the P4 re-measure):

| CVE | Rows | Winners by ecosystem |
|---|---|---|
| CVE-2026-93753 | 101 | npm |
| CVE-2023-54404 | 72 | npm |
| CVE-2021-3533 | 10 | pypi |
| CVE-2026-9229 | 1 | npm |

Stale (fact exists, payload missing), 109 rows / 9 CVEs: CVE-2026-73646
×85, CVE-2026-64641 ×13, CVE-2026-49981 ×4, CVE-2026-63402 ×2, and
CVE-2026-124317 / CVE-2026-128086 / CVE-2026-128241 / CVE-2026-134703
/ CVE-2026-64643 ×1 each. These are rescore-recoverable once winners
resweep, and they alone separate payload (99.619%) from fact-join
(99.761%).

## GHSA-only / no-alias rows (excluded by construction)

4,151 GHSA rows (80 distinct) and 5 GO rows (GO-2026-5932 ×4,
GO-2026-6909 ×1) are effective-visible, all with empty
`epss_probability`, 0 `other` schemes, 0 blank `cve_id`. The
findings-derived EPSS planner admits CVE-shaped ids only and counts
the rest `ExcludedNonCVE`; alias-aware GHSA coverage is Phase 3 work
(3.1/3.2). This exclusion is the ruled denominator, not post-hoc
gaming; all-active 94.512% stays published beside the C3 number.

## Ceiling arithmetic

95% of 81,083 needs `ceil(0.95 × 81,083) = 77,029` present rows. The
recoverable ceiling adds every stale row to present:
`76,633 + 109 = 76,742`, i.e. 94.646% — still 287 rows short. No
rescore can pass an all-active 95% bar on this corpus; the 4 catalog
misses (184 rows) plus 4,156 non-CVE rows are unreachable by FIRST
enrichment as shipped. Under the CVE-keyed denominator the bar needs
73,080 rows and clears with 3,553 to spare.

## Ecosystem split (CVE-keyed missing payload)

npm 274/68,347, pypi 10/6,721, composer 4/545, gomod 1/169, os 3/62,
go 1/3, maven 0/892, rubygems 0/184. No other ecosystem has a
payload-missing write-up owed beyond Q3a.

## Rerun note (QA read replica, 2026-11-28 16:11:57Z)

The committed file now runs end to end (`ON_ERROR_STOP`, exit 0,
`ROLLBACK`, read-only on the caught-up replica). Q3f/Q3g were added after
the pin-stamped run to back two definition asides; the first full-file
run exposed a missing `FROM vis` in both (fixed, proof is this rerun).
Corpus moved slightly since the 15:36:58Z stamp; verdicts are unchanged:

```text
Q1_c3_payload|76923|76613|99.597|81079|76613|94.492|4466|0
Q2_c3_factjoin|76923|76739|99.761|126|184|99.761
Q2b_ceiling_allactive|81079|76739|94.647|77026|287
Q3d_distinct|1986|1976|1982|99.496|99.799
Q3e_pin|2026-11-28 16:11:57.82362+00|t|2026-11-28 06:12:19.93594+00|81286|35772|1985
Q3f_lowercase|76923|0
Q3g_percentile|76613|76613
```

Q3f: 0 lowercase rows among 76,923 CVE-keyed winners, so `~` and `~*`
agree on this corpus. Q3g: probability-keyed and percentile-keyed
present counts are identical (76,613 = 76,613). Payload drift vs the
stamp is −20 present rows (−0.022pp); stale grew 109 → 126;
catalog-miss steady at 184; ceiling shortfall steady at 287.
