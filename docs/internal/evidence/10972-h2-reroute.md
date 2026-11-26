# #10972 H2 lane 1 (A2 re-route, Lane-1b routing-mass amendment): implementation proof

Date: 2026-11-26. Lane: `drive/10972-h2-reroute` (executor leaf).
Binding rulings: [arbiter-10972-h2shape.md](../../../.claude/arbiter-10972-h2shape.md)
Decision 3 A2-branch, amended by
[arbiter-10972-h2shape-delta.md](../../../.claude/arbiter-10972-h2shape-delta.md)
(Decisions 1–3). Theory shim:
[10972-h2-holders-shim.md](10972-h2-holders-shim.md) (lane
`drive/10972-h2-shim`, QA primary `eshu-postgresql-0`, PG 18.x, ~13M-row
supports table, 4 GB shared buffers, 2026-11-26).

## What changed (Lane-1b, same branch)

As-built Lane 1 counted through the probe template with both arms at
gate+1 (member arm `LIMIT 78437` over the payload GIN): on the failing
class the member arm re-fetched the unbounded heap pass the probe's K=100
design exists to bound (coordinator: warm timeouts, then 9,034 / 2,211 /
1,635 / 1,270 ms on member-carrying sets). Lane-1b amends the builder with
a member-specific fill bound: the direct (cve_id, advisory_id) arms stay
exact-to-gate (`LIMIT 78437`, saturate at 78,436); the member arm takes
fill bound `HolderMassProbeFillToCapRows` (100, the existing single
source — no new threshold) with saturation at gate+1
(`CASE WHEN COUNT(*) > 100 THEN 78436 ELSE COUNT(*) END` over `LIMIT 101`).
The probe call passes 100/100 and the probe text is byte-identical (pinned
by `TestHolderMassProbeQueryIsByteIdentical` against the committed
golden). Gate (78,435), router, cold packing, parallelism (4), and the 10s
guard are unchanged.

The report is a routing mass, never a true mass: at or under the gate it
is the unsaturated summed per-arm count for direct-only and member<=100
ids (still overestimating distinct rows via per-arm double-count and
superseded generations), saturating at gate+1 above it.
Routing truth table (also the shape-test and evidence contract): ranged
iff direct capped sum > gate, OR member rows > 100, OR exact sum > gate
with member <= 100. The bias is documented over-routing: a 101-member-row
id with zero direct rows ranges (16 near-empty statements, bounded);
double-counted rows only push toward ranged. Under-routing requires
member <= 100 AND summed mass <= gate, where the member contribution is
provably trivial.

Naming follows the routing-mass semantics everywhere (preferred seam
rename while unmerged): `HolderRoutingMassEstimator` /
`EstimateHolderRoutingMasses` / `HolderRoutingMassQuery`, stub
`routingMasses`/`routingMassErr`/`routingMassCalls`, router comments,
`HOLDER_FANOUT.md`, this note. `fanout_max_id_mass_rows` carries the
resolved routing mass. No "true"/"exact" claim survives for this value.

Small corrections in the same edit: the key-range statement counts come
from the read's `Statements` at the execution site (the >1,024-byte
fallback runs one shipped statement, not 16 pre-added ranges; pinned by
`TestAboveGateLongIDFallbackCountsOneStatement`); the routing-mass count
runs only when ranging is possible (a port without the key-range seam
runs no count and `maxMass` stays at the probe cap; pinned by the
`mass_probe`-is-1 assertion in
`TestSingleStatementReadRecordsDurationByKind`); the stale #11430
sentence in the hot-mass threshold comment is corrected.

## Gate value (unchanged)

Largest A1-passing shim singleton mass 156,870 rows (advisory ids, warm
~1.3s isolated) halved for margin = 78,435. Cap justification (delta
Decision 1): the only member masses with A1 evidence are 0 (pass) and
>=40,072 (fail); any cap in (100, 40k) would claim A1 safety in an
unmeasured band. Residual (delta P2-1): a direct-only or member<=100
singleton just under the gate has no concurrent A1 measurement at any
mass in the packet, so this note does not claim concurrent-at-gate
proven. No census id exercises the member-decisive truth-table row (all
six evidence rows are direct-driven or single-arm exact), and
HolderRoutingMassQuery has no live plan-guard row (holderPlanStatements
omits it; the query is exported only for the plan-shape proof). Both are
Lane-2 obligations: a seeded member-only live differential and a
generic-mode plan-guard row. Lane 2 (live replay with timeout share to
zero, now including the member sets and a routing-mass differential) owns
that proof.

## Performance Evidence: Lane-1b micro-shim (QA primary, 2026-11-26)

Derived from the amended builder by a temporary generator (never
hand-copied; removed after the run). Every statement ran in
`REPEATABLE READ, READ ONLY` with `SET LOCAL statement_timeout = '10s'`
(the production guard) and `SET LOCAL enable_seqscan = 'off'` (the
production mass-probe setting), one `PREPARE` + one `EXECUTE` per session
(custom plan, the production one-shot posture), per-run transactions.
Warm protocol: discard run 1 as cold, then >=5 warm repeats.
Diagnostic-only labeled exceptions (read-only): 120s/300s guard for the
cache-warming SELECTs and the exact per-arm census, disclosed per set
below. Substrate as the theory shim; the live primary drifted slightly
since the shim census (e.g. advisory ids 156,870 -> 157,543 rows) with no
verdict impact.

Bar (parent Decision 2, extended to the routing count by delta Decision
1/Required Proof): every warm statement p95 <= 2s AND max <= 5s, zero warm
timeouts, and reported masses matching the truth table against diagnostic
true counts.

