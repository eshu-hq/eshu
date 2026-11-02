# OSV OS Advisory Join Evidence (#9848)

OSV publishes Debian, Ubuntu, and Alpine advisories with a versionless,
qualified purl such as `pkg:deb/debian/curl?arch=source&distro=bookworm`. The
image scanner writes an installed `os_package` row whose key is
`packageIDFromPURL` of a versioned purl, `pkg:deb/debian/curl`, and migration
153 indexes the same expression (`split_part(btrim(payload->>'purl'), '@', 1)`).
The advisory side keyed itself with the same splitter, which only drops an
`@version`, so it kept the qualifiers and never met the installed key. Every
versionless OSV OS advisory loaded and joined nothing.

The fix is advisory-side only. `purlPackageKey` drops the qualifiers, the
subpath, and a version from an advisory purl. The installed key, its splitter,
and the migration-153 index expression are unchanged, so no installed row is
rekeyed and no reindex is needed.

Joining exposes a second defect. PR #9847 merges every release line of one
package into one affected fact whose ranges are the untagged union. An
installed row carries a release (`distro=debian-12`), but the ranges do not, so
the reducer cannot tell which line the row is on, or whether the advisory lists
that line at all. A release the record omits falls below every listed fix, so
"every range contains it" is no proof either. The rule (ruling D): an OSV OS
fact never yields `affected_exact`. Per fact:

| Ranges against the installed version | Finding | Reason |
| --- | --- | --- |
| a range contains it, none is fixed at or below it | `possibly_affected` | `os_release_unverified` |
| one range contains it, another is fixed at or below it | `possibly_affected` | `os_release_ambiguous` |
| none contains it, a fix is at or below it | `not_affected_known_fixed` | `dpkg_known_fixed` / `apk_known_fixed` |

Both `possibly_affected` reasons carry the missing evidence "advisory release
line for installed package unknown" and image_os_package reachability.
`not_affected_known_fixed` above every listed fix is an extrapolation until
ranges carry a release tag (PR-2b): a host on an unlisted line may still be
vulnerable. A vendor-sourced fact (`source: debian`, as in the golden cassette)
is published per release and keeps `affected_exact`.

Root-Cause Evidence: RED run on base `8fca5362cd` with the live fixtures
(`os_advisory_join_test.go`). Debian, Alpine, and versionless-Ubuntu cases
produce 0 findings in both entry orders. The lookup-key test shows the advisory
key `pkg:deb/debian/curl?arch=source&distro=bookworm` beside the installed key
`pkg:deb/debian/curl`. The versioned Ubuntu record (CVE-2016-5419) does join on
the base, and a trusty row at its own fix `7.35.0-1ubuntu2.8` reads
`affected_exact` because the xenial range contains it: the live false positive.
Ruling D RED, on the tree before the rule: 20 subtests read `affected_exact`
where they now expect `possibly_affected` (every unlisted-release row, every
below-every-fix row in both orders, and the four merge-order trusty rows).

## Fixtures

Pulled from `api.osv.dev/v1/vulns/<id>` on 2026-11-02 and kept as served apart
from `jq --indent 2`:

| File | Shape |
| --- | --- |
| `alpine-cve-2023-27537.json` | 11 release lines (v3.14 to v3.24); v3.14 to v3.16 fixed 8.0.1-r0, v3.17 7.88.1-r1, v3.18 and later 8.0.0-r0 |
| `ubuntu-cve-2022-27775.json` | versionless purls; focal fix 7.68.0-1ubuntu2.10, jammy 7.81.0-1ubuntu1.1 |
| `ubuntu-cve-2016-5419.json` | versioned purls; trusty fix 7.35.0-1ubuntu2.8, xenial 7.47.0-1ubuntu2.1 |
| `ghsa-35jh-r3h4-6jhm.json` | npm `lodash`, the registry control |

Unlisted-release rows pin the new rule: Debian bullseye and buster against
CVE-2023-38039, Alpine 3.13 against CVE-2023-27537, and Ubuntu bionic against
CVE-2022-27775 each read `possibly_affected` / `os_release_unverified`.

