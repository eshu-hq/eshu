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
so `derive_from_installed_evidence` plans at most the code default of 100 queries
(`defaultDerivedVulnerabilityTargets` in `intelligence_derivation.go`; the ceiling
is 5000). Release scopes do not exist on QA until #9993 ships.

Not measured: QA timing on real release scopes, and the share of real packages with
two releases once #9993 runs. The fixture is synthetic.


## Measurement

Thresholds, stated before the measurement (arbiter ruling arb-wave1-3, decision
A2). With the plan at the QA size of 100 queries: at most +10 ms median at `os` × 100,
at most +35 ms at `os` × 1000, `npm` unchanged within noise, shared buffers within
+200 blocks, and no sequential scan on either state table. The ruling excludes a
package cap, a cache, and dropped rows as mitigation. The statement changed after
the ruling's measurement, so byte identity with the measured statement (A3a) no
longer holds, and this section replaces the earlier figures.

**Plan shape.** The planner does not write one target per package. The installed
evidence path chunks queries into batch targets
(`vulnerabilityDerivedQueryChunks`), so a plan of 100 queries is 2 targets whose scope
ids run to about 1.2 KB, and a plan of 5000 queries is 76 targets with ids up to
1.8 KB. The limit counts queries, not targets. An earlier version of this note
measured single-package targets, a shape the planner does not write: it understated
the ceiling cost and missed a spill described below (review finding F2). Those
figures are replaced.

No-Regression Evidence: the full `ListReadinessQuery` from origin/main
(`788cd37784`) against the previous head statement (`4ded3e5669`) and the shipped
statement (this PR's head), on a private PostgreSQL 18.6 cluster (`fsync=off`,
`shared_buffers=512MB`, `work_mem=4MB`) with the real bootstrap schema, through
`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON)` with production arguments.
The fixture has 200,000 states (140,000 npm scopes, 20,000 Alpine packages with three
scopes each) and 200 historical planner runs. The latest plan names the first 50 (at
100 queries) or 2,500 (at 5000 queries) Alpine packages on two releases, chunked 66
packages per target. Each cell is the minimum and median of 25 interleaved rounds
with a rotating first mover, after one warm run per statement. "Five distros" lists
five OS distros in the instance configuration, as QA does; the base statement fans
each hit out once per configured distro, which is why its buffers grow.

**Host load was 49 at the start of this run and 28 at the end, on a shared machine.
Timings are noisy, so shared buffers and temp blocks, which do not depend on load,
are the primary signal.**

Plan of 100 queries (2 batch targets), the installed-evidence default. Delta against
origin/main, in ms, minimum / median for the shipped statement:

| Distros | Request | Base min / med | Shipped | Ruling bound (median) | Buffers base to shipped |
|---|---|---|---|---|---|
| 1 | `os` × 100 | 4.52 / 5.90 | +1.80 / +1.92 | +10 | 1608 to 1608 |
| 1 | `os` × 1000 | 38.30 / 44.47 | +14.80 / +13.09 | +35 | 16045 to 16045 |
| 1 | `npm` × 1000 | 21.93 / 27.44 | +1.45 / +2.61 | within noise | 8008 to 8008 |
| 5 | `os` × 100 | 15.28 / 18.06 | -4.31 / -6.13 | +10 | 4808 to 1608 |
| 5 | `os` × 1000 | 148.41 / 196.37 | -51.83 / -69.80 | +35 | 48119 to 16045 |
| 5 | `npm` × 1000 | 29.34 / 34.25 | +1.74 / +10.43 | within noise | 8008 to 8008 |

Every `os` cell meets its bound. The five-distro `npm` median (+10.43) is a load
artifact: the minimum is +1.74, and the previous statement read +4.13 in the same
run. Neither state table gets a sequential scan
(`TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive`).

Plan of 5000 queries (76 batch targets), the ceiling, outside the ruling's QA size.
The previous head statement spilled to disk here and origin/main does not:

