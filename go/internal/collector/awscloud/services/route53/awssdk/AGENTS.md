# AGENTS.md - internal/collector/awscloud/services/route53/awssdk guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `client.go` - AWS API call ordering, pagination, tag reads, and telemetry.
3. `mapper.go` - SDK-to-scanner record mapping.
4. `../README.md` - scanner-owned fact-selection contract.

## Invariants

- Use only read APIs: `ListHostedZones`, `ListResourceRecordSets`, and
  `ListTagsForResource`.
- Emit AWS API telemetry through `recordAPICall` for every SDK call.
- Trim `/hostedzone/` before calling `ListTagsForResource`.
- Preserve alias target DNS name and hosted-zone ID exactly as AWS reports
  them.
- Do not log or metric-label DNS names, hosted-zone IDs, record values, or
  tags.

## Common Changes

- Add new mapped fields in `mapper.go` and scanner-owned types together.
- Add a focused mapper or pagination test before changing response mapping.
- Keep Route 53 pagination in the adapter instead of looping over SDK pages in
  the scanner package.

## What Not To Change Without An ADR

- Do not add write APIs or source mutations.
- Do not bypass the `route53.Client` interface by returning AWS SDK types to
  the scanner package.
