# Global Accelerator AWS SDK Adapter

## Purpose

`awssdk` adapts the AWS SDK for Go v2 Global Accelerator client into the
metadata-only `globalaccelerator.Client` port. It pages accelerators, listeners,
and endpoint groups, reads accelerator tags, and maps the SDK types into the
scanner-owned model.

## Ownership boundary

This package owns AWS SDK call construction, pagination, region pinning, and
SDK-to-scanner mapping for Global Accelerator. It does not emit facts, decide
relationships, or interpret topology; that belongs to the parent scanner
package.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Client` implements `globalaccelerator.Client`.
- `NewClient` builds the adapter for one claimed AWS boundary and pins the SDK
  client region to `us-west-2`.

## Dependencies

- `github.com/aws/aws-sdk-go-v2/service/globalaccelerator` and its `types`
  package for the control-plane client.
- `internal/collector/awscloud` for the boundary and shared API-call recording.
- `internal/collector/awscloud/services/globalaccelerator` for the scanner-owned
  model returned to callers.
- `internal/telemetry` for spans and instruments.

## Telemetry

Every AWS call flows through `recordAPICall`, which emits the shared AWS
collector API-call event, a pagination span, and the API-call and throttle
counters with service, account, region, and operation attributes.

No-Observability-Change: the adapter reuses the existing AWS collector API-call
metric, pagination span, and throttle counter; no new telemetry names are added.

## Gotchas / invariants

- The Global Accelerator control plane is reachable only in `us-west-2`.
  `NewClient` pins the SDK client region with an options override so a claim in
  any region still resolves the global endpoint.
- The accepted API surface is `ListAccelerators`, `ListListeners`,
  `ListEndpointGroups`, and `ListTagsForResource`. `exclusion_test.go` reflects
  over the `apiClient` interface and fails the build if a mutation operation
  becomes reachable.
- Pagination uses `NextToken` at every level. The adapter stops when the token
  is empty and guards against a nil page.

## Related docs

- `../README.md` for the Global Accelerator scanner contract.
- `docs/public/services/collector-aws-cloud-scanners.md`
