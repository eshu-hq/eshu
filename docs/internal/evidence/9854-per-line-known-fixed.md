# #9854 per-line known-fixed for npm findings

Refs #9854. Refs #9430. Refs #9431.

The npm matcher can now say `not_affected_known_fixed` for a multi-line
advisory when the installed version is outside every affected range and at or
above a release line's Fixed event. Before, a blank scalar fix (#9430 blanks it
when an advisory names more than one fix) left every such version at
`possibly_affected / version_not_in_advisory_range`.

## Rule

`npmLineFixFor` (`go/internal/reducer/supplychain/core/ranges.go`) runs only
after no range, exact version, or raw range contains the observed version, and
only after the scalar fix check fails to match. One pass over the typed
SEMVER ranges, O(events):

- A **line** is a SEMVER range with an introduced event and no limit event.
  A range with a limit, and a Fixed with no introduced, are not lines.
- The verdict **fails closed** (stays `possibly_affected`) when the advisory
  holds a range the containment check cannot read: any non-SEMVER range
  (ECOSYSTEM, GIT, an unknown kind) in any package entry of the same
  PackageID, including a second source's entry. Containment skips those
  ranges, so one of them may contain the observed version. GIT is decided
  explicitly: fail closed.
- The verdict also fails closed when any SEMVER range lists its events out of
  version order. Containment walks events in listed order, OSV sorts them
  first; a range such as `[introduced 2.0.0, fixed 2.5.0, introduced 0, fixed
  1.5.0]` hides 2.2.0 from the listed-order walk. The same applies when one
  range holds an introduced event and a fixed or last_affected event at the
  same version: OSV leaves the tie order undefined, so the two walks can
  disagree.
- It fails closed when a package entry for the same PackageID holds no typed
  range, no non-blank exact version, and no raw range (the blank Gemnasium
  `affected_range` shape). The matcher cannot read what that source claims, so
  another entry's Fixed event must not decide.
- The nearest Fixed event at or below the observed version decides. A version
  below every introduced event has no such Fixed, so it stays
  `possibly_affected` (the 1.3% case in the QA sample).
