# rdsposture

## Purpose

Projects `rds_instance_posture` facts onto existing RDS `:CloudResource`
nodes as reducer-owned properties. This is issue #1233. This package moved
out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the RDS posture extraction (`ExtractRDSPostureRows`), the
additive domain definition and its handler, and the resource-uid index used
to confirm each posture's source RDS DB instance or Aurora cluster was
scanned as a CloudResource in the same scope generation.

It does **not** own the `aws_resource`/`rds_instance_posture` decoders
(`schemadecode`), the CloudResource uid derivation (`cloudjoin`), quarantine
(`factdecode`), the scoped fact read (`factload`), or readiness phases
(`gpphase`). Registration stays in the reducer root
(`defaults_additive_domains_cloud_relationships.go`), and the Cypher node
writer lives under `internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_relationships.go`)
- `RDSPostureMaterializationHandler` — the root additive-domain registry
- `RDSPostureNodeWriter` — `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.RDSPostureNodeWriter` with this type directly -- no root
  compat file
- `ExtractRDSPostureRows` — no cross-package caller today; kept exported for
  symmetry with the sibling extraction seams
  (`ec2blockkms.ExtractEC2BlockDeviceKMSPostureRows`,
  `iamescalation.ExtractIAMEscalationEdges`)
- `RDSPostureNodesNotReadyFailureClass` — `internal/storage/postgres`'s
  readiness claim gate -- a storage contract literal, not just a Go
  identifier -- called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/telemetry`,
`internal/truth`, `pkg/log`. Never `internal/reducer`.

## Telemetry

No package-specific metric instrument: this family has no
`eshu_dp_rds_posture_*` counter or gauge. Its only telemetry row is the
generic `reducer.rds_posture_materialization` span, which carries the shared
`eshu_dp_postgres_query_duration_seconds` metric like every other reducer
handler span, not an RDS-specific series. Each handler run's completion log
carries `resource_fact_count`, `posture_fact_count`, `node_update_count`,
`skipped_by_reason`, `skip_retract`, and per-stage `load` / `extract` /
`retract` / `graph_write` / `total_duration_seconds` fields — the same fields
the pre-move root file logged.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk in the moved production files is a package
clause, an import requalification, or an identifier requalification: symbols
the reducer root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`→`factdecode.*`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`,
`derefString`/`uniqueSortedStrings`→`payloadcore.*`,
`decodeAWSResource`/`decodeRDSInstancePosture`→`schemadecode.*`) are now
called on the leaf package directly. No handler branch, extraction rule,
readiness gate, retract/write ordering, or Cypher-facing row shape changed.
`go test ./internal/reducer/rdsposture/... -count=1` passes with the moved
test suite unchanged in assertion content (only import paths and unexported
symbol duplication for the package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
`reducer.rds_posture_materialization` span and the shared
`eshu_dp_postgres_query_duration_seconds` metric are unchanged, and the
completion log keeps the same key set at the new import path.

No-Regression Evidence: this PR also carries two comment-only
`// #nosec G115` annotations on pre-existing bounded modulo conversions
(`partitioning.go:26`, `repo_dependency_projection_concurrency_proof.go:180`).
Shrinking the root package lets gosec's loader complete and surface them;
both results are `%` a positive int divisor (range-checked at the call
site), so the value fits `int` and behavior is unchanged.
`go test ./internal/reducer/ -count=1` passes.

No-Observability-Change: the annotations emit nothing and change no
control flow; no metric, label, log field, queue, or runtime knob changes.