| Distros | Request | Base min / med | Previous head | Shipped | Temp blocks, base / previous / shipped |
|---|---|---|---|---|---|
| 1 | `os` × 100 | 6.61 / 6.96 | +4.64 / +4.86 | +4.15 / +4.49 | 0 / 0 / 0 |
| 1 | `os` × 1000 | 54.45 / 60.77 | +58.72 / +121.90 | +25.11 / +56.10 | 0 / 1378 / 0 |
| 1 | `npm` × 1000 | 26.57 / 32.67 | +0.78 / +2.14 | +1.61 / +1.35 | 0 / 0 / 0 |
| 5 | `os` × 100 | 12.69 / 14.46 | +5.30 / +5.62 | +4.50 / +5.18 | 0 / 0 / 0 |
| 5 | `os` × 1000 | 109.66 / 145.07 | +48.80 / +57.16 | +37.81 / +35.18 | 0 / 1537 / 0 |
| 5 | `npm` × 1000 | 26.13 / 29.31 | +1.47 / +2.01 | +1.51 / +2.36 | 0 / 0 / 0 |

**The spill.** Carrying the batch scope id (about 1.7 KB) through the planned-release
`DISTINCT ON` and the window sort exceeded `work_mem` in both (`Sort Method: external
merge`, 852 and 1378 temp blocks written). The shipped statement carries the target's
ordinal instead and joins the scope id back after the newest state per release is
chosen. It writes no temp blocks in any cell, buffers equal the previous statement, and
the full derived readiness live tests, the query-plan test and the mutants below pass
on it. The previous statement's shape was proved first as a scratch variant under the
same harness before it replaced the real one. Production `work_mem` is NOT_CHECKED: the
cluster here uses PostgreSQL's 4 MB default.

At the ceiling `os` × 1000 costs +25.11 ms at the minimum (one distro) and +37.81 ms
(five distros), against the QA-size bound of +35 ms. The ruling does not bind the
ceiling, and the minimum is the load-robust figure. A 5000-query plan means a fleet of
thousands of distinct packages, which QA does not have (56 Alpine facts).

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

Shape changes the shim justified, measured on single-package targets before the
batch shape was known. These pairs are adjacent shim runs on the extracted CTE at
host load 20 to 55, so they show direction and rough size only:

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
beyond the plan expansion. Buffers are equal to the previous statement in every
measured cell (tables above), the ruling bounds hold at the 100-query plan for one
and five distros, and the shipped statement writes no temp blocks at the 5000-query
plan where the previous one wrote 1378 and 1537.

Mutants: 19 single-edit mutants of the shipped statement (dropping each rule, binding,
tie-break and reason filter) ran against the derived readiness live tests and the
query-plan test in a private tree. 18 turn a test red. One survives, `NULLS LAST` in
the scope-id dedupe of `kept`: it differs only when a scope has a real state but no key
row for a package the plan names in the same scope, and `upsertVulnerabilitySourceStateQuery`
writes state and keys in one statement, so no writer produces that data. The first run
left four survivors (a pending row retiring a legacy scope, the legacy rule and the
scope-id lookup crossing instances, and `NULLS LAST`); the first three now have pins.

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
- **Two versions of one package on one release share one newest-wins group.** When
  they land in different batches that are both planned, an older failed version
  (older chunk failed, newer fresh: reads `fresh`) or an unwritten one (one planned
  chunk unwritten, the other fresh: reads `fresh`) is hidden. The base reads both
  the same, so this is not a regression. The rule stays because superseded versions
  stay in the state table for good, and demanding every version fresh would read
  stale forever. The gap is the cost of that rule; #10135 does not close it.
- **A skipped package beside a stale matched one changes verdict.** Base read a
  package matched stale plus a package with skip evidence as `skipped` with
  `target_incomplete` false. The shipped statement reads `stale` with
  `target_incomplete` true, because the stale scope is real work the skip evidence
  does not cover. Clients that treated the base answer as settled see an incomplete
  verdict now. The public readiness page and the package README state it.
- Skip evidence names no release. A package matched on v3.18 and skipped on v3.17
  reads covered, under the existing "a match wins over a skip" rule.
  `planner_skipped_packages` also reads only the latest plan, so the `skipped`
  verdict depends on the window too; it fails toward `pending`.
- Debian and Ubuntu scopes of one `os` package share one newest-wins group, the
  same defect across distros. They carry no release qualifier, so this PR leaves
  them unchanged.
- The release parse reads the first `release` value, so `?release=&release=v3.18`
  has no release, where a regular expression would read v3.18. The
  coordinator writes one value per scope, so this takes a hand-written row.
  `TestSupplyChainImpactReadinessDerivedEmptyReleaseValueHasNoReleaseLive` pins it,
  and the legacy rule keys on the absence of the `release=` parameter, never on an
  empty value.
- The rotation test skips until #9993 lands, because the planner mints no release
  scope ids before it. It was proved red and green in a scratch tree that applies
  the #9993 planner diff.
