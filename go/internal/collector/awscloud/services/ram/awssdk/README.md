# AWS Resource Access Manager SDK Adapter

## Purpose

`internal/collector/awscloud/services/ram/awssdk` adapts the AWS SDK for Go v2
Resource Access Manager client into the scanner-owned records the RAM scanner
consumes. It owns pagination, SDK-to-scanner mapping, and AWS API telemetry for
the RAM read surface.

## Ownership boundary

This package owns SDK pagination, response mapping, throttle detection, and
pagination spans. It does not own fact emission, redaction policy, credential
acquisition, or graph projection. The scanner-owned domain types live in the
parent `ram` package.

## Exported surface

See `doc.go` for the godoc contract.

- `Client` - the SDK adapter implementing `ram.Client`.
- `NewClient` - constructs the adapter for one claimed AWS boundary.

The package depends on an unexported `apiClient` interface that names only the
Get and List operations the adapter calls, so the metadata-only read surface
stays explicit and auditable.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/ram` and its `types` package for the RAM
  client, paginators, and response shapes.
- `internal/collector/awscloud` for the boundary and API-call recording.
- `internal/collector/awscloud/services/ram` for the scanner-owned record types
  this adapter produces.
- `internal/telemetry` for the AWS API-call counters and pagination span name.

## Telemetry

The adapter records one `aws.service.pagination.page` span per API call and
increments `eshu_dp_aws_api_calls_total` (and `eshu_dp_aws_throttle_total` on
throttles) with bounded service, account, region, operation, and result labels.
It never puts ARNs, principal ids, or tags into metric labels.

## Gotchas / invariants

- The accepted `apiClient` interface names only GetResourceShares,
  ListResources, ListPrincipals, and ListResourceSharePermissions. It excludes
  every mutation operation and GetPermission. `exclusion_test.go` reflects over
  the interface and fails the build if a mutation method or the GetPermission
  policy-body read ever becomes reachable.
- `mapPermission` reads only name, ARN, version, type, status, and
  default-version flags from `ResourceSharePermissionSummary`. That summary
  carries no policy document body, so a leak is structurally impossible.
- Every read is scoped to resource owner SELF (GetResourceShares, ListResources,
  ListPrincipals). The adapter never lists shares owned by other accounts.
- `ListResourceShares` drains the GetResourceShares paginator first, then for
  each share ARN drains the ListResources, ListPrincipals, and
  ListResourceSharePermissions paginators.

## Evidence

Collector Performance Evidence: `go test ./internal/collector/awscloud/services/ram/...`
covers the bounded RAM metadata path: one paginated GetResourceShares stream
scoped to resource owner SELF, then per-share paginated ListResources,
ListPrincipals, and ListResourceSharePermissions streams. No mutation API and no
permission-policy-body read is reachable, and the collector performs no graph
writes.

No-Regression Evidence: `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/...`
covers SELF-owner metadata mapping, share pagination, and the adapter exclusion
contract; the scanner package covers fact and relationship emission.

Collector Observability Evidence: RAM uses the existing AWS collector
`aws.service.pagination.page` span plus `eshu_dp_aws_api_calls_total`,
`eshu_dp_aws_throttle_total`,
`eshu_dp_aws_resources_emitted_total{service="ram"}`,
`eshu_dp_aws_relationships_emitted_total`, and `aws_scan_status` rows. Metric
labels stay bounded to service, account, region, operation, result, and
resource type.

No-Observability-Change: the existing AWS collector telemetry contract already
diagnoses RAM scans through `aws.service.scan`, `aws.service.pagination.page`,
API/throttle counters, resource/relationship counters, and `aws_scan_status`.
No new instrument or label was added.

## Related docs

- `../README.md` for the RAM scanner contract.
- `../../../awsruntime/README.md` for the runtime surface.
- `docs/public/services/collector-aws-cloud-scanners.md` for the user-facing
  coverage table.
