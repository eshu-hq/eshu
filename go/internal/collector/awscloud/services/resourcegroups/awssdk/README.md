# AWS Resource Groups SDK Adapter

## Purpose

`internal/collector/awscloud/services/resourcegroups/awssdk` adapts the AWS SDK
for Go v2 Resource Groups client into the metadata-only `Client` interface the
scanner consumes. It paginates `ListGroups`, enriches each group with its query
type via `GetGroupQuery`, and lists members via `ListGroupResources`.

## Ownership boundary

This package owns AWS SDK pagination, point reads, safe metadata mapping, and
per-call telemetry for Resource Groups. It does not own scanner fact selection,
the membership classifier, STS credentials, fact persistence, or graph writes.

## Exported surface

- `Client` - implements `resourcegroups.Client` over the AWS SDK.
- `NewClient` - builds the adapter for one claimed AWS boundary.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/resourcegroups` and its `types` package.
- `internal/collector/awscloud` for the boundary and API-call recorder.
- `internal/collector/awscloud/services/resourcegroups` for the scanner-owned
  domain types and `Client` interface.
- `internal/telemetry` for spans and instruments.

## Telemetry

Every paginator page and point read is wrapped in `recordAPICall`, which records
the shared AWS API-call counter, throttle counter, and pagination span with
bounded labels (service, account, region, operation, result).

## Gotchas / invariants

- Keep Resource Groups SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- The `apiClient` interface is the single seam to the AWS SDK and exposes only
  `ListGroups`, `GetGroupQuery`, and `ListGroupResources`. A reflection guard in
  `client_test.go` asserts no mutation method can be reached.
- Never persist the resource-query body. `stackIdentifierFromQuery` extracts only
  the `StackIdentifier` field and only for a CloudFormation-stack-backed group;
  no tag-filter keys, values, or other query fields are read.
- Member ARNs are passed through verbatim from `ListGroupResources`; the adapter
  never synthesizes an ARN or rewrites a partition.
- Skip members with a nil identifier or empty ARN rather than emitting a blank
  member.
- Keep metric labels bounded to service, account, region, operation, and result.

## Related docs

- `../README.md` for the Resource Groups scanner contract.
- `../../../awsruntime/README.md` for the registry and runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
