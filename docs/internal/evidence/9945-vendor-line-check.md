# #9945: a vendor advisory's release line is checked against the host

Refs #9945. Follows #9929.

## What changed

The OS advisory join trusted a vendor-sourced `affected_package` fact (source
equal to the installed row's `vendor_advisory_source`) as written for the host's
release, but never compared the line. Two releases of one distribution can share
a version string, so a Debian 11 list could name a Debian 12 host.

The join now reads the fact's purl `distro` qualifier through `osHostReleaseLine`
(the table that keys the installed row) and compares it with the host's line,
before the fact reaches either source-package or binary-package matching:

| Fact line vs host | Selection | Verdict effect | Counter `result` |
|---|---|---|---|
| Same line | `ReleaseLine` set | Unchanged: list match stays `affected_exact` | `vendor_source` |
| Another release or vendor | Marked unlisted; fixed versions and list cleared | Weak `os_release_not_in_advisory` signal at most | `vendor_line_mismatch` |
| Unreadable, absent, conflicting, or host line unknown | No `ReleaseLine`; fixed versions cleared, list and ranges kept | `possibly_affected` / `os_release_unverified` when a list or range names the host; never `affected_exact`, never `not_affected_known_fixed`, no fix offered | `vendor_line_unknown` |

The rule, as the telemetry page, the package README, and the code state it:

> On the OS advisory path a fact is release evidence only when `ReleaseLine` is
> set, which happens only after the join verified the fact against the installed
> row's line (a tagged OSV range naming the line, or a vendor purl `distro`
> equal to the line). A vendor-sourced fact (any non-OSV source on an OS fact)
> without a verified line never yields `affected_exact` and never yields
> `not_affected_known_fixed`; it contributes `possibly_affected`
> (`os_release_unverified`) when its list or ranges name the host, and its fixed
> versions are withheld from the finding and the remediation. A vendor fact
> whose purl names another readable line is `ReleaseUnlisted`: fixed and exact
> versions cleared, only the `os_release_not_in_advisory` signal remains.

Trust is positive: `osAffectedFactCarriesReleaseEvidence` is `ReleaseLine != ""`
and nothing else, and the earlier `VendorLineUnverified` flag is gone. A flag
whose absence granted trust left two paths open that selection never reached.

All readers of a vendor fact follow the rule:

- The row join (`selectOSAdvisoryRelease`) line-checks each vendor fact against
  the installed row.
- The authorized-scan conflict pass (`filterAuthorizedBranchesToHostLine`) keeps
  a vendor branch only when its line is the host's, appended as
  `selectOSVendorFactLine` returns it. Branches for another release, with no
  readable line, or on an unknown host line are dropped, so the primary verdict,
  which already fails closed, stands.
- An OS-family SBOM component with no joined installed row runs the same
  boundary with the zero row (`index.go`), so every vendor fact there takes the
  unknown arm.
- A non-OSV OS fact whose source is not the installed row's vendor (blank, `nvd`,
  `glad`, `ghsa`, another distribution) takes the unknown arm too, with no host
  line, so it never earns `ReleaseLine` from a matching purl and offers no fix,
  even beside a verified fact of the host's own vendor. Before this round that
  fact kept its fix whenever the row had a vendor source, and its scalar or
  range fix could read `not_affected_known_fixed`.
- The legacy rpm list loop (`evaluateRPMVersionMatch`, a fact with no ECOSYSTEM
  range, and the SBOM-only rpm path) reads an exact-version list match as
  `affected_exact` only for a fact that carries `ReleaseLine`. The host-line
  table has no rpm row, so no rpm fact can earn one today: an rpm list match
  reads `possibly_affected` / `os_release_unverified`. An rpm vendor fact cannot
  read exact until the table gets an rpm row. A later unreadable entry still
  reads `malformed_advisory_range`.

Non-OS facts, OSV, and a fact with no OS shape are not line-checked.

Limits, stated plainly:

- The SBOM-only path does not read the component purl's own `distro` qualifier
  as the host line. A vendor fact on that path therefore reads
  `possibly_affected` even when its line equals the component's. That is a
  second host-line source with its own contract, and tagged OSV facts are not
  narrowed on that path either.
- A vendor record that carries only a scalar fix, with no list and no range,
  has nothing left once the fix is withheld, so an unknown-line record of that
  shape reads `possibly_affected` / `version_not_in_advisory_range`, not
  `os_release_unverified`. It is still never exact or known fixed.
