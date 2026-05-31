# Compute Optimizer SDK Adapter

## Purpose

`internal/collector/awscloud/services/computeoptimizer/awssdk` adapts the AWS
SDK for Go v2 Compute Optimizer client into the metadata-only `Client` interface
the scanner consumes. It pages the recommendation get APIs, maps SDK types into
scanner-owned metadata, and records API-call/throttle telemetry.

## Ownership boundary

This package owns AWS SDK pagination, SDK-to-scanner type mapping, opt-in and
throttle classification, and API-call telemetry for Compute Optimizer. It does
not own fact selection, identity keying, graph edges, or envelope construction;
that is the parent `computeoptimizer` package.

## Exported surface

- `Client` - the SDK-backed implementation of
  `computeoptimizer.Client.Snapshot`.
- `NewClient` - builds a `Client` for one claimed AWS boundary.

## Read surface (metadata-only)

The `apiClient` interface lists ONLY Get reads:

- `GetRecommendationSummaries`
- `GetEC2InstanceRecommendations`
- `GetAutoScalingGroupRecommendations`
- `GetEBSVolumeRecommendations`
- `GetLambdaFunctionRecommendations`

`exclusion_test.go` reflects over the interface and fails the build if any
enrollment mutation, recommendation-preference mutation, export start, or other
non-Get method is ever added.

## Behavior

- Every get API is paged to exhaustion on `NextToken`.
- An account not opted in to Compute Optimizer (`OptInRequiredException`, or an
  access-denied error naming the opt-in requirement) yields an empty snapshot
  instead of an error, so a not-enrolled scan completes cleanly.
- The top-ranked (rank 1) recommendation option is mapped for the recommended
  type/memory size; only the savings-opportunity percentage is kept, never the
  underlying customer cost data point.
- CloudWatch utilization metric data points are never mapped.
- Throttle errors are classified through the shared throttle classifier and
  recorded on the throttle counter; the adapter does not retry.

## Telemetry

`recordAPICall` wraps each API call in the shared
`aws.service.pagination.page` span and records the
`eshu_dp_aws_api_calls_total` and `eshu_dp_aws_throttle_total` counters with the
service, account, region, operation, and result labels. No bespoke metric is
added.

## Evidence

No-Regression Evidence: metadata-only control-plane adapter; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/computeoptimizer/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Related docs

- `../README.md` - scanner contract and resource_id shapes.
- `docs/public/services/collector-aws-cloud-scanners.md`
