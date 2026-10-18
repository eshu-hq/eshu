# Semantic

## Purpose

The semantic package owns semantic-evidence source fact kinds: documentation
observations and code hints emitted from LLM-assisted extraction. It exists
so the facts root stays a registry, not a family container (issue #6951).

## Ownership boundary

Source-evidence declarations, payload types, validation, stable-id
derivation, and SDK encoding only. The package owns kind constants, family
kind lists, schema-version lookups, the `Semantic*Payload` types, and the
`contract.Declare` family declaration. It does not own admission,
projection, or read models — the query semantic-evidence surface and
reducers consume these kinds downstream.

## Exported surface

- `SemanticFactKinds`, `SemanticSchemaVersion` — semantic source kinds
- `SemanticDocumentationObservationPayload`, `SemanticCodeHintPayload` and
  related ref types — payload shapes
- `Validate*`, `Encode*`, `*StableID` — payload validation, encoding, identity
- `SemanticFamilyDeclaration` — registry declaration

See `doc.go` for the full contract.

## Dependencies

- `internal/facts/contract`, `internal/facts/docs`, `internal/facts/encode`
- `sdk/go/factschema` — payload encoding contracts
- Standard library otherwise. Must not import `internal/facts`.

## Telemetry

None of its own. Kind declaration and encoding are compile-time data plus
pure functions.

## Gotchas / invariants

- Kind strings and payload shapes are contracts: collectors emit them and
  the registry, schema-version table, and conformance fixtures match on
  them. Changing a shape is a contract change with a migration, never part
  of a move.
- Stable ids must stay byte-identical: encode through `encode.StableID`,
  never a local hash, so ids match the root forwarder and the SDK.
- The schema-version table in the parent `schema_version.go` must list the
  family, or the classifier and drift guard go blind to semantic kinds.

## Related docs

- `docs/internal/naming.md` (nesting rules behind the `semantic/` split)
