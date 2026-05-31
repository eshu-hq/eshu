# AGENTS.md - internal/collector/awscloud/services/auditmanager guidance

## Read First

1. `README.md` - scanner purpose, resource/edge table, and invariants.
2. `scanner.go` - service_kind canonicalization and fact selection.
3. `relationships.go` - the framework, S3, KMS, and account edge builders.
4. `helpers.go` - resource_id derivation and partition-aware ARN synthesis.
5. `awssdk/README.md` - SDK adapter contract and the read-surface gate.
6. `../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Metadata-only. Never read or persist collected audit evidence, evidence finder
  records, change logs, delegation comments, control narratives (testing
  information, action-plan instructions, control-mapping source bodies), or
  assessment report URLs. The `awssdk` adapter excludes every such read by
  construction; do not loosen it.
- Canonicalize `service_kind` with the merged `switch strings.TrimSpace(...)`
  case that writes `awscloud.ServiceAuditManager` back. The AST guard in
  `awscloud/servicekind_guard_test.go` fails the build otherwise.
- Every relationship sets a declared `awscloud.ResourceType*` (or allowlisted)
  `target_type` and a `target_resource_id` that matches how the target scanner
  publishes its `resource_id`. Verify against the target scanner before keying a
  new edge; skip an edge rather than dangle it.
- Synthesize ARNs only through the boundary partition
  (`awscloud.PartitionForBoundary`); never hardcode `arn:aws:`.
- Keep every Go file under 500 lines.

## Common Changes

- Add a new Audit Manager metadata field by extending the scanner-owned type and
  the `awssdk` mapper, writing a scanner or adapter test first. Persist only
  metadata; never an evidence, narrative, or report-URL field.
- Add a new edge only after confirming the target scanner's published
  `resource_id`. Add a relationship constant in `constants_auditmanager.go` with
  a real doc comment.

## What Not To Change Without An ADR

- Do not read evidence, evidence folders, change logs, delegations, insights, or
  control narratives, and do not call `GetControl`, `GetEvidence*`,
  `GetChangeLogs`, `GetAssessmentReportUrl`, or any mutation API.
- Do not infer workload, environment, deployment, or ownership truth from Audit
  Manager names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
