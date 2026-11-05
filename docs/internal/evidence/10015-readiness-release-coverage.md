# #10015 impact readiness: coverage per release

Refs #10015. Must merge before the #9993 Alpine release PRs, which make two
derived scopes per package normal.

## What changed

`readinessDerivedSourceStateCTE` kept one newest state per requested package, so
a fresh `release=v3.18` scope hid a stale, failed, or missing `release=v3.17`
scope of the same package. It now keeps the newest state per package, distro and
release. Every state row counts. Two rules adjust that, and neither reads the plan
to remove a row:

- A release the latest planner run names with no state row reads `pending`. A
  state row and its key rows are written together, so a scope the collector has
  not reached leaves nothing to join, and only the plan shows it is expected.
- A scope with no `release=` parameter at all (a legacy scope) stops counting for
  a package and distro once a real state row with a release exists for the same
  instance, package and distro. OSV answers a bare Alpine query with no
  advisories, so a legacy scope never covered a release, and without this rule it
  would read stale for good. A pending row does not trigger it. A scope with an
  empty first release value (`?release=&release=v3.18`) has a release parameter,
  so it stands.

The read path gains one `workflow_runs` plan expansion for OS requests. No write,
lock, lease, queue, index, or schema changed. The statement runs in the same
repeatable-read snapshot, so concurrency behavior is unchanged.

An earlier shape also dropped any release row the plan did not name. Rotation
showed why that is wrong, and it was removed. The next section has the evidence.

## Why the plan cannot retire a row

The coordinator reads a limit-sized window of the installed fleet each tick
(`ListOSPackageAdvisoryTargets`, 100 targets by default, rotating by hash slot) and
plans only the releases inside it. A plan that does not name a release says
nothing about whether the release left the fleet. A completeness flag cannot be
read from the plan either: a single-pass mode reads offset 0 under the same limit,
the budget check counts deduplicated queries and not rows, the plan carries no
total, and a tenant grant can rewrite a stored plan to a smaller one (arbiter
ruling arb-10015, decision 2).

Measured with the real reader and the real planner (the #9993 planner applied in a
scratch tree) over nine consecutive windows, two fact-id orders, a fleet of 450
Alpine OS package rows (150 packages on two releases):

| Package shape | Truth | Previous statement, window reads wrong |
|---|---|---|
| `pkg-104`: v3.17 fresh, v3.18 failed and newest | failed, naming v3.18 | 4 of 18: read `fresh` (the plan named only v3.17) |
| `pkg-200`: v3.18 fresh, stale legacy scope | fresh | 14 of 18: read `stale` |
| `pkg-250`: v3.18 fresh, v3.17 failed | failed, naming v3.17 | 4 of 18: read `fresh` (the plan named only v3.18) |
| `pkg-120`: v3.18 fresh, v3.17 never written | pending when the plan names v3.17, else fresh | 0 of 18 |

The new statement reads all four correctly in all 18 window reads
(`TestSupplyChainImpactReadinessDerivedRotationKeepsVerdictsLive`). Four of nine
windows name exactly one release of `pkg-104` in each fixture order. The arbiter
measured the number of two-release packages with exactly one release named per
window at 47 to 58 of 150 in the hash order and 0 to 100 of 150 when fact ids group
by image. QA would not have shown this: its 56 Alpine rows fit in one window.

## Request path and real sizes

Readiness is on the request path with no cache. `readSupplyChainImpactReadinessSnapshot`
in `go/internal/query/supply/chain/findings_handler.go` calls
`ReadSupplyChainImpactReadiness` on every findings page, and
`readiness_postgres.go` runs `ListReadinessQuery` in a repeatable-read snapshot each
time. The MCP tool `list_supply_chain_impact_findings` reaches the same store
([MCP tool contract matrix](../../public/reference/mcp-tool-contract-matrix.md)).

An `os` × N cell is one image with N OS packages: `resolveReadinessTarget` takes
every `sbom.component` of the image through `readinessDigestComponentTargetQuery`,
which has no limit. A request with one package id is `os` × 1.

Only Alpine carries a release: on the #9993 branch,
`installed_evidence_release.go` returns no release for any other ecosystem. Debian
and Ubuntu packages stay on one row, so the two-row cost applies to Alpine packages
times their planned releases. `os` × 1000 with two planned releases per package is
a ceiling, not a size anyone has reported.

QA figures, from the QA census of 2026-11-04 and the deployed configuration read
the same day: 56 `vulnerability.os_package` facts, all `pkg:apk/alpine` on Alpine
3.21, and 7 Alpine OSV scopes. The deployed configuration sets no `target_limit`,
so `derive_from_installed_evidence` plans at most the code default of 100 targets
(`defaultDerivedVulnerabilityTargets` in `intelligence_derivation.go`; the ceiling
is 5000). Release scopes do not exist on QA until #9993 ships.