| set | warm exec ms (discard-cold first) | p95 | max | timeouts | verdict |
|---|---|---|---|---|---|
| member CVE-2026-42043 alone | cold timeout (discard); cache-fill 6,176 (disclosed, excluded: 33,864 `read=` buffers, effective-cold); 169, 147, 146, 169, 229*, 124, 141, 124 | 229 | 229 | 0 | PASS |
| mixed 5-chunk probe-hot ids, ONE batched statement | 6 cold-timeout runs (disclosed: canceled runs never warm the cache); diagnostic 120s warmup SELECT (labeled) completed; guarded 1,389 (discard), 924, 554, 562, 584, 611 | 924 | 924 | 0 | PASS |
| hot5 advisory, ONE batched statement | 4 cold timeouts + 5,421 + 609 (disclosed); diagnostic 120s warmup SELECT (labeled) completed; guarded 780 (discard), 407, 409, 457, 414, 530 | 530 | 530 | 0 | PASS |

\* 229 ms warm with partial `read=` (settling); runs 4–6 and all member
warms after it are all-hit.

Per-statement shape (representative warms, all custom plan): direct arms
are index-only scans on the partial btrees (~16k heap fetches per arm per
id); the member arm is a bitmap heap scan stopping at the bound
(`Heap Blocks: exact=103` member alone, `exact=515` mixed5 batch, lossy
0, recheck 0, no temp); top shared buffers all-hit (131k member, 558k
mixed5, 520k hot5). The member arm always executes (the `ON` clause
zeroes its contribution once the direct arms saturate; it does not skip
the fetch): 101-row stop, ~22 ms warm — that bounded fetch is the fix.

Reported masses vs diagnostic true counts (exact per-arm `COUNT(*)`,
300s labeled census, same table snapshot per statement):

| id | true cve/adv/member | routing mass | truth-table row | match |
|---|---|---|---|---|
| GHSA-7r86-cg39-jmmj | 0 / 157,543 / 0 | 78,436 | direct capped > gate → ranged | yes |
| CVE-2026-42043 | 48,809 / 48,804 / 48,804 | 78,436 | direct sum 97,613 > gate → ranged | yes |
| CVE-2026-73569 | 43,981 / 43,758 / 43,758 | 78,436 | direct sum 87,739 > gate → ranged | yes |
| GHSA-4528-x5fx-h6hg | 0 / 42,738 / 0 | 42,738 | member 0, sum <= gate → shipped, exact | yes |
| CVE-2026-112943 | 40,302 / 40,302 / 40,302 | 78,436 | direct sum 80,604 > gate → ranged | yes |
| hot5 advisory (each) | 0 / ~157,543 / 0 | 78,436 | direct capped > gate → ranged | yes |

GHSA-4528 first reported 42,563 on an earlier snapshot, then 42,738 on
the census snapshot: live-primary drift (+175 rows between snapshots),
re-run matched the diagnostic exactly — per-snapshot exactness holds;
saturated reports are drift-immune. The hot5 batch matches the as-built
regression expectation (~395–480 ms pass shape; measured 407–530 ms):
its member arm was already skipped by contribution, and the direct-arm
text is unchanged.

`force_generic_plan` diagnostic (delta D-1, one run each): member
396 ms, completes, zero timeouts (member arm 143 ms under generic);
hot5 4,950 ms, completes, zero timeouts (generic picks per-id index
scans with whole-table estimates, adv arm ~971 ms x5). No planner knob
is needed or authorized: production one-shot statements take custom
plans — the same posture the shipped probe survives in — and the changed
(member) arm is fast under both plan modes. The generic exposure is
reported, not gated; the pass/fail bars above are the production-shape
(custom) statements.

## No-Regression Evidence: focused proof at the lane tip

Code measured at the pre-note tip (the lane tip adds only this note):

- `go/internal/reducer/supplychain/core`: ok (full package, incl. the
  routing-mass boundary table, the long-id fallback pin, and the
  `mass_probe`-is-1 P3-3 pin).
- `go/internal/reducer/supplychain/core -race` over the hot/key-range/
  split/mass/reroute/fallback/duration surface: ok.
- `go/internal/storage/postgres -run 'TestHolderRoutingMass|
  TestHolderMassProbe'`: ok (member-bound shape, single-source, and
  probe byte-identity pins; live tests skip, no DB here).
- `go/internal/telemetry`: ok.
- `go build ./...`, `go vet` on the three touched packages, `gofmt`:
  clean. `git diff --check`: clean.
- `TestHotSingletonRoutingMassGateBoundary` covers 5,000 / 42,388 /
  78,435 (shipped) and 78,436 (ranged: one row over the gate, with the
  member-heavy and hottest-advisory classes saturating to the same
  producible report), plus the unknown (zero) routing-mass below-gate pins.
- `TestMultiHotChunkCountsRoutingMassOnce` pins the one-batched-count-
  per-chunk shape: a single routing-mass call with exactly the two-id hot
  set, both ids ranged, 32 key-range + 1 shipped statements.

## Observability Evidence: existing signals, no new surface

No-Observability-Change: no new metric, kind, span, or log key. The routed
read is confirmed through existing signals:
`eshu_dp_supply_chain_impact_fanout_read_duration_seconds` by the existing
`mass_probe` (probe + routing-mass count), `holders_key_range_read`
(whole read), and `holders_key_range` (per-statement) kinds; the
`holder_key_range_statement` span event plus WARN log for slow/failed/
fallback statements; and the unchanged `fanout_holder_statements`,
`fanout_hot_ids`, `fanout_max_id_mass_rows` (now the resolved routing
mass), `fanout_key_ranged_ids`, `fanout_holder_key_range_statements`
sub-signals.
