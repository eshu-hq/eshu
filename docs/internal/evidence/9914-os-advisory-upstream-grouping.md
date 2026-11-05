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
- **Selection order.** Among observations of one source, a record whose fact
  lists the host's release line (a vendor fact once #9945 verified its line) outranks
  an untagged or unverified one, which outranks one that lists other lines only; the
  `AdvisoryID` order breaks ties inside a tier.
- **Deciding record.** An OS-class finding built from an installed row is named by
  the live record whose fact holds the installed version: a fact with release
  evidence whose range contains the version with no fix at or below it, or whose
  exact-version list names it (`osHoldingFacts`). Among holders the highest fix for
  the host wins, compared with the verdict's own comparator, because a lower fix
  leaves the host inside the other record's range; equal fixes keep selection
  order. The fix and the range come from that record alone, so a holder naming no
  fix leaves the fix empty and does not borrow another record's. Severity, KEV, EPSS
  and the source lists do not change. A cross-record disagreement is not a conflict:
  a later bulletin raising a fix is ordinary data, and two branches of one advisory
  still read `advisory_fixed_version_conflict`.
- **With no holder.** The first observation in selection order that supplied an
  affected_package fact and is not retracted names the finding, and the fix and
  range are the first live ones in that order. A scored NVD or GHSA record supplies
  the score without renaming the finding, so identity does not depend on what the
  pass loaded. Every other ecosystem keeps the scored-source rule (#9693).
- **Retraction.** A retracted record does not decide a group that has a live
  record for the same package: its fact ids stay in `evidence_fact_ids` and its
  status in `advisory_sources`. A record is retracted when every cve observation
  of its `AdvisoryID` is. A wholly retracted group keeps its verdict and its
  `AdvisoryRetraction` (#9695). An OS-class finding judges this over the records
  that list its package: a live record for another package, or a cve-only NVD or
  GHSA record, supplies severity and sources but never said this package is
  affected, so it cannot keep a withdrawn assertion alive
  (`recordsListingThePackage`). A withdrawn record that is the only one listing the
  package still names the finding and offers no fix, the finding shows the retraction,
  and a scored record beside it still supplies the score.
- **Products.** A group whose packages are all upstream copies still emits its
  NVD product findings, reading only the native cve observations. A group with a
  natively keyed package still skips them.
- **Derived pass.** The conflict gate reads the per-target index, not a second
  decode of the raw facts, so it sees exactly what the primary path grouped.
- **Data-driven.** The rule keys on `upstream`, not on the id prefix, so the
  Alma and Rocky errata (`ALSA-*`, `RLSA-*`) that carry an `upstream` list group
  the same way. See "Scope and limits" below.

## Identity move, measured

Finding identity hashes `cve_id` and `advisory_id`, so every grouped OS finding
gets a new `finding_id` and a new winners key, and, where two live records disagree,
the name moves to the record whose range holds the installed version; the pass's
replace-set retraction
tombstones the old row and writes the new one in the same transaction, and both
reach the winners dirty log. The read API filters `cve_id`, so a query by
`DEBIAN-CVE-*` returns nothing and a query by `CVE-*` now returns the distro
findings. A `cve_id`-scoped suppression on a distro id stops matching.

The name follows the host. Once a host upgrades past the highest fix no record
holds and the first sorted record names the finding, so the upgrade itself moves
`finding_id`, and two hosts on different sides of a fix can read different
`advisory_id`s for one CVE. That is the same replace-set move, not a new path.

Local parity differential, base `e3d816172a` against head, the same inputs. Each
row compares status, match reason, `advisory_id`, package, `cve_id`, `finding_id`,
fix, range, retraction and score. There are 1,012 rows: 23 inputs (the 12 committed
OSV fixtures plus 11 merged-group fixtures, below), 21 host rows plus the no-host
row, both entry orders. Only 196 rows have any finding on either side; the rest are
a fixture crossed with a host it does not apply to, which prove nothing beyond "no
finding appears from nowhere".

| Result | Committed (528 rows) | Grouped records (176) | One record lists the package (132) | Two live records (176) |
| --- | --- | --- | --- | --- |
| Empty on both sides | 464 | 128 | 96 | 128 |
| Byte-identical finding (same `finding_id`) | 24 | 0 | 0 | 0 |
| One finding to one, `cve_id` and `finding_id` move | 32 | 0 | 36 | 0 |
| One bulletin split into one finding per upstream CVE (Alma 1 to 3, Rocky 1 to 4) | 8 | 0 | 0 | 0 |
| Two findings merged into one, same `(status, match_reason)` set | 0 | 24 | 0 | 44 |
| Two findings merged into one, `(status, match_reason)` set differs | 0 | 24 | 0 | 4 |

No committed fixture has two records for one CVE, so the committed rows cover
re-key and split only. The merged-group fixtures are written for this:

- Grouped records: a `DEBIAN-CVE` record beside a `DLA` for another release line,
  with the `DLA` withdrawn, rejected, live, and live beside a scored NVD record.
- One record lists the package: a withdrawn or rejected `DLA` beside a live record
  listing another package, and a withdrawn `DLA` beside a live scored NVD record.
- Two live records: a `DLA` and a `DSA` for one release line with fixes u10 and u12,
  u12 and u10, u12 and u14, and u8 and u10.

The hosts include a Debian 11 row on `deb11u11`, which sits between those fixes.
All 28 verdict-set differences are on those fixtures, and each is the defect the
change fixes: base reads two findings for one package and one CVE, one per record,
and head reads one.

| Merged fixture, host | Base `(status, match_reason)` set | Head | Head `advisory_id` |
| --- | --- | --- | --- |
| `DLA` live, Debian 11 below its fix | `affected_exact`, `possibly_affected` | `affected_exact` | the `DLA` |
| `DLA` live, Debian 11 on `deb11u11` | `not_affected_known_fixed`, `possibly_affected` | `not_affected_known_fixed` | the `DLA` |
| `DLA` live, Debian 12 | `affected_exact`, `possibly_affected` | `affected_exact` | the `DEBIAN-CVE` |
| `DLA` retracted, Debian 11 below its fix | `affected_exact`, `possibly_affected` | `possibly_affected` / `os_release_not_in_advisory` | the `DEBIAN-CVE` |
| `DLA` retracted, Debian 11 on `deb11u11` | `not_affected_known_fixed`, `possibly_affected` | `possibly_affected` / `os_release_not_in_advisory` | the `DEBIAN-CVE` |
| `DLA` retracted, Debian 12 | `affected_exact`, `possibly_affected` | `affected_exact` | the `DEBIAN-CVE` |
| `DLA` fix u10 and `DSA` fix u12, `deb11u11` | `affected_exact`, `not_affected_known_fixed` | `affected_exact`, fix u12 | the `DSA` |
| `DLA` fix u12 and `DSA` fix u10, `deb11u11` | `affected_exact`, `not_affected_known_fixed` | `affected_exact`, fix u12 | the `DLA` |

Against the commit before the two accuracy fixes (`9c99579a2a`), 46 of the 1,012 rows
move, every one on the fixtures written for them, and no `(status, match_reason)` set
changes:

| Fix | Rows moved | Fields that moved |
| --- | --- | --- |
| Retraction over the records listing the package | 36 | `AdvisoryRetraction` only. It now agrees with the base on every row of those fixtures |
| Name from the record holding the version | 10 | `advisory_id`, `finding_id`, fix and range |

Head also reads the same finding in both entry orders on all 1,012 rows, and the
live-`DLA` fixture reads the same `finding_id` with and without the scored NVD
record beside it on all 44 rows.

The two committed Debian fixtures carry no `upstream`, so they stay
byte-identical; a live Debian record that carries it moves the way the Ubuntu
ones do. The unit tests in `os_advisory_upstream_join_test.go`,
`os_advisory_upstream_liveness_test.go`, `os_advisory_deciding_record_test.go`,
`os_advisory_holding_record_test.go`, `os_advisory_retraction_scope_test.go`,
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
  re-scoping. For an immediate effect re-scope by `advisory_id` (the finding's
  deciding record id), alone or together with `cve_id=CVE-X`. A `cve_id`-only
  re-scope lands on each holding scope's next run. Verify with the finding's
  `suppression_state` on the read API after the holder's next pass; this change adds
  no telemetry for it.

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
`BenchmarkBuildSupplyChainImpactFindingsAlertEPSS` stays flat: it reads 119 to 118
allocs/op and 16.8 KB to 16.7 KB here, and an independent run on another host read 117
to 117 allocs/op and 15,795 to 15,796 B/op. One binary moves between 117 and 119 from
sample to sample (`9c99579a2a`, 3 samples), so neither pair is a change.

The holding step is measured against `9c99579a2a` with
`BenchmarkBuildSupplyChainImpactFindingsTwoLiveRecords` and
`BenchmarkBuildSupplyChainImpactFindingsUpstreamBundle`: 3 interleaved samples of 3
iterations at load average 16 to 22, so allocation counts and bytes only, no timing:

| Bench | Allocs/op, before then after | B/op, before then after |
| --- | --- | --- |
| `TwoLiveRecords`, 100 CVEs with two live records each | 80,135 to 80,153, then 80,337 to 80,350 | 6.66 MB, 6.67 MB |
| `TwoLiveRecords`, 1,000 CVEs | 1,243,936 to 1,243,946, then 1,245,939 to 1,245,946 | 96.61 MB, 96.69 MB |
| `UpstreamBundle`, 100 bulletins, 3 packages, 1 CVE | 95,697 to 95,703, then 95,693 to 95,701 | 9.56 MB, 9.58 MB |
| `UpstreamBundle`, 100 bulletins, 3 packages, 90 CVEs | 6,718,298 to 6,718,305, then 6,718,288 to 6,718,303 | 837.57 MB, 838.87 MB |

Two live records add 2 allocations per finding. A finding with one fact returns
before any work (`TestOSHoldingFactsDoesNotAllocateForOneFact`), and the retraction
filter allocates only when something is retracted
(`TestRecordsListingThePackageDoesNotAllocateWithoutARetraction`). On the bundle cells
the allocation count does not move and bytes rise about 0.15%, which fits the two new
observation fields; that cause is not measured on its own.

Known-exploited fan-out (QA, read-only, 2026-11-04): for 240 real KEV ids the 2-kind
statement reads 146,928 candidate rows (614 distinct generations of the known_exploited
scope, about 612 rows per id) to keep 240 active ones. Warm it costs about 2.4 s (C8)
against about 2.1 s (5-kind); cold-first 19.7 s on a fresh list, and one cold-first run
hit the probe session's 30 s statement cap and printed no plan. The 5-kind statement
reads the same 146,930 rows (cold-first 11.3 to 13.2 s on different ids, so not a
like-for-like pair). The plan is the cve_lookup_v2 key probe in every run that printed
one; the cost is candidate-row fan-out, not the plan, and the chunk size does not bound
it because the fan-out is per id. This cost shape is pre-existing on the cve_lookup_v2
probe and is newly reachable from an OS pass only because the base OS pass never read KEV
rows for its CVEs. Accepted for this change (#9994). Root cause tracked in #10020: the
retained generations of the KEV scope. NOT_CHECKED: the production loader's statement
timeout and what a cold chunk above it does to the pass (expected: the pass fails before
acknowledgement and the queue retries it warm); a true cold-first pair on the same ids;
EPSS rows, which were not present for these ids. These figures are for one statement of
240 ids. The statement now binds at most 120, so a list reads the same rows over twice
as many statements; that split was not timed.

## Observability

`eshu_dp_supply_chain_impact_active_evidence_branch_seconds` and
`..._branch_rows_total` carry `branch="vulnerability_upstream_cve_id"`, one sample
per statement.

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

## What the load asks for

A distro record is stored under its own id. Nothing filed under the CVE it names
reaches the pass unless the filter asks, so before this change an operator's
suppression scoped to that CVE silently did nothing, and the finding read no KEV
and no EPSS. `TestHandlerLoadsTheSignalsOfTheCVEAFindingCarries` pins this; one
batch tests cannot see it. At 242415ed46 the only filter on its input asked for
`DEBIAN-CVE-2023-38039`, and the finding read `KnownExploited` false, no EPSS and
suppression state `active`.

The first design put the CVE ids on `CVEIDs`. The read-only shim on the QA primary
failed it:

| Measurement | Result |
|---|---|
| Custom plan on the cve key, plan-only sweep | `cve_lookup_v2` at 150 to 360 ids, `advisory_lookup_v2` at 400 and 500 |
| The same list past the flip, 600 to 1,922 ids | canceled at the shim session's 30 s statement timeout, every run (6 of 6) |
| Executions a connection plans that way | the first 5; the generic plan from the 6th stays on `cve_lookup_v2` |
| 1,922 ids, one generic statement | 203 ms median |
| 1,922 ids, 9 custom statements of at most 240 ids | 421 ms median, every statement on `cve_lookup_v2` (27 of 27) |
| 961 real CVE ids, added rows | 4,543 envelopes (9.1% of 50,000), 10.33 MiB (8.1% of 128 MiB) |

The 9-statement row was measured at the earlier bound of 240. At 120 the same list
takes 17 statements, which are not timed on QA yet.

QA holds no `affected_product`, EPSS or suppression rows, so those terms were not
measured, and `affected_product` is the one that could spend the budget. So:

- `UpstreamCVEIDs` is its own field. It reads `vulnerability.known_exploited`,
  `vulnerability.epss_score` and the suppression scope `cve_id`, and never
  `vulnerability.cve`, `affected_package` or `affected_product`.
- **OS findings take severity only from the record that decided them until the
  widening is measured (#9994).** A scored record the pass loaded for another reason, such
  as a language finding keyed to the same CVE, still supplies the score.
- A loaded KEV, EPSS or suppression fact whose CVE a record's `upstream` list
  names stays off `CVEIDs`. The filter feeds those ids back each round, so without
  this the next round's full cve probe would load the excluded kinds anyway. A
  signal for any other CVE keeps the full arm, because an EPSS or KEV intent
  re-reads the findings for its CVE through it.
- The KEV and EPSS probe runs in statements of at most 120 ids, and its rows union
  exactly because a row answers to one list entry. The suppression statement reads
  the whole list in one statement because it carries the LIMIT; the shim measured
  that statement's plan at 0.157 ms with no suppression rows on QA.

Limits, measured or not:

- The 2-kind statement flips where the 5-kind one does, so the kind filter did not
  move the cliff. Plan-only on QA, `PREPARE`/`EXECUTE` under each `plan_cache_mode`,
  for real CVE ids and for ids that match nothing, 12 of 12 cells as below:

  | Ids | Custom plan | Generic plan |
  |---|---|---|
  | 240 | key probe, cost 927,383 | key probe, cost 66,088 |
  | 400 | whole-index walk, cost 1,120,897 | key probe, cost 66,088 |
  | 1,000 | whole-index walk, cost 1,120,898 | key probe, cost 66,088 |

  That table shows one flip between 380 and 400 ids, and it missed another. A plan-only
  QA sweep of the 5-kind statement at `random_page_cost` 1.1 (#9994's evidence note)
  probes at 1 to 171 ids, walks `advisory_lookup_v2` at 172 to 199, probes at 200 to 380
  and walks from 400. At page cost 4 it probes at 180 and walks at 199. The planner
  prices each id at the default 0.005 and sums them below 200 ids, then uses the
  independent formula, so the estimate peaks just under 200. These samples at 240, 400
  and 1,000 never landed in that window.

  The chunker emits those sizes: a list of 172 to 199 ids runs whole, and a longer one
  leaves a remainder of any length. So the bound is 120 (`MaxCVEIDsPerStatement`,
  arbiter ruling arb-wave1-3). Every length from 1 to 120 of the 5-kind statement probes
  at both page costs, and at 120 the walk costs 1.41 and 1.55 times the probe. Balancing
  chunks under 240 does not help, because a list of 172 to 199 ids is one chunk under any
  bound of 199 or more. `TestUpstreamChunksStayBelowTheLowestMeasuredWalk` reads the sizes
  from the production loader and fails at a bound of 172 or more (seeded at 172 and 240;
  green at 171 and 120).

  For this 2-kind statement the QA evidence is indirect. Its costs at 240, 400 and 1,000
  ids are the 5-kind costs scaled by 0.98, and the ruling's monotonicity argument covers
  1 to 139 ids. NOT_CHECKED, both required before the goal closes and neither blocking this
  bound: a direct QA plan check of the 2-kind statement at 171, 172, 185, 199 and 200 ids,
  and a warm timing of the 1,922-id list at 120-id chunks.

  On the local fixture, `TestSupplyChainImpactBranchPlansStayOnAnchorsLive` proves the same
  anchor at 120 ids under both plan modes, and
  `TestSupplyChainImpactUpstreamPlanHoldsAtEveryChunkSizeLive` plans every size from 1 to
  120 under both plan modes at page costs 1.1 and 4. Both pass on the CI Postgres image
  (the digest in `live-postgres-readiness.yml`) at stock settings (`random_page_cost` 4)
  and on a private PostgreSQL 18.6. The fixture holds 8,503 EPSS and KEV rows plus 40,000
  inline padding rows (14,299 heap pages). Without the padding (4,299 pages) both tests
  fail: the custom plan at page cost 4 leaves the anchor from 113 ids on the private
  server and from 117 on the CI image, reading through
  `fact_records_scope_generation_idx`. The planner ignores statistics from the partial
  `cve_lookup_v2` index, so ids are priced at the default whatever their values; the key
  probe wins only when the signal rows are a small share of the heap, and the padding
  supplies that share. This fixture does not reproduce the QA walk of `advisory_lookup_v2`
  at 172 to 199 ids or the flip from about 400, so those rest on the QA plans above.
- The arm a CVE takes depends on the loaded set, not on the order facts arrive or
  which fact triggered the pass: a CVE with any loaded CVE-keyed fact takes the full
  arm, and a CVE named only by an upstream list takes the signal arm
  (`TestUpstreamArmYieldsToANativeRecordArrivingLater`). Both arms read the same
  three signal kinds, so a withheld CVE loses no signal it had.
- One divergence from the base: a KEV, EPSS or suppression fact and a distro record
  in one intent's own scope, with no native fact for the CVE. The base promoted the
  CVE and loaded its NVD record; this code keeps it on the signal arm. Production
  scopes keep these apart (`vuln-intel://osv/...`, `vuln-intel://nvd/...`,
  `vuln-intel://cisa/kev`, `vuln-intel://first/epss`), so this is not expected to
  arise.
- OS severity is scope-stable on this code. No production path puts a distro record
  and a native record for its CVE in one pass: deb and apk package ids and purls
  match no NVD or GHSA row, and signal passes cannot reach a distro record. That
  comes from reading the code and is not checked on QA data.
- This change adds no id to `vulnerability_cve_id`, which #9994 chunks at the
  same bound.
- A KEV, EPSS or suppression pass cannot reach a distro record (no index reads
  `upstream`), so it writes no OS finding and reopens no OS holder. KEV and EPSS
  never reached OS findings on the base either. A suppression scoped by `cve_id`
  alone is new: on the base a `cve_id=DEBIAN-CVE-X` scope applied on the operator
  pass itself; on this code a `cve_id=CVE-X` scope applies when each holding scope
  next runs its own pass, which reads it through the signal arm. For an immediate
  effect scope the suppression by `advisory_id` (the finding's deciding record id,
  shown as `advisory_id`), alone or together with `cve_id=CVE-X`; the advisory arm
  reaches the record on the operator pass and the holder fan-out reopens the
  holders. The holder's own cadence is not measured here. Tracked: #10019.

No-Regression Evidence: the upstream statement issues nothing for a pass with no
`upstream` key (`TestUpstreamListIsCleaned`, and the supplied-list gate in
`supplyChainImpactBranchRuns`), and the withheld ids only shrink what
`vulnerability_cve_id` reads. `TestSupplyChainImpactActiveEvidenceMatchesRetiredStatementLive`
and `TestSupplyChainImpactActiveEvidenceBufferBudgetLive` pass with the new branch.

## Accepted deviations from the ruling

Ready to paste into the PR body. An arbiter ruling reviewed all three and accepted
them, and nothing was reverted.

- **Decision 1, the OS-class test.** The rule is `vendorForOSPackageClass(ecosystem)`
  or the ecosystem `os`. The ruling's test example said `dpkg`, which names the
  fixture's package class and is not an ecosystem key, so a literal `dpkg` ecosystem
  keeps the scored-source rule. Whether any producer emits one was not checked; if
  one does, that is a normalizer defect to fix at the producer.
- **Decision 3, `UpstreamCopy`.** The ruling named `AffectedPackage`. The flag is also
  on `ImpactCVE`, because `representative()` ranks the distro copy over NVD and the
  product branch must judge native observations only.
- **Decision 5, liveness.** A record is retracted when every cve observation of its
  `AdvisoryID` is retracted, and a package whose record has no loaded cve observation
  is live. The filter is not limited to OS ecosystems, so a two-record language
  group with one retracted record can change verdict. No test moved. The golden
  corpus and cassette run says whether any fixture row moved; cite it in the PR body.

Readings of the second ruling, for a reviewer to check:

- **Retraction is per record, not per observation.** The ruling names the observations
  with an affected fact. A second cve fact of the same `AdvisoryID` also counts, as
  `liveAdvisoryPackages` and rule 5 of decision 1 read a record. Otherwise a live copy
  beside a withdrawn one flips the retraction with whichever fact the package paired
  with (`TestOSRetractionKeepsARecordWithOneLiveCVEFactLive`).
- **A holder's fix is the only one offered.** The ruling's seam says selection reads the
  deciding observation first, and its rule says the fix comes from that observation. The
  code follows the rule and does not fall through: a holder naming no fix leaves the fix
  empty, because taking another record's fix is the defect being fixed.
- **A holder with no fix is not compared.** It keeps its place in sorted order, so among
  holders `[A no fix, B u10]` A names the finding, and `[A u10, B no fix, C u12]` C does.
- **The seam carries `holdingFixRank`, not the fix string.** Selection then needs no
  comparator. The comparator is the verdict's own family for every OS class, not
  `rangeComparator`, which has no rpm or `os` case.
- **One test's expected name moved.** `TestUpstreamGroupingBundleBesideAPerCVERecord` read
  `UBUNTU-CVE` on a fix lower than the USN's. It now covers equal fixes, which keep sort
  order, and each side's higher fix.

## Scope and limits

- Alma and Rocky are in scope. The rule keys on `upstream`, not on the id prefix,
  and their errata carry it, so one bulletin reads one finding per upstream CVE.
  That is a net row increase on those hosts (1 to 3 on the Alma fixture, 1 to 4
  on the Rocky fixture), and it is the correct count: no per-CVE record exists
  for them to merge with.
- A language group with two or more records carrying package facts follows the
  same retraction rule, because the liveness filter is not limited to OS
  ecosystems. No existing test moved, and this was not measured against a fleet.
- A known asymmetry: a withdrawn GHSA beside a live NVD cve observation on a language
  package still reads no retraction (`TestLanguageRetractionKeepsTheGroupRule`). That is
  the base's rule, and #9914 does not change it.
- An SBOM and scanned row that disagree on the version leave the installed version
  unknown, so no record holds it (`TestConflictingInstalledVersionsDoNotPickAHolder`).
- The per-target indexes of the authorized derived pass apply the grouping but
  are not counted in the grouping counter.
- A quarantined (unreadable) `vulnerability.cve` record cannot be grouped, since
  its `upstream` is unreadable; it stays under its own key.
- Records stored before the producer wrote `upstream` read as `ungrouped` until
  they are collected again.
