# ociruntime Agent Guidance

## Read First

1. `README.md` and `doc.go` for scan flow and telemetry.
2. `source.go` for scan orchestration, manifest handling, warning facts, and
   fact construction.
3. `claimed_source.go` for claim target resolution and generation reuse.
4. `go/internal/collector/ociregistry/README.md` for OCI fact identity.
5. `go/internal/collector/service.go` for the shared commit boundary.
6. `go/internal/telemetry/README.md` for metric and span contracts.

## Local Rules

- Keep provider SDK imports out of this package. Provider auth and endpoint
  details belong behind `ClientFactory`.
- Keep registry host, repository, tag, digest, fact ID, URL, and credential
  values out of metric labels.
- Treat tags as mutable observations; digest identity wins.
- If `Docker-Content-Digest` is missing and manifest bytes are present, compute
  the digest from those exact bytes and emit warning evidence. Never guess from
  tag or repository.
- Treat unsupported Referrers API behavior as warning evidence, not a
  no-referrers assertion.
- Claimed scans must match one configured target by normalized `scope_id` and
  reuse the claimed `generation_id` for idempotent retries.
- Do not write facts to Postgres directly from this package.

## Change Rules

- Add scan behavior with `Source.Next` or `ClaimedSource.NextClaimed` tests
  covering fact kinds, scope kind, generation ID, fencing token, and warnings.
- Add telemetry through `go/internal/telemetry` and update docs that list metric
  type, labels, and purpose.
- Extend `parseManifest` with tests for OCI and Docker-compatible media types
  before changing manifest behavior.
