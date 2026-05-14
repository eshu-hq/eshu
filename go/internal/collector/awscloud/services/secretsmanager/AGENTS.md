# AGENTS.md - internal/collector/awscloud/services/secretsmanager guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Secrets Manager domain types.
3. `scanner.go` - secret resource emission.
4. `relationships.go` - direct KMS and rotation Lambda relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Secrets Manager API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read secret values, version payloads, resource policy JSON, external
  rotation partner metadata, or mutate Secrets Manager resources.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, environment, or deployable-unit truth from secret names, tags, or
  account aliases.
- Preserve stable Secrets Manager secret identities across repeated
  observations in the same AWS generation.
- Keep secret names, ARNs, tags, KMS key IDs, Lambda ARNs, and raw AWS error
  payloads out of metric labels.

## Common Changes

- Add a new Secrets Manager metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Secrets Manager directly reports both
  sides and the target identity is not secret material.
- Extend SDK pagination and optional-not-found handling in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not add value reads, version reads, resource-policy persistence, external
  rotation partner metadata persistence, mutations, or graph writes.
- Do not resolve secret names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
