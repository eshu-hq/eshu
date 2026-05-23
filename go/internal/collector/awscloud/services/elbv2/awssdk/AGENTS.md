# AGENTS.md - internal/collector/awscloud/services/elbv2/awssdk guidance

## Read First

1. `README.md` - package purpose, flow, and invariants.
2. `client.go` - AWS API call ordering, pagination, tag batching, and telemetry.
3. `mapper.go` - SDK-to-scanner record mapping.
4. `conditions.go` - typed ELBv2 rule condition mapping.
5. `../README.md` - scanner-owned fact-selection contract.

## Invariants

- Use only read APIs. `DescribeLoadBalancers`, `DescribeListeners`,
  `DescribeRules`, `DescribeTargetGroups`, and `DescribeTags` are expected.
- Do not call `DescribeTargetHealth`; live target state is excluded from this
  topology collector slice.
- `DescribeTags` must stay batched at 20 resource ARNs per call.
- Emit AWS API telemetry through `recordAPICall` for every SDK call.
- Do not log or metric-label ARNs, DNS names, target group names, rule
  conditions, tags, or certificate ARNs.
- Do not persist OIDC client secrets or other auth action secret material.

## Common Changes

- Add new mapped fields in `mapper.go` and scanner-owned types together.
- Add new rule condition support in `conditions.go` and `../attributes.go`
  together.
- Add a focused mapper test before changing response mapping.

## What Not To Change Without An ADR

- Do not add write APIs or source mutations.
- Do not add live target health facts.
- Do not bypass the `elbv2.Client` interface by returning AWS SDK types to the
  scanner package.
