# Extension Conformance

## Purpose

`extensionconformance` is the in-tree host wrapper around the public collector
conformance harness. It loads a component manifest and fixture files from disk,
maps the manifest onto the portable harness input, and delegates the verdict to
`sdk/go/collector/conformance`. The verdict logic (manifest proof metadata,
fixture contract validation, reducer-consumer checks, and the
`eshu.extension.conformance.v1` report) lives once in the public SDK module so
the same result is produced inside and outside the monorepo.

## Ownership boundary

This package owns host-side orchestration only: manifest loading and fixture
file I/O. The conformance verdict, report contract, and proof-metadata rules
are owned by the public `sdk/go/collector/conformance` package. It does not
manage installed component state, claim workflow work, write durable facts,
project graph truth, or run remote proof environments. Those responsibilities
stay with `internal/component`, the workflow coordinator, storage/reducer
packages, and external proof harnesses.

## Exported surface

See `doc.go` for the godoc contract. The exported `Mode`, `Status`,
`FindingCode`, `Report`, `Finding`, and `Summary` types are aliases of the
public `sdk/go/collector/conformance` types, so CLI and automation output stay
byte-stable. Callers use `Run` with a `Request` (manifest path, fixture paths,
mode) and receive the public `Report`. `FindingFixtureReadFailed` is the only
host-local finding code, because file I/O is the host's responsibility.

## Dependencies

- `go/internal/component` loads and validates component manifests from disk.
- `sdk/go/collector/conformance` owns the conformance verdict and report.
- `sdk/go/collector` decodes and validates collector SDK result fixtures.

The package intentionally avoids storage, graph, API, MCP, and workflow
dependencies so fixture-mode checks stay cheap and deterministic. It reads
payload schemas only from the versioned factschema fixture pack when a manifest
declares `payloadSchemaRef`.

## Telemetry

No-Observability-Change: this package emits no telemetry. It is a local
validation helper used by CLI and test harnesses; runtime proof that executes
services must add telemetry or cite the existing service signals in the owning
runtime package.

No-Regression Evidence: fixture-mode conformance behavior is covered by
`go test ./internal/extensionconformance -count=1`.

`emitter_payload_conformance_test.go` closes a gap the fixture-pack tests above
could not: it builds a REAL envelope from each of the internal AWS/IAM/S3 fact
emitters (`go/internal/collector/awscloud`, `go/internal/collector/secretsiam`)
and validates the actual payload it produces against the committed JSON Schema
for that exact fact kind, loaded via `factschema.SchemaBytes`, through
`conformance.ValidatePayloadSchemas`. `TestFixturePackPayloadsAgreeWithConformance`
only proves curated fixture-pack payloads agree with their schemas; this test
proves a live emitter's own output does. A companion test strips a
schema-required field from a real payload and asserts validation fails, so the
proof cannot pass by accident.

## Gotchas / invariants

- Fixture validation is fail-closed. Missing fixtures, invalid JSON, undeclared
  fact kinds, unsafe payload keys, and unsupported reducer consumers block both
  publication and hosted activation.
- A manifest `payloadSchemaRef` maps the component's namespaced fact kind to a
  fixture-pack schema shape. Schema-invalid fixtures block both publication and
  hosted activation with `payload_schema_invalid`.
- `ModeCompose` currently preserves the requested mode in reports but still
  requires explicit fixture inputs. Compose-backed runtime proof will extend
  this package rather than treating fixture-only validation as remote proof.
- The idempotent re-emission check validates the same fixture twice through the
  SDK validator. A fixture must remain stable under repeated host validation.

## Related docs

- `docs/public/reference/component-package-manager.md`
- `docs/public/reference/plugin-trust-model.md`
- `sdk/go/collector/README.md`
- `sdk/go/collector/conformance/README.md`
