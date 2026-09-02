# querytestutil — agent instructions

## Read first

- `doc.go` — why this package exists at all.
- `README.md` — the ownership boundary.

## Invariants

1. Every helper lives in an ordinary `.go` file and is exported. A helper in a
   `_test.go` file here is unreachable from other packages' tests, which
   defeats the package's only purpose.
2. This package imports `testing` and standard library only. It MUST NOT import
   `internal/query` or any handler family — root imports the families for its
   aliases, so such an import re-creates the cycle.
3. No production behavior. If production code needs it, it belongs in
   `querycontract`.

## Common changes

Adding a helper: confirm it is used by at least two packages' tests first. A
single-consumer helper belongs in that consumer's own `_test.go`.

## Anti-patterns

- Moving a helper here "for tidiness" when only one package uses it.
- Adding a dependency on a handler family to make one helper more convenient.