Not measured: QA timing on real release scopes, and the share of real packages with
two releases once #9993 runs. The fixture is synthetic.


## Measurement

Thresholds, stated before the measurement (arbiter ruling arb-wave1-3, decision
A2). With the plan at 100 targets: at most +10 ms median at `os` × 100, at most
+35 ms at `os` × 1000, `npm` unchanged within noise, shared buffers within +200
blocks, and no sequential scan on either state table. The ruling excludes a
package cap, a cache, and dropped rows as mitigation. The statement changed after
the ruling's measurement, so byte identity with the measured statement (A3a) no
longer holds, and this section replaces the earlier figures.

No-Regression Evidence: the full `ListReadinessQuery` from the base commit
(`c44574eba8`, origin/main before this PR) against the previous head statement
(`31b7355e52`, with the plan-driven retirement) and the shipped statement
(`f83074e0b5`), on a private PostgreSQL 18.6 cluster (`fsync=off`,
`shared_buffers=512MB`) with the real bootstrap schema, through
`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON)` with production arguments.
The fixture has 200,000 states: 140,000 npm scopes and 20,000 Alpine packages with
three scopes each (two current releases and one superseded version), 200
historical planner runs, and a latest plan trimmed to 100 or 5,000 targets. Every
requested Alpine package among the first 50 (or 2,500) has two planned releases,
the worst case. Each cell is the minimum and median of 25 interleaved rounds with a
rotating first mover, after one warm run per statement.

**Host load was 24 to 46 during the 100-target run and about 33 during the
5000-target run, on a shared machine. Timings are noisy (the base `os` × 1000 median
swings between 59 and 86 ms across runs), so shared buffers, which do not depend on
load, are the primary signal.** Buffers for the shipped statement equal the previous
statement in every cell.

Plan at 100 targets, the installed-evidence default. Delta against the base
statement, in ms, minimum / median:

| Request | Base min / med | Previous head | Shipped | Ruling bound (median) | Buffers base to shipped |
|---|---|---|---|---|---|
| `os` × 1 | 0.98 / 1.03 | +0.22 / +0.21 | +0.22 / +0.21 | none | 24 to 24 |
| `os` × 100 | 6.08 / 7.51 | +3.01 / +2.44 | +2.63 / +1.99 | +10 | 1608 to 1608 |
| `os` × 1000 | 60.19 / 84.72 | +11.23 / +37.16 | +16.00 / +26.20 | +35 | 15009 to 15009 |
| `npm` × 1000 | 24.55 / 36.74 | +1.14 / -0.44 | -0.27 / -1.46 | within noise | 7009 to 7009 |

Every cell meets its bound, and neither state table gets a sequential scan
(`TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive`). The `os` × 1000 median
for the previous head (+37.16) is above its bound only because that run's base
median sat at a loaded 84.72 ms; the minimums are +11.23 and +16.00.

Plan at the 5000-target ceiling, outside the ruling's QA size, same harness:

| Request | Base min / med | Previous head | Shipped | Buffers base to shipped |
|---|---|---|---|---|
| `os` × 1 | 1.52 / 1.88 | +4.61 / +5.46 | +4.38 / +5.71 | 52 to 66 |
| `os` × 100 | 7.53 / 8.21 | +7.11 / +9.73 | +6.08 / +7.78 | 1636 to 1650 |
| `os` × 1000 | 42.02 / 86.02 | +23.58 / +60.06 | +22.25 / +16.27 | 16073 to 16087 |
| `npm` × 1000 | 27.59 / 29.34 | +0.03 / +1.87 | +0.94 / +1.36 | 7037 to 7037 |

At the ceiling, `os` × 1 pays a fixed cost of about +5 ms for reading the plan, and
`os` × 100 sits above this note's own 5 ms limit (+6.08 minimum). `os` × 1000 costs
+22.25 ms at the minimum, down from +30.95 ms on the statement the earlier note
measured, and under the first coordinator limit of about 25 ms. The statement now
has one window aggregate where it had two. The two runs differ in host load, so the
size of any saving from that is not established.

`TestSupplyChainImpactReadinessPackageAnchorLive`, the package-anchor leg, runs the
shipped statement on a different fixture. Its statement figure, interleaved three
rounds each, base tree against shipped tree, first mover alternating, host load 16
to 39:

| Tree | execution_ms (3 rounds) | shared buffers (3 rounds) |
|---|---|---|
| Base (`c44574eba8`) | 488.3, 566.2, 229.1 | 117939, 118082, 117885 |
| Shipped | 269.6, 258.3, 210.3 | 117939, 117885, 118082 |

Buffers do not move beyond the spread inside one tree. The earlier handoff's single
figure (1497 ms) was one loaded run with no base to compare to; it is superseded.

Answers, from one mixed fixture with failed, stale, rate-limited, missing,
legacy unqualified, batch-planned, unplanned, skipped, and a v3.16 release the plan
does not name, read at 100 and 2610 packages (`scope_count` at 2610 shown):

