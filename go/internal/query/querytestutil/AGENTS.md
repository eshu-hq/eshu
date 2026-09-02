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

## Current state: one consumer, on purpose

`MustMapField` has exactly one consuming package today — root `query`'s own
tests. That is a real exception to the two-consumer rule below, and it is worth
understanding before you apply either.

The rule exists to stop helpers migrating here for tidiness. This helper is here
for a different reason: it is a precursor to the #6060 family split, and the
split cannot happen without it. A helper declared in a `_test.go` file is not
part of the importable package, so the first family subpackage to move its tests
would fail to compile on a missing symbol that has nothing to do with the code
being moved. Landing the move separately keeps that mechanical 67-file rename
out of the diff that actually splits a family.

Once the first family lands, the second consumer exists and this note stops
being an exception. Until then, do not cite this package as precedent for moving
a single-consumer helper.

## Common changes

Adding a helper: confirm it is used by at least two packages' tests, OR that it
is blocking a specific family move the way `MustMapField` was. A helper that is
neither belongs in its consumer's own `_test.go`.

## Anti-patterns

- Moving a helper here "for tidiness" when only one package uses it and no
  family move is waiting on it.
- Adding a dependency on a handler family to make one helper more convenient.
