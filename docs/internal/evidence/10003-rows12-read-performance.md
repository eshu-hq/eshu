# #10003 Rows 1-2: Delta Overlay Cost for the Code-Call and JVM Readers

`LoadActiveCodeCallSymbolDefinitionFacts` and `ListActiveJVMReachabilityFacts`
joined `fact.generation_id = scope.active_generation_id`, so a delta-active
git scope lost its base full generation's unchanged rows (row 1: 1 symbol
instead of 2, retracting the still-correct `main->bar` edge; row 2: 0 rows
instead of 1, findings stuck at unknown reachability). The fix routes both
reads through the shared `deriveDeltaOverlayBaseQuery` overlay (#10064):
active pages unchanged, one delta-target lookup, derived base pages only for
reached delta scopes, resolve, overlay inputs, all on one read-only
repeatable-read snapshot.

## Performance Evidence:

Source: branch `drive/10003-delta-readers` (row-1/row-2 overlay commits).
Disposable PostgreSQL 18 (`postgres:18-alpine` container, default settings)
on a shared developer host, schema from `ApplyBootstrap`, ANALYZEd, 5 runs
per statement, literal parameters, page 1. Two fixtures: a 5,000-file base
with a 1-file delta (code-call reader) and a 2,000-file base with a 1-file
delta (JVM reader).

| Reader | Active | Base | All-full | (active+base)/all-full |
|---|---|---|---|---|
| Code-call | 2.73 ms | 16.25 ms | 15.21 ms | 1.25x |
| JVM | 1.69 ms | 14.70 ms | 15.96 ms | 1.03x |

The acceptance bar is the counterfactual `(active + base) / all-full <= 1.5x`
per binding arbiter ruling: the literal `base / active` ratio measures
data-size skew for small deltas (1-row active vs 5k-row base), not query
efficiency, and lane 3's registering comment records the counterfactual as
the fair denominator. Both readers clear it. The derived base keeps the
active text's filters, keyset cursor, ordering, and limit by construction
(`delta_overlay_query.go`); `fact_records` access is the identical shape in
all three plans per reader (JVM uses
`fact_records_jvm_reachability_repo_file_idx` with zero seq scans; the
code-call fixture seq-scans the tiny single-scope table for the unchanged
active text too, and the same pattern is index-driven at lane-3 scale).

Statement tallies (live, pinned per reader): delta-active `1+1+1+2+1`
(active, targets, base, resolve, inputs), no-delta `1+1+0+0+0` — the base
scan runs solely for reached delta scopes, and the steady state adds only
the target lookup. No reader gains a statement when no scope is delta-active.

Why safe: the overlay adds cost only on delta-active scopes (transient, one
delta per scope at a time) and costs essentially what a correct full read
costs; the incorrect old read paid less because it read less. Reopen bounds
(from the arbiter ruling): larger-fixture counterfactual above 1.5x, a new
derivation seq scan, steady state above +1, or production overlay-latency
signals.

## Observability Evidence:

`eshu_dp_code_call_symbol_definition_read_delta_union_total` and
`eshu_dp_jvm_reachability_read_delta_union_total` (read + closed outcome
set, mirroring the #9991 manifest counter) are wired under the existing
nil-instruments guard and asserted moving on the live overlay path
(`union_base_found: 1` per delta-active scope). Coverage rows in
`docs/public/observability/telemetry-coverage-reducer-domains.md`; the
telemetry-coverage verifier agrees with the base. Per-reader loaded-bytes
telemetry is follow-up (no sibling precedent, per the arbiter ruling).
