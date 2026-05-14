# AGENTS.md - cmd/collector-aws-cloud guidance

## Read First

1. `README.md` - command purpose, configuration, and invariants.
2. `config.go` - collector instance selection and target-scope parsing.
3. `service.go` - claim-aware runner and runtime wiring.
4. `status_committer.go` - commit-side AWS scan status updates after fenced
   fact persistence.
5. `go/internal/collector/awscloud/awsruntime/README.md` - claim runtime
   contract.
6. Service `awssdk` README files under
   `go/internal/collector/awscloud/services/` - SDK adapter contracts.
7. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - security and
   runtime requirements.

## Invariants

- Do not accept static AWS credential fields.
- Require `ESHU_AWS_REDACTION_KEY` when ECS or Lambda is enabled so environment
  values cannot cross persistence boundaries in plaintext.
- Keep this command process-only. AWS credentials belong in `awsruntime`; AWS
  service pagination belongs in service `awssdk` adapters.
- Keep ELBv2 target health out of stable AWS collector facts; target health is
  live status, not routing topology.
- Keep Route 53 DNS names, hosted-zone IDs, and record values out of metric
  labels.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  evidence is metadata only.
- Keep Lambda function code and presigned package download URLs out of facts.
  Lambda image URIs, aliases, event-source mappings, execution roles, subnets,
  and security groups are reported join evidence only.
- Keep SQS message bodies and queue policy JSON out of facts. The command may
  enable `sqs`, but the SDK adapter owns the safe metadata allowlist.
- Keep SNS message payloads, topic policy JSON, delivery-policy JSON,
  data-protection-policy JSON, and raw non-ARN endpoints out of facts. The
  command may enable `sns`, but the SDK adapter owns safe topic and subscription
  mapping.
- Keep EventBridge event payloads, event bus policy JSON, target input payloads,
  input transformers, HTTP target parameters, and raw non-ARN targets out of
  facts. The command may enable `eventbridge`, but the SDK adapter owns safe bus,
  rule, and target mapping.
- Keep S3 object inventory, object keys, bucket policy JSON, ACL grants,
  replication rules, lifecycle rules, notification configuration, inventory
  configuration, analytics configuration, and metrics configuration out of
  facts. The command may enable `s3`, but the SDK adapter owns safe bucket
  metadata mapping.
- Keep DynamoDB item values, table scans, table queries, stream records,
  backup/export payloads, resource policies, PartiQL output, and mutations out
  of facts. The command may enable `dynamodb`, but the SDK adapter owns safe
  table metadata mapping.
- Do not log credential values, trust policy JSON, resource ARNs, tags, or raw
  source payloads as metric labels.
- Preserve the split between scanner-side status in `awsruntime` and
  commit-side status in `status_committer.go`.

## Common Changes

- Add a new AWS service by extending target validation, adding scanner package
  tests, adding a service `awssdk` adapter, package docs, and branching in
  `awsruntime.DefaultScannerFactory.Scanner`.
- Run `scripts/verify-package-docs.sh` whenever the change adds or edits a Go
  package under this command or `go/internal/collector/awscloud`.
- Run `scripts/verify-performance-evidence.sh` whenever the change touches
  claim concurrency, leases, worker fanout, batching, pagination pressure, or
  downstream graph/materialization cost. The PR must include tracked
  Performance Evidence and Observability Evidence markers.
- Add new command configuration with config tests first.
- Add SDK pagination in the service adapter so spans and AWS API counters stay
  complete.

## What Not To Change Without An ADR

- Do not bypass workflow claims or claim-aware commits.
- Do not cache cross-account credentials beyond the claim lease.
- Do not make this command write graph truth or reducer-owned rows directly.
