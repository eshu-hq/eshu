# replay/costcounting

Deterministic **cost-counting** assertions for the replay framework (epic
#4102, issue #4125, R-16). It asserts the *operation counts* a replayed
scenario produces — not wall-clock — against a committed per-scenario budget,
so an algorithmic regression (N+1 writes, quadratic fan-out) fails the gate on
every PR, credential-free.

## Scenarios (C-14, issue #4367)

One scenario per distinct `reducer_domain`
(`specs/fact-kind-registry.v1.yaml`), each driving that domain's production
graph writer:

| Domain (`projection:<domain>`) | Test file | Writer driven | Primary instrument |
|---|---|---|---|
| `code_graph_projection` | `cost_counting_test.go` | `storage/cypher.CanonicalNodeWriter` | `eshu_dp_canonical_atomic_writes_total` |
| `semantic_entity_materialization` | `semantic_entity_cost_test.go` | `storage/cypher.SemanticEntityWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `documentation_materialization` | `documentation_edges_cost_test.go` | `storage/cypher.EdgeWriter` | `eshu_dp_shared_edge_write_groups_total` |
| `aws_cloud_runtime_drift` | `aws_cloud_runtime_drift_cost_test.go` | `reducer.PostgresAWSCloudRuntimeDriftWriter` | `eshu_dp_postgres_query_duration_seconds` (observation count) |
| `ec2_instance_node_materialization` | `ec2_instance_node_cost_test.go` | `storage/cypher.EC2InstanceNodeWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `rds_posture_materialization` | `rds_posture_cost_test.go` | `storage/cypher.RDSPostureNodeWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `s3_external_principal_grant_materialization` | `s3_external_principal_grant_cost_test.go` | `storage/cypher.S3ExternalPrincipalGrantWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `s3_internet_exposure_materialization` | `s3_internet_exposure_cost_test.go` | `storage/cypher.S3InternetExposureNodeWriter` | `eshu_dp_neo4j_batches_executed_total` |
| `secrets_iam_trust_chain` | `secrets_iam_graph_cost_test.go` | `storage/cypher.SecretsIAMGraphWriter` (writer-level only, ADR #1314 governance-gated) | `eshu_dp_neo4j_batches_executed_total` |

`code_graph_projection` reuses the existing nested-directory-tree scenario: the
"code" family's `file`/`repository` kinds project through
`CanonicalNodeWriter`, and the repository/directory canonical writes that test
already drives ARE the code-graph canonical projection path, so no second
scenario is needed to honestly claim the domain.

## How it works

Each scenario drives its domain's production writer through:

1. a real `go.opentelemetry.io/otel/sdk/metric.ManualReader` + `MeterProvider`,
2. the production `telemetry.NewInstruments(meter)` registry (so the real
   `eshu_dp_*` counters record), and
3. an in-memory counting executor/queryer (no graph backend, no Postgres, no
   Docker) — wrapped in the production `storage/cypher.InstrumentedExecutor`
   (semantic entity and the five node/edge writers: EC2, RDS, S3 external
   principal grant, S3 internet exposure, secrets/IAM graph), wrapped in the
   production `storage/postgres.InstrumentedDB` (aws_cloud_runtime_drift's
   Postgres fact writer), or passed straight to a writer whose own
   `Instruments` field the production wiring already sets (canonical writer,
   edge writer).

After the write call, each test `Collect`s the reader and asserts its
**primary** `eshu_dp_*` instrument is within the committed budget, reading it
off the real otel reader — not a hand-counted statement slice — so it cannot
drift from what production records.

## Input data

`code_graph_projection` drives its writer over a committed cassette
materialization
(`testdata/cassettes/replayoffline/nested-directory-tree.json`). Every other
domain's writer (`SemanticEntityWriter`, `EdgeWriter`, `EC2InstanceNodeWriter`,
`RDSPostureNodeWriter`, `S3ExternalPrincipalGrantWriter`,
`S3InternetExposureNodeWriter`, `SecretsIAMGraphWriter`,
`PostgresAWSCloudRuntimeDriftWriter`) operates over flat rows or candidates,
not a `CanonicalMaterialization`, so their deterministic input is an
in-package Go literal fixture — the same convention `semantic_entity_test.go`
already uses — defined in each scenario's test file.

## Budget

Each scenario has a `.cost-budget.json` file under
`testdata/cassettes/replayoffline/` that pins the **exact deterministic
counts**:

- `nested-directory-tree.cost-budget.json`: `eshu_dp_canonical_atomic_writes_total: 4`, `statements_executed: 5`.
- `semantic-entity-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 12`.
- `documentation-materialization.cost-budget.json`: `eshu_dp_shared_edge_write_groups_total: 1`, `statements_executed: 2`.
- `aws-cloud-runtime-drift.cost-budget.json`: `eshu_dp_postgres_query_duration_seconds: 2`, `statements_executed: 2`.
- `ec2-instance-node-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 1`.
- `rds-posture-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 1`.
- `s3-external-principal-grant-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 1`.
- `s3-internet-exposure-materialization.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 1`.
- `secrets-iam-trust-chain.cost-budget.json`: `eshu_dp_neo4j_batches_executed_total: 1`, `statements_executed: 1`.

The fixture-backed budgets (every scenario except `code_graph_projection`)
carry a `cassette` field explaining there is no cassette, and their
`refresh_path` is a hand edit of the fixture rows and budget file together in
the same reviewed diff, since no credentialed cassette refresh applies.
Because every count is deterministic, an increase trips the gate and must be
refreshed deliberately, keeping the diff reviewable.

## Teeth

Every scenario has an `_N1_ExceedsBudget` mandatory negative control: it
drives the **same** production writer once per input unit (directory / fixture
row) instead of once for the whole batch — the N+1 anti-pattern — and asserts
the accumulated instrument value **exceeds** the budget. If the budget were too
loose, this test fails. False-green guards also fail the positive tests if any
instrument reads 0.

`aws_cloud_runtime_drift` is the one exception: `PostgresAWSCloudRuntimeDriftWriter`
has no UNWIND-style batching to break (one `ExecContext` call per candidate,
unconditionally), so calling it once per candidate produces the identical
observation count as calling it once with all candidates. Its N+1 control is
shaped instead as duplicate invocation — calling the writer twice with the
SAME candidate set, simulating a retry-without-idempotency-check or
duplicate-admission regression — which genuinely doubles the observation
count. See `aws_cloud_runtime_drift_cost_test.go`'s doc comments for the full
reasoning.

## Relation to Epic B

Complements the B-8/B-9 wall-clock benches: counts here, nanoseconds there. Over
the same deterministic cassette corpus where a cassette exists.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/costcounting/ -count=1 -v
```
