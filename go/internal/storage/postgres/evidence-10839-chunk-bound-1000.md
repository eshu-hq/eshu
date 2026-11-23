# Keyed winners chunk bound 1000 (#10839)

`defaultWinnersChunkMaxIdents` 200 -> 1000: the arbiter-ruled capacity value
(2026-11-23) for the Decision-6 expiry shortfall. One default constant only;
no SQL, lock, worker, batch, window, cadence, or clamp change (#11157 stays
13,107; 1000 x 5 = 5000 params, 13x under it).

## Theory (proven before the edit)

Shim re-run on the pre-change tree (disposable PG18, this lane):
`TestSupplyChainImpactWinnersResweepKeyedConvertSlowWindowPlanLive` +
`TestSupplyChainImpactWinnersResweepKeyedScopeExpansionPlanLive` PASS —
convert 10.835 ms / delete 39.178 ms ANALYZE at the 41,400-row slow window,
zero `loops=41400`, max local buffers 867 (< 5M bound), `chunk_applied`
MATERIALIZED present. Per-tick settle cost is linear in chunk/kept rows; the
remaining ceiling is the by-design 200-identifier bound this change raises.

## Performance Evidence (gated Decision-5 replay, Bar-2'):

Identical 5000-row window (70 scope + 4930 fact pins), byte-identical TEMPLATE
clones, knob override (not the constant): 200 control 235 ticks / 29.623s /
max 638ms vs 1000 candidate 47 ticks / 12.869s / max 554ms; concurrency arm
47 ticks / 15.067s under 133,110 re-marks, 0 errors, 0 deadlocks, identical
hashes, parity clean, watermarks advanced. Gated legs: settled-identifiers/s
3646.7 vs 1584.3 = 2.30x; dirty-rows-deleted/s 388.5 vs 168.8 = 2.30x.
Diagnostics only: version-sums/s 0.86x (internal rework the 1000-arm avoids),
kept/s 2.30x, keys/s 2.30x.

Headroom projection (QA-scale wall time NOT_CHECKED, confirmed by the Bounded
segment + A'''): at the binding 1,420/min inflow leg the control gross was
~1,770/min; 2.30x projects ~4,070/min gross, ~2,650/min headroom over inflow,
draining a 1.25M-class backlog in under 8 hours at sustained inflow.

## No-Regression Evidence (accuracy invariants untouched):

Sound S/H horizon, oldest-first chunk order, exact-applied delete, and
scope-remainder conversion ordering are unchanged: the diff touches no SQL
and no settle predicate. Contract proof: `TestWinnersChunkDefaultBoundIsCapacityRuled`
(failing-first: RED `{identifiers:200}` pre-change, GREEN post-change) pins
the 1000 default, the 5000-param ceiling headroom, and a saturated
default-bound tick (1000 admitted, truncated, one INSERT, oldest-first);
the #10839 batch tests and the #11157 clamp tests still pass unmodified.

## Observability Evidence (no new telemetry):

No new spans/metrics: the existing `BuildActiveFacts` tick-phase histogram and
per-tick statement count expose the change (fewer, larger ticks). Live bars
ride with the rollout: p95 <= 40s, zero ticks >= 60s, else single-knob
rollback to 200.
