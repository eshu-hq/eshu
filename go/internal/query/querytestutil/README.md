# querytestutil

## Purpose

Test helpers reachable from `internal/query` and, once they exist, its
handler-family subpackages. Split out during the #6060 family moves.

Today there is exactly one consuming package, root `query`'s tests. See
`AGENTS.md` for why a single-consumer helper lives here and why that is not
precedent for the next one.

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

This package is intended for tests only, and nothing outside a test imports it
today (`rg -l 'query/querytestutil' --glob '*.go' --glob '!*_test.go'` returns
nothing). While that holds, the linker drops it from production binaries. That
is a consequence of the invariant rather than a guarantee on its own: a
production import would quietly pull `testing` into a shipped binary, so treat
one as a defect to fix, not as a fact to document.

## Related docs

`go/internal/query/README.md` for the family-split contract.