| Statement | `incomplete_reasons` and verdict | Counts |
|---|---|---|
| Base | `target_incomplete=false` at 2610, read as `skipped`: the defect | `scope_count=2609`, one scope per package |
| Previous head | names each uncovered release in the plan, drops `osv:stale:alpine:v3.16` | `scope_count=5219`, `stale_count=1` |
| Shipped | names each uncovered release, including `osv:stale:alpine:v3.16` | `scope_count=5220`, `stale_count=2` |

The shipped statement differs from the previous head in one row: the stale v3.16
scope of `alp-5` that the plan does not name now counts. That is the known limit
below, shown on purpose.

A hash join on the plan in place of the window aggregate was rejected on the
earlier statement. It removes a sort, but the planner chose a far worse join: +123
ms at `os` × 1000.

Shape changes the shim justified. These pairs are adjacent shim runs on the
extracted CTE at host load 20 to 55, so they show direction and rough size only:

| Change | Measured effect |
|---|---|
| Parse the release after the name join, not over the whole plan | `single os × 1000`: 895 ms to 511 ms |
| Materialize requested names before the config join | the expansion ran once rather than 4 times; `single os × 1`: 16.1 ms to 6.2 ms |
| Join requested names above the expansion | `single os × 1000`: 556 ms to 193 ms |
| Sort coverage keys under `COLLATE "C"` | `single os × 100`: 109 ms to 58 ms |
| Skip the release parse on scopes with no `release=` (full statement) | `npm × 1000`: +8.31 ms to +1.58 ms |
| Sort tie-breaks and kept scopes under `COLLATE "C"` (full statement) | `os × 1000`: +53.35 ms to +41.98 ms (minimum) |
| Then dedupe matched packages for totals and parse the release with `SPLIT_PART` (full statement) | `os × 1000`: +41.98 ms to +30.95 ms (minimum); the two were not separated |

Observability Evidence: `incomplete_reasons[]` names each uncovered release as
`osv:<state>:<distro>:<release>` (for example `osv:failed:alpine:v3.17`) beside
the plain `osv:<state>` entry. The derived `source_states[]` row reports
`pending_count`, `failed_count`, and `stale_count`, and `scope_ids` lists the worst
scopes first, including a planned scope with no state yet. No metric, span, or log
changed: readiness is a read path, and the existing WARN `readiness_snapshot` line
still covers a failed read.

Performance Evidence: the shipped statement adds no read, index, table or lock
beyond the earlier plan expansion. Buffers are equal to the previous statement in
every measured cell (table above), the 100-target ruling bounds hold, and the
5000-target cost at `os` × 1000 fell from +30.95 ms to +22.25 ms (minimum).

## Known limits

- **A release that left the fleet keeps its package not covered.** If a release has
  a last state that is not fresh and the fleet no longer installs it, the package
  reads not covered and the reason names that release (for example
  `osv:stale:alpine:v3.16`) until its state is fresh again. The base read that shape
  `fresh` because the newest state won. The answer is loud and fail-closed, and it
  replaces a silent false `fresh`. QA has one Alpine release line (3.21) and no
  instance of this today. A sound fix needs a complete fleet source (the active
  `vulnerability.os_package` facts, or a table the coordinator maintains), which the
  plan cannot give. That work is #10135. Rejected in advance: a union of recent
  plans, an age cutoff on the state row, and any rule that treats a missing plan
  entry as a missing release.
- **Pending detection depends on the window.** A release the plan names with no
  state reads `pending`. A release outside the current window is not detected.
  The arbiter measured the `pkg-120` shape (v3.18 fresh, v3.17 in the fleet and
  never written) reading `pending` in 2 of 9 windows and `fresh` in the other 7.
  The base never detects it. The acceptance criterion on the issue is re-scoped to
  match.
- Skip evidence names no release. A package matched on v3.18 and skipped on v3.17
  reads covered, under the existing "a match wins over a skip" rule.
  `planner_skipped_packages` also reads only the latest plan, so the `skipped`
  verdict depends on the window too; it fails toward `pending`.
- Debian and Ubuntu scopes of one `os` package share one newest-wins group, the
  same defect across distros. They carry no release qualifier, so this PR leaves
  them unchanged.
- The release parse reads the first `release` value, so `?release=&release=v3.18`
  has no release, where the regular expression it replaced read v3.18. The
  coordinator writes one value per scope, so this takes a hand-written row.
  `TestSupplyChainImpactReadinessDerivedEmptyReleaseValueHasNoReleaseLive` pins it,
  and the legacy rule keys on the absence of the `release=` parameter, never on an
  empty value.
- The rotation test skips until #9993 lands, because the planner mints no release
  scope ids before it. It was proved red and green in a scratch tree that applies
  the #9993 planner diff.
