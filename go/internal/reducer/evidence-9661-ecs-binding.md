<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: workload-to-ECS-service binding (#9661)

Reducer materialization sibling to `WorkloadCloudRelationshipMaterializationHandler`
that binds workloads to live ECS services from resolved Terraform/Terragrunt
declarations (arbiter verdict mechanisms 1b scaffold + 1c bounded resolution).
Query surfaces are untouched and keep consuming materialized identity first.

Benchmark Evidence:

`BenchmarkExtractWorkloadECSBindingMatrix` (12-service correlation matrix:
literals, variable defaults, locals, terragrunt inputs, duplicates,
shared names, for_each, anchor-owned, uncorroborated, env legs) and
`BenchmarkExtractWorkloadECSBindingScale` (40 services against a
shim-scale census: 187 declarations across 32 repos, binding 40 rows):

- Matrix: ~76-230 us/op across repeated runs on a shared host (noise
  band, not a gated claim).
- Scale: ~0.75-0.89 ms/op for 40 services x 187 declarations.
- Baseline: the pre-#9661 Handle runs only the sibling anchor
  extraction. The binding section adds zero I/O for scopes without ECS
  service facts (short-circuit before any census or lookup) and exactly
  one bounded census read plus one instance-env read per evaluation
  otherwise; extraction itself is pure CPU over already-loaded facts.
- Input shape: scope `aws_resource` facts plus the global declaration
  census (filtered resolver closure, complete-or-blocked per #11084:
  safety ceiling 50000 rows, hitting it blocks declaration binds with
  an `ecs_census_incomplete` tally). Terminal counts: 3 bound rows on
  the matrix, 40 on scale.
- Backend: `go test -bench` binary on linux/arm64 (shared host);
  numbers are corroboration with spread per the timing-proof rules.

Why safe: no per-row I/O, linear-ish scaling (single precompute over
the census, then per-service filtering), writer MERGE idempotency, and
the scope gate keeps every non-AWS scope on the legacy path.

Census index evidence:

`TestECSBindingCensusPlanIsIndexBackedLive` runs the shipped census SQL
(`EXPLAIN`, read-only tail derived from the constant) against prod-shape
indexes over a 25k-row fact table: no sequential scan on `fact_records`,
index-backed generation join. `TestECSBindingCensusReadsActive...Live`
proves active-only visibility with verbatim metadata,
`TestECSBindingCensusReadsFilteredClosureLive` proves the exact closure
set (ECS declarations corpus-wide plus same-directory variables/locals
and same-repo bridges, nothing else), and
`TestECSBindingCensusCeilingReportsIncompleteLive` proves the ceiling
reports incomplete with zero declarations. `TestECSBindingVetoThroughRealStoreLive`
proves a duplicate competitor read through the real store vetoes the
bind while a unique declaration binds. Live tests skip without
`ESHU_POSTGRES_TEST_DSN` and pass against PostgreSQL 18 locally.

#11084 complete-or-blocked census:

Root-Cause Evidence: the prefix census (5000 of 126,551 QA rows) failed
open on the uniqueness vetoes — the failing-first
`TestECSBindingHandleIncompleteCensusBlocksDeclarationBind` bound 1 row
on a truncated census pre-fix. The shipped "truncation only fails
closed" comments were false for the duplicate, shared-name-elsewhere,
for_each/count poison, and bridge-shadowing legs, which scan only
retained rows.

Performance Evidence: synthetic 126,656-row closure shim on local PG18
(scratch DB, fixture-shaped indexes): C1 = 60 ECS declarations
(32.4 ms); C2 = 864 rows (60 + 36 same-directory var/local + 768
same-repo bridges); need-driven single statement median 50.5 ms over 3
runs vs 80.7 ms for the retired full-census shape (OR-form 102.5 ms and
naive UNION 142 ms rejected; set-equality 0/0 against the OR form).
One statement means one snapshot; no Seq Scan on `fact_records`.
Extraction benches before/after on the shared host (300 iters x2):
matrix 43.1/43.0 us/op to 40.3/42.2; scale (40 services x 187
declarations) 483.3/480.9 us/op to 483.7/481.3; Handle declaration path
10.7/10.9 us/op to 9.9/10.0 — no regression at equal bind results.

Production replica re-check (2026-11-21, coordinator, read-only QA
read replica with `pg_is_in_recovery()=t`, exact shipped SQL shapes,
`default_transaction_read_only=on`, 120 s statement timeout, exit 0):
C1 = 178 ECS declarations in active generations; C2 full filtered
closure = 13,983 rows; C3 retired-shape census = 126,551 rows
(matches the #11084 recorded corpus). `EXPLAIN (ANALYZE, BUFFERS)`
median-of-3: new single-statement shape 3881.8 ms (3930.7/3881.8/3857.6)
vs retired shape 3678.9 ms (3678.9/3601.1/3966.1), ratio 1.055x.
Zero Seq Scan on `fact_records` (all accesses index-backed via
`fact_records_iac_active_inventory_idx` and
`fact_records_scope_generation_idx`; the single parallel seq scan is
the 4,790-row `scope_generations` driver). Amended Decision 2 (arbiter,
same day): single-statement accepted iff C2 ≤ 25,000 (measured 13,983)
AND median-of-3 replica Execution ≤ 1.10x old shape on identical
corpus/state (measured 1.055x) AND no Seq Scan on `fact_records`;
the 50,000 complete-or-blocked ceiling and re-escalation above it are
unchanged. Keyset paging becomes required only if C2 exceeds 25,000,
the median exceeds 1.10x, or a `fact_records` seq scan appears.

Observability Evidence: new `ecs_census_incomplete` skip tally merges
into the handler's existing `N candidate(s) skipped` total; no new
metric, span, or log key. The retired `(declaration census truncated)`
annotation is replaced by `(declaration census incomplete: binds
blocked)` on the `ECS declaration-bound edge(s)` summary suffix, so an
operator can distinguish binds-blocked from no-declarations-exist;
malformed anchor facts route through the existing
`eshu_dp_reducer_input_invalid_facts_total` quarantine;
workload-name resolution reuses
`eshu_dp_workload_handle_resolution_total{surface="reducer"}`; domain
execution stays covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. Ambiguous names emit the shared
alertable `workload handle ambiguous` log. Coverage row lives with the
workload-resolution family in
`docs/public/observability/telemetry-coverage.md` (net-zero edit).
