# AGENTS.md — RDS projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildRDSPostureMaterializationReducerIntent` must keep the shared
  `aws_resource_materialization:<scope>` entity key -- do not give it a
  family-distinct key; the durable claim gate matches that prefix directly.
- The builder triggers on `rds_instance_posture` fact-kind presence alone and
  must not filter on `publicly_accessible`; private-instance posture (backup,
  encryption, deletion-protection, IAM database auth) is queryable evidence
  independent of public exposure.
- The builder anchors on the earliest matching fact so the reducer claim stays
  stable across reprojections.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run focused child and root RDS tests, ordered fan-out parity,
package-doc verification, and the projector package tree.
