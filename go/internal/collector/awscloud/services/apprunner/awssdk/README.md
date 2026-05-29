# AWS App Runner SDK Adapter

## Purpose

`internal/collector/awscloud/services/apprunner/awssdk` adapts the AWS SDK for
Go v2 App Runner client into the scanner-owned records the App Runner scanner
consumes. It owns NextToken pagination, per-resource describe enrichment,
SDK-to-scanner mapping, and AWS API telemetry for the App Runner read surface.

## Ownership boundary

This package owns SDK pagination, describe enrichment, response mapping,
throttle detection, and pagination spans. It does not own fact emission,
credential acquisition, or graph projection. The scanner-owned domain types live
in the parent `apprunner` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK adapter implementing `apprunner.Client`.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

The package depends on an unexported `apiClient` interface that names only the
List and Describe operations the adapter calls, so the metadata-only read
surface stays explicit and auditable.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/apprunner` and its `types` package for
  the App Runner client and response shapes.
- `internal/collector/awscloud` for the boundary, API-call recording, and the
  shared throttle/telemetry helpers.
- `internal/collector/awscloud/services/apprunner` for the scanner-owned record
  types this adapter produces.
- `internal/telemetry` for the AWS API-call counters and pagination span name.

## Telemetry

The adapter records one `aws.service.pagination.page` span per API call and
increments `eshu_dp_aws_api_calls_total` (and `eshu_dp_aws_throttle_total` on
throttles) with bounded service, account, region, operation, and result labels.
It never puts ARNs, environment values, secret references, or tags into metric
labels.

## Gotchas / invariants

- The accepted `apiClient` interface excludes CreateService, DeleteService,
  UpdateService, PauseService, ResumeService, StartDeployment, DeleteConnection,
  AssociateCustomDomain, and every Create/Update/Delete operation.
  `exclusion_test.go` reflects over the interface and fails the build if a
  mutation or lifecycle method ever becomes reachable.
- `mapService` reads runtime environment-variable NAMES only
  (`sortedKeys`) and converts `RuntimeEnvironmentSecrets` into ARN-only secret
  references (`secretReferences`). It never reads an environment-variable value
  or a source repository credential. The scanner-owned `Service` type has no
  field that could carry a value, so a leak would not compile.
- App Runner has no SDK paginators; the adapter drives NextToken pagination by
  hand for every List operation. Service, autoscaling, observability, and VPC
  ingress connection summaries are enriched through a per-resource Describe so
  the scanner sees full configuration detail. Connections and VPC connectors
  carry full detail in their list responses and need no describe.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, env values, secrets, and tags out of
  metric labels.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/apprunner/...`
covers the bounded App Runner metadata path: one paginated ListServices stream
with a DescribeService and ListTagsForResource enrichment per service, one
paginated ListConnections stream, one paginated ListAutoScalingConfigurations
stream with a DescribeAutoScalingConfiguration enrichment per revision, one
paginated ListObservabilityConfigurations stream with a
DescribeObservabilityConfiguration enrichment per revision, one paginated
ListVpcConnectors stream, and one paginated ListVpcIngressConnections stream
with a DescribeVpcIngressConnection enrichment per connection. No mutation or
lifecycle API is reachable, and the collector performs no graph writes.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers service, connection, autoscaling, observability, VPC-connector, and
VPC-ingress fact emission, relationship target types and join keys, structural
exclusion of environment-variable values and source credentials, runtime
registration, and command configuration. The adapter reflection contract test
proves the mutation and lifecycle APIs are unreachable.

Collector Observability Evidence: App Runner uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total{service="apprunner"}`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and
resource type.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses App Runner scans through `aws.service.scan`,
`aws.service.pagination.page`, API/throttle counters, resource/relationship
counters, and `aws_scan_status`. No new instrument or label was added.

## Related docs

- `../README.md` for the App Runner scanner contract.
- `../../../awsruntime/README.md` for the runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
