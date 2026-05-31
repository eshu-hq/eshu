# resiliencehub/awssdk adapter

Adapts the AWS SDK Resilience Hub control-plane API into the resiliencehub
scanner's metadata-only `Client` port. All Resilience Hub SDK access lives here;
the scanner package never imports the AWS SDK.

## Accepted read surface

The `apiClient` interface is limited to:

- `ListApps`, `DescribeApp` (for the policy ARN and app tags)
- `ListResiliencyPolicies`
- `ListAppInputSources`, `ListAppVersionAppComponents`,
  `ListAppVersionResources` (published `release` version)
- `ListAppAssessments`
- `ListTagsForResource`

A reflection guard test (`exclusion_test.go`) fails the build if any
mutation, resource-import, assessment-start, or assessment-result/drift/
recommendation reader reaches this interface.

## Versioned reads

Input sources, components, and physical resources are version-scoped. The
adapter reads the published `release` version and treats
`ResourceNotFoundException` (an app with no published version) as a partial-scan
warning, not a fatal error.

## Protected resources

`ListAppVersionResources` returns both ARN-identified and Resilience Hub-native
(non-ARN) physical resources. The adapter keeps only ARN-identified resources
(`PhysicalResourceId.Type == Arn`) so the scanner never emits a
protected-resource edge it cannot join.

## Telemetry

Every paginator page is wrapped in `recordAPICall`, which emits the shared AWS
pagination span and increments the shared AWS API-call and throttle counters.
Metric labels stay bounded to service, account, region, operation, and result.

## Evidence

No-Regression Evidence: metadata-only control-plane adapter; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/resiliencehub/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
