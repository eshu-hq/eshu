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
| Another release or vendor | Marked unlisted and unverified; fixed versions and list cleared | Weak `os_release_not_in_advisory` signal at most | `vendor_line_mismatch` |
| Unreadable, absent, conflicting, or host line unknown | Marked unverified | No release evidence; never `affected_exact` on the row join or the authorized-scan pass | `vendor_line_unknown` |

The authorized-scan conflict pass reads vendor facts twice.
`filterAuthorizedBranchesToHostLine` drops a vendor branch whose line is known
and differs, so a foreign fix is not offered as the host's. The one branch that
then applies is line-checked like the join checks it
(`selectApplicableAuthorizedBranch` calls `selectOSVendorFactLine`), so a branch
with a foreign, unreadable, or unverifiable line reads `possibly_affected` with
`os_release_unverified` instead of re-deriving `affected_exact`. Non-OS facts,
OSV, NVD, GLAD, and another vendor's facts are not line-checked and are returned
unchanged.

Not covered: an SBOM component with no joined installed row never reaches the
join, so a vendor fact there is not line-checked. Rows with no image digest
share one finding that the first row decides, so their order can change a
verdict; digest-resolved fleets are order independent
(`TestVendorLineVerdictIsFleetOrderIndependentWithDigests`).

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
- Mutation check: eight mutants of the rule (unknown read as matched, mismatch
  read as matched, foreign list kept, unverified flag ignored, authorized filter
  keeps the foreign branch, conflicting qualifiers accepted, an unknown host line
  ending the selection pass, and the other-line helper ignoring the fact source)
  each fail at least one test.
- Parity differential: 128 verdict rows produced by the unchanged code at base
  65fa0f2f2a and by this change are byte-identical (same sha256). The rows are
  118 OSV (alpine, debian and ubuntu fixtures, each in both fact orders), 2 OSV
  source-package joins, and 8 matching-line vendor facts (dpkg and apk, binary
  and source-package join). They hold no NVD row and no authorized-scan row; the
  non-vendor facts of other sources and the authorized path are covered by the
  unit tests above, not by this differential.

## Performance

No-Regression Evidence: no query, index, lock, claim, or lease changed; this is
in-memory join logic. `BenchmarkSelectOSAdvisoryRelease` (the changed function)
was run as interleaved A/B between a base-built and a change-built test binary,
30 alternating pairs, on a loaded host (load average 16 to 50, so read the
minimum and median together):

| Row | Base min / median | Change min / median | Allocs (both) |
|---|---|---|---|
| untagged OSV fact | 19.5 / 22.6 ns | 20.8 / 23.3 ns | 0 |
| listed | 235.1 / 280.0 ns | 233.9 / 279.8 ns | 5 (352 B) |

The untagged OSV row, the hot path, keeps zero allocations; the added cost is one
case-insensitive source compare. A vendor-sourced fact now takes a new path that
base returned untouched (17.9 ns, 0 allocs): about 290 ns and 5 allocs
(304 B) for a matching or differing line and about 217 ns and 3 allocs (272 B)
for an unreadable one, once per vendor fact per (CVE, package) group. No producer
of such facts exists yet, so no deployed workload pays this today.

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
