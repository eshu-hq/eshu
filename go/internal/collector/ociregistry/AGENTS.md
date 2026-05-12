# AGENTS.md — internal/collector/ociregistry guidance

## Read First

1. `README.md` — package purpose, exported surface, and invariants
2. `identity.go` — repository and descriptor identity normalization
3. `envelope.go`, `warning.go` — durable fact-envelope construction
4. `ociruntime/README.md` — runtime scan orchestration and telemetry
5. `docs/docs/adrs/2026-05-10-oci-container-registry-collector.md` —
   source-truth boundary and implementation slices

## Invariants

- OCI registry metadata is reported evidence. Do not claim canonical workload,
  image, package, or repository ownership in this package.
- Digest identity wins. Tags are mutable and must remain weak observations.
- ECR belongs here, not in the package-registry collector lane.
- Strip URL credentials and sensitive token query parameters before adding URLs
  to payloads or source refs.
- Redact unknown OCI annotation values unless an ADR defines an allowlist.
- Do not put registry hosts, repository paths, image tags, digests, URLs, or
  credentials in metrics.

## Common Changes

- Add a new provider by extending `Provider` and identity tests.
- Add a new fact envelope builder only after `internal/facts` exposes the fact
  kind and schema version. Keep source confidence explicit.
- Add live registry calls in `ociruntime` or a provider subpackage, not in
  identity helpers or envelope builders.

## What Not To Change Without An ADR

- Do not move package-manager feeds into this package.
- Do not materialize graph nodes or relationships from this package.
- Do not treat tag names such as `latest` as immutable image identity.
- Do not interpret SBOM, signature, attestation, or vulnerability meaning here.
