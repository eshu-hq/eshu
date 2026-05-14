# AGENTS.md - internal/collector/awscloud/services/cloudwatchlogs guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CloudWatch Logs domain types.
3. `scanner.go` - log group resource emission.
4. `relationships.go` - direct CloudWatch Logs relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep CloudWatch Logs API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read log events, log stream payloads, Insights query results, export
  payloads, resource policies, subscription payloads, or mutate CloudWatch Logs
  resources.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from log group names, tags, or account
  aliases.
- Preserve stable CloudWatch Logs log group identities across repeated
  observations in the same AWS generation.
- Keep log group names, ARNs, tags, KMS key IDs, and raw AWS error payloads out
  of metric labels.

## Common Changes

- Add a new CloudWatch Logs metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when CloudWatch Logs directly reports both
  sides and the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not add CloudWatch Logs data-plane calls, log stream payload reads,
  Insights query calls, export payload reads, resource-policy persistence,
  subscription payload reads, mutations, or graph writes.
- Do not resolve log group names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
