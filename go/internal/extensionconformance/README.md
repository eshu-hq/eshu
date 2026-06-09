# Extension Conformance

## Purpose

`extensionconformance` checks optional component evidence before publication or
hosted activation. It loads a component manifest, derives the collector SDK
contract from the manifest-declared emitted facts, validates fixture results,
and returns a report with blocker classifications.

## Ownership boundary

This package owns conformance orchestration only. It does not validate the
manifest schema itself, manage installed component state, claim workflow work,
write durable facts, project graph truth, or run remote proof environments.
Those responsibilities stay with `internal/component`, the workflow
coordinator, storage/reducer packages, and external proof harnesses.

## Exported surface

See `doc.go` for the godoc contract. Callers use `Run` with a `Request` and
receive a `Report` containing the mode, status, findings, and fixture summary.
The exported mode, status, and finding constants keep CLI and automation output
stable.

## Dependencies

- `go/internal/component` loads and validates component manifests.
- `sdk/go/collector` validates collector SDK result fixtures against the
  derived host contract.

The package intentionally avoids storage, graph, API, MCP, and workflow
dependencies so fixture-mode checks stay cheap and deterministic.

## Telemetry

No-Observability-Change: this package emits no telemetry. It is a local
validation helper used by CLI and test harnesses; runtime proof that executes
services must add telemetry or cite the existing service signals in the owning
runtime package.

No-Regression Evidence: fixture-mode conformance behavior is covered by
`go test ./internal/extensionconformance -count=1`.

## Gotchas / invariants

- Fixture validation is fail-closed. Missing fixtures, invalid JSON, undeclared
  fact kinds, unsafe payload keys, and unsupported reducer consumers block both
  publication and hosted activation.
- `ModeCompose` currently preserves the requested mode in reports but still
  requires explicit fixture inputs. Compose-backed runtime proof will extend
  this package rather than treating fixture-only validation as remote proof.
- The idempotent re-emission check validates the same fixture twice through the
  SDK validator. A fixture must remain stable under repeated host validation.

## Related docs

- `docs/public/reference/component-package-manager.md`
- `docs/public/reference/plugin-trust-model.md`
- `sdk/go/collector/README.md`
