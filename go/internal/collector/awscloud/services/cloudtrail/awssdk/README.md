# AWS CloudTrail SDK Adapter

## Purpose

`internal/collector/awscloud/services/cloudtrail/awssdk` adapts AWS SDK v2
CloudTrail calls into the metadata-only records consumed by the scanner. It
is the only place in this service tree that imports the AWS SDK.

## Ownership boundary

This package owns AWS SDK pagination, response mapping, and adapter-level
telemetry for CloudTrail. It does not own fact selection, envelope
construction, credential acquisition, or registration. Those belong to the
scanner package and `awsruntime`.

## Exported surface

- `NewClient(config, boundary, tracer, instruments) *Client` - constructor
  used by `runtimebind`.
- `Client.ListTrails`, `Client.ListEventDataStores`, `Client.ListChannels`,
  `Client.ListDashboards` - the metadata read paths required by the
  scanner-owned `Client` interface.

The internal `apiClient` interface is the SDK-side security boundary: it
omits `LookupEvents`, every Lake query data-plane method, and every mutation
API. `TestAPIClientInterfaceExcludesEventPayloadAndMutationAPIs` fails the
build if any of those slip onto the interface.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/cloudtrail` for SDK types and the
  default `Client`.
- `github.com/aws/smithy-go` for throttle classification.
- `internal/collector/awscloud` for boundary metadata and telemetry
  recording.
- `internal/telemetry` for shared OTel attribute helpers.

## Telemetry

Each SDK call is wrapped in `recordAPICall`, which:

- opens an `aws.service.pagination.page` span,
- records `eshu_dp_aws_api_calls_total` with `service`, `account`, `region`,
  `operation`, and `result` attributes,
- records `eshu_dp_aws_throttle_total` when the error is a recognized
  throttle code.

## Gotchas / invariants

- `GetTrailStatus` is used only to read the boolean logging status and the
  latest delivery/notification error strings. It is never used to extract
  event records.
- `GetEventSelectors` and `GetInsightSelectors` outputs are reduced to
  counts and a per-resource-type count map before they leave the adapter.
  Selector bodies do not cross the package boundary.
- `GetDashboard` reads widget counts only; widget `QueryStatement` SQL is
  never persisted and never returned through `Dashboard`.
- `ListEventDataStores` + `GetEventDataStore` is the metadata path; Lake
  query APIs are excluded from `apiClient` so they cannot be reached.
- Resource-not-found and `InsightNotEnabledException` errors are softened to
  empty results so a partial CloudTrail surface does not poison the scan.

## Related docs

- `../README.md` for the scanner contract.
- `../../README.md` for shared AWS observation contracts.
- `docs/public/services/collector-aws-cloud-scanners.md` for scanner
  coverage.
