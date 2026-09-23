# Postgres fact payload JSON codec

## Purpose

This package owns the JSONB encode/decode for the fact payload column and
the small empty-string SQL-binding helpers that most `storage/postgres`
writers and readers share. It exists as its own leaf package so that
`facts/`, both queue families, `relationship/`, and the collector can all
call it without importing each other or the parent `postgres` package.

## Ownership boundary

This package owns payload JSON encoding/decoding and the empty-to-nil and
empty-to-default string helpers. It does not own any table, DDL, or SQL
statement text — those stay with the family that issues the query (facts,
queues, relationships, collector). It never imports the parent `postgres`
package or any other `storage/postgres` family package.

## Exported surface

- `MarshalPayload(payload map[string]any) ([]byte, error)` encodes a fact
  payload as Postgres-JSONB-safe JSON; an empty or nil payload marshals to
  `"{}"`.
- `UnmarshalPayload(raw []byte) (map[string]any, error)` decodes a JSONB
  column back into a payload map; empty input or an empty decoded object
  returns a nil map with no error.
- `EmptyToNil(value string) any` returns `nil` for an empty string so
  callers bind SQL `NULL` instead of `""`.
- `EmptyToDefault(value, fallback string) string` returns `fallback` when
  `value` is empty; the fact-write path uses it to stamp the
  version-less-fact sentinel `"0.0.0"` on `SchemaVersion`.

See `doc.go` for the godoc contract.

## Dependencies

None beyond the standard library (`bytes`, `encoding/json`, `fmt`).

## Telemetry

None. This package is a pure, allocation-bounded codec with no I/O; callers
that write or read through it own request-scoped telemetry.

## Gotchas / invariants

- `MarshalPayload` sanitizes control bytes and `\u0000` escapes because
  Postgres JSONB rejects them outright; it distinguishes a literal
  backslash-escaped `\u0000` source string (kept) from an actual null-byte
  escape (stripped) by counting preceding backslashes.
- `UnmarshalPayload` treats an empty decoded object the same as empty input
  (nil map, no error) so callers do not have to special-case `{}`.
- `EmptyToDefault(SchemaVersion, "0.0.0")` is the sentinel every reducer
  decode path expects back from Postgres for a version-less fact — see the
  reducer-side comments in `go/internal/reducer/AGENTS.md` and
  `go/internal/reducer/schemadecode/factschema_decode.go` that cite this
  package.
- Do not import the parent `postgres` package or any sibling
  `storage/postgres` family package: that is an import cycle this leaf
  exists to avoid.

No-Observability-Change: this extraction moves only the payload JSON codec
and the empty-string helpers. The encode/decode logic and every call site's
behavior are byte-identical; no metric, span, or log name changes.

No-Regression Evidence: the moved package's own tests
(`TestMarshalPayloadSanitizesForPostgresJSONB`,
`BenchmarkMarshalPayloadSourceText`) plus the full
`storage/postgres` package test suite run green against the repointed
callers with no logic change.

## Related docs

- [Postgres storage](../../README.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