- A `last_affected` boundary below the observed version that is nearer than
  every Fixed keeps `possibly_affected`. `last_affected`-only advisories stay
  fail-closed (0.1% of the QA sample; decision recorded on #9854).
- An uncomparable event version returns `malformed_advisory_range`.
- The reason stays `npm_semver_known_fixed`; only the evaluator changed.
  Cargo, Hex, Swift, and Pub delegate to the npm code but call
  `evaluateScalarSemverMatch`, so they keep their prior answer.

The explanation headline (`passedLineFixedVersion`,
`go/internal/query/supply/chain/impact/decode_helpers.go`) names the fix of the
line the observation has moved past: the nearest Fixed at or below it.

## Theory proof (QA, read-only, from the issue comment)

Sample of the first 2,000 of 13,169 npm `possibly_affected /
version_not_in_advisory_range` winners (207 advisories, 57 packages), scaled:
98.6% multi-line; 80.0% would flip (about 10,535, 95% CI about 10,300 to
10,770); 0.1% `last_affected`-only; 1.3% below the first introduced. The
comparator in that proof was a simple semver stand-in; the matcher tests below
are the real check. This seat has no QA access, so the post-change QA share is
NOT_CHECKED.

## Proof

- RED first: `TestNPMOutsideEveryRangeIsKnownFixedWhenFixedScalarBlank` failed
  on main for js-yaml 3.15.0, 4.1.1 and 3.99.0 (controls 4.0.5 and 3.0.0
  passed), and 12 rows of `TestNPMPerLineKnownFixedDecision` failed; both
  pass now.
- `TestNPMPerLineKnownFixedDecision`: js-yaml lines, equal to the fix,
  introduced `0` only, fixed without introduced, overlapping lines, prerelease
  versions and prerelease fixes, a later re-introduced range, two pairs in one
  range, GIT ranges, malformed events, empty ranges, no packages,
  `last_affected`-only, nearest-boundary ordering, limit ranges, a malformed
  observed version, and the scalar path unchanged.
- `TestNPMPerLineKnownFixedStaysNPMOnly`: cargo, hex, swift, pub, go keep
  `possibly_affected` for the multi-line shape.
- `TestNPMPerLineKnownFixedFailsClosedOnUnevaluatedRanges` (review findings):
  an ECOSYSTEM range mixed into one entry, an ECOSYSTEM-only entry from a
  second source, a GIT range, an unknown-kind range, unsorted events, an empty
  same-package entry (nil, empty, and blank-only versions), and an introduced
  and a fixed event at the same version in either order, each beside a SEMVER
  line whose Fixed is below the observed version. All 11 read known-fixed
  before the guards and `possibly_affected` now.
  `TestNPMPerLineKnownFixedTieOrderStillReadsContainment` pins that
  `[i 0, f 1.5.0, i 1.5.0, f 2.0.0]` at 1.5.0 still reads `affected_exact`.
- `TestNPMPerLineKnownFixedParity`: 700 seeded random advisories times 90
  versions (63,000 rows). The generator draws one to three ranges of one or
  two introduced..terminator pairs, shuffles event order, labels ranges
  ECOSYSTEM or GIT, uses prerelease versions, and splits the ranges across one
  or two package entries. The oracle follows the OSV algorithm (events sorted,
  union over every range kind) on integer versions and shares no comparator
  with the matcher. 5,659 rows flip `possibly_affected` to known-fixed, 0 other
  status moves, no flip happens where OSV says affected or on an
  advisory with a non-SEMVER or unsorted range, and on the 301 advisories the
  matcher may read, the flip set equals the oracle's nearest-Fixed policy. The
  other 399 advisories exercise the guards. Command: `go test ./internal/reducer/supplychain/core -run
  TestNPMPerLineKnownFixedParity -v`. The test covers that input space only;
  it says nothing about shapes it never generates (for example `limit` events
  mixed into the corpus, or non-semver versions).
- Mutants (each run against `TestNPM*`, `ranges.go` restored and compared
  after): `>=` to `>` on Fixed is killed by `Parity` and `Decision`
  (`equal_to_the_only_fix`); dropping the last_affected-nearer branch by
  `Parity` and `Decision`; dropping `closedLineRange` by `Decision` (2 rows);
  picking the farthest Fixed by `Parity` only; dropping the non-SEMVER guard
  by `FailsClosedOnUnevaluatedRanges` (4 rows) and `Parity`; dropping the
  order guard by `FailsClosedOnUnevaluatedRanges` (1 row) and `Parity`; dropping the
  empty-entry guard by `FailsClosedOnUnevaluatedRanges` (3 rows); dropping the
  same-version tie guard by `FailsClosedOnUnevaluatedRanges` (2 rows). The
  parity generator draws strictly increasing versions and no empty entries, so
  it does not exercise those last two guards.
- Explanation: `TestBuildSupplyChainVersionExplanationNamesPassedLineFix`
  failed (4.1.1 and 5.0.0 named 3.14.2) before the headline change.
- Golden surface: the only `possibly_affected` rows in
  `testdata/golden/e2e-20repo-snapshot.json` are the three-subject
  CVE-2026-91913 fixture (single line 1.2.3 to 1.3.0, observed 1.2.2, below
  the introduced event). It stays `possibly_affected`, so the snapshot and
  the `supply-chain-demo.json` cassette need no change.

## Telemetry

No-Observability-Change: no new metric, span, log key, or queue stage. The
flip is visible on the existing
`eshu_dp_supply_chain_impact_findings_total{domain="supply_chain_impact",outcome}`
counter (`emitCounters` in `handler_helpers.go`): `outcome=possibly_affected`
falls and `outcome=not_affected_known_fixed` rises by the flipped rows. The
persisted `match_reason` stays `npm_semver_known_fixed`, so the new rows also
move from the `comprehensive` to the `precise` detection profile, the same as
every scalar-path known-fixed row.

## Performance

Benchmark Evidence: `BenchmarkNPMKnownFixedPerLine`, darwin/arm64 (loaded
host, three 20,000-iteration samples, judged on allocs and bytes), measured
after the two fail-closed guards. Worst case: blank scalar and an observed
version above every line, so every line is read. 1x is 4 lines per advisory.

| Ranges per advisory | scalar-only (before) | per-line (after) |
| --- | --- | --- |
| 1x (4) | 591-612 ns, 136 B, 17 allocs | 1429-1445 ns, 312 B, 39 allocs |
| 5x (20) | 2899-3002 ns, 648 B, 81 allocs | 7220-7679 ns, 1592 B, 199 allocs |
| 10x (40) | 6211-6768 ns, 1288 B, 161 allocs | 15833-16351 ns, 3192 B, 399 allocs |

Allocations grow 39, 199, 399 across 1x, 5x, 10x (linear in lines). The pass
now does one containment-sized walk plus one order comparison per event, and
runs only for rows outside every range. No query, write, lock, or queue path
changes.

## Follow-up: containment walks listed order, OSV sorts

`versionRangeContainsDecision` (`ranges.go`, shared by every ecosystem)
evaluates a range's events in listed order. The OSV algorithm evaluates them
sorted by version, and the schema says sorting is not required of the
producer. The collector keeps source order. For `[introduced 2.0.0, fixed
2.5.0, introduced 0, fixed 1.5.0]` and observed 2.2.0, the listed-order walk
ends not affected because the trailing `fixed 1.5.0` resets it; OSV says
affected. Before #9854 that error landed on `possibly_affected`; this change
guards the per-line verdict against it but does not touch the shared walk.
Fixing the walk (sort before evaluating) changes every ecosystem and is
tracked as #9865.

A related shape is latent: `comparatorBranchContains` returns "valid, not
contained" on the first unmatched field and never validates the rest, so a raw
hyphen range such as `2.0.0 - 2.3.0` reads as `=2.0.0`. It predates this change
and is unreachable today (the Gemnasium parser rejects the bare `-` token and
the OSV envelope emits no raw range). It is tracked with #9865.

## Other ecosystems (follow-up, not changed here)

A throwaway probe (deleted) fed the js-yaml two-line shape, blank scalar,
observed 3.15.0 and 4.1.1, to every matcher with Kind SEMVER and ECOSYSTEM:
nuget, pypi, composer, rubygems, cargo, go, hex, pub, swift all return
`possibly_affected / version_not_in_advisory_range`. The shape is real in
code; no real OSV rows for those ecosystems were read, so none is changed
here. Also seen: with Kind `ECOSYSTEM`, nuget, composer, cargo, hex, pub and
swift return `possibly_affected` even for an observed version inside a range
(4.0.5), because those matchers skip non-SEMVER ranges. That is a separate
question from this change.

## NOT_CHECKED

- QA share before and after the change (no QA access from this seat).
- `derived_partition` with split-fact input (the diagnosis side observation).
- Real OSV rows for the other ecosystems.
- QA prevalence of advisories with GIT, ECOSYSTEM, or unsorted ranges. The
  QA flip estimate above was computed before those guards, so the real flip
  share can be lower.
- `make pre-push`, `make pre-pr`, the golden live gate (coordinator-owned).
