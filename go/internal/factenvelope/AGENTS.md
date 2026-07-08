# AGENTS.md - internal/factenvelope guidance

## Read first

1. `README.md` - package ownership, generated adapter boundary, and invariants.
2. `doc.go` - godoc contract for callers.
3. `adapter_test.go` - compatibility tests for SDK, durable, and factschema
   envelope mapping.
4. `go/internal/facts/AGENTS.md` - durable envelope invariants.
5. `sdk/go/collector/AGENTS.md` - public SDK wire-contract rules.
6. `sdk/go/factschema/AGENTS.md` - contracts-module decode-envelope rules.

## Invariants

- Keep this package adapter-only. It must not commit facts, claim work, run
  graph writes, or validate payload shapes.
- Preserve public `sdk/go/collector.Fact` JSON tags. Any rename is a protocol
  change and does not belong in a quiet adapter update.
- Preserve durable `facts.Envelope` field names and semantics. Host-owned
  fields must be supplied by the caller, not inferred from extension payloads.
- Treat only empty schema versions and the persisted `0.0.0` sentinel as
  version-less. A real unsupported major must pass through to the Decode seam.

## Common changes

- When adding an envelope field, update the generator source, regenerate the
  adapter, and keep the drift test red until the field has an explicit mapping
  or documented drop.
- When changing the SDK fact shape, also update `sdk/go/collector` schema and
  fixture tests if the public wire JSON changes.

## Failure modes

- A stale generated adapter means a new field may be silently dropped between
  extensionhost intake and durable fact persistence.
- A missing payload clone can let caller-owned maps mutate after the envelope is
  handed to downstream stages.
- Normalizing a genuine unsupported major to `1.0.0` would hide version skew and
  decode the wrong contract.

## What not to change without review

- Do not add imports from graph, queue, storage, reducer, projector, API, MCP,
  or runtime packages.
- Do not make this package the owner of payload validation. Payload validation
  stays in `sdk/go/collector`, `sdk/go/factschema`, and extensionhost schema-ref
  validation.
