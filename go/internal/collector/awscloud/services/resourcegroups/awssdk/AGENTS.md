# AGENTS.md - internal/collector/awscloud/services/resourcegroups/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Resource Groups SDK pagination, safe metadata mapping, and
   telemetry.
3. `../scanner.go` - scanner-owned Resource Groups fact selection.
4. `../README.md` - Resource Groups scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Resource Groups SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep the `apiClient` interface limited to `ListGroups`, `GetGroupQuery`, and
  `ListGroupResources`. Adding any mutation or configuration-write method
  violates the metadata-only contract and trips the reflection guard.
- Never persist the resource-query body. `stackIdentifierFromQuery` reads only
  the `StackIdentifier` field, only for a CloudFormation-stack-backed group.
- Pass member ARNs through verbatim. Never synthesize an ARN or rewrite a
  partition in the adapter.
- Skip members with a nil identifier or empty ARN.
- Keep metric labels bounded to service, account, region, operation, and result.

## Common Changes

- Add a new safe metadata field to a mapped group or member by extending the
  mapping and the scanner-owned type, writing a focused adapter test first. If
  the field can carry query-body or tag content, leave it out.
- Extend pagination only with read paginators the SDK already exposes for the
  three permitted operations.

## What Not To Change Without An ADR

- Do not add a mutation, `PutGroupConfiguration`, or tag-write method to
  `apiClient`.
- Do not parse or persist any resource-query field other than the stack
  identifier.
- Do not add AWS credential loading or STS calls to this package.
- Do not add graph writes, reducer logic, or query behavior.
