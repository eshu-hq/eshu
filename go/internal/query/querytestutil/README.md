# querytestutil

## Purpose

Test helpers shared between `internal/query` and its handler-family
subpackages. Split out during the #6060 family moves.

## Ownership boundary

Owns helpers used by more than one package's tests. A helper used by exactly
one package belongs in that package's own `_test.go` file, not here.

This package owns no production behavior and must not grow any. Anything that
production code needs belongs in `querycontract`.

## Exported surface

See `doc.go` for the godoc contract.

- `MustMapField` — walks a decoded JSON/OpenAPI document one key at a time,
  failing with the offending key name.

## Dependencies

`testing` only. That is deliberate: a shared test helper that imports
`internal/query` would re-create the import cycle this package exists to avoid,
because root imports every family for its compatibility aliases.

## Telemetry

None. Test-only package.

## Gotchas / invariants

Helpers here MUST live in ordinary `.go` files and be exported. Moving one into
a `_test.go` file makes it unreachable from every other package's tests and
silently undoes the split — the compiler reports it as an ordinary undefined
symbol in the consuming package, not as a packaging mistake.

Nothing outside a test imports this package, so it is dropped from production
binaries by the linker.

## Related docs

`go/internal/query/README.md` for the family-split contract.
