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
  census (cap 5000 rows, truncation reported visibly and failing
  closed). Terminal counts: 3 bound rows on the matrix, 40 on scale.
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
proves active-only visibility with verbatim metadata, and
`TestECSBindingCensusTruncationIsVisibleLive` proves the cap reports
instead of subsetting. Live tests skip without `ESHU_POSTGRES_TEST_DSN`
and pass against PostgreSQL 18.6 locally.

No-Observability-Change:

No new metric, span, or log key. The per-reason skip tally merges into
the workload-cloud handler's evidence summary (`ECS declaration-bound
edge(s)` plus the census-truncation note); malformed anchor facts route
through the existing `eshu_dp_reducer_input_invalid_facts_total`
quarantine; workload-name resolution reuses
`eshu_dp_workload_handle_resolution_total{surface="reducer"}`; domain
execution stays covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. Ambiguous names emit the shared
alertable `workload handle ambiguous` log. Coverage row lives with the
workload-resolution family in
`docs/public/observability/telemetry-coverage.md` (net-zero edit).
