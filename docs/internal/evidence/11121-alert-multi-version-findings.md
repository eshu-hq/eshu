# 11121: one alert finding per installed version of a package

Issue eshu-hq/eshu#11121, part of #10860, stacked on #11113. This note covers
the provider security-alert impact path, which is the alert-path twin of the
#11113 manifest-path fix. Before this change, an open alert produced at most
one finding. The candidate rows in the alert's manifest were reduced to one
row: the newest `ObservedAt`, then the lower fact id. Rows of one lockfile
share `ObservedAt`, so the lower fact id decided. A fixed row could therefore
hide an affected one. Now each distinct observed version in the alert's
manifest classifies once.

## What the change does

`appendSecurityAlertImpactFindings`
(go/internal/reducer/supplychain/core/security_alert.go) calls
`appendSecurityAlertVersionFindings` for each alert. That function works in
four steps:

1. It reads the exact candidates and the ambiguity bit from
   `securityalert.ExactSecurityAlertConsumptions`
   (go/internal/reducer/securityalert/reconciliation_match.go). These are the
   same rows and the same bit that `MatchSecurityAlertConsumption` uses, and
   both read one shared candidate collector.
2. It splits the candidates by observed version.
3. It picks one row per version through `SelectSecurityAlertConsumption`,
   which applies the old pick rule.
4. It classifies each picked row.

`securityAlertImpactAlreadyExists` now also compares the observed version, so
a finding that already exists for one version suppresses only that version. A
candidate set with no version keeps the single pick. An alert row with no
version keeps the old rule, under which any finding for the advisory
suppresses it. The rule is not symmetric: an existing finding with no version
no longer suppresses a versioned alert row, so a range-only advisory finding
and the alert's version-proven finding both stand. An ambiguous match still
yields nothing, even when the repositories involved hold different versions
(`TestSecurityAlertAmbiguousRepositoriesWithDifferentVersions11121`, review
F2). When the alert names no `ManifestPath`, every manifest of the repository
is an exact candidate, so the split gives one finding per version across all
of those manifests (review F6).

The impact path now also reads an alert row's `installed_version` when the row
has no observed version (review F4). Reconciliation and the manifest path
already did, so a row whose only version evidence is `installed_version`
(`TestSecurityAlertInstalledVersionRowIsVersioned11121`) no longer reads as
versionless on one path and versioned on the other. This can give an existing
versionless alert finding a version, and so a new identity, on its holder's
next pass.

Reconciliation keeps one decision per alert, and its consumption pick still
calls `MatchSecurityAlertConsumption`. The impact the decision reports does
change, as described next.

