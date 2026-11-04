# OS Source-Package Join Read Shape: UNION Evidence (#9859 Criterion 4 + 5)

An OSV Debian advisory names the source package (`openssl`), while the
installed rows built from it carry binary names (`libssl3`). The reducer
already indexes installed rows under both keys (#9859 branch work); this note
records the narrowed-read half: the arbiter's UNION ruling for criterion 4
and the criterion-5 bench on the UNION shape.

## Read shape

`listOSPackageAdvisoryTargetsForPackagesQuery` pages the candidates CTE as a
UNION of two single-arm keyset sub-pages — one binary arm, one source arm —
each with its own `fact_id > $2 ORDER BY fact_id LIMIT $3`, with an outer
`ORDER BY fact_id LIMIT $3`. UNION, never UNION ALL: a row whose binary name
equals its source name is a candidate of both arms and must drain exactly
once. The drain code, page size, crossing/truncated semantics, parameter
order, and one-statement contract are untouched, and migrations 274 (source
index) and 153 (binary index) are unchanged.

A bare OR across the two expression predicates made the generic plan cost the
primary-key range scan as cheaper than either expression index: at 50,000
installed rows the generic plan scanned `fact_records` on its pkey and
filtered ~350,000 rows per page. Each UNION arm is a single sargable
predicate, so each keeps its own expression index under the same generic plan.

## Criterion-4 proof

`TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive` seeds 50,000
`vulnerability.os_package` rows plus 300,000 filler facts (the realistic
production mix), runs `ANALYZE`, and EXPLAINs the shipped query constant
under `PREPARE` + `force_generic_plan` — the plan the reducer runs through
the pgx prepared-statement cache. Three cells: the real advisory key set, a
source-only key set, and a dense deep-cursor page (1,000 rows share one
source key; the cursor starts halfway through them). Every cell asserts both
index names present, no `Seq Scan on fact_records`, no `fact_records_pkey`,
at most 1,000 buffers and at most 1,000 rows removed.

`TestListOSPackageAdvisoryFactEnvelopesMixedArmsPageToCompletionLive` drains
600 binary-only plus 600 source-only rows with interleaved fact ids across
three pages through the public drain: each active row exactly once, 200
inactive-generation source rows read past and never returned, and one
both-arm row (binary name equals source name) exactly once. The key set is
derived from the production oracle
(`supplychaincore.InstalledOSPackageSourceKey`), and the both-arm case is
also pinned in `TestListOSPackageAdvisoryFactEnvelopesPURLFormsMatchTheGoMatcherLive`,
where a UNION ALL would return it twice and fail the exact-set assertion.
The generation-flip test passes unmodified.

Performance Evidence: per-cell before (OR shape, commit `011aa10d25`) and
after (UNION shape), same host (linux/arm64, 16 CPUs), Go 1.26.6,
PostgreSQL 18.6, disposable database, `PREPARE` + `force_generic_plan`.

| Cell | Buffers before | Buffers after | Rows removed before | Rows removed after | Time before | Time after |
| --- | --- | --- | --- | --- | --- | --- |
| realKeys (2 sparse keys) | 19,478 | 13 | 349,998 | 0 | 70.681 ms | 0.049 ms |
| sourceKeys (source-only) | 19,478 | 11 | 349,999 | 0 | 69.406 ms | 0.039 ms |
| denseDeepCursor (500-row page inside 1,000) | 646 | 79 | 0 | 500 | 1.762 ms | 1.165 ms |

Buffers are the outermost plan node's shared hits; rows removed sums the
`Rows Removed by Filter` lines. The sparse cells go from a full-table filter
to two index seeks (both index names in every plan). The dense cell already
sought on the OR shape when the cursor landed inside a matching run; on the
UNION shape the source arm picks a bitmap heap scan and its page-shared
recheck removes 500 rows — inside the 1,000 bound, with 8x fewer buffers.
ns/op is indicative on this shared host; buffers and removed rows are exact.

## Criterion-5 bench

`BenchmarkOSSourcePackageAdvisoryJoinScales` builds one fleet-wide pass from
the converted openssl source advisory plus 10, 50, and 100 installed libssl3
rows carrying the extra source key. 8 rounds, `-benchtime=1000x`, same host
as above. The fleet-wide pass yields one finding per CVE and package, so the
row count is the scale and ns/row must stay flat.

Performance Evidence: `BenchmarkOSSourcePackageAdvisoryJoinScales` medians
of 8 rounds (UNION tree; the reducer code is identical on the OR tree, so
there is no before/after — the SQL change does not touch this path).

| Scale | Rows | ns/op | B/op | Allocs/op | ns/row |
| --- | --- | --- | --- | --- | --- |
| 1x | 10 | 40,400 | 46,574 | 279 | 4,040 |
| 5x | 50 | 106,600 | 136,228 | 685 | 2,132 |
| 10x | 100 | 186,500 | 254,552 | 1,189 | 1,865 |

No-Regression Evidence: `BenchmarkOSAdvisoryJoin` (fleet-wide) and
`BenchmarkOSAdvisoryJoinTargets` (per-target) on the UNION tree, same
methodology as the #9848 note (8 rounds; 1000x fleet, 200x targets).
ns/op is host-specific (that note ran on Apple silicon); allocs are exact
and comparable. The #9848 after-numbers were 1,722 / 2,329 / 3,083 allocs
(fleet 1x/5x/10x) and 13,900 / 67,650 / 134,100 (targets); the UNION tree
measures 1,915 / 2,934 / 4,194 and 15,176 / 73,870 / 146,488. The delta is
the #9859 reducer cost (dual-key index entries, source derivation and
signals), identical on the OR and UNION trees. ns/target stays flat
(178.5 / 174.1 / 172.9 µs), so the join is not superlinear; the miss-10x
and registry-10x controls hold at 2,225 and 2,038 allocs.

No-Observability-Change: the UNION rewrite adds no telemetry. The
source-join signals (`os_advisories_joined_via_source`, the derivation
evidence fields) ship in the #9859 branch with their own tests and are
unchanged by this lane.

## Not covered

- A release tag on each range, so a row reads only its own line (PR-2b).
- The fleet-wide pass reports one installed row per advisory and package.
- The dense cell's 500 bitmap-recheck removals are inherent to sharing heap
  pages across the cursor; a heap-ordered source posting list would remove
  the recheck but is not worth an index rebuild for 500 filtered rows.
