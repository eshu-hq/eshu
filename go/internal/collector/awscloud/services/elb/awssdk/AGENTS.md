# AGENTS.md - internal/collector/awscloud/services/elb/awssdk guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `client.go` - AWS API call ordering, pagination, tag batching, and telemetry.
3. `mapper.go` - SDK-to-scanner record mapping.
4. `../README.md` - scanner-owned fact-selection contract.

## Invariants

- Use only read APIs. `DescribeLoadBalancers` and `DescribeTags` are the entire
  accepted surface. Keep the `apiClient` interface to those two operations.
- Do not call `DescribeInstanceHealth`; live instance state is excluded from
  this topology collector slice.
- `DescribeTags` must stay batched at 20 load balancer names per call.
- Emit AWS API telemetry through `recordAPICall` for every SDK call.
- Do not log or metric-label load balancer names, DNS names, certificate ARNs,
  tags, or instance ids.
- Map only the public `SSLCertificateId` ARN from a listener. Never read or
  persist certificate bodies or private keys.

## Common Changes

- Add new mapped fields in `mapper.go` and scanner-owned types together.
- Add a focused mapper test before changing response mapping.

## What Not To Change Without An ADR

- Do not add write APIs or source mutations.
- Do not add `DescribeInstanceHealth` or live instance health facts.
- Do not bypass the `elb.Client` interface by returning AWS SDK types to the
  scanner package.
