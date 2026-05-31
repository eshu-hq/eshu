# AWS DAX SDK Adapter

## Purpose

`internal/collector/awscloud/services/dax/awssdk` implements the `dax.Client`
interface against the AWS SDK for Go v2 DAX control-plane client. It is the only
place in the DAX scanner that imports the AWS SDK.

## Ownership boundary

This package owns DAX SDK pagination, type mapping, tag reads, and API-call
telemetry. It does not own scanner fact selection (the parent `dax` package), AWS
credential loading, STS, fact persistence, or graph writes.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK-backed `dax.Client` adapter.
- `NewClient` - builds a `Client` for one claimed AWS boundary.

## Read surface (metadata-only)

The private `apiClient` interface lists exactly four read operations:

- `DescribeClusters` - cluster metadata (paginated).
- `DescribeSubnetGroups` - subnet group metadata (paginated).
- `DescribeParameterGroups` - parameter group name and description (paginated).
- `ListTags` - resource tags per cluster ARN (paginated).

`DescribeParameters` is deliberately excluded so individual parameter values are
never read. No Create/Delete/Update/Increase/Decrease/Reboot/Tag/Untag mutation
operation is reachable. `client_test.go` asserts this by reflection.

## Gotchas / invariants

- Every list call pages to exhaustion on `NextToken`; `MaxResults` is 100 (DAX
  requires 20-100).
- DAX subnet groups and parameter groups have no ARN, so no tags are read for
  them; only cluster tags are read via `ListTags(ResourceName=clusterARN)`.
- DAX does not report a server-side-encryption KMS key ARN. The mapper records
  only the SSE status; it never synthesizes a KMS key reference.
- The discovery endpoint address and port are plain connection metadata, not
  secrets. Cached DynamoDB item data, query results, and node endpoint payloads
  are never read.
- Throttle errors are classified through the shared smithy `APIError` check and
  recorded on the throttle counter; the adapter does not retry.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no
change to existing hot paths. `go test ./internal/collector/awscloud/services/dax/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Related docs

- `../README.md` - DAX scanner contract.
- `docs/public/services/collector-aws-cloud.md`
- `docs/public/guides/collector-authoring.md`