- Rows with no image digest share one finding that the first row decides, so
  their order can flip a verdict (#9984, deferred by arbiter ruling; the same
  class already exists for tagged OSV facts). Digest-resolved fleets are order
  independent (`TestVendorLineVerdictIsFleetOrderIndependentWithDigests`).
- Until #9914 decision 8 lands, `vulnerable_range` and `advisory_id` may come
  from a foreign-line or unknown-line vendor record when it sorts first by
  `AdvisoryID`. The verdict and `FixedVersion` do not: the unknown arm clears
  the fix and the mismatch arm clears the fix and the list. Base was worse, so
  this is not a regression.

Producer note: the carrier is the purl `distro` qualifier in `debian-12`,
`ubuntu-22.04`, `alpine-3.18` form. OSV's own Debian purls use codenames
(`distro=bookworm`), which `osHostReleaseLine` cannot read; those read as
`vendor_line_unknown` and fail closed. No in-tree producer emits a vendor-sourced
OS fact yet, so no live fact changes.

Files outside the #9914/#9945 partition (decision 13) that this change edits,
each for a reason named above: `index.go` (the SBOM-only boundary),
`derived_conflict.go` (the authorized-scan pass), `rpm_version.go` (the rpm list
rule), `os_package_advisory_load.go` (the two counter sub-signals),
`supplychainmodel/types.go` (the `ReleaseLine` field and its doc),
`telemetry/instruments.go` (description text only), and the
`vulnerabilityintelligence/supply-chain-demo.json` cassette (one purl line).

## Proof

- RED first: `os_vendor_line_join_test.go` (commit 2c769b19d3) covers the binary
  name join and the source package join for dpkg and apk, each with a differing
  line, an unknown line, and a matching-line control, in both fact orders.
- Unit tests: `os_vendor_line_test.go` (line reader table, three outcomes,
  idempotence, input slice untouched, an OSV-only input unchanged and 0 allocs, non-host sources taking the unknown arm,
  permutation independence, result labels, counter and success-log sub-signals
  through the real join stats, host-line OSV against a vendor list) and
  `os_vendor_line_authorized_test.go`.
- Authorized pass: `TestAuthorizedScanNeverTrustsAnUnverifiedVendorBranch` goes
  through `BuildAuthorizedScanImpactFindings` with a codename purl, no purl, a
  foreign line on an unknown host, and an own-line branch beside a codename
  branch (each `possibly_affected` / `os_release_unverified`), plus two
  own-line controls that stay `affected_exact`.
- SBOM-only path: `TestSBOMOnlyVendorFactIsNeverExact` (dpkg and apk; the
  component's own line, another release, an unreadable line, no purl; both fact
  orders) and `TestSBOMOnlyVendorFixIsNeverPresentedAsTheHosts`.
- Unknown-line fix: `TestUnknownLineVendorFixIsNeverTheHostsFix` (dpkg and apk,
  scalar and range, both fact orders, plus a verified own-line fact that still
  decides).
- Evidence rule: `TestReleaseEvidenceIsOnlyAVerifiedLine`, and
  `TestRPMVendorFactIsUnverifiedUntilTheHostLineIsReadable` for the rpm path.
- Non-host source and rpm list (this round): `TestNonHostSourceFixIsNeverKnownFixedBesideAVerifiedFact`
  (a Debian 12 host, a verified own fact, and a fact from each of blank, `nvd`,
  `ubuntu`, `glad`, `ghsa` with a scalar or range fix below the host, both fact
  orders; red before, green after), `TestRPMExactVersionListNeedsReleaseEvidence`
  (joined row and SBOM-only, vendor and blank source),
  `TestRPMExactVersionListWithReleaseEvidenceStaysExact`, and
  `TestUnknownLineVendorMalformedRangeStillFailsClosed`.
