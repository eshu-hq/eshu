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
- **Deciding record.** Among observations of one source, a record whose fact
  lists the host's release line outranks an untagged one (a vendor-sourced
  fact, in any verification state), which outranks one that lists other lines
  only; the `AdvisoryID` order breaks ties inside a tier. `advisory_id`, the
  fix and the range come from the winner.
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

Local census over every committed OSV fixture (12), 20 host rows plus the
no-host row each, both entry orders: 504 rows, base `e3d816172a` against head,
the same inputs, the full finding compared field by field.

| Result | Rows |
| --- | --- |
| Byte-identical finding (same `finding_id`) | 464 |
| Same finding except `cve_id` (1 finding to 1, `finding_id` moves) | 32 |
| One bulletin split into one finding per upstream CVE (Alma 1 to 3, Rocky 1 to 4) | 8 |
| `(status, match_reason)` set differs | 0 |

The 32 re-keyed rows are the Alpine and Ubuntu fixtures that carry `upstream`.
The two committed Debian fixtures carry none, so they stay byte-identical; a
live Debian record that carries `upstream` moves the same way the Ubuntu ones
do. Verdict parity holds on all 504 rows. The QA row-move count and the
winners census need the QA replica and are not in this note.

## Performance

Performance Evidence: the pass adds one prefix comparison per
`vulnerability.cve` record and no allocation for a record that is not an OS
vendor record and carries no `upstream` list. Only records that name a CVE do
work: one copy of the record and of its package facts per CVE key, plus one
map of join keys. No backend call, no new query.

No-Regression Evidence: `BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping`
(index build, 3 samples per cell, same host, load average about 10, so time is
indicative and the allocation counts are exact). Base `e3d816172a` against head:

| Fixture | Base allocs/op | Head allocs/op | Base B/op | Head B/op | ns/op, base then head (median) |
| --- | --- | --- | --- | --- | --- |
| 20,000 language advisories, no upstream | 580,319 | 580,319 | 48.26 MB | 50.12 MB | 28.9 ms, 28.0 ms |
| 5,000 Debian records, no upstream | 145,120 | 145,122 | 12.16 MB | 12.59 MB | 6.9 ms, 6.9 ms |
| 5,000 DSA records naming one CVE each plus the NVD record | 205,154 | 230,252 | 18.98 MB | 27.64 MB | 10.3 ms, 12.2 ms |
| 20,000 language, 2,000 ungrouped OS, 2,000 grouped OS | 720,320 | 730,385 | 58.53 MB | 76.03 MB | 36.5 ms, 38.6 ms |

The untagged hot path adds zero allocations; its bytes grow 3.8% because
`ImpactCVE` gained a 24-byte `Upstream` slice header per record. The grouped
case pays for the copies it makes (one record and one package fact per extra
CVE key), which is the work the change exists to do. The grouped fleet is the
only cell that moves outside noise.

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

- Alma and Rocky errata carry `upstream` lists, so the data-driven rule groups
  them too. The ruling names Debian, Ubuntu and Alpine; limiting the rule to
  those three would need an id-prefix allowlist, which the ruling rejects. The
  Alma and Rocky test rows now expect one finding per upstream CVE.
- The per-target indexes of the authorized derived pass apply the grouping but
  are not counted in the grouping counter.
- A quarantined (unreadable) `vulnerability.cve` record cannot be grouped, since
  its `upstream` is unreadable; it stays under its own key.
- Records stored before the producer wrote `upstream` read as `ungrouped` until
  they are collected again.
