# AGENTS.md - internal/collector/awscloud/services/ssm/awssdk guidance

## Read First

1. `README.md` - adapter boundary and telemetry.
2. `client.go` - SDK call surface and API-call recording.
3. `mapper.go` - safe response mapping into scanner-owned types.
4. `../README.md` - service package metadata-only contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md`.

## Invariants

- Keep the AWS SDK contained in this adapter package.
- Use DescribeParameters and ListTagsForResource only for this slice. Do not
  add GetParameter, GetParameters, GetParametersByPath, GetParameterHistory,
  decryption, or mutation calls without an ADR and security review.
- Keep operation labels aligned with AWS SDK operation names.
- Record every AWS call through `recordAPICall` so status rows and metrics keep
  API call and throttle counts.
- Do not put parameter names, paths, ARNs, tags, KMS IDs, page tokens, or raw
  AWS error text in metric labels.

## Common Changes

- Add a safe metadata field by first adding adapter and scanner tests, then
  mapping it through `mapParameter`.
- Add optional pagination behavior in `client.go` and keep page tokens scoped
  to SDK inputs only.
- Update `README.md` evidence if call shape, telemetry, or security boundary
  changes.

## What Not To Change Without An ADR

- Do not read parameter values, history values, raw policy JSON, decrypted
  content, or mutation APIs.
- Do not add credential loading, STS calls, fact persistence, graph writes, or
  reducer correlation here.
