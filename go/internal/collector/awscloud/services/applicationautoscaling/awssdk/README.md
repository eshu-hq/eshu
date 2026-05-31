# Application Auto Scaling SDK Adapter

## Purpose

`internal/collector/awscloud/services/applicationautoscaling/awssdk` adapts the
AWS SDK for Go v2 Application Auto Scaling client into the metadata-only
`applicationautoscaling.Client` interface. It reads scalable target, scaling
policy, and scheduled action metadata, paginating each Describe call to
exhaustion and fanning out across every supported service namespace.

## Read surface

The `apiClient` interface lists exactly three read operations:

- `DescribeScalableTargets`
- `DescribeScalingPolicies`
- `DescribeScheduledActions`

No register, deregister, put, delete, or scaling-action operation is reachable.
`exclusion_test.go` reflects over the interface and fails the build if a
mutation method is ever added.

## Behavior

- Each Describe call requires a `ServiceNamespace`; the adapter iterates the
  supported namespace set and merges the results.
- Pagination follows `NextToken` until exhausted.
- A namespace throttled after SDK retries records a non-fatal
  `WarningThrottleSustained` observation and is skipped; the scan continues for
  the remaining namespaces. The adapter does not retry internally.
- Step-scaling and target-tracking configuration bodies are dropped during
  mapping; only the bound CloudWatch alarm ARNs are kept.

## Ownership boundary

This package owns SDK type mapping, pagination, namespace fan-out, and throttle
classification. It does not own fact selection, identity mapping, or relationship
join keys, which live in the parent `applicationautoscaling` package.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/applicationautoscaling/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
