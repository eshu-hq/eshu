# AGENTS.md - internal/collector/ociregistry/ociruntime guidance

## Read First

1. `go/internal/collector/ociregistry/ociruntime/README.md` - runtime flow,
   telemetry, and invariants
2. `go/internal/collector/ociregistry/ociruntime/source.go` - scan orchestration
   and fact construction
3. `go/internal/collector/ociregistry/README.md` - OCI fact identity contract
4. `go/internal/collector/service.go` - shared collector commit boundary
5. `go/internal/telemetry/README.md` - metric and span contract

## Invariants This Package Enforces

- Do not add provider SDK imports here. Provider auth and endpoint details live
  in command wiring or provider subpackages behind `ClientFactory`.
- Do not add registry host, repository, tag, digest, or fact IDs to metric
  labels. Use spans or logs for high-cardinality context.
- A missing Docker-Content-Digest header emits warning evidence. If manifest
  bytes are present, compute the OCI digest from those exact bytes; never guess
  digest identity from the tag or repository.
- Unsupported Referrers API behavior emits a warning fact, not a no-referrers
  assertion.

## Common Changes And How To Scope Them

- Add scan behavior with a `Source.Next` test that checks emitted fact kinds,
  scope kind, generation ID, and warning behavior.
- Add telemetry by updating `source.go`, `go/internal/telemetry`, and the docs
  that list metric type, labels, and purpose.
- Add manifest parsing support by extending `parseManifest` and covering both
  OCI and Docker-compatible media types.

## Anti-Patterns

- Writing facts to Postgres directly from this package.
- Treating tags as canonical image identity.
- Swallowing registry failures without either returning an error or emitting a
  warning fact for non-fatal capability gaps.
