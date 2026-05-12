# GHCR OCI Registry Adapter

## Purpose

`internal/collector/ociregistry/ghcr` maps GitHub Container Registry
repositories onto the provider-neutral OCI registry contract. GHCR uses
`ghcr.io` for image references and Distribution calls, with a repository-scoped
token endpoint for pulls.

## Ownership boundary

This package owns GHCR repository-name normalization, token acquisition, and
Distribution client construction. Workflow claims, telemetry, graph writes,
package-to-source correlation, and query surfaces belong to later runtime and
reducer slices.

## Exported surface

- `Config` — repository plus optional GHCR credentials.
- `RegistryHost` — canonical GHCR image-reference host.
- `DistributionBaseURL` — GHCR Distribution endpoint.
- `RepositoryName` — validates owner/image repository names.
- `RepositoryIdentity` — builds an `ociregistry.RepositoryIdentity`.
- `NewDistributionClient` — creates a pull-token-backed Distribution client.

## Dependencies

- `internal/collector/ociregistry` for provider identity.
- `internal/collector/ociregistry/distribution` for token and OCI HTTP calls.

## Telemetry

This package emits no metrics, spans, or logs. Runtime telemetry wraps provider
calls in the future OCI registry collector.

## Gotchas / invariants

- GHCR repository names must include owner and image path.
- GitHub usernames and tokens must not enter facts, logs, metrics, docs, or PR
  text.
- Live tests are opt-in and default to a public GHCR image.

## Related docs

- `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`
