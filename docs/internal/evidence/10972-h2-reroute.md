# #10972 H2 lane 1 (A2 re-route): implementation proof

Date: 2026-11-26. Lane: `drive/10972-h2-reroute` (executor leaf).
Binding ruling: [arbiter-10972-h2shape.md](../../.claude/arbiter-10972-h2shape.md)
Decision 3 A2-branch. Theory shim:
[10972-h2-holders-shim.md](10972-h2-holders-shim.md) (lane
`drive/10972-h2-shim`, QA primary `eshu-postgresql-0`, PG 18.x, ~13M-row
supports table, 4 GB shared buffers, 2026-11-26).

## What changed

`go/internal/reducer/supplychain/core/fanout_split.go` only, plus its seam,
tests, and comments: a probe-hot singleton resolves its true mass through one
batched exact count (`HolderTrueMassEstimator`, implemented by
`SupplyChainImpactFanOutStore.EstimateHolderTrueMasses`) and reads as 16
fact_id key-range statements (`FindHolderScopesByKeyRange` under its pinned
`force_custom_plan`) above `HolderKeyRangeMassGate` (78,435 rows), or as the
shipped single holder statement at or under it. Ports without either seam keep
the shipped statement. Cold packing, parallelism (4), the 10s guard, and the
member-token fallback branch are untouched; no index DDL.

## Gate value

Largest A1-passing shim singleton mass 156,870 rows (advisory ids, warm ~1.3s
isolated) halved for margin = 78,435. Every shim failing set lands above it
(member 96,437; mixes 87,453 / 80,144) and every A1-passing set at or under
it (42,388 direct-only; 5,000 mid-band), so the gate routes exactly the
failing dimension.

## Performance Evidence: shim baseline (QA primary, 2026-11-26)

Baseline-bound from the theory shim (same substrate as above, QA primary,
warm repeats under the production 10s guard; this lane re-runs no primary
timings): A1 singletons p95 3.45s at 4-parallel (over the 2s bar) with
member-carrying singletons timing out outright; A2 union-of-16 per-statement
p95 323ms at 4-parallel (max 581ms, zero timeouts over 320 statements) and
warm max 362ms isolated, byte-equal holder sets everywhere checked. The
re-route moves exactly the above-gate singletons onto the A2 shape the shim
proved; at-or-under-gate singletons keep the statement the shim proved at
~1.3s isolated. No new statement shape, knob, or index is introduced.

## No-Regression Evidence: focused proof at the lane tip

Code measured at `bba42c3f` (`go test -count=1`; the lane tip adds only this
note, and the suites below are re-run there):

- `go/internal/reducer/supplychain/core`: ok (full package, incl. the 4 new
  A2 pins and the updated `mass_probe/ok: 2` below-gate pin).
- `go/internal/reducer/supplychain/core -race` over the hot/key-range/split
  surface: ok.
- `go/internal/storage/postgres -run 'TestHolderTrueMass|TestHolderMassProbe'`:
  ok (exact-query shape and single-source pins; live tests skip, no DB here).
- `go/internal/telemetry`: ok.
- `go build ./...`, `go vet` on the three touched packages, `gofmt`: clean.
- `TestHotSingletonTrueMassGateBoundary` covers 5,000 / 42,388 / 78,435
  (shipped) and 78,436 / 80,144 / 156,870 (ranged).

## Observability Evidence: existing signals, no new surface

No-Observability-Change: no new metric, kind, span, or log key. The routed
read is confirmed through existing signals:
`eshu_dp_supply_chain_impact_fanout_read_duration_seconds` by the existing
`mass_probe` (probe + exact count), `holders_key_range_read` (whole read),
and `holders_key_range` (per-statement) kinds; the
`holder_key_range_statement` span event plus WARN log for slow/failed/
fallback statements; and the unchanged `fanout_holder_statements`,
`fanout_hot_ids`, `fanout_max_id_mass_rows` (now the resolved true mass),
`fanout_key_ranged_ids`, `fanout_holder_key_range_statements` sub-signals.
