# Docker Hub OCI Registry Adapter

## Purpose

`internal/collector/ociregistry/dockerhub` maps Docker Hub image references onto
the provider-neutral OCI registry contract. Docker Hub exposes canonical image
names under `docker.io`, serves Distribution requests from
`registry-1.docker.io`, and mints pull tokens through its token service.

## Ownership boundary

This package owns Docker Hub repository-name normalization, token acquisition,
and Distribution client construction. Workflow claims, telemetry, graph writes,
rate-limit policy, and query surfaces belong to later runtime slices.

## Exported surface

- `Config` — repository plus optional Docker Hub credentials.
- `RegistryHost` — canonical image-reference host.
- `DistributionBaseURL` — Docker Hub Distribution endpoint.
- `RepositoryName` — adds the `library/` namespace for official images.
- `RepositoryIdentity` — builds an `ociregistry.RepositoryIdentity`.
- `NewDistributionClient` — creates a pull-token-backed Distribution client.

## Dependencies

- `internal/collector/ociregistry` for provider identity.
- `internal/collector/ociregistry/distribution` for token and OCI HTTP calls.

## Telemetry

This package emits no metrics, spans, or logs. Runtime telemetry wraps provider
calls in the future OCI registry collector.

## Gotchas / invariants

- Single-segment Docker Hub names are official-library repositories and become
  `library/<name>` for Distribution calls.
- Docker Hub credentials and tokens must not enter facts, logs, metrics, docs,
  or PR text.
- Live tests are opt-in and default to a public official-library image.

## Related docs

- `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`