Live census through the PR #9847 converter (2026-11-02 curl records): of 244
Ubuntu records, 150 yield a fact purl with an `@version` and join on the base,
94 do not; 298 Debian and 196 Alpine records yield none and join nothing. Order
independence is pinned by converting every fixture in both entry orders. The
collector's `TestOSVRecordEnvelopesNeverShareAFactID` covers the new fixtures.

Per-status census of the same live records (OSV query files fetched 2026-11-02),
one installed curl row each. "Before" is this branch with the ruling-D rule
disabled, "after" is the rule:

| Row | Records | `affected_exact` before / after | `os_release_unverified` after | `os_release_ambiguous` | known fixed | not in range |
| --- | --- | --- | --- | --- | --- | --- |
| Debian bullseye `7.74.0-1.3+deb11u7` | 298 | 140 / 0 | 140 | 0 | 158 | 0 |
| Debian bookworm `7.88.1-10+deb12u2` | 298 | 93 / 0 | 93 | 2 | 203 | 0 |
| Alpine 3.13 `7.79.1-r0` | 196 | 94 / 0 | 94 | 0 | 70 | 32 |
| Ubuntu jammy `7.81.0-1ubuntu1` | 244 | 66 / 0 | 66 | 71 | 107 | 0 |

Which Ubuntu record joined on the base depended on the purl the merge kept.
The merge sorts entries by purl and `?` sorts before `@`, so a record with one
versionless entry keeps the versionless purl. `TestOSVUbuntuJoinIgnoresWhichEntryPurlTheMergeKeeps`
feeds the live CVE-2016-5419 record and a copy with its trusty purl's version
removed, each in both entry orders, against three rows. At `ebaf4dfa0d` the
mixed copy gives 0 findings and the versioned record gives `affected_exact` at
trusty's own fix; after this change all four inputs give the same answer.

Mutants: `osAffectedFactCarriesReleaseEvidence` returning true turns 22
subtests red (the 20 above plus the two hand-made OSV range rows in
`os_package_test.go`); returning false turns
`TestCov6EvaluateOSPackageVersionMatch` and
`TestAuthorizedScanImpactDisjointAffectedBranches` red, so vendor-fact
exactness is pinned too.

## Scale

There are two OS paths, and only one scales with images:

