# AGENTS.md - internal/collector/awscloud/services/ec2/awssdk guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `client.go` - AWS API call ordering, pagination, and telemetry.
3. `mapper.go` - SDK-to-scanner record mapping.
4. `../README.md` - scanner-owned fact-selection contract.

## Invariants

- Use only EC2 read APIs.
- Emit AWS API telemetry through `recordAPICall` for every SDK page request.
- Preserve AWS tags exactly as reported.
- Set `IncludeManagedResources=true` on network interface scans.
- Do not return AWS SDK types to the scanner package.
- Do not log or metric-label resource IDs, ARNs, descriptions, or tags.

## Common Changes

- Add new mapped fields in `mapper.go` and scanner-owned types together.
- Add a focused mapper or pagination test before changing response mapping.
- Keep EC2 pagination in the adapter instead of looping over SDK pages in the
  scanner package.

## What Not To Change Without An ADR

- Do not add write APIs or source mutations.
- Do not inventory EC2 instances from this adapter.
- Do not bypass the `ec2.Client` interface by returning AWS SDK types to the
  scanner package.
