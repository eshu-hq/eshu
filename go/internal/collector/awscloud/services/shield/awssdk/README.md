# Shield Advanced AWS SDK Adapter

## Purpose

`awssdk` adapts the AWS SDK for Go v2 Shield Advanced client into the
metadata-only `shield.Client` port. It pages protections, reads the per-account
subscription summary and state, and maps the SDK types into the scanner-owned
model.

## Ownership boundary

This package owns AWS SDK call construction, pagination, region pinning, the
no-subscription (`ResourceNotFoundException`) case, and SDK-to-scanner mapping
for Shield. It does not emit facts, classify protected resources, decide
relationships, or interpret topology; that belongs to the parent scanner
package.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Client` implements `shield.Client`.
- `NewClient` builds the adapter for one claimed AWS boundary and pins the SDK
  client region to `us-east-1`.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/shield` and its `types` package for the
  control-plane client.
- `internal/collector/awscloud` for the boundary and shared API-call recording.
- `internal/collector/awscloud/services/shield` for the scanner-owned model
  returned to callers.
- `internal/telemetry` for spans and instruments.

## Telemetry

Every AWS call flows through `recordAPICall`, which emits the shared AWS
collector API-call event, a pagination span, and the API-call and throttle
counters with service, account, region, and operation attributes.

No-Observability-Change: the adapter reuses the existing AWS collector API-call
metric, pagination span, and throttle counter; no new telemetry names are added.

## Gotchas / invariants

- The Shield control plane is reachable only in `us-east-1`. `NewClient` pins
  the SDK client region with an options override so a claim in any region still
  resolves the global endpoint.
- The accepted API surface is `ListProtections`, `DescribeSubscription`, and
  `GetSubscriptionState`. `exclusion_test.go` reflects over the `apiClient`
  interface and fails the build if a mutation operation becomes reachable.
- `mapSubscription` carries the ARN, state, and auto-renew flag only.
  `SubscriptionLimits`, `TimeCommitmentInSeconds`, `StartTime`, `EndTime`, and
  `ProactiveEngagementStatus` are intentionally dropped as billing/operational
  detail.
- `DescribeSubscription` returns a nil subscription (not an error) when the
  account has none, so the scanner emits no subscription resource for accounts
  without Shield Advanced.
- Pagination uses `NextToken`. The adapter stops when the token is empty and
  guards against a nil page.

## Related docs

- `../README.md` for the Shield Advanced scanner contract.
- `docs/public/services/collector-aws-cloud-scanners.md`
