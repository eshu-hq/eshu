# AWS EC2 Auto Scaling SDK Adapter

## Purpose

`internal/collector/awscloud/services/autoscaling/awssdk` adapts the AWS SDK for
Go v2 Auto Scaling client into the scanner-owned records the `autoscaling`
package consumes. It owns pagination, the per-group lifecycle-hook describe
fan-out, SDK-to-scanner mapping, AWS API telemetry, throttle detection, and
pagination spans.

## Ownership boundary

This package owns AWS SDK behavior for the Auto Scaling scanner. It does not own
fact selection, redaction, scope identity, persistence, graph writes, reducer
admission, or query behavior. Mapping decisions that drop sensitive fields are
made here at the adapter boundary so the sensitive data never reaches the
scanner.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the Auto Scaling SDK adapter implementing
  `autoscaling.Client` for one claimed boundary.
- `NewClient` - constructs the adapter from an `aws.Config`, boundary, tracer,
  and telemetry instruments.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/autoscaling` and its `types` package for
  the AWS read surface.
- `internal/collector/awscloud` for the boundary, API-call recording, and the
  scanner `Client` contract it satisfies.
- `internal/collector/awscloud/services/autoscaling` for the scanner-owned
  record types.
- `internal/telemetry` for spans, API-call counters, and throttle counters.

## Telemetry

Each AWS call is wrapped by `recordAPICall`, which starts the
`aws.service.pagination.page` span, records `eshu_dp_aws_api_calls_total` with
service/account/region/operation/result labels, and increments
`eshu_dp_aws_throttle_total` on throttle errors. ARNs, tags, and any
caller-supplied data stay out of metric labels.

## Gotchas / invariants

- The `apiClient` interface is Describe-only. Never add CreateAutoScalingGroup,
  UpdateAutoScalingGroup, DeleteAutoScalingGroup, SetDesiredCapacity,
  TerminateInstanceInAutoScalingGroup, or any Create/Update/Delete/Set
  operation. `exclusion_test.go` enforces this; do not weaken it.
- `mapLaunchConfiguration` reads identity only. Never map UserData,
  BlockDeviceMappings, SecurityGroups, KeyName, or IamInstanceProfile. The
  scanner-owned `LaunchConfiguration` type does not declare those fields, so a
  leak would not compile.
- `mapLifecycleHook` never carries `NotificationMetadata`.
- `splitSubnetIdentifier` parses the comma-separated `VPCZoneIdentifier` into
  bare subnet IDs so the subnet edge join key matches the EC2-owned subnet
  resource_id form.
- `mapGroup` prefers the launch template ID over the name so the launch-template
  edge join key matches the EC2 launch-template resource_id form.
- DescribeLifecycleHooks is not paginated by AWS; it returns all hooks for one
  group in a single call. The adapter fans it out per discovered group.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/autoscaling/awssdk/...`
covers the bounded mapping and read surface: subnet identifier splitting,
launch-template ID preference, UserData and notification-metadata exclusion, and
the reflective metadata-only guard. The adapter performs no graph writes.

No-Regression Evidence: the reflective `exclusion_test.go` proves the read
surface is Describe-only and cannot mutate Auto Scaling resources or control
capacity.

Collector Observability Evidence: the adapter reuses the existing
`aws.service.pagination.page` span and `eshu_dp_aws_api_calls_total` /
`eshu_dp_aws_throttle_total` counters with bounded labels.

No-Observability-Change: no new instrument or label was added; the adapter
reuses the shared AWS collector telemetry contract.

## Related docs

- `../README.md` - Auto Scaling scanner contract.
- `docs/public/services/collector-aws-cloud-scanners.md`