- Mutation check: each of these mutants fails at least one named top-level test:
  unknown read as matched, mismatch read as matched, foreign list kept, the
  authorized filter keeping a foreign vendor branch, conflicting qualifiers
  accepted, an unknown host line ending the selection pass (M14), the vendor
  predicate ignoring the OSV exclusion (M19), the evidence rule restored to the
  source-based rule, a fix restored for an unverified vendor fact, the unknown
  arm keeping its fixed versions, the SBOM-only boundary removed, the non-host
  detection removed, a non-host fact checked against the host line, the rpm list
  granting exact without evidence, and the rpm unverified read dropped. Two
  guards that survived before are now held: the unverified-vendor arm dropping
  `malformed` (N9, `TestUnknownLineVendorMalformedRangeStillFailsClosed`) and the
  unverified-vendor predicate ignoring `ReleaseUnlisted` (N6,
  `TestSelectOSVendorFactLine`). N6 is held by the predicate unit row only:
  `osPackageRangesVerdict` takes a `ReleaseUnlisted` fact through its own arm
  first, so the clause changes no verdict through the builder today. Survivors
  that remain, all graded P3: the source not trimmed (decode trims, so it is
  equivalent) and two of the three OS-shape clauses of the non-host predicate
  (the `os://` package id and the purl clauses; the ecosystem clause is now held).
  The old unverified-flag mutant no longer exists with the flag.
- Parity differential, unchanged classes: 128 verdict rows produced by the base
  65fa0f2f2a and by this change are byte-identical (same sha256). The rows are
  118 OSV (alpine, debian and ubuntu fixtures, each in both fact orders), 2 OSV
  source-package joins, and 8 matching-line vendor facts (dpkg and apk, binary
  and source-package join). They hold no NVD row and no authorized-scan row.
- Parity differential, wider classes: 68 more rows run at base and at this
  change. They are 8 authorized-scan rows over the OSV fixtures, and 60 rows
  that join one fact to a Debian 12 host, with the fact's source in blank, `nvd`,
  `glad`, `osv`, `ghsa`, `ubuntu`, its purl in five shapes (none, no qualifier,
  `debian-12`, `debian-11`, a codename), alone and beside a verified Debian 12
  vendor fact. 43 rows are byte-identical. The other 25 are the "beside a
  verified vendor fact" rows whose extra fact has a source other than the
  host's: status stays `affected_exact`, the reason moves from
  `dpkg_affected_range` to `dpkg_exact_affected_version`, because that extra
  fact no longer counts as release evidence and the verified vendor fact's list
  decides. No status changed in these 68 rows; that sentence holds for the 68
  rows only. Where an extra fact is the only thing that names the host, its
  status does change (it reads `possibly_affected`), which is the "changed on
  purpose" class below. After the non-host fix of this round the same 25 rows
  still differ from base in reason only (re-run: the same 43 identical, 0 status
  changes). The probe is not committed; its inputs are the ones listed here.
