# Component Extension Collector

## Purpose

`collector-component-extension` runs trusted, claim-capable component
activations through the process-backed collector SDK host. It consumes workflow
claims planned by the workflow coordinator, launches the configured extension
process with a bounded JSON request, validates the SDK result, and commits
accepted facts through the normal claimed collector boundary.

## Ownership boundary

This binary owns component activation readback, process-runner configuration,
and claim-aware host wiring for public collector SDK extensions. It does not
install components, publish packages, pull OCI images, verify Sigstore
provenance, write graph truth, or expose API/MCP inventory. Reducers remain the
only owner of graph nodes and relationships derived from extension facts.

## Exported surface

The command package exposes no library API. Its internal wiring centers on
`loadRuntimeConfig`, `buildClaimedService`, and `newClaimID`; godoc in
`doc.go` describes the binary contract.

## Dependencies

- `internal/component` for registry readback, trust policy, manifests, and the
  shared activation config handle.
- `internal/collector/extensionhost` for SDK request/result validation and
  process execution.
- `internal/collector` for `ClaimedService` claim, retry, heartbeat, and commit
  behavior.
- `internal/storage/postgres` for workflow claim mutation and durable fact
  commits.
- `internal/runtime`, `internal/app`, and `internal/telemetry` for hosted
  process wiring.

## Telemetry

The binary uses service name `collector-component-extension`, status server
name `collector-component-extension`, and Postgres store name
`collector_component_extension`. It inherits `/healthz`, `/readyz`, `/metrics`,
`/admin/status`, OTEL spans, and collector commit metrics from shared runtime,
collector, and storage packages.

## Gotchas / invariants

- `ESHU_COMPONENT_HOME` is required and must point at the same registry the
  workflow coordinator used while planning work.
- Trust policy fails closed. The worker skips untrusted, revoked, incompatible,
  failed, disabled, or non-claim-capable activations.
- The process adapter reads a local activation config file. The raw path stays
  in registry state and is converted to a stable config handle before workflow
  rows or SDK claims see it.
- The optional activation `host` block is the only config content promoted into
  workflow planning. It may name `sourceSystem`, `scope.id`, and `scope.kind`
  so the SDK claim matches the external source instead of a synthetic component
  scope.
- The worker supports `spec.runtime.adapter: process` only. OCI execution is
  intentionally blocked until a runnable digest-pinned artifact adapter lands.
- Claim retries, terminal failure, stale fencing, heartbeats, and completion
  are owned by `collector.ClaimedService`; do not bypass that boundary.

No-Regression Evidence: `go test ./cmd/collector-component-extension -run 'TestLoadRuntimeConfig|TestBuildClaimedService' -count=1` proves the worker selects one trusted claim-capable process activation, rejects untrusted and unsupported OCI activations, applies activation host scope metadata, wires `extensionhost.Source`, and preserves `collector.ClaimedService` max-attempt behavior for the component claim conflict domain.

No-Observability-Change: the worker adds no new metric labels, queue domains,
graph writes, or API/MCP routes. Operators diagnose progress and failure
through existing collector `/admin/status`, workflow claim rows, failure
classes, commit counters, and the service metrics endpoint.

## Related docs

- `docs/public/extend/community-extension-authoring.md`
- `docs/public/reference/component-package-manager.md`
- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/public/run-locally/docker-compose.md`
