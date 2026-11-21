# 11113: one finding per installed version of a package in a repository

Issue eshu-hq/eshu#11113, part of #10860. The binding arbiter ruling of
2026-11-21 (#11102 outcome, item 2) found this a defect and ruled the fix:
"classify once per (repository, distinct installed version) as #10057 does
per (release line, installed version), rows with no version keep the current
rule". It also ruled out three alternatives: "dropping observed_version from
the key, query-time dedup, a new 'best single version' rank". Before this change, a repository that installs several
versions of one package got one manifest finding, judged on whichever
consumption row loaded first. A fixed row could therefore hide a vulnerable
one. Now each distinct observed version in the repository classifies once.
This note records the false negative on QA and the cost of the fix.

## What the change does

`appendSupplyChainImpactManifestFindings`
(go/internal/reducer/supplychain/core/sbom_subjects.go) runs each
repository's rows through `splitRepositoryConsumptionByVersion`
(go/internal/reducer/supplychain/core/manifest_consumption.go). That helper
makes one group per distinct observed version, ordered by version. A version
is read by `consumptionObservedVersion`, the same rule that sets the finding's
`observed_version`.

- Inside a group, rows sort preferred first, then by fact id. The row that
  represents the group therefore does not depend on load order either.
- A repository whose rows carry no version keeps one finding, as before.
- A row with no version beside versioned rows adds no finding.
- `observed_version` stays in the writer and winners keys. There is no
  query-time dedup and no best-version rank.

The change adds no new lock, queue, batch, knob, I/O, or retry path.

## QA false negative (read-only census, 2026-11-21)

Root-Cause Evidence: the census read the QA read replica with
`default_transaction_read_only=on` and a 60-second statement timeout. It
covered all repository-scoped npm winners rows (263 packages, 69,564 rows,
64,100 (repository, CVE, advisory, package) groups) and the active-generation
npm dependency `content_entity` rows (26,704 rows). The analysis ran on a
disposable local copy. Each exact version's verdict comes from the winners of
the same advisory; 41 versions that carry two verdicts were excluded.

- 3,463 groups hold active versions on both sides of the fix boundary.
- 1,074 of those have no `affected_exact` winner and read
  `not_affected_known_fixed` only. They span 95 repositories and 265
  (CVE, advisory, package) triples. The top packages are axios, brace-expansion
  and js-yaml.
- Example: a repository holds brace-expansion 1.1.15, 2.0.2, 2.1.1 and 5.0.12
  for CVE-2026-33750, and its only winner is 1.1.15 `not_affected_known_fixed`.
  For that advisory, 2.0.2 is `affected_exact`.

The RED fixture `TestManifestFindingsClassifyEachInstalledVersion11113` shows
the order dependence on the base. With the fixed row first, only
5.0.13 `not_affected_known_fixed` is reported. With the affected row first,
only 1.1.11 `affected_exact` is reported. The live proof
`TestSupplyChainImpactWinnersResweepMultiVersionRepositoryLive` shows the
holder disagreement on the base: two holders with opposite load orders each
keep one version, so both winners keys read `source_count` 1.

## Cost

Performance Evidence: everything below ran on go1.26.11 linux/arm64 against a
disposable Postgres 18, on a shared host (load average 25-41), so copies and
allocations carry the weight. The base is `e244c42b73`. The corpus has the
QA brace-expansion shape:

- one advisory, fixed in 2.0.2;
- 252 repositories, 155 of which install 2 to 4 lockfile versions;
- 510 consumption rows per holder, each holder in its own load order.

The runs used the real handler, `PostgresSupplyChainImpactWriter`, queue, and
keyed winners resweep.

- **Finding rows (149 holders).** 37,548 rows before (149 x 252) and 71,371
  after (149 x 479), a factor of 1.90 on this shape. The census gives the
  npm-wide factor on QA as 1.298 (64,100 groups to 83,193 keys). Winners keys
  stay at 479, because the disagreeing holders together already covered every
  version; each key's copies grow to every holder.
- **Pass cost, interleaved A/B (40 holders, two databases, three rounds).**

  | Run | p50 per pass | Total per round |
  | --- | --- | --- |
  | Base, steady | 436 / 495 / 458 ms | 17.9 / 20.0 / 19.3 s |
  | Fix, steady | 664 / 682 / 589 ms | 29.7 / 27.5 / 24.9 s |

  That is +35-40% per steady pass for 1.90 times the findings.
- **Pass cost, sequential (149 holders).** Base steady p50 is 1324 ms. The
  transition pass (base state, then the fixed binary) has p50 1623 ms and a
  maximum of 4986 ms.
- **Winners.** A full `RebuildAllWinners` takes 0.75-0.92 s on the base and
  1.39-1.98 s with the fix, linear in copies. The keyed tick after the
  149-holder transition took 6.73 s, against 5.54 s for the base first pass.
  The first keyed tick on a never-analyzed scratch database took 46-47 s.
  With `ANALYZE` first, the same tick took 3.8 s, so this is a
  planner-statistics effect of the scratch database and not part of the code
  path.
- **Classify.** `BenchmarkManifestMultiVersionFindings11113` (same shape,
  `-benchtime=20x -count=3`): base 5.9-7.8 ms/op, 6.63 MB/op, 53.0k allocs,
  252 findings. Fix 9.96-10.15 ms/op, 11.20 MB/op, 87.9k allocs, 479 findings.
  Bytes per finding drop from 26.3 KB to 23.4 KB.

## Deploy and convergence

The binding arbiter ruling on sequencing and deploy timing (2026-11-21) says
#11113 "needs no full winners resweep and no contract-version bump" and
deploys "in the NEXT roll (R2), bundled with #11117 PR1+PR2". Its
required-in-PR items are below. The winners SQL
is unchanged and there is no migration: `observed_version` was already in the
winners key (`supply_chain_impact_canonical_winners_store.go`). The extra
findings reach the winners through the existing `fact_upsert` capture owner
(`supply_chain_impact_winners_capture.go`). The dirty identifier carries no
`observed_version` (`core/winners_keyed.go`), so one keyed recompute produces
every version key under it.

**Rerun path.** The fix takes effect for a holder only when that holder runs
its next pass. No stamp forces a rerun, and a winners full rebuild does not
heal it, because it reads the findings the holders already wrote. Until a
holder re-passes, its rows keep the old one-version answer. A forced holder
re-drive needs its own arbiter review; there is no hand-SQL path.

**Write burst.** Each holder's first pass on the new binary adds one finding
per extra installed version and retracts nothing, unless the old winning row
had no version. That is about 1.3 times the finding rows npm-wide (QA census)
and 1.9 times for brace-expansion-shaped packages (this corpus), across the
roughly 149 holder copies per finding. After the burst, steady passes of
holders with multi-version repositories cost about 35-40% more.

**Lag exposure.** At most 19,093 extra distinct dirty identifiers
(83,193 - 64,100) appear. Each one is re-marked once per holder pass, about
149 holders, for an upper bound of about 2.8M re-marks (arithmetic, not
measured). They land on a keyed path that already runs at about 6 keys/s and
near 100% duty (#11120). Dirty-log depth and watermark lag will therefore
rise while holders re-pass. `overKeyedCap` cannot trip: the cap is 40,063
identifiers, the window is 5,000 dirty rows, and marks from many holders
collapse to one dirty-log row per identifier.

**ANALYZE is not a pre-step.** The 46-47 s tick above came from a
never-analyzed scratch database. Watch
`eshu_dp_supply_chain_impact_winners_resweep_duration_seconds{path="keyed"}`
and the tick-phase histogram. If keyed tick time is more than 10% over the
pre-roll band after the first holder passes, `ANALYZE` of the winners table,
the dirty log, and the staging table is the remedy. `fact_records` is
analyzed only when an `EXPLAIN` shows a misestimate.

**Signals for the R2 note:**

- the `multi_version_repository_groups` sub-signal;
- dirty-log depth and oldest-age gauges;
- winners resweep duration by path;
- winners watermark age;
- `fact_records` growth;
- the full-start reason from #11117. A `path="full"` sample with reason
  `keyed_cap` after the startup full is a bug.

**Suppressions have no version.** A suppression scope names CVE, advisory,
package, purl, repository, and subject digest, but never an installed version
(`vulnerabilitySuppressionScope` in
go/internal/reducer/supplychain/core/evaluation.go). An existing suppression
on (repository, CVE, package), such as an accepted-risk or not-affected
decision taken when the repository showed only the fixed version, therefore
also covers the affected version this change surfaces. Operators should
review such suppressions after R2. This is verified from the scope type; the
QA count of such suppressions is NOT_CHECKED.

**Rollback.** Rollback is image-only, with no data step and no full rebuild.
An old-binary holder pass emits one finding per repository, and the writer
retracts the rest through the `fact_retract` capture owner. Rows of holders
that do not re-pass remain, and they are true findings.

**Post-R2 acceptance.** Re-run the straddle census on QA (read-only replica)
after the R2 deploy. The target is 1,074 known-fixed-only straddle groups
going to 0. Report the residue, split into holders that have not re-passed
and anything else.

**Known limits.**

- Versions split on the exact string. Two spellings of one semver version (for
  example `1.2.3` and `v1.2.3`) form two groups. A declared exact pin that
  differs from the lockfile's version also adds a finding.
- Within one version, the representative row is preferred first, then the
  lower fact id. A development-scope row can therefore represent a version
  that a runtime row also installs.
- The live winners proof drives the `reducer_package_consumption_correlation`
  arm. The `content_entity` manifest arm, which the QA findings use, is
  covered by the unit tests in `multi_version_findings_11113_test.go`.

The figures in this note come from a synthetic QA-shaped corpus at load
average 25-41: copies and allocations carry the weight, and the timings are
noisy. The QA counts are from the 2026-11-21 read-only census.

Observability Evidence: each pass writes the `multi_version_repository_groups`
sub-signal, the number of (repository, CVE, advisory, package) groups whose
final findings hold two or more observed versions, even when it is zero. It
carries no metric, label, or CVE id. `TestMultiVersionRepositoryGroupsSubSignal11113`
pins the sub-signal, which is documented in
docs/public/reference/telemetry/supply-chain-active-evidence.md.
