# JFrog OCI Registry Adapter

## Purpose

`internal/collector/ociregistry/jfrog` maps JFrog Artifactory Docker/OCI
repository configuration onto the provider-neutral OCI Distribution client.
It lets Eshu validate Artifactory as an OCI registry without mixing JFrog
package feeds into the OCI lane.

## Ownership boundary

This package owns Artifactory base URL normalization, Docker repository key
path construction, and credential mapping. Live collector claims, telemetry,
fact emission, graph writes, and package-manager feeds belong elsewhere.

## Exported surface

- `Config` — Artifactory base URL, Docker repository key, and credentials.
- `DistributionBaseURL` — returns the Artifactory Docker API base URL.
- `NewDistributionClient` — creates a Distribution client for a Docker/OCI
  repository key.
- `RepositoryIdentity` — builds an `ociregistry.RepositoryIdentity` for one
  image repository.

## Dependencies

- `internal/collector/ociregistry` for provider identity.
- `internal/collector/ociregistry/distribution` for OCI HTTP calls.

## Telemetry

This package emits no metrics, spans, or logs. Runtime telemetry wraps provider
calls in the future OCI registry collector.

## Gotchas / invariants

- The repository key is an Artifactory Docker/OCI repository key, not an image
  repository name.
- Package-manager feeds hosted by the same Artifactory instance do not belong in
  this package.
- Credentials are passed to HTTP clients only and must not be logged or
  documented in repo files.
- Live JFrog OCI smoke tests are opt-in maintainer checks. They use the public
  ESHU JFrog OCI test environment contract. Maintainers may map private
  shell-local JFrog aliases locally, but endpoints, repository keys, image
  repository names, usernames, tokens, and passwords must stay out of repo docs,
  commits, CI config, and PR text.

## Related docs

- `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md`
- `docs/docs/adrs/2026-05-12-package-registry-collector.md`
