# #9806 Ruling Reconciliation: Live-Record Census Rerun

Method: throwaway probe `TestZZReconcileCensus` (run, diffed, then deleted;
never part of the deliverable) that classifies five committed OSV OS fixture
records through the production chain below the dormant typed decode —
converter tagger (`OSVRecordEnvelopesReport`), release selection boundary
(`selectOSAdvisoryRelease`), matcher (`evaluateSupplyChainVersionMatch`) —
over a listed/unlisted host matrix with at-fix and above-all observed
versions. The probe decodes the converter payload maps directly, so both trees
see identical tagged facts.

- Base: `origin/main` at `c367d6c996` (#9927 behavior).
- Head: `drive/9806-reconcile-ruling` at the reconciliation commit.
- Corpus: `debian-cve-2023-38039`, `debian-cve-2003-1605`,
  `alpine-cve-2023-27537`, `ubuntu-cve-2016-5419`, `ubuntu-cve-2022-27775`
  (2 Debian, 1 Alpine, 2 Ubuntu) × listed lines + 2 unlisted lines each ×
  each listed fix and one above-all observed version. 101 cells.

## Flip counts (base -> head)

| Base verdict | Head verdict | Cells |
| --- | --- | --- |
| `not_affected_known_fixed` / `dpkg_known_fixed_unlisted_release` | `possibly_affected` / `os_release_not_in_advisory` | 14 |
| `possibly_affected` / `os_release_unlisted` | `possibly_affected` / `os_release_not_in_advisory` | 6 |

## Stability (identical on both trees)

- `affected_exact` / `apk_affected_range`: 3 cells.
- `not_affected_known_fixed` / `apk_exact_known_fixed`: 2 cells.
- `not_affected_known_fixed` / `apk_known_fixed`: 25 cells.
- `not_affected_known_fixed` / `dpkg_exact_known_fixed`: 4 cells.
- `not_affected_known_fixed` / `dpkg_known_fixed`: 18 cells.
- `possibly_affected` / `os_release_ambiguous`: 29 cells.

No cell moved except the 20 retired-verdict flips above: zero regressions in
matched, exact, ambiguous, and range-derived known-fixed reads.

## Scope limits

- The typed decode still drops `releases` until the SDK pin, so live facts stay
  untagged and read the old rules; this census measures the tagged world the
  ruling governs (the world the pin fixtures will land in).
- Contained-version rows are pinned by the committed reconcile tests, not by
  this census (the observed matrix uses at-fix and above-all versions only).
- ESM pocket behavior is pinned by the converter vector table and the reducer
  pocket test; no committed fixture carries a pocket tag yet.

## Bench marking (P2, issuecomment-6606547536)

`BenchmarkOSAdvisoryJoin` (fleet-wide join path) and
`BenchmarkOSAdvisoryJoinTargets` (authorized per-target path, which runs the
`markAuthorizedFixedVersionConflicts` host-line filter and the conflict-local
releases copy) at small benchtime (`-benchtime=10x -count=1`, `goos: linux`,
`goarch: arm64`), head `9398a6d13e` (on `origin/main` `d23d109691`),
2026-11-03T19:10Z. This tree contains the filter and the releases copy, so
the numbers below measure the shipped path:

| Benchmark | ns/op | ns/unit | B/op | allocs/op |
| --- | --- | --- | --- | --- |
| `BenchmarkOSAdvisoryJoin/1x` | 233,540 | 46,708/advisory | 211,980 | 1,875 |
| `BenchmarkOSAdvisoryJoin/5x` | 415,769 | 83,154/advisory | 423,203 | 2,893 |
| `BenchmarkOSAdvisoryJoin/10x` | 652,611 | 130,522/advisory | 708,366 | 4,157 |
| `BenchmarkOSAdvisoryJoin/miss-10x` | 327,174 | 65,435/advisory | 303,100 | 2,185 |
| `BenchmarkOSAdvisoryJoin/registry-10x` | 289,314 | 289,314/advisory | 289,338 | 2,038 |
| `BenchmarkOSAdvisoryJoinTargets/targets-1x` | 1,803,127 | 180,313/target | 1,322,706 | 14,739 |
| `BenchmarkOSAdvisoryJoinTargets/targets-5x` | 8,633,526 | 172,671/target | 6,415,740 | 71,838 |
| `BenchmarkOSAdvisoryJoinTargets/targets-10x` | 17,064,293 | 170,643/target | 12,712,256 | 142,449 |
| `BenchmarkOSAdvisoryJoinTargets/targets-10x-counted` | 17,552,872 | 175,529/target | 12,819,873 | 143,641 |

ns/target stays flat as targets scale 10 -> 50 -> 100 (180,313 ->
172,671 -> 170,643): the per-target path, host-line filter included, scales
linearly at small benchtime. No before/after claim: 10 iterations is a smoke
number, not a full benchmark, and the base tree cannot run the same pair
(the conflict path and the filter do not exist there).

Benchmark Evidence: `BenchmarkOSAdvisoryJoin` and
`BenchmarkOSAdvisoryJoinTargets` at `-benchtime=10x -count=1` on head
`9398a6d13e` (table above); ns/target flat across 10/50/100 targets, allocs
scale with rows, so the per-target host-line filter and the conflict-local
releases copy cost nothing measurable beyond the existing per-row work.

No-Observability-Change: the filter and the releases copy emit no new
counter, span, or log; unmatched hosts keep counting under the existing
`eshu_dp_supply_chain_impact_os_release_not_in_advisory_total` and the
`os_release_not_in_advisory` sub-signal, and the filter leaves findings
untouched rather than writing new evidence.
