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

## Gotchas / invariants

`Error()` and `Unwrap()` are exported, and that is load-bearing for the root
package. Package `query` keeps `type queryDecodeError = decode.Error`, and
a type alias reaches a type's exported methods but **not** its unexported ones
across a package boundary. Had these been named `error()` and `unwrap()`, all 73
existing references in root would have needed rewriting, which is exactly what
`RepositoryAccessFilter` cost in the same epic.

`Unwrap` must keep returning the underlying `*factschema.DecodeError`, because
callers reach its `ErrUnsupportedSchemaMajor` sentinel through `errors.Is` and
`errors.As`. Returning a wrapper, or nil, breaks that silently: the code still
compiles and the sentinel simply stops matching.

## Move evidence (#6642 Part D)

This package renamed in place from `go/internal/query/querydecode` to
`go/internal/query/decode` per `docs/internal/naming.md` rules 1 and 4: the
`query` prefix on a subpackage that already lives under `query/` is stutter.
Only the package clause, the doc comment, and its eight importers' import
paths differ (`error.go`/`error_test.go` were already destuttered, so no
file rename was needed). The exported surface (`Error`, `New`) carried no
leading `Decode`/`Query` word, so nothing needed destuttering there. No
decode behavior, error classification, or wrapped-error semantics changed.

## No-Regression Evidence

Baseline `origin/main` at the move vs this branch: `go build ./...` and
`go test ./internal/query/... -count=1` pass with 0 failures. The decode
paths that exercise this package live in the root package's
`factschema_decode_*_test.go` files, `internal/query/incident/store`,
`internal/query/supplychain/{advisory,impact}`, and
`internal/query/packagereg`, all exercising this leaf through their existing
importers unchanged. The test-name set under the leaf
(`go test ./internal/query/decode/... -list '.*'`) is identical to
`./internal/query/querydecode/...` at `origin/main`.

## No-Observability-Change

This package emits no metric, span, or log of its own; it is a pure
in-process error-value leaf. No new runtime behavior, so no new telemetry.

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
