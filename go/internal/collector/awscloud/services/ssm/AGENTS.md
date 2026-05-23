# AGENTS.md - internal/collector/awscloud/services/ssm guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned SSM Parameter Store domain types.
3. `scanner.go` - parameter resource emission.
4. `relationships.go` - direct KMS relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SSM API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read parameter values, history values, raw descriptions, raw allowed
  patterns, raw policy JSON, decrypted content, or mutate SSM resources.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, environment, or deployable-unit truth from parameter names, paths,
  tags, or account aliases.
- Preserve stable SSM parameter identities across repeated observations in the
  same AWS generation.
- Keep parameter names, paths, ARNs, tags, KMS key IDs, and raw AWS error
  payloads out of metric labels.

## Common Changes

- Add a new SSM metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when SSM directly reports both sides and
  the target identity is not secret material.
- Extend SDK pagination and optional-not-found handling in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not add value reads, history reads, raw policy persistence, decrypted
  content, mutations, or graph writes.
- Do not resolve parameter paths or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
