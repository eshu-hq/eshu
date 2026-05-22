# collector-package-registry

## Purpose

`collector-package-registry` runs the claim-aware `package_registry` hosted
collector binary.

## Ownership boundary

This command owns process wiring: package-registry collector-instance selection,
target JSON parsing, credential environment resolution, telemetry bootstrap,
`collector.ClaimedService` setup, Postgres ingestion, workflow claim fencing,
and the hosted runtime status surface.

Package identity, metadata parsers, and envelope construction live in
`internal/collector/packageregistry`. Runtime fetch and claim handling live in
`internal/collector/packageregistry/packageruntime`.

## Exported surface

This is a command package. Use `doc.go` for the command contract. Local
verification can run `go run ./cmd/collector-package-registry`; the installed
binary also supports `--version` and `-v`.

## Dependencies

The command wires `collector.ClaimedService`, `packageruntime.ClaimedSource`,
Postgres ingestion and workflow stores, `scope.CollectorPackageRegistry`, and
`telemetry` providers.

## Telemetry

The binary exposes `/healthz`, `/readyz`, `/metrics`, and `/admin/status`.
Package-registry observation metrics and registry-collector status rows are
emitted by the runtime components this command wires. Package names, feed URLs,
versions, artifact paths, credential environment variable names, and credential
values stay out of metric labels and status details.

## Gotchas / invariants

- `ESHU_COLLECTOR_INSTANCES_JSON` must contain exactly one matching enabled,
  claim-capable `package_registry` instance unless
  `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` selects one.
- Heartbeat interval must be less than the claim lease TTL.
- Credential fields are environment-variable indirections. Resolve values at
  runtime and do not copy them into facts, logs, metrics, status, or docs.
- `package_limit` rejects oversized target scope; `version_limit` bounds
  version reads and lets the collector emit truncation warnings.
- Preserve the target `document_format`; `artifactory_package` means a wrapper
  around package-native metadata.

## Related docs

- `go/internal/collector/packageregistry/README.md`
- `go/internal/collector/packageregistry/packageruntime/README.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/deployment/service-runtimes.md`
