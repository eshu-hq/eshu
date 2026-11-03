# #9865 OSV range events are read in version order

Refs #9865. Refs #9854.

The range-containment walk read a range's events in the order the feed listed
them. The OSV algorithm evaluates them sorted by version, and the OSV schema
does not require a feed to sort. Nothing upstream sorts: the collector keeps
source order and the affected-package merge concatenates ranges unsorted. A
range such as `[introduced 2.0.0, fixed 2.5.0, introduced 0, fixed 1.5.0]`
reported 2.2.0 as not contained, because the trailing `fixed 1.5.0` reset the
state. OSV says affected.

## Rule

`rangeEventsVulnerable` (`go/internal/reducer/supplychain/core/range_events.go`)
is the one event walk. Every ecosystem that reads typed range events calls it:
the shared semver walk (npm, cargo, hex, swift, pub, composer, rubygems, dpkg,
apk, Go reachability), Go modules, PyPI, and NuGet. Maven reads raw ranges only
and has no event walk.

An event changes the state only when the observed version reaches it
(introduced and fixed: observed at or above the version; last_affected:
observed above it), and each triggered event sets the state outright
(introduced: affected; fixed and last_affected: not affected). So the state
after the sorted walk is the state set by the greatest triggered event. One
pass keeps that event: O(events), no sort, no copy.

- **Ties.** OSV leaves the order of events at one version undefined. Among
  triggered events at one version the one listed last wins, which is what a
  stable sort gives and what the old walk did for a tie. No answer for a tie
  changed.
- **Uncomparable events** still return malformed: any event version that cannot
  be compared with the observed version, or with the event it would replace.
- **Go floor.** An introduced `0` still applies to every observed version,
  including a pseudo-version below `v0.0.0`.
- **Limit events** are gated before the walk, as before.
- **Display walk.** `explainEventsVulnerable`
  (`go/internal/query/supply/chain/impact/explain_range_events.go`) is the same
  rule for the explanation headline; the query layer cannot import the reducer.
  `matchingLineFixedVersion` now names the lowest Fixed above the observed
  version, the Fixed that ends the containing pair. It named the first listed
  Fixed, which for a multi-pair range could sit below the observed version.

### npm hyphen range and short-circuit (found in #9854 review, N3)

`comparatorBranchContains` returned "valid, not contained" on the first
unmatched field and never read the rest, so the npm hyphen range
`2.0.0 - 2.3.0` read as `=2.0.0`. Now:

- A branch reads every field. A malformed field after an unmatched one makes
  the branch malformed instead of a valid "not contained".
- `A - B` reads as `>=A <=B`, both bounds inclusive. It is well formed only for
  two full `major.minor.patch` versions. npm widens a partial bound
  (`1.2 - 2.3` is `>=1.2.0 <2.4.0`), which the matcher cannot expand, so a
  partial bound, a missing bound, a doubled hyphen, or a bound carrying an
  operator fails closed (malformed, `possibly_affected`).
- Reachability: unreachable today (the Gemnasium parser rejects the bare `-`
  token and the OSV envelope emits no raw range). The scalar known-fixed path
  shared the exposure and is fixed by the same change.

### What stays as it was

