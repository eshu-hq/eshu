# AWS Cloud Runtime

## Purpose

`internal/collector/awscloud/awsruntime` adapts AWS service scanners to the
workflow-claimed collector runtime. It parses `(account_id, region,
service_kind)` claim targets, authorizes them against configured target scopes,
acquires claim-scoped credentials, records durable scanner-side status, and
returns collected generations for the shared collector commit path.

## Ownership boundary

This package owns claim parsing, target authorization, credential lease
coordination, per-account concurrency, production scanner selection, AWS
pagination checkpoint invalidation, and scanner-side scan-status updates. It
does not own AWS service response mapping, fact-envelope identity, workflow row
persistence, commit-side status updates, graph writes, reducer admission, or
query behavior.

```mermaid
flowchart LR
  A["workflow.WorkItem"] --> B["ClaimedSource.NextClaimed"]
  B --> C["Target authorization"]
  C --> D["AccountLimiter.Acquire"]
  D --> E["CredentialProvider.Acquire"]
  B --> I["CheckpointStore.ExpireStale"]
  E --> H["DefaultScannerFactory.Scanner"]
  H --> F["ServiceScanner.Scan"]
  F --> G["collector.CollectedGeneration"]
```

## Exported surface

See `doc.go` for the godoc contract.

- `Config` - collector instance and target-scope authorization configuration.
- `TargetScope` - account, allowed regions, allowed services, and credential
  routing.
- `AccountLimiter` - in-process per-account claim limiter and concurrency
  observer.
- `CredentialConfig` - non-secret credential mode, role ARN, and external ID.
- `Target` - one authorized AWS claim target.
- `CredentialProvider` - acquires a claim-scoped credential lease.
- `CredentialLease` - releases temporary credential material after a scan.
- `AWSConfigLease` - exposes claim-scoped AWS SDK configuration to service
  adapters.
- `SDKCredentialProvider` - production credential provider using workload
  identity or STS AssumeRole.
- `DefaultScannerFactory` - production service registry for AWS scanners. ECS
  and Lambda scanners receive the command-provided redaction key for
  environment values.
- `ScannerFactory` - creates a service scanner for one target and lease.
- `ServiceScanner` - scans one service claim into fact envelopes.
- `CheckpointStore` - durable pagination checkpoint store used by long service
  scans.
- `ScanStatusStore` - durable scanner-side status store for start, API count,
  throttle count, warning, and partial-run evidence.
- `ClaimedSource` - implements the collector claimed-source contract.

## Dependencies

- `internal/collector` for `CollectedGeneration` and `FactsFromSlice`.
- `internal/collector/awscloud` for claim boundaries and warning envelopes.
- `internal/collector/awscloud/checkpoint` for durable pagination checkpoint
  scope and store contracts.
- `internal/collector/awscloud/services/iam`,
  `internal/collector/awscloud/services/ecr`,
  `internal/collector/awscloud/services/ec2`,
  `internal/collector/awscloud/services/ecs`,
  `internal/collector/awscloud/services/eventbridge`,
  `internal/collector/awscloud/services/eks`,
  `internal/collector/awscloud/services/elbv2`,
  `internal/collector/awscloud/services/lambda`,
  `internal/collector/awscloud/services/route53`,
  `internal/collector/awscloud/services/sqs`,
  `internal/collector/awscloud/services/sns`, and
  `internal/collector/awscloud/services/s3` plus their `awssdk` adapters for
  production service scanners.
- `internal/facts` for warning fact types.
- `internal/scope` for AWS scope and collector identity.
- `internal/workflow` for durable work item claims.
- AWS SDK for Go v2 `config`, `sts`, and credential cache support.

## Telemetry

This package starts claim, credential, and scan spans through `ClaimedSource`.
Service `awssdk` adapters emit per-API call counters, throttle counters, and
pagination spans. The command registers the instruments:

- `eshu_dp_aws_api_calls_total`
- `eshu_dp_aws_throttle_total`
- `eshu_dp_aws_claim_concurrency`
- `eshu_dp_aws_assumerole_failed_total`
- `eshu_dp_aws_budget_exhausted_total`
- `eshu_dp_aws_pagination_checkpoint_events_total`
- `eshu_dp_aws_resources_emitted_total`
- `eshu_dp_aws_relationships_emitted_total`
- `eshu_dp_aws_tag_observations_emitted_total`
- `eshu_dp_aws_scan_duration_seconds`
- `aws.collector.claim.process`
- `aws.credentials.assume_role`
- `aws.service.scan`
- `aws.service.pagination.page`

## Gotchas / invariants

- `AcceptanceUnitID` is JSON with `account_id`, `region`, and `service_kind`.
  The runtime does not parse ARNs or free-form strings to discover claim scope.
- Credential acquisition happens after target authorization. An unauthorized
  claim never receives credentials.
- `CredentialLease.Release` runs after scanner construction and scan attempts.
  Implementations must clear temporary credential material there.
- `SDKCredentialProvider` loads AWS SDK config with adaptive retries and passes
  configured STS external IDs.
- `DefaultScannerFactory` is the only production registry for service scanners;
  add full-scan services there instead of branching in the command.
- ECS and Lambda service scans require a non-empty redaction key because
  environment values are treated as sensitive even when the variable name looks
  harmless.
- EC2 service scans collect network topology only. They do not emit EC2
  instance inventory facts.
- Target scopes default to one active claim per account when
  `max_concurrent_claims` is unset.
- STS or workload-identity failures emit an `assumerole_failed` warning fact for
  the claimed generation and record `credential_failed` scan status.
- Stale pagination checkpoints are expired at claim start for the current
  workflow generation before credentials are acquired.
- Scanner-side status records API call counts and throttle counts before fact
  commit. The command's commit wrapper records whether the fenced fact
  transaction later committed or failed.
- Route 53 alias targets are reported DNS evidence only; do not infer workload
  or deployable-unit truth in the runtime.
- Lambda aliases, event-source mappings, image URIs, execution roles, subnets,
  and security groups are reported join evidence only; do not infer workload or
  deployable-unit truth in the runtime.
- EKS clusters, OIDC providers, node groups, add-ons, IAM roles, subnets, and
  security groups are reported join evidence only; do not infer Kubernetes
  workload or deployable-unit truth in the runtime.
- SQS scanners must stay metadata-only. The runtime registry wires the SQS SDK
  adapter, but it must not broaden the service contract to message reads,
  message mutations, or queue policy persistence.
- SNS scanners must stay metadata-only. The runtime registry wires the SNS SDK
  adapter, but it must not broaden the service contract to publishing,
  subscription mutations, policy persistence, data-protection-policy
  persistence, or raw non-ARN endpoint persistence.
- EventBridge scanners must stay metadata-only. The runtime registry wires the
  EventBridge SDK adapter, but it must not broaden the service contract to
  PutEvents, rule/target mutations, event bus policy persistence, target input
  payload persistence, input-transformer persistence, HTTP-parameter
  persistence, or raw non-ARN target persistence.
- S3 scanners must stay metadata-only. The runtime registry wires the S3 SDK
  adapter, but it must not broaden the service contract to object inventory,
  bucket policy persistence, ACL grant persistence, replication persistence,
  lifecycle persistence, notification persistence, or mutation APIs.
- This package does not decide retryability for AWS service errors. The caller
  owns claim failure and retry policy through `collector.ClaimedService`.

## Related docs

- `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md`
- `docs/docs/guides/collector-authoring.md`
