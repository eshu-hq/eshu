# Query decode errors

## Purpose

One error type. It marks a single fact as unusable for the response being built,
carrying the fact kind, the fact id, the missing field, and the classification,
so a read path can drop that row instead of silently returning a row of empty
strings.

## Ownership boundary

This package owns the query layer's decode-failure shape. It does not decode
anything itself, does not quarantine facts, and does not know about read models.
The query layer is a read path, so unlike the projector it never quarantines a
durable fact record; it classifies one decoded row as unusable.

## Exported surface

`Error` and `New`, described in [doc.go](doc.go).

## Dependencies

The Go standard library plus `sdk/go/factschema`, for the
`*factschema.DecodeError` this type wraps and the `ClassificationInputInvalid`
constant it defaults to.

That dependency is why this is its own package rather than part of
`querycontract`. A family imports `querycontract` for types and should not
inherit anything else through it; the same reasoning put the handler span in
`queryspan`.

## Telemetry

No-Observability-Change: this package emits no metric, span, or log. It builds a
value. Callers decide whether a decode drop is logged, and at what level.

## Gotchas / invariants

`Error()` and `Unwrap()` are exported, and that is load-bearing for the root
package. Package `query` keeps `type queryDecodeError = querydecode.Error`, and
a type alias reaches a type's exported methods but **not** its unexported ones
across a package boundary. Had these been named `error()` and `unwrap()`, all 73
existing references in root would have needed rewriting, which is exactly what
`RepositoryAccessFilter` cost in the same epic.

`Unwrap` must keep returning the underlying `*factschema.DecodeError`, because
callers reach its `ErrUnsupportedSchemaMajor` sentinel through `errors.Is` and
`errors.As`. Returning a wrapper, or nil, breaks that silently: the code still
compiles and the sentinel simply stops matching.

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
