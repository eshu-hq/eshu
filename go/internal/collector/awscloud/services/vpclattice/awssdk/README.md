# VPC Lattice SDK Adapter

## Purpose

`internal/collector/awscloud/services/vpclattice/awssdk` adapts the AWS SDK for
Go v2 VPC Lattice client into the metadata-only `vpclattice.Client` interface.
It paginates the control-plane list reads, enriches each service and target
group with a read-only detail call, maps the SDK responses into scanner-owned
metadata types, and records AWS API-call and throttle telemetry.

## Read surface

The accepted `apiClient` interface is limited to control-plane reads:

- `ListServiceNetworks`, `ListServiceNetworkVpcAssociations`,
  `ListServiceNetworkServiceAssociations`
- `ListServices`, `GetService`, `ListListeners`
- `ListTargetGroups`, `GetTargetGroup`, `ListTargets`
- `ListTagsForResource`

`GetService` supplies the ACM certificate ARN, auth type, and DNS entry the
`ListServices` summary omits. `GetTargetGroup` supplies the backing VPC
identifier and protocol the `ListTargetGroups` summary may omit. Neither reads a
policy body. `exclusion_test.go` reflects over the interface and fails the build
if a policy-read or mutation method ever reaches the adapter.

## Telemetry

Every paginator page and point read is wrapped in `recordAPICall`, which opens
the shared `aws.service.pagination.page` span and increments the shared
`eshu_dp_aws_api_calls_total` and `eshu_dp_aws_throttle_total` counters. Metric
labels stay bounded to service, account, region, operation, and result. No new
instrument is introduced.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no
change to existing hot paths. `go test ./internal/collector/awscloud/services/vpclattice/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.

## Related docs

- `../README.md` - VPC Lattice scanner contract.
- `../../../README.md` - AWS cloud envelope contract.
- `docs/public/services/collector-aws-cloud-scanners.md`
