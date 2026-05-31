# AGENTS.md - internal/collector/awscloud/services/dax/awssdk guidance

## Read First

1. `README.md` - adapter purpose, read surface, and invariants.
2. `client.go` - the `apiClient` read interface, pagination, and telemetry.
3. `mapper.go` - SDK-to-scanner type mapping.
4. `../README.md` - DAX scanner contract this adapter feeds.

## Invariants

- This is the only DAX package allowed to import the AWS SDK for Go v2.
- The `apiClient` interface must list only DescribeClusters,
  DescribeSubnetGroups, DescribeParameterGroups, and ListTags. Never add
  DescribeParameters (parameter values) or any
  Create/Delete/Update/Increase/Decrease/Reboot/Tag/Untag mutation operation.
- Never read or persist cached DynamoDB item data, query results, or node
  endpoint payloads. The discovery endpoint address is plain connection metadata.
- DAX does not report a server-side-encryption KMS key ARN. Record only the SSE
  status; never synthesize a KMS key field or reference.
- Page every list response to exhaustion on `NextToken`. Do not retry inside the
  adapter; surface throttle errors through the shared classifier and counter.
- Keep ARNs, names, endpoint addresses, and tags out of metric labels.

## Common Changes

- Add a new metadata field by extending the scanner-owned type in the parent
  package, then map it here with a focused `client_test.go` case first.
- Extend pagination or tag handling here, never in the parent scanner package.

## What Not To Change Without An ADR

- Do not widen the `apiClient` read surface beyond the four describe/list reads.
- Do not add mutation, parameter-value, item-data, or query-result reads.
- Do not add AWS credential loading or STS calls; the boundary provides config.
