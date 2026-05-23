# ociregistry Agent Guidance

## Read First

1. `README.md` and `doc.go` for package scope.
2. `identity.go` for repository and descriptor identity normalization.
3. `envelope.go` and `warning.go` for durable fact-envelope construction.
4. Provider README files before changing provider normalization.
5. `ociruntime/README.md` for scan orchestration, warning behavior, and
   telemetry.
6. `docs/public/guides/collector-authoring.md` for the general collector fact
   contract.

## Local Rules

- Treat OCI registry metadata as reported evidence only. Do not claim canonical
  workload, image, package, repository ownership, graph truth, or query truth.
- Digest identity wins. Tags are mutable observations and must not mint image
  identity.
- Keep ECR in this OCI lane; AWS package feeds belong elsewhere.
- Strip URL credentials and sensitive token query parameters before payload or
  source-ref emission.
- Redact unknown OCI annotation values unless architecture-owner approval adds
  a current allowlist, package tests, and public collector docs.
- Keep registry hosts, repository paths, tags, digests, URLs, credentials, and
  private topology out of metrics.
- Do not interpret SBOM, signature, attestation, or vulnerability meaning here.

## Change Rules

- Add providers by extending `Provider`, adding endpoint/auth normalization
  tests in a provider subpackage, and wiring command/runtime code separately.
- Add fact envelopes only after `internal/facts` exposes the fact kind and
  schema; keep source confidence explicit.
- Add live registry calls in `ociruntime` or provider subpackages, not in
  identity helpers or envelope builders.
- Do not move package-manager feeds here or materialize graph relationships
  from collector evidence.
