# #9914: group OSV distro records under the CVEs their upstream list names

Refs #9914.

A Debian, Ubuntu or Alpine OSV record is published under the distro's own id
(`DEBIAN-CVE-X`, `DSA-N-1`, `DLA-N-1`, `USN-N-1`, `ALPINE-CVE-X`) and names the
CVEs it covers only in its `upstream` list. The collector sets `cve_id` from
aliases, which these records do not have, so each record read as its own group
and one installed package could read several findings for one CVE. The index
build now files each such record under every `CVE-*` id in its typed `upstream`
list (`groupOSAdvisoriesByUpstream`, `os_advisory_upstream_group.go`). The
producer's `cve_id` and the stored facts do not change.

## Rules

- **Key.** For a `vulnerability.cve` record, the keys are the `CVE-` prefixed
  entries of its own key and its `upstream` list (trimmed, case folded,
  de-duplicated). `DEBIAN-CVE-*`, `UBUNTU-CVE-*` and `DSA-*` entries are not
  keys. No key other than the record's own: the record keeps its group.
- **Bulletin.** A record with N keys is a member of N groups, so one installed
  package reads one finding per upstream CVE. Each finding carries the
  bulletin's cve and affected_package fact ids, not the other CVEs' ids.
- **Order.** The pass runs once after the batch is indexed, so it does not
  depend on which fact of a record arrives first. It is not transitive: records
  that name each other stop after one step.
- **Join.** A record's packages are found by `AdvisoryID`, never `cve_id`: an
  alias can give unrelated records one `cve_id`. A record with no advisory id
  fails closed as `ungrouped`.
