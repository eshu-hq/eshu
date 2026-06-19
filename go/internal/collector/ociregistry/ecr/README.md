# ECR OCI Registry Adapter

## Purpose

`internal/collector/ociregistry/ecr` maps Amazon ECR registry coordinates onto
the provider-neutral OCI registry contract. It keeps ECR in the OCI lane and
provides the small seam where AWS `GetAuthorizationToken` output becomes
Distribution client credentials.

## Ownership boundary

This package owns registry URI helpers, repository identity helpers, and ECR
authorization-token conversion. AWS profile choice, STS assume-role policy,
workflow claims, telemetry, graph writes, and query surfaces belong to runtime
wiring.

## Exported surface

- `PrivateRegistryHost` — builds `<account>.dkr.ecr.<region>.amazonaws.com`.
- `PublicRegistryHost` — returns the public ECR registry host.
- `RepositoryIdentity` — builds an `ociregistry.RepositoryIdentity`.
- `DistributionBaseURL` — returns the HTTPS Distribution API base URL for an
  ECR registry host.
- `NewDistributionClient` — creates a Distribution client with ECR credentials.
- `NewReferrerClient` / `ReferrerClientOptions` — build a Distribution client
  whose basic-auth credentials come from a fresh `GetAuthorizationToken`
  exchange, resolving the registry host from the options or the token-exchange
  proxy endpoint. Used by the SBOM attestation `oci_referrer` ECR auth path.
- `AuthorizationTokenAPI` — narrow fakeable ECR token API.
- `GetDistributionCredentials` — requests and converts ECR auth data. The
  token request does not pass a registry id because AWS now treats that input as
  deprecated.
- `BasicAuthFromAuthorizationToken` — decodes ECR's base64 token into username
  and password for Distribution HTTP calls.

## Dependencies

- `internal/collector/ociregistry` for provider identity.
- `internal/collector/ociregistry/distribution` for OCI HTTP calls.
- AWS SDK v2 ECR interfaces for token retrieval.

## Telemetry

This package emits no metrics, spans, or logs. Runtime telemetry wraps AWS calls
in the future OCI registry collector.

## Gotchas / invariants

- ECR is an OCI/container registry, not a package-registry provider.
- AWS profiles, account IDs, repository names, and credentials must stay out of
  repo docs. Use local env or AWS shared config when running live validation.
- Authorization tokens become request credentials only; do not log decoded
  tokens.
- Registry id selection belongs to the target host, not the token request.
- The live test is opt-in and requires a local AWS config with ECR permissions.

## Related docs

- `docs/public/deployment/service-runtimes-collectors.md`
