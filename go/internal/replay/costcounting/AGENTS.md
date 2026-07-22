# replay/costcounting — agent scope

## Owned surface

- `go/internal/replay/costcounting/` — the R-16 deterministic cost-counting gate.
- `testdata/cassettes/replayoffline/*.cost-budget.json` — the per-scenario
  operation-count budgets.

## Scenarios (C-14, issue #4367)

One scenario per distinct `reducer_domain` (`specs/fact-kind-registry.v1.yaml`):
`code_graph_projection` (`cost_counting_test.go`, drives
`cypher.CanonicalNodeWriter`), `semantic_entity_materialization`
(`semantic_entity_cost_test.go`, drives
`cypher.SemanticEntityWriter.WriteSemanticEntities` through
`cypher.InstrumentedExecutor`), `documentation_materialization`
(`documentation_edges_cost_test.go`, drives `cypher.EdgeWriter.WriteEdges` with
`EdgeWriter.Instruments` set), `aws_cloud_runtime_drift`
(`aws_cloud_runtime_drift_cost_test.go`, drives
`reducer.PostgresAWSCloudRuntimeDriftWriter` through `postgres.InstrumentedDB`
— a Postgres fact writer, not a Cypher writer, so it asserts
`eshu_dp_postgres_query_duration_seconds` observation count instead of a neo4j
batch counter), `ec2_instance_node_materialization`
(`ec2_instance_node_cost_test.go`, drives
`cypher.EC2InstanceNodeWriter.WriteEC2InstanceNodes`),
`rds_posture_materialization` (`rds_posture_cost_test.go`, drives
`cypher.RDSPostureNodeWriter.WriteRDSPostureNodes`),
`s3_external_principal_grant_materialization`
(`s3_external_principal_grant_cost_test.go`, drives
`cypher.S3ExternalPrincipalGrantWriter.WriteS3ExternalPrincipalGrants`),
`s3_internet_exposure_materialization`
(`s3_internet_exposure_cost_test.go`, drives
`cypher.S3InternetExposureNodeWriter.WriteS3InternetExposureNodes`), and
`secrets_iam_trust_chain` (`secrets_iam_graph_cost_test.go`, drives
`cypher.SecretsIAMGraphWriter.WriteServiceAccountNodes` at the writer level
only — ADR #1314 governance-gated, `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED`
is never touched). The five node/edge writers listed above all route through
`cypher.InstrumentedExecutor`, the same wrapper
`go/cmd/reducer/observed_service_wiring.go` applies to the real Neo4j/NornicDB
executor. Shared test helpers (the group-counting executor, the
path-parameterized budget loader) live in `cost_scenario_helpers_test.go`. See
README.md for the full instrument/budget table before adding another
scenario.

`codeowners_ownership` (`codeowners_ownership_cost_test.go`, issue #5419 Phase
6) closes the replay-coverage gap left open through Phase 5: it mirrors
`documentation_materialization` exactly (same `cypher.EdgeWriter.WriteEdges`
path, same `eshu_dp_shared_edge_write_groups_total` primary instrument), with
one difference — both fixture rows route to the SAME Cypher template
(`batchCanonicalCodeownersOwnershipEdgeCypher`, no per-kind branch), so they
batch into one statement and `statements_executed=1` rather than
documentation's 2.

The C-14 closeout added 15 more scenarios (5 graph writers —
`azure_resource_materialization`, `gcp_resource_materialization`,
`kubernetes_correlation`, `observability_coverage_correlation`,
`incident_routing_materialization`; 10 Postgres writers —
`container_image_identity`, `ci_cd_run_correlation`,
`sbom_attestation_attachment`, `cloud_asset_resolution`,
`service_catalog_correlation`, `security_alert_reconciliation`,
`supply_chain_impact`, `incident_repository_correlation`,
`package_source_correlation`, `reducer_derived_findings` via
`multi_cloud_runtime_drift`), taking the C-7 dashboard to 100%. The Postgres
writers read `eshu_dp_postgres_query_duration_seconds` observation count via
`postgres.InstrumentedDB` over a counting fake `ExecQueryer`; helpers for that
path (`collectAttributedHistogramCount`, `countingExecQueryer`,
`newInstrumentedReducerDB`) live in `postgres_cost_helpers_test.go`. Per-row
Postgres writers commit an exact-equality budget encoding known per-row write
amplification pending a batched-insert migration tracked as a follow-on issue.
`projection:config_state_drift` is exempted (counter-only terraform domain, no
reducer write to bound) in `specs/replay-depth-requirements.v1.yaml`.

## Non-negotiable invariants

- The PRIMARY assertion MUST read a real `eshu_dp_*` instrument off the
  `sdkmetric.ManualReader` (via the production `telemetry.NewInstruments`
  registry), NOT a hand-counted statement slice. A re-implemented counter is a
  false green — the whole point is to assert what production actually records.
- The gate MUST drive a production writer for the domain's real projection
  hook (`cypher.CanonicalNodeWriter`, `cypher.SemanticEntityWriter`,
  `cypher.EdgeWriter`, or the real intent/query path for other counters),
  never a re-implementation of the projection.
- Keep the N+1 negative control and prove it EXCEEDS the budget. If a refactor
  makes it fit within budget, the budget is too loose — tighten it, do not
  delete the control. When the writer batches same-key rows together (as
  `SemanticEntityWriter` does per entity label), the N+1 fixture MUST share a
  batching key across its rows — distinct keys already emit one statement each
  regardless of call count and would make the negative control a no-op. When
  the writer has NO batching at all (an unbatched per-row writer such as
  `PostgresAWSCloudRuntimeDriftWriter`, one statement per row regardless of
  call grouping), per-row calls are the no-op instead — shape the control as
  DUPLICATE invocation (call the writer twice with the SAME input set) so it
  models the real regression class (retry without idempotency check,
  duplicate admission) and genuinely exceeds the budget.
- Budgets are the EXACT deterministic counts. Because the scenario is
  deterministic, do not pad the budget "to absorb evolution" — a legitimate
  count change must refresh the budget deliberately (R-6 path for
  cassette-backed scenarios; a reviewed hand edit of the fixture rows and
  budget file together for the in-package-fixture scenarios) so the diff is
  reviewed, which is the gate's value.
- Keep the false-green guards: a 0 instrument value MUST fail (the instrument
  isn't recording).
- Stay credential-free: no Postgres, no graph backend, no Docker.

## Skill routing

- `eshu-diagnostic-rigor` for the instrument/throughput reasoning.
- `golang-engineering` for Go edits and tests.
- `telemetry-coverage-discipline` if you add a new `eshu_dp_*` instrument to assert.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/costcounting/ -count=1
```