Alert reconciliation, a review addition: `matchSecurityAlertImpact`
(reconciliation_match.go) returned the first impact in envelope order for the
alert's repository, package and advisory, and ignored `observed_version`. With
one finding per installed version (#11113), an open alert could therefore
report the fixed copy's `not_affected_known_fixed` as `eshu_impact_status`, the
value the alerts API serves. Admission is
`securityAlertImpactMatches`: the same repository and package, and a CVE or
GHSA id the alert names. A future identifier match (for example #11103's
record ids) belongs in that predicate, before the rank. The rank runs first
over admitted findings whose version the alert's own manifest holds
(`securityAlertManifestVersions`, review F1). It falls back to every admitted
finding only when none of those matches. Within that set the most severe
status wins (`affected_exact`, then `affected_derived`, `possibly_affected`,
`unknown_impact`, `not_affected_known_fixed`), then the lower fact id. `securityAlertConsumptionForImpact` then points the decision's
dependency evidence and observed version at the row of that version.
`SecurityAlertImpact` gains `ObservedVersion`, read from the finding payload.
`TestReconciliationReportsTheAffectedVersionOfAMultiVersionLockfile11121` was
RED first. It failed with the fixed impact first, reporting
`not_affected_known_fixed (impact-fixed)`. Two mutations also fail it: ranking
by fact id alone, and never re-pointing the evidence.
`TestReconciliationRanksOnlyImpactsOfTheAlertManifest11121` was RED first for
review F1. The alert manifest holds only 3.1.0 and another manifest holds
3.0.3, and the row read `affected_exact (impact-z-affected)` while naming the
3.1.0 row as its evidence.

Two behavior changes for reviewers:

- The fix commit edited the RED test's impact ids to `impact-a-fixed` and
  `impact-z-affected`, so the fixed copy sorts first by fact id and a
  fact-id-only rank also fails. That strengthens the test without changing
  its assertion.
- When different advisories match one alert, for example a CVE-keyed and a
  GHSA-keyed finding, the pick used to be the first in envelope order. It is
  now the most severe, then the lower fact id.

The change adds no new lock, queue, batch, knob, I/O, retry path, winners SQL,
or migration. Alert findings reach the winners through the same writer and
capture owner as the #11113 findings, and the #11113 live winners proof
(`TestSupplyChainImpactWinnersResweepMultiVersionRepositoryLive`) covers that
key and write path.

## QA false negative (read-only census, 2026-11-21)

Root-Cause Evidence: the QA read replica was read with
`default_transaction_read_only=on` and a 60-second statement timeout. It held
112 active `security_alert.repository_alert` facts, all npm and open, in one
scope. The winners held 25,802 rows for those packages, and 3 of them came
from the alert path (their winning finding's `evidence_path` carries
`security_alert.repository_alert`). The rest were already covered by
advisory-path findings, and `securityAlertImpactAlreadyExists` skips the alert
for those. All 3 groups are in one repository, and each holds two versions in
one `package-lock.json`:

| Package / advisory | Versions | Winner | Result |
| --- | --- | --- | --- |
| `dompurify`, `GHSA-mrv2-3pqr-j5jr` (`<= 3.4.16`) | 3.4.16, 3.4.18 | 3.4.18 `not_affected_known_fixed` | false negative |
| `dompurify`, `GHSA-cg2g-xh64-v4q7` (`>= 3.4.4, <= 3.4.16`) | 3.4.16, 3.4.18 | 3.4.18 `not_affected_known_fixed` | false negative |
| `serialize-javascript`, `CVE-2026-115897` | 6.0.2, 7.0.6 | 6.0.2 `affected_exact` | 7.0.6 finding missing |

In total that is 3 affected groups, 2 false negatives, and 1 repository.

The RED fixture `TestSecurityAlertFindingsClassifyEachInstalledVersion11121`
shows the problem on the #11113 head. Take an open alert on fast-uri
`< 3.1.0` and a lockfile with 3.0.3 and 3.1.0. With the fixed row at the lower
fact id, only 3.1.0 `not_affected_known_fixed` is reported. With the affected
row at the lower fact id, only 3.0.3 `affected_exact` is reported.

`TestSecurityAlertExistingFindingSuppressesOnlyItsVersion11121` tests the
already-exists hypothesis. On the base, an existing finding for 3.1.0
suppressed the whole alert, so 3.0.3 was never reported. That proves the
hypothesis.

## Cost

No-Regression Evidence: `BenchmarkBuildSupplyChainImpactFindingsAlertEPSS`
(one alert, one consumption, the full `BuildSupplyChainImpactFindings`,
`-benchtime=2000x -count=3`, go1.26.11 linux/arm64, shared host). The base is
the #11113 head `e3735e99ec`.

| Run | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| Base | 18,907 / 20,349 / 23,870 | 15,900 | 119 |
| Fix | 16,309 / 17,751 / 15,044 | 15,930 | 120 |

The timings overlap, and the fix costs one allocation and about 30 bytes per
alert. Per alert, the work is one grouping pass over its exact candidates and
one sort of its distinct versions. On QA the extra output is the 3 groups
above, one extra finding each, times their holder copies. That is negligible
next to the #11113 write burst it ships with.

## Deploy and convergence

This change ships in the same release (R2) as #11113 and inherits its staged
deploy note (docs/internal/evidence/11113-multi-version-findings.md,
"Deploy and convergence"). In short:

- The fix takes effect for a holder only when that holder re-passes. No stamp
  forces a rerun, and a winners full rebuild does not heal it.
- The rollback is image-only.
- Post-R2 acceptance: re-run this census on the QA read-only replica. The
  target is 2 false negatives going to 0, plus the missing serialize-javascript
  7.0.6 finding present. Also check that the two dompurify alerts'
  reconciliation rows read `affected_exact` with observed version 3.4.16.
  Report the residue.

Observability Evidence: no new signal. The #11113 sub-signal
`multi_version_repository_groups` counts alert-path findings too;
`TestSecurityAlertMultiVersionCountsInSubSignal11121` pins that, and
docs/public/reference/telemetry/supply-chain-active-evidence.md documents it.
