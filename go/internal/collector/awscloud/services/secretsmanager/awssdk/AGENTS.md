# AGENTS.md - internal/collector/awscloud/services/secretsmanager/awssdk guidance

## Read First

1. `README.md` - adapter boundary and telemetry.
2. `client.go` - SDK call surface and API-call recording.
3. `mapper.go` - safe response mapping into scanner-owned types.
4. `../README.md` - service package metadata-only contract.
5. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage and metadata-only data boundaries.

## Invariants

- Keep the AWS SDK contained in this adapter package.
- Use ListSecrets only for this slice. Do not add GetSecretValue,
  BatchGetSecretValue, ListSecretVersionIds, GetResourcePolicy, or mutation
  calls without architecture-owner approval and security review.
- Keep operation labels aligned with AWS SDK operation names.
- Record every AWS call through `recordAPICall` so status rows and metrics keep
  API call and throttle counts.
- Do not put secret names, ARNs, tags, KMS IDs, Lambda ARNs, page tokens, or raw
  AWS error text in metric labels.

## Common Changes

- Add a safe metadata field by first adding adapter and scanner tests, then
  mapping it through `mapSecret`.
- Add optional pagination behavior in `client.go` and keep page tokens scoped to
  SDK inputs only.
- Update `README.md` evidence if call shape, telemetry, or security boundary
  changes.

## What Not To Change Without Architecture-Owner Approval

- Do not read secret values, version values, resource policy JSON, or partner
  rotation metadata.
- Do not add credential loading, STS calls, fact persistence, graph writes, or
  reducer correlation here.
