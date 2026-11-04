# #10015 impact readiness: coverage per release

Refs #10015. Must merge before the #9993 Alpine release PRs, which make two
derived scopes per package normal.

## What changed

`readinessDerivedSourceStateCTE` kept one newest state per requested package, so
a fresh `release=v3.18` scope hid a stale, failed, or missing `release=v3.17`
scope of the same package. It now keeps the newest state per package and release.
The latest planner run supplies the expected releases, because a scope the
collector has not reached has no state row and no key row. A planned release with
no state reads `pending`. Once the plan names releases for a package and distro, a
release it dropped and a legacy scope with no release stop counting. Otherwise
every release row stands.

The read path gains one `workflow_runs` plan expansion for OS requests. No write,
lock, lease, queue, index, or schema changed. The statement runs in the same
repeatable-read snapshot, so concurrency behavior is unchanged.

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

Thresholds, stated before the measurement. Mine: shared-buffer hits grow by at
most 10% or 200 blocks, median execution time grows by at most 10% or 5 ms, and
neither state table gets a sequential scan. The coordinator's limit for accepting
a miss: at most about 10% or about 25 ms on the full statement at `os` × 1000.

Arbiter ruling arb-wave1-3 replaced that coordinator limit, because `os` × 1000 at a
5000-target plan is not the size on the request path. With the plan at 100 targets:
at most +10 ms median at `os` × 100, at most +35 ms at `os` × 1000, npm unchanged
within noise, buffers within +200 blocks, and no sequential scan. The ruling also
excludes a package cap, a cache, and dropped rows as mitigation.

No-Regression Evidence: the full `ListReadinessQuery` from the base commit
(792fa0a3b3) against each candidate, on a private PostgreSQL 18.6 cluster
(`fsync=off`, `shared_buffers=512MB`) with the real bootstrap schema, through
`EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON)` with production arguments.
The fixture has 200,000 state rows: 140,000 npm scopes and 20,000 Alpine packages
with three scopes each (two current releases and one superseded version), 200
historical planner runs, and a latest plan at the 5,000-target budget. Every
requested Alpine package has two planned releases, the worst case. Each cell comes
from 25 interleaved rounds with a rotating first mover, after one warm run per
statement. Host load was about 13 on a shared machine, so buffers are the primary
signal. An earlier run at this size was stopped and discarded when load reached 51
and the unchanged npm statement went from 31 ms to 141 ms.

Delta against the base statement, in ms, as minimum and median. The shipped row is
byte-identical to the statement the shipped constant renders.

| Request | Base min / med | Shipped: two rows, tuned | One row per package | First candidate: two rows |
|---|---|---|---|---|
| `os` × 1 | 1.57 / 1.84 | +4.22 / +4.66 | +4.59 / +4.80 | +4.29 / +4.86 |
| `os` × 100 | 7.19 / 8.21 | +6.70 / +7.23 | +7.17 / +7.77 | +8.85 / +10.09 |
| `os` × 1000 | 56.37 / 59.06 | +30.95 / +30.80 | +32.66 / +35.16 | +53.35 / +56.02 |
| `npm` × 1000 | 30.25 / 31.34 | +1.65 / +1.64 | +1.77 / +2.23 | +1.55 / +1.65 |

Buffers at `os` × 1000 go from 15062 to 15076 for all three candidates, and npm
buffers do not change. A rerun of the same statement at load 15 to 27 gave a shipped
median of +5.32 ms, +7.99 ms, and +29.62 ms for `os` × 1, × 100, and × 1000, and
+2.24 ms for `npm` × 1000.

With the plan at 100 targets, the installed-evidence default, in the same harness
at load 17 to 29. The plan names the first 50 packages on two releases; every other
requested package still keeps a row per release:

| Request | Base min / med | Shipped: two rows, tuned | One row per package | Ruling bound (median) | Buffers |
|---|---|---|---|---|---|
| `os` × 1 | 0.72 / 0.84 | +0.16 / +0.20 | +0.19 / +0.18 | none | 24 to 24 |
| `os` × 100 | 4.66 / 5.87 | +1.86 / +2.37 | +2.28 / +2.10 | +10 ms | 1608 to 1608 |
| `os` × 1000 | 45.29 / 52.84 | +14.97 / +13.99 | +12.70 / +13.10 | +35 ms | 15009 to 15009 |
| `npm` × 1000 | 23.84 / 25.45 | +2.17 / +1.75 | +2.06 / +2.45 | within noise | 7009 to 7009 |

Every cell meets its bound, and neither state table gets a sequential scan. In
relative terms the shipped statement still adds about 40% at `os` × 100 and about
26% at `os` × 1000. At the 5000-target ceiling, `os` × 1 pays a fixed cost of about
+5 ms for reading the plan.

Against the thresholds:

- Buffers pass everywhere: at most +14 blocks.
- Neither state table gets a sequential scan. The new
  `TestSupplyChainImpactReadinessReleaseScopeQueryPlanLive` enforces this.
- `npm` × 1000 passes at about +1.6 ms. `os` × 1 passes at under +5 ms.
- **`os` × 100 (+7.23 ms) misses my 5 ms limit.**
- **`os` × 1000 (+30.80 ms, about 52%) misses my limit and the coordinator's limit
  of about 25 ms.** This is the extra cost a correct answer carries on this fixture.
  The full statement takes about 90 ms here, and the request is 1000 OS packages
  that all have two planned releases.

Where the time goes at `os` × 1000, from one `EXPLAIN ANALYZE` of the shipped
statement with timing on: the plan branch takes about 10 ms, the two window
aggregates about 4.5 ms, and the two coverage sorts about 11 ms. The base statement
has one such sort.

Answers, from one mixed fixture with failed, stale, rate-limited, missing, retired,
legacy unqualified, batch-planned, unplanned, and skipped packages, read at 100
and 2610 packages:

| Statement | `incomplete_reasons` and verdict | Counts |
|---|---|---|
| Base | `target_incomplete=false` at 2610, read as `skipped`: the defect | one scope per package |
| First candidate | `target_incomplete=true`, names each uncovered release | `scope_count=5219`, `fresh_count=5213` |
| Shipped | the same as the first candidate | the same as the first candidate |
| One row per package | the same as the first candidate | `scope_count=2608`, `fresh_count=2603` |

The shipped statement differs from the first candidate in one field: the order of
the ten-entry `scope_ids` preview, because ties now sort under `COLLATE "C"`. The
one-row statement under-reports `scope_count`, `fresh_count`, and
`rate_limited_count`, because it drops every release but the worst before counting.
It also loses time to the shipped statement, so the shipped statement keeps a row
per release.

A hash join on the plan in place of the two window aggregates was rejected. It
removes a sort, but the planner chose a far worse join: +123 ms at `os` × 1000.

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

## Known limits

- Skip evidence names no release. A package matched on v3.18 and skipped on v3.17
  reads covered, under the existing "a match wins over a skip" rule.
- Debian and Ubuntu scopes of one `os` package share one newest-wins group, the
  same defect across distros. They carry no release qualifier, so this PR leaves
  them unchanged.
