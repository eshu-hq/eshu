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

Invariants 2 and 3 are checked, not just asserted. `internal/queryplan`'s
`DiscoverQueryCallsites` omits this package from the production query-callsite
inventory, and that omission is only sound while the package is test-only, so
the walk proves it before skipping: a non-standard-library import here, a `Run`
or `RunSingle` call anywhere but a fake's own `Run`/`RunSingle` delegating to
its receiver, a non-test file under `internal/query` importing this package, or
this directory holding no non-test Go file at all each fail
`TestHotCypherManifestCoversEveryProductionQueryCall`. A helper that genuinely
needs one of those does not belong here.

## Current state: one consumer, on purpose

`MustMapField` and `FakeGraphReader` each have exactly one consuming package
today — root `query`'s own tests. That is a real exception to the two-consumer
rule below, and it is worth understanding before you apply either.

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

## Adapting a fake without churning its callers

`FakeGraphReader` arrived with 155 root test files already constructing the
helper it replaced, using keyed literals over unexported fields. Exporting those
fields would have meant renaming every one of them.

It did not. Root keeps an unexported adapter with the original field names and
delegates its methods here, so the callers are untouched and the dispatch rules
exist once. Use that shape for the remaining shared fakes, and note the size of
what is waiting: `fakePortContentStore` has 125 consuming test files,
`openContentReaderTestDB` 85, `contentReaderQueryResult` 81,
`fakeScopedTokenResolver` 50.

An adapter is only worth having if it actually delegates. Prove it by breaking
the rule here and confirming a consuming test in the OTHER package fails. The
helper's own tests are not enough — those fail whether or not anyone delegates.

Run the WHOLE consuming suite for that proof, not `go test -run` with the tests
you expect to break. Two ways that goes wrong, both hit here:

- `-run` exits 0 when its pattern matches nothing, so a control naming a test
  that does not exist reports a pass and inverts your conclusion.
- `-run` with real names measures your filter rather than the dependency. The
  first attempt at this proof named four tests, saw four failures, and wrote
  "four root tests" into the commit and these docs. The real number is 10
  (6539 tests run, 0 failing at baseline).

`fakePortContentStore` needs more than an adapter. Its fields are typed with
unexported root read models (`repositoryEntryPointReadModel`,
`documentationFindingListReadModel`, and others), and those types have to reach a
neutral package before the fake can follow.

## Common changes

Adding a helper: confirm it is used by at least two packages' tests, OR that it
is blocking a specific family move the way `MustMapField` was. A helper that is
neither belongs in its consumer's own `_test.go`.

## Anti-patterns

- Moving a helper here "for tidiness" when only one package uses it and no
  family move is waiting on it.
- Adding a dependency on a handler family to make one helper more convenient.
