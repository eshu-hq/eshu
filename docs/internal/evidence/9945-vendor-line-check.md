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

All three readers of a vendor fact follow the rule:

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
- The legacy rpm loop (`evaluateRPMVersionMatch`, a fact set with no ECOSYSTEM
  range) reads an exact-version list match as `affected_exact` without asking
  for release evidence. The host-line table has no rpm row, so no rpm vendor
  fact can be verified either way. No producer emits one. It sits outside this
  change's files and is recorded for the owner of `rpm_version.go`.

Producer note: the carrier is the purl `distro` qualifier in `debian-12`,
`ubuntu-22.04`, `alpine-3.18` form. OSV's own Debian purls use codenames
(`distro=bookworm`), which `osHostReleaseLine` cannot read; those read as
`vendor_line_unknown` and fail closed. No in-tree producer emits a vendor-sourced
OS fact yet, so no live fact changes.

## Proof

- RED first: `os_vendor_line_join_test.go` (commit 2c769b19d3) covers the binary
  name join and the source package join for dpkg and apk, each with a differing
  line, an unknown line, and a matching-line control, in both fact orders.
- Unit tests: `os_vendor_line_test.go` (line reader table, three outcomes,
  idempotence, input slice untouched, non-host facts unchanged and 0 allocs,
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
- Mutation check: every mutant below fails at least one named top-level test:
  unknown read as matched, mismatch read as matched, foreign list kept, the
  authorized filter keeping a foreign vendor branch, conflicting qualifiers
  accepted, an unknown host line ending the selection pass, the vendor predicate
  ignoring the OSV exclusion, the evidence rule restored to the source-based
  rule, a fix restored for an unverified vendor fact, the unknown arm keeping its
  fixed versions, the SBOM-only boundary removed, and the zero-row vendor
  detection removed. The old unverified-flag mutant no longer exists with the
  flag.
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
  decides. No status changed. The probe is not committed; its inputs are the
  ones listed here.
- Changed on purpose, no producer: a blank-source or non-OSV-source OS fact that
  reaches the evaluator without a verified line used to read as release
  evidence and now does not. Legacy tests that built such facts directly now
  carry a verified `ReleaseLine` (the range-event order tests and their OSV
  oracle, the apk range case in the OS coverage test, and the detection-profile
  reasons), or a purl line and a vendor source (the alpine known-fixed profile
  test and the unlisted-release vendor list test). The rpm vendor path reads the
  withheld fix. No vendor-sourced OS producer exists, so no live finding moves.

## Performance

No-Regression Evidence: no query, index, lock, claim, or lease changed; this is
in-memory join logic. `BenchmarkSelectOSAdvisoryRelease`, three samples at this
change on a quiet host (load average 4.6):

| Row | ns/op (3 samples) | Allocs |
|---|---|---|
| untagged OSV fact, row with a vendor source (the real shape) | 20.1, 20.4, 20.9 | 0 |
| untagged OSV fact, row without a vendor source | 37.3 to 39.9 | 0 |
| untagged OSV fact, zero row (SBOM-only) | 37.9 to 39.5 | 0 |
| tagged, host line listed | 193.5 to 194.2 | 5 (352 B) |
| vendor fact, line matched or mismatched | 200.5 to 204.3 | 5 (304 B) |
| vendor fact, unknown line | 146.5 to 147.5 | 3 (272 B) |
| vendor fact, zero row (SBOM-only) | 102.9 to 107.7 | 1 (240 B) |

The untagged OSV hot path keeps zero allocations. A row with no vendor source
never joins an advisory, so the second row's extra 18 ns (one source
classification) is not reachable in production. The new SBOM-only guard asks
`osPackageFamilyFromPURL` for every component finding with no installed row,
language packages included; `TestLanguageComponentPURLSkipsTheOSBoundaryWithoutAllocating`
pins that it allocates nothing for npm, pypi, golang, maven and empty purls. The
costs above are paid once per vendor fact per (CVE, package) group, and no
producer of such facts exists yet. A same-run A/B against base for these rows was
not repeated this round.

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
- Wall-time on a quiet host (the benchmark ran on a shared, loaded machine).
- Any deployed environment: no vendor-sourced OS fact exists in one.
