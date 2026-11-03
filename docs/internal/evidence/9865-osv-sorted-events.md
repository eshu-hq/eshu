# #9865 OSV range containment evaluates events sorted

Refs #9865. Refs #9854. Refs #9900.

`versionRangeContainsDecision` walked a range's events in the order the feed
listed them. The OSV reference algorithm evaluates `sorted(range.events)`,
so a range whose pairs a feed lists out of version order missed versions it
contains: with events `[introduced 2.0.0, fixed 2.5.0, introduced 0, fixed
1.5.0]` and observed `2.2.0`, the walk ended on `fixed 1.5.0` and reported
not contained. The miss landed on `possibly_affected` for most families, and
on `not_affected_known_fixed` for OS families via the other pair's fix. The
#9854 per-line guard failed closed on unsorted events, so the new per-line
known-fixed rule could not turn this miss into a false `not_affected` row;
the containment walk itself kept the defect for every ecosystem.

## Fix

`go/internal/reducer/supplychain/core/ranges_sorted.go` adds one shared
walk, `sortedEventsContainDecision`, used by every ecosystem that reads range
events: the shared `versionRangeContainsDecision` (npm/cargo/go/hex/swift/pub
semver, RubyGems, Composer, dpkg/apk ECOSYSTEM) and the former copies in
`goModuleRangeContainsDecision`, `nugetSemverRangeContainsDecision`, and
`pypiRangeContainsDecision`, which now differ only in their comparator. The
helper sorts a copy of the version-bearing events with a stable sort, so ties
keep listed order exactly like the reference. A feed that already lists
events in order takes a zero-allocation fast path over the input slice.
Malformed and limit contracts are unchanged: every version-bearing event is
validated before the walk, which previously evaluated each one with no early
exit, and the limits check still runs first. The `npmLineFixFor` fail-closed
guard on unsorted events stays as defense in depth. `comparatorBranchContains`
(and its Go-module twin) now reads every field before answering, so a hyphen
range such as `2.0.0 - 2.3.0` reads invalid instead of `=2.0.0` not contained
(reviewer finding N3, unreachable today: the Gemnasium parser rejects the
bare `-` token and no envelope emits a raw range).

## Accuracy proof

- Red tests failed for the intended reason, then passed: the unsorted
  two-pair shape returns `affected_exact` for `2.2.0` in six matcher families
  (npm, go, composer, rubygems, pypi, nuget), contained for the dpkg/apk
  walks, invalid for the hyphen branch, and listed tie order is preserved.
- Parity differential over all 24 permutations of the two-pair event order x
  4 observed versions x 7 comparators against an independent interval oracle:
  672 rows, 0 wrong moves. Seeded randomized differential against the
  independent integer-comparator oracle over shuffled multi-range advisories
  (prereleases, `last_affected` terminators): 25,740 rows, 362 shuffled
  ranges, 0 wrong moves.
- Read-only QA corpus (6 families x 4 observed versions + 1 dpkg row): 7 rows
  move to `affected_exact` (6 from `possibly_affected`, dpkg from
  `not_affected_known_fixed`); no other row moves.

## Benchmark Evidence (#9865)

`BenchmarkSortedRangeContainsScale` (linux/arm64, Go 1.26.10), observed
`3.0.0` above every range so each range pays the ordering check; 1x is 4
two-pair ranges per advisory. Baseline is base `524ed0503f` with the same
bench file; the old walk cost is order-independent.

| input | scale | baseline ns/op | after ns/op | baseline B/op | after B/op | baseline allocs | after allocs |
|-------|-------|----------------|-------------|---------------|------------|-----------------|--------------|
| unsorted | 1x | 1880-1932 | 6146 | 328 | 3144 | 41 | 113 |
| unsorted | 5x | 8925-9031 | 30223 | 1608 | 15528 | 201 | 561 |
| unsorted | 10x | 17899-17928 | 64893 | 3208 | 31368 | 401 | 1121 |
| sorted | 1x | 3477 | 5485 | 632 | 984 | 79 | 123 |
| sorted | 5x | 17666 | 27474 | 3192 | 4952 | 399 | 619 |
| sorted | 10x | 34891 | 56333 | 6392 | 9912 | 799 | 1239 |

Unsorted allocations scale linearly in the range count (bytes 4.99x/9.98x,
allocs 4.96x/9.92x for 5x/10x). The sorted (common) input takes the
zero-allocation fast path and pays only the ordering scan. Absolute cost stays
small (about 700 B and a microsecond per unsorted range) and the matcher runs
fully in memory per finding: no new backend call, queue stage, lock, or graph
write. No-Regression Evidence (#9865): same linear shape with small constant
overhead; sorted input keeps the pre-fix decision path plus the scan.

## Observability

No-Observability-Change (#9865): the sorted walk and the comparator-branch
validation are pure in-process functions over already-loaded advisory facts
that issue no query, add no failure path, and emit no metric of their own.
Decision volume stays covered by
`eshu_dp_supply_chain_impact_findings_total`, malformed input by
`eshu_dp_reducer_input_invalid_facts_total`, and pass cost by
`eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. The new stage file is registered in
the supply-chain section of
`docs/public/observability/telemetry-coverage-reducer-domains.md`.
