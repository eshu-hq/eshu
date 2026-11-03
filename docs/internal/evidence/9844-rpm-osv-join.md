# AlmaLinux and Rocky Linux OSV Join Evidence (#9844)

OSV publishes AlmaLinux and Rocky Linux advisories under the upstream distro
namespace (`pkg:rpm/almalinux/curl`, `pkg:rpm/rocky-linux/curl`), while the
image scanner writes installed `os_package` rows under the canonical vendor
(`pkg:rpm/alma/curl`, `pkg:rpm/rocky/curl`). Before this change the collector
skipped every AlmaLinux and Rocky Linux entry as `unsupported_ecosystem`, so
no rpm-family host got advisory evidence. Adding only the ecosystem alias
would have been worse than the skip: the facts would have loaded and joined
nothing, a silent miss that looks like coverage.

The fix is advisory-side only. `osPackageVendorFromPURL` gains the rpm case
(`alma`/`almalinux` to `alma`, `rocky`/`rockylinux`/`rocky-linux` to `rocky`,
canonical vendor strings only), and one shared helper,
`canonicalAdvisoryOSPackageKey`, derives `pkg:rpm/<vendor>/<lowername>` from
that vendor plus the lowercased name. Both `affectedOSPackageLookupKeys` (the
narrow-read key set) and the `osPackageMatchesAffectedPackage` re-verification
use it, so the read and the join agree. The installed key, the
`split_part(purl, '@', 1)` splitter, the scanner contract, and the OSV purls
stay untouched. No other rpm-family spelling is aliased: `centos`, `fedora`,
`amazonlinux`, and `redhat` still normalize to nothing.

An OSV rpm fact carries ECOSYSTEM ranges over every release line with no
release tag, so it routes through the release-aware verdict exactly like the
deb/apk OSV facts (ruling D, inherited from #9848): it can never settle
`affected_exact`. Per row:

| Installed version against the advisory ranges | Finding | Reason |
| --- | --- | --- |
| a range contains it, none is fixed at or below it | `possibly_affected` | `os_release_unverified` |
| none contains it, a fix is at or below it | `not_affected_known_fixed` | `rpm_known_fixed` |

A vendor rpm fact (scalar fixed version, exact-version list, no ECOSYSTEM
ranges) keeps the existing rpm matcher, including `affected_exact` on a list
match. No release tag is added to any range (#9927 stays dormant).

Failing-first proof runs the live records through the real converter in both
entry orders, joined to scanner-shaped rpm rows (`testdata/osv/alsa-2021-4511.json`
fixes curl at `7.61.1-22.el8`, `testdata/osv/rlsa-2021-1610.json` fixes curl
at `0:7.61.1-18.el8`): `TestOSVOSAdvisoryJoinsAlmaRockyRows` (below/at/above
the fix), `TestOSVOSAdvisoryJoinAlmaRockyNegatives` (Alma advisory against a
Rocky row, a redhat row, and a dpkg row, plus the Rocky/Alma mirror), the
narrow-read key pin `TestOSVOSAdvisoryLookupKeysMeetTheInstalledKey`, the
`alma`/`rocky` join cells in
`TestOSAdvisoryJoinStatsSeparateLoadedFromJoined`, the flipped collector test
`TestClaimedSourceAdmitsAlmaAndRockyEntries`, and the alias and consumption
census pins (`TestNormalizeEcosystemAdmitsAlmaAndRockyAliases`,
consumption-key and derived-scope-key rows).

## Scale

The change adds one lookup key per rpm advisory fact and one string compare
per candidate row on the re-verification path; deb/apk facts gain one
immediately-empty helper call. The verdict delegation runs only when a fact
carries an ECOSYSTEM range, so vendor rpm facts keep their existing path.

No-Regression Evidence: `BenchmarkOSAdvisoryJoin` and
`BenchmarkOSAdvisoryJoinTargets` (the #9848/#9900 bench rows), before
`6ae96792de` after rebase to `7bcb7979ed`, after this change, one round each
at `-benchtime=100x`, linux/arm64, Go 1.26.6, shared host (ns/op indicative,
allocs/bytes exact).

| Fleet-wide | Before | After | ns/advisory before | ns/advisory after | Allocs before | Allocs after |
| --- | --- | --- | --- | --- | --- | --- |
| 1x | 234,033 ns/op | 227,901 ns/op | 46,807 | 45,580 | 1,874 | 1,873 |
| 5x | 394,595 ns/op | 391,862 ns/op | 78,919 | 78,372 | 2,892 | 2,892 |
| 10x | 587,787 ns/op | 583,760 ns/op | 117,557 | 116,752 | 4,152 | 4,152 |
| miss-10x (wget rows) | 311,362 ns/op | 316,761 ns/op | 62,272 | 63,352 | 2,185 | 2,185 |
| registry-10x (npm) | 295,328 ns/op | 295,953 ns/op | 295,328 | 295,953 | 2,038 | 2,038 |

| Per-target | Before | After | ns/target before | ns/target after | Findings |
| --- | --- | --- | --- | --- | --- |
| targets-1x (10) | 1,744,575 ns/op | 1,757,982 ns/op | 174,458 | 175,798 | 17 / 17 |
| targets-5x (50) | 8,458,835 ns/op | 8,498,536 ns/op | 169,177 | 169,971 | 85 / 85 |
| targets-10x (100) | 16,528,000 ns/op | 16,616,456 ns/op | 165,280 | 166,165 | 167 / 167 |
| targets-10x-counted (100) | 16,799,650 ns/op | 17,089,757 ns/op | 167,996 | 170,898 | 167 / 167 |

Deltas are within run-to-run noise (at most +1.7% on a 100x round); allocs
hold and findings are identical, so the added lookup key costs nothing
measurable on the deb/apk rows the bench exercises.

Observability Evidence: `eshu_dp_supply_chain_impact_os_advisory_joins_total`
counts each OS advisory fact per index by `source` and `outcome`; the closed
`source` set grows from `debian`, `ubuntu`, `alpine`, `other` to `debian`,
`ubuntu`, `alpine`, `alma`, `rocky`, `other`, pinned by
`TestEmitOSAdvisoryJoinsCarriesOnlySourceAndOutcome` and the alma/rocky cells
above. The success line keeps the `os_advisories_considered` and
`os_advisories_joined` sub-signals. Operator reference:
[Supply-Chain Active Evidence Signals](../../public/reference/telemetry/supply-chain-active-evidence.md#os-advisory-joins).

## Not covered

- Live range-content uncertainties resolved from the fetched OSV bodies
  (NOT_CHECKED).
- An Alma image without a scanner target override row emission
  (NOT_CHECKED).
- The deployed scanner-worker version in QA (NOT_CHECKED).
- No scanner end-to-end run; mirrors the #9900 precedent (NOT_CHECKED).