- Changed on purpose, no producer: a blank-source or non-OSV-source OS fact that
  reaches the evaluator without a verified line used to read as release
  evidence and now does not. The full list of base fixtures that flipped (the
  base test files run against this change's code):
  - Blank source, flipped by the evidence rule:
    `TestCov6EvaluateOSPackageVersionMatch` (apk ranged),
    `TestSupplyChainImpactExactOSPackageReasonsQualifyForPreciseProfile` (dpkg,
    apk), `TestRangeEventsAreEvaluatedInVersionOrder` (debian, alpine),
    `TestRangeEventOrderParityAgainstOSVOracle`. These now carry a verified
    `ReleaseLine`.
  - Named vendor source with no purl line, which now needs the purl:
    `TestAuthorizedScanImpactPartitionSameDigest`, `...ConflictingFixedVersions`,
    `...DisjointAffectedBranches`, `...PublicPrivateExtensionIsolation`,
    `...DisjointBranchGapStaysPartial`;
    `TestBuildSupplyChainImpactFindingsLeavesOSPackageSubjectDigestBlankWithoutScannerAnalysis`,
    `...AnchorsOSPackageSubjectDigestOnScannerAnalysis`,
    `...RemediationSupportsVendorAPKBranch`, `...RemediationSupportsVendorDPKGBranch`,
    `...UsesVendorAPKOSPackageEvidence`, `...UsesVendorDPKGOSPackageEvidence`,
    `...UsesVendorRPMOSPackageEvidence`, `...UsesExactAPKKnownFixedProfile`,
    `...QuarantinesOSPackageMissingInstalledVersion`;
    `TestGoldenCassetteDebianOSPackageChainSynthesizesFinding`;
    `TestOSVOSAdvisoryVersionListNeverExact/vendor-sourced`;
    `TestSelectOSAdvisoryRelease/vendor-sourced_fact`.
  - Direct matcher fixtures that were given a `ReleaseLine`:
    `TestUntaggedVendorVersionListBeatsOSVSingleFix`,
    `TestVendorVersionListBeatsOSVRanges`, `TestVendorVersionListBeatsOSVRangesRPM`,
    `TestVendorVersionListUnreadableEntries`, `TestVendorVersionListEdgeCases`,
    `TestUnlistedReleaseVendorVersionListWins`.
  - Moved by the rpm list rule of this round: `TestCov6RPMVersionMatchEdges`
    (its hit fixture now carries a `ReleaseLine`),
    `TestBuildSupplyChainImpactFindingsUsesVendorRPMOSPackageEvidence` (status and
    reason; the profile moves from precise to comprehensive),
    `TestSBOMPathRetainsVendorOSVersionAuthority` and
    `TestSBOMSubjectConflictingOSRowsStayPartial` (exact becomes
    `possibly_affected` / `os_release_unverified`).
  Only one base assertion moved with no input change: the vendor rpm
  `FixedVersionSource` in `os_package_test.go` is now withheld. No vendor-sourced
  OS producer exists, so no live finding moves.

## Performance

No-Regression Evidence: no query, index, lock, claim, or lease changed; this is
in-memory join logic. `BenchmarkSelectOSAdvisoryRelease` and
`BenchmarkEvaluateOSPackageVersionMatchAndClassify`, three samples each at the
final tree, run one after another (not interleaved) on a shared machine with a
load average near 5 to 6, so read the ns column as rough:

| Row | ns/op (3 samples) | Allocs |
|---|---|---|
| untagged OSV fact, row with a vendor source (the real shape) | 39.9, 41.1, 43.1 | 0 |
| untagged OSV fact, row without a vendor source | 36.6, 37.1, 38.4 | 0 |
| untagged OSV fact, zero row (SBOM-only) | 36.9, 37.2, 38.2 | 0 |
| tagged, host line listed | 199.0 to 208.2 | 5 (352 B) |
| vendor fact, line matched or mismatched | 208.1 to 219.7 | 5 (304 B) |
| vendor fact, unknown line | 152.7 to 159.7 | 3 (272 B) |
| vendor fact, zero row (SBOM-only) | 107.4 to 109.9 | 1 (240 B) |
| dpkg exact affected, verified fact (bench fixture now sets `ReleaseLine`) | 305.0 to 307.8 | 0 |
| dpkg exact known fixed | 290.6 to 307.1 | 0 |

The untagged OSV hot path keeps zero allocations, and
`BenchmarkBuildSupplyChainImpactIndexWithQuarantine` still reports 100159
allocs/op. Every fact that is neither tagged nor the host vendor's now has its
source classified (R2-1), so the real-shape untagged OSV row moves from about 20 ns to about 40 ns per fact: one
source classification, no allocation. The earlier statement that this second row
is unreachable in production no longer holds and is withdrawn. The "dpkg exact
affected" bench fixture had no `ReleaseLine`, so after the positive evidence rule
it measured the unverified path (440 ns, 1 alloc); it now sets one and measures
the verified path the finding takes. The new SBOM-only guard asks
`osPackageFamilyFromPURL` for every component finding with no installed row,
language packages included;
`TestLanguageComponentPURLSkipsTheOSBoundaryWithoutAllocating` pins that it
allocates nothing for npm, pypi, golang, maven and empty purls. The vendor-arm
costs are paid once per non-OSV fact per (CVE, package) group, and no producer of
such facts exists yet. A same-run A/B against base for these rows was not
repeated this round.

Observability Evidence: the existing counter
`eshu_dp_supply_chain_impact_os_advisory_release_selections_total{source,result}`
gains `result` values `vendor_line_mismatch` and `vendor_line_unknown`
(`vendor_source` now means the line matched). The reducer success log carries
`os_advisories_vendor_line_mismatch` and `os_advisories_vendor_line_unknown`
beside the existing release sub-signals. An operator reads a rising
`vendor_line_unknown` share as a producer purl shape the reader does not cover,
and `vendor_line_mismatch` as a vendor feed carrying other releases' facts.
The telemetry reference page and the coverage row are updated.

## Not checked

- `internal/reducer/maintenance/census` fails here because it needs an
  `eshu-deploy` checkout (`ESHU_DEPLOY_CHECKOUT`); the package is untouched.
- Wall-time on a quiet host (the benchmarks ran on a shared, loaded machine, one after another).
- Any deployed environment: no vendor-sourced OS fact exists in one.
