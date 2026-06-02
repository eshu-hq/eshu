# RDS Posture Projection (issue #1233)

`DomainRDSPostureMaterialization` promotes metadata-only
`rds_instance_posture` facts into queryable properties on existing RDS
`CloudResource` nodes. It does not create new nodes and does not duplicate the
generic AWS relationship edge path.

## Contract

- Source facts: `aws_resource` plus `rds_instance_posture` from the same scope
  generation.
- Node family: existing `CloudResource` nodes for `aws_rds_db_instance` and
  `aws_rds_db_cluster`.
- Readiness: `cloud_resource_uid` / `canonical_nodes_committed`.
- Write shape: batched `UNWIND`, anchored
  `MATCH (r:CloudResource {uid: row.uid})`, then `SET` reducer-owned `rds_*`
  posture fields.
- Retract shape: scope/evidence-source-filtered `REMOVE` of only
  reducer-owned RDS posture fields.
- No relationship writes: KMS keys, security groups, subnet groups, IAM roles,
  parameter groups, and option groups already arrive as `aws_relationship`
  facts and remain owned by `aws_relationship_materialization`.

`publicly_accessible=true` becomes `rds_public_exposure_state =
candidate_public_endpoint`; it is not internet-reachable truth. Internet
exposure still requires the later security-group/path derivation.

## Evidence

No-Regression Evidence: `go test ./internal/reducer -run RDSPosture -count=1`
proves source-resource gating, deterministic dedupe, retryable readiness misses,
first-generation retract skip, handler write counts, and additive registry
wiring. `go test ./internal/storage/cypher -run RDSPosture -count=1` proves
uid-anchored MATCH+SET, no node fabrication, scope/evidence annotation, and
property-only retract. `go test ./internal/storage/postgres -run RDSPosture
-count=1` proves queue-level readiness blocking.

Observability Evidence: `reducer.rds_posture_materialization` wraps the handler
path. The completion log reports resource/posture counts, node-update count,
skip tally, and load/extract/retract/write durations. Cypher statements carry
`phase=rds_posture` and `label=CloudResource:RDSPosture`, so existing graph
query-duration and batch-size metrics expose slow or empty posture writes
without adding new metric labels.
