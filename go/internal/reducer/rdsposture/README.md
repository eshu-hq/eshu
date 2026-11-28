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
