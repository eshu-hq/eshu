# OCI Registry Identity

## Purpose

`internal/oci/registry` owns the shared OCI repository identity the
coordinator planner resolves from configured targets: the provider
vocabulary, raw and normalized repository identity, provider-neutral
normalization, and the pure per-provider repository-name and endpoint
parsing used to build an identity. It moved out of the in-tree
OCI-registry collector (`internal/collector/registry/oci`, deleted with
#10620) when the provider satellites took over production; the planner is
its only consumer.

## Ownership Boundary

This package is pure parsing and normalization. It never opens a registry
connection, fetches metadata, resolves credentials, or commits facts. The
provider distribution clients live in the extracted satellites, which carry
their own copies of these constructors.

## Exported Surface

- `Provider` and the `Provider*` constants name the supported registry
  providers.
- `RepositoryIdentity` carries the raw registry/repository tuple; the
  `*RepositoryIdentity` constructors build one per provider.
- `NormalizedRepositoryIdentity` carries the stable identity used in fact
  keys and scope IDs.
- `NormalizeRepositoryIdentity` applies the provider-neutral rules.
- `DockerHubRepositoryName`, `GHCRRepositoryName`,
  `ECRPrivateRegistryHost`, `DockerHubRegistryHost`, and
  `GHCRRegistryHost` are the planner-facing parse helpers.

## Dependencies

Standard library only.

## Telemetry

None: pure functions with no I/O.

## Gotchas / Invariants

- Repository names fold to lowercase; registries keep path case below the
  host.
- Every error that rejects a configured value must stay free of
  credentials: URL parsers strip userinfo before failing (covered by the
  credentialed-URL tests).
- Adding a provider means a new `*RepositoryIdentity` constructor plus a
  `case` in the coordinator planner (`internal/coordinator/oci/registry`),
  with planner coverage mirroring
  `TestOCIRegistryWorkPlannerNormalizesProviderEndpointFields`.

## Related Docs

- `internal/coordinator/oci/registry/README.md` for the planner that
  consumes this package.