- The fleet-wide pass (`BuildSupplyChainImpactFindings`, no SBOM) yields one
  finding per CVE and package, read from the first installed row (#9858).
  Its cost is the index build, O(rows), plus one verdict per advisory.
- The authorized per-target pass (`BuildAuthorizedScanImpactFindings`) builds
  one index and runs one verdict per advisory per target, so it is linear in
  targets.

The verdict is O(ranges) per fact. After the converter's dedup an OS fact holds
up to one range per release line (2 distinct ranges in the CVE-2023-38039
Debian fixture, 3 in the Alpine fixture). Registry advisories are untouched:
the `registry-10x` row below is equal in time, bytes, and allocs. The Postgres
read sends the same number of keys as before (one advisory key is replaced by
another), and the index plan holds at 50,000 rows.

No-Regression Evidence: `BenchmarkOSAdvisoryJoin` builds one fleet-wide pass
from the five live OS records plus 10, 50, and 100 installed curl rows per
vendor (debian, ubuntu, alpine). `BenchmarkOSAdvisoryJoinTargets` builds 10, 50,
and 100 authorized targets, each with one curl row and one completed analysis,
sharing the five records under one `public_registration` dependency. Before is
`ebaf4dfa0d` with the bench and fixture test files copied onto a detached
worktree; after is this change. 8 interleaved rounds, first mover alternated,
`-benchtime=1000x` (fleet) and `200x` (targets), Apple M5 Max, Go 1.26.10,
darwin/arm64. Load average 31 to 41 (1 min): a shared, loaded host, so ns/op is
indicative only, while allocs and bytes are exact. Raw output:
`benchstat-d.txt` in the run's scratch directory.

| Fleet-wide | Before | After | Change | Allocs before | Allocs after | B/op before | B/op after |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1x | 117.2 µs | 231.0 µs | +97% | 823 | 1,722 | 80.36 KiB | 167.06 KiB |
| 5x | 230.6 µs | 335.7 µs | +46% | 1,429 | 2,329 | 174.8 KiB | 261.6 KiB |
| 10x | 364.0 µs | 473.2 µs | +30% | 2,183 | 3,083 | 302.1 KiB | 389.1 KiB |
| miss-10x (wget rows) | 352.6 µs | 359.0 µs | ~ (p=0.878) | 2,153 | 2,165 | 293.7 KiB | 294.6 KiB |
| registry-10x (npm) | 331.5 µs | 323.4 µs | ~ (p=0.959) | 2,018 | 2,018 | 281.4 KiB | 281.3 KiB |

| Per-target | Before | After | ns/target after | Allocs before | Allocs after | Findings before | Findings after |
| --- | --- | --- | --- | --- | --- | --- | --- |
| targets-1x (10) | 1.654 ms | 1.990 ms (+20%) | 199.0 µs | 11,750 | 13,900 | 3 | 17 |
| targets-5x (50) | 8.063 ms | 9.617 ms (+19%) | 192.3 µs | 56,980 | 67,650 | 17 | 85 |
| targets-10x (100) | 15.52 ms | 19.23 ms (+24%) | 192.3 µs | 113,500 | 134,100 | 33 | 167 |

On the per-target path 10x costs 19.23 ms against 10 x 1.990 = 19.90 ms for
1x, so it is not superlinear: ns/target stays flat and allocs per target hold
at about 1,340 to 1,390. The added cost is the findings the base never produced
(the base joins only the versioned Ubuntu record). On the fleet-wide pass the
added cost is about 900 allocs and 87 KiB per pass, the same at every scale; a
pass whose rows do not match (`miss-10x`) pays only the join count's key slices
(+12 allocs). `BenchmarkPURLPackageKey`: `purlPackageKey` 21.5 to 30.2 ns/op and
`packageIDFromPURL` 23.8 to 28.0 ns/op on the same purls, both 0 allocs.

Postgres: `TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive` on a private
PostgreSQL 18.6 cluster seeds 50,000 `vulnerability.os_package` rows and reads
the generic prepared plan with the real key set (`os://` and `pkg:` keys for two
packages). Bitmap Index Scan on `fact_records_os_package_purl_prefix_idx`,
rows=2, Index Searches 2, shared hit 12 in total, execution 0.155 ms, no
sequential scan. The pre-#9848 qualified key returns 0 rows on the same seed.

Observability Evidence: `eshu_dp_supply_chain_impact_os_advisory_joins_total`
counts each OS advisory fact per pass by `source` (`debian`, `ubuntu`,
`alpine`, `other`) and `outcome` (`joined`, `not_joined`); `joined` means at
least one installed row met it. The success line carries
`os_advisories_considered` and `os_advisories_joined`. Considered above zero
with joined at zero is the shape this defect had. Tests pin the closed label set
(`TestEmitOSAdvisoryJoinsCarriesOnlySourceAndOutcome`) and the sub-signals
(`TestRecordBuildStatsWritesOSAdvisorySubSignals`). Operator reference:
[Supply-Chain Active Evidence Signals](../../public/reference/telemetry/supply-chain-active-evidence.md#os-advisory-joins).

## Not covered

- A release tag on each range, so a row reads only its own line (PR-2b).
- The join is by binary package name. Debian and Ubuntu advisories name the
  source package, so `libssl3`/`openssl`, `libc6`/`glibc`, and
  `libcurl4`/`curl` do not join yet (#9859).
- The fleet-wide pass reports one installed row per advisory and package, the
  first in row order (#9858).
- Producer precondition for enabling an OS derived target. Read at
  `eshu-hq/eshu-scanner-worker@b18339cc43`: the dpkg parser labels every row
  `debian` with `pkg:deb/debian/<name>` (`dpkg_parser.go`, `buildDebianPURL`),
  and both image readers reject an `ID=ubuntu` image as unsupported
  (`validateDistro` has no Ubuntu; dpkg requires distro `debian`). An Ubuntu
  image therefore emits no rows, unless a target sets `distro: debian`; that
  override would join Debian advisories to Ubuntu versions, and the reducer
  cannot tell. Not exercised end to end here.
- The rpm family (#9844).