The npm per-line known-fixed rule (#9854) keeps its guard that refuses a
verdict when a SEMVER range lists its events out of order or ties an
introduced event with a terminator. The order half of that guard existed
because containment misread such a range; it is no longer needed for
correctness. It stays as a fail-closed guard so this change does not also
widen the set of known-fixed rows. Dropping it is a separate change with its
own parity run: it would let an out-of-order range beside a version outside
every range read as known-fixed, which OSV supports. The tie half is still
required.

## Theory proof (before any build)

Decision: build an O(n) selection walk, not a sort. Threshold, written first
(`shim-decision`, scratch): zero disagreements with an independent oracle over
at least 200,000 shuffled event lists; the current walk must disagree on the
issue shape; allocations linear in events. Deadline 30 minutes from 18:56 EST;
the shim finished at 18:58.

| Measure | Result |
| --- | --- |
| Selection vs integer-version OSV oracle (sorted, stable ties) | 250,000 random shuffled lists, 0 disagreements |
| Current walk vs the same oracle | 78,982 disagreements |
| Issue shape, observed 2.2.0 | oracle affected, current walk not contained, selection affected |
| Allocs/op, current vs selection (4/20/40 events) | 8/40/80 vs 14/78/158 |

One threshold line as first written failed: "0 allocations added". The added
allocations are not in the walk. Each event-versus-event compare goes through
`compareOSVSemver`, which allocates while normalizing a version. A sort-a-copy
walk needs n-1 adjacent compares (the same cost) plus a slice copy and
n log n, so selection dominates it. The line was restated, with the reason, as
the issue's own criterion: allocations linear in events.

## Proof

- RED first (on main `190c9e17cf`):
  `TestRangeEventsAreEvaluatedInVersionOrder` failed 8 rows for each of 15
  ecosystem and kind pairs (npm, cargo, hex, swift, pub, go SEMVER and
  ECOSYSTEM, pypi ECOSYSTEM and SEMVER, nuget, composer, rubygems SEMVER and
  ECOSYSTEM, debian, alpine) and passes now. The tie and uncomparable-event
  tests passed before and after, which is the point: no tie answer moved.
  `TestComparatorRangeReadsNPMHyphenRange`,
  `TestComparatorBranchValidatesEveryField`, and
  `TestNPMHyphenRawRangeReachesTheVerdict` failed on the hyphen forms and the
  short-circuit. The display tests failed on the issue shape and on the
  multi-pair headline.
- `TestRangeEventOrderParityAgainstOSVOracle`: 400 seeded advisories, one to
  three ranges of one to three pairs, `last_affected` terminators, open lines,
  introduced `0`, stray events that create ties, events shuffled, across 13
  family and kind pairs and 36 observed versions: 187,200 rows, 0 disagree with
  the oracle, 0 disagree where the two tie orders agree. The oracle uses
  integer versions and shares no comparator with the matcher. The embedded
  pre-change walk gives the row moves in this corpus: 4,875 rows move to
  "a range contains the version" and 55,458 leave it. That reads as
  `affected_exact`, or as `os_release_ambiguous` or `os_release_unverified`
  for the debian and alpine families, whose ranges are per release (#9848).
  Both directions are real: a list that
  names a terminator before its introduced event made the old walk say
  affected. The corpus shuffles uniformly, so these counts say nothing about
  how often real feeds list events out of order.
- Mutants, each run against the focused suites with the source restored and
  compared afterwards (20, all killed):
  - Walk (M1 to M11): pick the smallest triggered event; drop the
    event-versus-event compare; first-listed wins a tie; `last_affected`
    inclusive; ignore a compare failure; ignore the state; drop the Go floor;
    revert each of the shared, PyPI, NuGet, and Go walks to listed order. M5
    first survived because no built-in comparator is pairwise-invalid, and M7
    because only the helper, not the Go matcher, was tested. A fake pairwise
    comparator and an end-to-end Go pseudo-version test now kill them.
  - Hyphen (H1 to H4, H6): exclusive upper bound, exclusive lower bound,
    accept a partial bound, short-circuit on the first miss, ignore the
    hyphen. A sixth mutant (drop the doubled-hyphen check) survived because the
    check was redundant, so the code was deleted instead.
  - Display (E1 to E4): smallest event, no compare, inclusive `last_affected`,
    first Fixed instead of lowest. E3 first failed to compile and was redone
    in compilable form.
- #9854 rows: two fail-closed rows used the issue shape at 2.2.0, which is now
  correctly `affected_exact`. They were re-pointed at 1.7.0 (outside every
  range) so they still prove the guard. The unknown-kind row (R3-2 in the
  #9854 review) now uses the non-blank kind `NPM`, so it exercises the
  production path rather than a blank kind that decode drops.

## Performance

Benchmark Evidence: `BenchmarkRangeEventContainment`, darwin/arm64 on a
loaded host (load average 9), three samples each, judged on allocations. The
observed version is above every range, the worst case, because every event is
triggered and compared with the running best. 1x is 4 two-event ranges per
advisory; 5x and 10x scale the ranges.

| Ranges per advisory | listed order (before) | version order (after) |
| --- | --- | --- |
| 1x (4) | 490-569 ns, 128 B, 16 allocs | 805-888 ns, 192 B, 24 allocs |
| 5x (20) | 2336-2769 ns, 640 B, 80 allocs | 3991-4602 ns, 960 B, 120 allocs |
| 10x (40) | 5800-7690 ns, 1280 B, 160 allocs | 9276-10927 ns, 1920 B, 240 allocs |

Allocations grow 24, 120, 240 across 1x, 5x, 10x: linear in ranges, a constant
1.5x over the prior walk. The extra cost is one event-versus-event compare for
each triggered event, inside `compareOSVSemver`. It runs once per finding and
advisory in memory; no query, write, lock, queue, or worker path changes.

## Telemetry

No-Observability-Change: no new metric, span, log key, or queue stage. A row
that moves is visible on the existing
`eshu_dp_supply_chain_impact_findings_total{domain="supply_chain_impact",outcome}`
counter (`emitCounters` in `handler_helpers.go`). A range that contains the
observed version raises `outcome=affected_exact` and lowers
`outcome=possibly_affected` or `not_affected_known_fixed` by the same count.

## QA row moves

NOT_CHECKED. This seat has no QA access. The acceptance item asks for the
count of QA rows that change from `possibly_affected` to `affected_exact`,
measured read-only. A read-only measurement needs the stored OSV
`affected_ranges` events for the package-advisory winners currently
`possibly_affected` with reason `version_not_in_advisory_range`, then, per
advisory and observed version, the OSV verdict with the events sorted versus
as listed. Report the rows that move in each direction, per ecosystem. The
corpus above bounds nothing about that share.

## Related and not changed

- The #9854 order guard (above) is kept.
- Other ecosystems still return `possibly_affected` for a multi-line shape with
  a blank scalar fix; that is the #9854 follow-up, not this change.

## NOT_CHECKED

- QA row moves in either direction (above).
- How often real feeds list events out of order or tie them.
- The live golden gate, `make pre-push`, and `make pre-pr` (coordinator-owned).

`-race` was run: `go test -count=1 -race -p 2 ./internal/reducer/supplychain/...
./internal/query/supply/... ./internal/accuracygate/...` passed for the author,
and the reviewer reran it green at the same head.
