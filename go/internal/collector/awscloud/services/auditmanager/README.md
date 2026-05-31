# AWS Audit Manager Scanner

## Purpose

`internal/collector/awscloud/services/auditmanager` reads AWS Audit Manager
assessment, framework, and control control-plane metadata for one claimed
account and region and emits reported `aws_resource` and `aws_relationship`
facts. It maps the compliance-program graph: which assessments exist, the
framework each was created from, the S3 bucket and KMS key that hold their
evidence and reports, and the AWS accounts in each assessment's scope.

## Ownership boundary

This package owns Audit Manager fact selection: which assessment, framework, and
control metadata becomes a resource, and which dependencies become edges. It
does not own SDK calls (see `awssdk/`), credential acquisition, graph writes,
reducer admission, or query behavior.

It is metadata-only. It never reads or persists collected audit evidence,
evidence finder records, change logs, delegation comments, control narratives
(testing information, action-plan instructions, control-mapping source bodies),
or assessment report URLs, and never mutates Audit Manager state.

## Exported surface

See `doc.go` for the godoc contract.

- `Scanner` - emits Audit Manager metadata facts for one claimed boundary.
- `Scanner.Scan` - observes assessments, frameworks, controls, and their direct
  framework, S3, KMS, and in-scope-account dependencies.
- `Client` / `Snapshot` / `Assessment` / `Framework` / `Control` - the
  metadata-only result contract the `awssdk` adapter implements.

## Resources and relationships

| Resource | Type | Key fields |
| --- | --- | --- |
| Assessment | `aws_auditmanager_assessment` | ARN, id, compliance standard, status, framework reference, reports-destination type, in-scope account ids/service names, timestamps |
| Framework | `aws_auditmanager_framework` | ARN, id, compliance standard, framework type (Standard/Custom), control-set and control counts, timestamps |
| Control | `aws_auditmanager_control` | ARN, id, control type (Standard/Custom/Core), evidence data-source category names, timestamps |

| Edge | Target type | Target key |
| --- | --- | --- |
| `auditmanager_assessment_uses_framework` | `aws_auditmanager_framework` | framework ARN (the framework node's resource_id) |
| `auditmanager_assessment_reports_to_s3` | `aws_s3_bucket` | synthesized partition-aware `arn:<partition>:s3:::<bucket>` from the `s3://` reports destination |
| `auditmanager_assessment_encrypted_with_kms_key` | `aws_kms_key` | account settings KMS key ARN (`target_arn` set only when ARN-shaped) |
| `auditmanager_assessment_in_account` | `aws_account` | partition-aware `arn:<partition>:iam::<account-id>:root` per in-scope account |

In-scope service names are deprecated by AWS (the API returns them empty), so
they are recorded as an assessment attribute, never an edge. There is no
per-assessment KMS key in the Audit Manager API; the encryption key is the single
account-level customer managed key from `GetSettings`, attached to each
assessment's KMS edge because Audit Manager encrypts all assessment evidence and
reports with it.

## Dependencies

- `internal/collector/awscloud` for the boundary, observation, envelope, and
  partition helpers (`PartitionForBoundary`).
- `internal/facts` for the durable fact envelope contract.
- `internal/collector/awscloud/internal/relguard` (test only) for the graph-join
  guard assertion.

## Telemetry

The scanner emits no telemetry directly. The `awssdk` adapter wraps every API
call in the shared AWS pagination span and the AWS API-call/throttle counters:

- `aws.service.pagination.page`
- `eshu_dp_aws_api_calls_total`
- `eshu_dp_aws_throttle_total`

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/auditmanager/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Gotchas / invariants

- An account that has not enabled Audit Manager (account status not `ACTIVE`)
  yields an empty result with an `auditmanager_not_registered` warning, not a
  scan failure.
- Every synthesized ARN derives its partition from the scan boundary
  (`PartitionForBoundary`); `arn:aws:` is never hardcoded, so GovCloud and China
  edges join real nodes.
- The KMS edge keys the reported key value; `target_arn` is set only when the
  value is ARN-shaped (a bare alias keeps the value but emits no `target_arn`).
- Relationships are omitted, never dangled, when an endpoint identity is missing.

## Related docs

- `docs/public/services/collector-aws-cloud-scanners.md`
- `docs/public/services/collector-aws-cloud-security.md`
