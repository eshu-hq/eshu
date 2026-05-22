# internal/collector/ociregistry

## Purpose

`internal/collector/ociregistry` normalizes OCI container registry evidence
before it enters durable fact envelopes for the `oci_registry` collector
family.

## Ownership boundary

This package owns repository and digest identity normalization,
reported-confidence envelope builders, mutable tag observations, manifests,
image indexes, descriptors, referrers, warnings, and redaction of unknown
annotations or credential-bearing URLs.

It does not call live registries, select provider clients, materialize graph
truth, or answer queries. Provider clients live in subpackages and runtime
execution lives in `ociruntime`.

## Exported surface

Use `go doc ./internal/collector/ociregistry` for the full contract. The main
surface is:

- Provider, visibility, auth-mode, media-type, and collector constants.
- `RepositoryIdentity`, `DescriptorIdentity`, and their normalized forms.
- Observation types for repositories, tags, manifests, indexes, descriptors,
  referrers, and warnings.
- `New*Envelope` builders for durable fact emission.

## Dependencies

- `internal/facts` supplies envelope contracts.
- Provider subpackages implement Docker Hub, GHCR, ECR, JFrog, Harbor, Google
  Artifact Registry, Azure Container Registry, and OCI Distribution behavior.
- `ociruntime` calls providers and this package to produce collected
  generations.

## Telemetry

This root package emits no metrics or spans. Runtime and provider packages own
request, pagination, status, warning, and collection telemetry.

## Gotchas / invariants

- Digest-backed identity is canonical. Tags are mutable observations and must
  not mint image identity.
- Fact IDs are generation-specific; stable keys remain source-stable inside a
  generation.
- Unknown annotations and credential-bearing URLs must be redacted before facts
  are persisted.
- Envelope builders validate boundary fields. Do not bypass them with ad hoc
  fact construction.
- Canonical graph projection for OCI registry evidence belongs to projector and
  Cypher writers, not collectors.

## Focused tests

```bash
go test ./internal/collector/ociregistry/... -count=1
go test ./cmd/collector-oci-registry -count=1
go doc ./internal/collector/ociregistry
```

## Related docs

- `docs/public/reference/collector-reducer-readiness.md`
- `docs/public/guides/collector-authoring.md`
- `go/cmd/collector-oci-registry/README.md`
