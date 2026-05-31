# Proton SDK adapter

`package awssdk` is the AWS SDK Proton adapter behind `proton.Client`. It pages
the Proton control-plane reads and maps each SDK response into the scanner-owned
metadata types, keeping every spec/schema/parameter body out of the snapshot.

## Read surface

The `apiClient` interface lists exactly the accepted reads:

- `ListEnvironments`, `ListServices`, `ListEnvironmentTemplates`,
  `ListServiceTemplates` — the resource list reads.
- `GetService` — the single detail read, mapped to source-repository linkage
  (branch, repository id, CodeStar connection ARN) **by reference only**. The
  `Service.Spec` and `Pipeline.Spec` manifest bodies on the detail are never
  mapped.
- `ListServiceInstances` — read once for the whole account (no per-service
  filter); only the service-name/environment-name join keys are kept, never the
  instance spec or input parameter values.
- `ListTagsForResource` — resource tags.

Every mutation (`Create*`, `Update*`, `Delete*`, `Cancel*`, `Reject*`,
`Accept*`, `Notify*`, `Tag*`, `Untag*`, `Add*`, `Remove*`), every
sync-status/config and sync-blocker reader, every `*Outputs` deployment-output
reader, every `*ProvisionedResources` reader, and every component reader is
excluded by construction. `exclusion_test.go` reflects over the interface and
fails the build if a forbidden method is ever added.

## Telemetry

Each list page and the per-service detail read are wrapped in `recordAPICall`,
which opens the shared AWS pagination span and increments the shared AWS
API-call and throttle counters, labelled by service, account, region, operation,
and result. No bespoke metric is added.

## Pagination, empty state, throttling

Every list API is paged to exhaustion through its `NextToken`. An empty account
returns an empty snapshot cleanly. Throttle errors are classified by
`isThrottleError` and surfaced on the shared throttle counter; the adapter does
not retry internally (the SDK's standard retryer owns that).

## Performance and observability

No-Regression Evidence: metadata-only control-plane adapter; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/proton/...` green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
