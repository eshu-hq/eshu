# AGENTS.md - internal/collector/awscloud/services/dynamodb guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned DynamoDB domain types.
3. `scanner.go` - table resource emission.
4. `relationships.go` - direct DynamoDB relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DynamoDB API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read table items, query or scan tables, read stream records, fetch
  backup payloads, fetch export payloads, fetch resource policies, run PartiQL,
  or mutate DynamoDB resources.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from table names, tags, indexes, or
  account aliases.
- Preserve stable DynamoDB table identities across repeated observations in the
  same AWS generation.
- Keep table names, ARNs, tags, index names, KMS key IDs, and raw AWS error
  payloads out of metric labels.

## Common Changes

- Add a new DynamoDB metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when DynamoDB directly reports both sides
  and the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not add DynamoDB data-plane calls, stream record reads, backup/export
  reads, resource-policy persistence, PartiQL calls, mutations, or graph writes.
- Do not resolve table names, tags, or indexes into workload ownership here;
  correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