- **Deciding record.** Among observations of one source, a record whose fact
  lists the host's release line outranks an untagged one (a vendor-sourced
  fact, in any verification state), which outranks one that lists other lines
  only; the `AdvisoryID` order breaks ties inside a tier. The fix and the range
  come from the winner. An OS-class finding's `advisory_id` names the first
  observation in that order that supplied an affected_package fact and is not
  retracted, so a scored NVD or GHSA record supplies the score without renaming
  the finding, and identity does not depend on what the pass loaded. Every other
  ecosystem keeps the scored-source rule (#9693).
- **Retraction.** A retracted record does not decide a group that has a live
  record for the same package: its fact ids stay in `evidence_fact_ids` and its
  status in `advisory_sources`. A record is retracted when every cve observation
  of its `AdvisoryID` is. A wholly retracted group keeps its verdict and its
  `AdvisoryRetraction` (#9695).
- **Products.** A group whose packages are all upstream copies still emits its
  NVD product findings, reading only the native cve observations. A group with a
  natively keyed package still skips them.
- **Derived pass.** The conflict gate reads the per-target index, not a second
  decode of the raw facts, so it sees exactly what the primary path grouped.
- **Data-driven.** The rule keys on `upstream`, not on the id prefix, so the
  Alma and Rocky errata (`ALSA-*`, `RLSA-*`) that carry an `upstream` list group
  the same way. See "Scope question" below.

## Identity move, measured

Finding identity hashes `cve_id` and `advisory_id`, so every grouped OS finding
gets a new `finding_id` and a new winners key; the pass's replace-set retraction
tombstones the old row and writes the new one in the same transaction, and both
reach the winners dirty log. The read API filters `cve_id`, so a query by
`DEBIAN-CVE-*` returns nothing and a query by `CVE-*` now returns the distro
findings. A `cve_id`-scoped suppression on a distro id stops matching.

Local parity differential, base `e3d816172a` against head, the same inputs, the
full finding compared field by field. There are 672 rows: 16 inputs (the 12
committed OSV fixtures plus 4 merged-group fixtures, below), 20 host rows plus
the no-host row, both entry orders. Only 100 rows have any finding on either
side; the rest are a fixture crossed with a host it does not apply to, which
prove nothing beyond "no finding appears from nowhere".

| Result | Committed fixtures (504 rows) | Merged-group fixtures (168 rows) |
| --- | --- | --- |
| Empty on both sides | 444 | 128 |
| Byte-identical finding (same `finding_id`) | 20 | 0 |
| One finding to one, `cve_id` and `finding_id` move | 32 | 0 |
| One bulletin split into one finding per upstream CVE (Alma 1 to 3, Rocky 1 to 4) | 8 | 0 |
| Two findings merged into one, same `(status, match_reason)` set | 0 | 24 |
| Two findings merged into one, `(status, match_reason)` set differs | 0 | 16 |

No committed fixture has two records for one CVE, so the committed rows cover
re-key and split only. The four merged-group fixtures are written for this: a
`DEBIAN-CVE` record beside a `DLA` for another release line, with the `DLA`
withdrawn, rejected, live, and live beside a scored NVD record. All 16
verdict-set differences are the Debian 11 and Debian 12 hosts on those fixtures,
and each is the defect the change fixes: base reads two findings for one package
and one CVE, one per record, and head reads one.

| Merged fixture, host | Base `(status, match_reason)` set | Head | Head `advisory_id` |
| --- | --- | --- | --- |
| `DLA` live, Debian 11 | `affected_exact`, `possibly_affected` | `affected_exact` | the `DLA` |
| `DLA` live, Debian 12 | `affected_exact`, `possibly_affected` | `affected_exact` | the `DEBIAN-CVE` |
| `DLA` retracted, Debian 11 | `affected_exact`, `possibly_affected` | `possibly_affected` / `os_release_not_in_advisory` | the `DEBIAN-CVE` |
| `DLA` retracted, Debian 12 | `affected_exact`, `possibly_affected` | `affected_exact` | the `DEBIAN-CVE` |

Head also reads the same finding in both entry orders on all 672 rows, and the
live-`DLA` fixture reads the same `finding_id` with and without the scored NVD
record beside it on all 42 rows.

The two committed Debian fixtures carry no `upstream`, so they stay
byte-identical; a live Debian record that carries it moves the way the Ubuntu
ones do. The unit tests in `os_advisory_upstream_join_test.go`,
`os_advisory_upstream_liveness_test.go`, `os_advisory_deciding_record_test.go`,
`os_advisory_upstream_product_test.go` and `os_advisory_upstream_derived_test.go`
pin each rule on its own.

## QA plan

The row-move count and the winners census need the QA replica and are not in
this note. Run them read-only before and after the first full pass:

- Winners: the rows with `cve_id` matching `DEBIAN-CVE-`, `UBUNTU-CVE-` or
  `ALPINE-CVE-`, or `advisory_id` matching `^(DSA|DLA|USN|ALSA|RLSA)-`, plus the
  rows with `cve_id LIKE 'CVE-%'` and an OS evidence path, which are the legacy
  alias-keyed records that move in the same pass.
- Row move: the change in `eshu_dp_supply_chain_impact_findings_retracted_total`
  across the first pass and the peak of
  `eshu_dp_supply_chain_impact_winners_dirty_log_depth`.
- Suppressions: the count scoped to a `DEBIAN-CVE-`, `UBUNTU-CVE-`, `ALPINE-CVE-`,
  `DSA-`, `DLA-`, `USN-`, `ALSA-` or `RLSA-` id, which stop matching and need
  re-scoping to the CVE.

Tripwire: if the post-pass winners count for OS evidence paths exceeds 2x the
pre-pass count, stop and report before any further rollout. Debian and Ubuntu
are expected to decrease, because per-CVE records already exist for the CVEs a
bundle names; a large increase means a join defect.

## Performance

Performance Evidence: the pass adds one prefix comparison per
`vulnerability.cve` record and no allocation for a record that is not an OS
vendor record and carries no `upstream` list. Only records that name a CVE do
work: one copy of the record and of its package facts per CVE key, plus one
map of join keys. No backend call, no new query.

No-Regression Evidence: `BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping`
(index build, 3 interleaved samples of 3 iterations per cell, same host, load
average 25 to 40, so time is indicative and the allocation counts are exact).
Base `e3d816172a` against head:

| Fixture | Base allocs/op | Head allocs/op | Base B/op | Head B/op | ns/op, base then head (median) |
| --- | --- | --- | --- | --- | --- |
| 20,000 language advisories, no upstream | 580,323 | 580,320 | 48.27 MB | 55.72 MB | 42.1 ms, 38.7 ms |
| 5,000 Debian records, no upstream | 145,120 | 145,122 | 12.16 MB | 12.59 MB | 9.5 ms, 9.5 ms |
| 5,000 DSA records naming one CVE each plus the NVD record | 205,154 | 225,270 | 18.98 MB | 30.78 MB | 13.9 ms, 18.1 ms |
| 20,000 language, 2,000 ungrouped OS, 2,000 grouped OS | 720,320 | 728,394 | 58.53 MB | 76.38 MB | 44.7 ms, 49.8 ms |
| 300 bulletins, 3 packages, naming 1 CVE | 51,951 | 53,787 | 2.49 MB | 3.65 MB | 2.9 ms, 3.4 ms |
| 300 bulletins, 3 packages, naming 10 CVEs | 51,951 | 65,825 | 2.54 MB | 13.69 MB | 4.0 ms, 7.0 ms |
| 300 bulletins, 3 packages, naming 90 CVEs | 51,951 | 162,978 | 2.95 MB | 111.42 MB | 3.9 ms, 37.3 ms |

The untagged hot path adds no allocation. `ImpactCVE` grew from 192 to 224 bytes
(the `Upstream` slice header and the `UpstreamCopy` flag); `AffectedPackage` did
not grow, because its flag fits the padding beside `ReleaseUnlisted`. The byte
cost on that path is not one number, because append growth lands differently per
element size. Same bench, 3 samples, across sizes:

| Language advisories | 5,000 | 10,000 | 15,000 | 19,000 | 20,000 | 21,000 | 25,000 | 30,000 | 40,000 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Head B/op against base | +3.5% | +15.4% | +3.8% | +3.9% | +15.4% | +4.4% | +16.1% | +3.9% | +15.3% |

So a non-OS fleet pays about 4% typically and about 15% at the sizes where it
takes one more growth step, with allocation counts unchanged. This is transient
index memory in a pass, and it moves no query.

The grouped cells pay for the copies they make, which is the work the change
exists to do. The #9781 evidence budget charges loaded envelopes and retained
payload bytes, never these copies, and there is no fan-out cap, because a cap
would hide findings. Whole classification,
`BenchmarkBuildSupplyChainImpactFindingsUpstreamBundle`, 100 bulletins of 3
packages with every package installed:

| CVEs named per bulletin | Findings, base then head | B/op, base then head | KB per finding, base then head | ns/op, base then head (median) |
| --- | --- | --- | --- | --- |
| 1 | 300, 300 | 9.19 MB, 9.56 MB | 30.6, 31.9 | 8.8 ms, 9.0 ms |
| 10 | 300, 3,000 | 9.20 MB, 91.98 MB | 30.7, 30.7 | 9.1 ms, 66.5 ms |
| 90 | 300, 27,000 | 9.34 MB, 837.57 MB | 31.1, 31.0 | 8.7 ms, 609.5 ms |

The cost per finding does not move; the row count is the acceptance criterion.
Paths the change does not target stay flat: `BenchmarkOSFleetDigestFanout` reads
3,655 to 3,659, 32,751 to 32,757 and 69,494 to 69,502 allocs/op at 1x, 5x and 10x
with bytes equal to within 0.2%, and
`BenchmarkBuildSupplyChainImpactFindingsAlertEPSS` reads 119 to 118 allocs/op,
16.8 KB to 16.7 KB.

## Observability

Observability Evidence: `eshu_dp_supply_chain_impact_os_advisory_upstream_groups_total{source,result}`
counts the examined `vulnerability.cve` records by the closed source set
(debian, ubuntu, alpine, alma, rocky, other) and result (grouped, ungrouped), 12
series at most, and the reducer success log carries `os_advisories_grouped` and
`os_advisories_ungrouped`. A source that reads only `ungrouped` means the
producer's `upstream` lists do not reach the reducer; that is also the state of
any fleet whose stored records predate the field. The planted RED is
`TestOSAdvisoryUpstreamGroupCounterPlantedRed`. The OS join counter still counts
each loaded advisory fact once however many CVEs it was filed under
(`TestOSAdvisoryJoinCensusCountsEachFactOnce`).

## Scope question and limits

- Alma and Rocky are in scope. The rule keys on `upstream`, not on the id prefix,
  and their errata carry it, so one bulletin reads one finding per upstream CVE.
  That is a net row increase on those hosts (1 to 3 on the Alma fixture, 1 to 4
  on the Rocky fixture), and it is the correct count: no per-CVE record exists
  for them to merge with.
- A language group with two or more records carrying package facts follows the
  same retraction rule, because the liveness filter is not limited to OS
  ecosystems. No existing test moved, and this was not measured against a fleet.
- The per-target indexes of the authorized derived pass apply the grouping but
  are not counted in the grouping counter.
- A quarantined (unreadable) `vulnerability.cve` record cannot be grouped, since
  its `upstream` is unreadable; it stays under its own key.
- Records stored before the producer wrote `upstream` read as `ungrouped` until
  they are collected again.
