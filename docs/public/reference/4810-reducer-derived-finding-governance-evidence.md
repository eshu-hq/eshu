# 4810 Reducer-Derived Finding Governance Evidence

Issue #4810 moves three reducer-owned finding kinds onto governed
`sdk/go/factschema` payloads and stamps their persisted fact rows with
schema version `1.0.0`:

- `reducer_supply_chain_impact_finding`
- `reducer_aws_cloud_runtime_drift_finding`
- `reducer_multi_cloud_runtime_drift_finding`

The change does not add a queue, worker, graph write, retry loop, batch knob, or
new read path. It changes the in-memory payload construction for the existing
Postgres writers from ad hoc maps to typed factschema encoders, then writes the
same logical fact rows through the existing canonical fact upsert path with the
additional `schema_version` column populated.

No-Regression Evidence: final local proof ran on the 20-repository B-7 golden
corpus with Postgres `postgres:18-alpine` and NornicDB
`timothyswt/nornicdb-cpu-bge:v1.1.9@sha256:9a5126d306a48c01869809da47a869a4521b9328a7ab1c855327f5fd7541e4cd`.
Baseline gate ceilings are the checked-in B-7 timing contract: pipeline wall
time 30m0s, phase bootstrap 10s, collect 25s, first drain 86.2s, graph query 8s,
and maintenance drains 10s. After the change,
`bash scripts/verify-golden-corpus-gate.sh` passed with 415 pass, 0
required-fail, 0 advisory-warn, elapsed 1m32s. Observed phase timings were
bootstrap=2s, collect=20s, first_drain=64s, maintenance_drains=6s, and
graph_query=3s. Terminal drain checks reported `fact_work_items_residual=0`,
`shared_projection_intents_nonterminal=0`, and required reducer domains present.
Focused writer tests also assert that each governed writer uses an insert query
with `schema_version`, passes `1.0.0`, writes one fact per finding, and preserves
the existing payload signals.

No-Observability-Change: this change reuses the existing reducer writer error
returns, fact-record persistence, B-7 drain/status checks, replay-coverage
reporting, and fact-schema-version readbacks. It adds no metric instrument,
metric label, span name, structured log key, HTTP route, MCP tool, runtime knob,
worker, lease, queue table, or graph backend operation. Operators continue to
diagnose this path through the existing reducer write failures, fact work-item
terminal status, shared projection intent terminal status, golden-corpus phase
timings, and `list_fact_schema_versions` / `get_fact_schema_version` query
surfaces.
