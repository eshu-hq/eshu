# querytestutil — agent instructions

## Read first

- `doc.go` — why this package exists at all.
- `README.md` — the ownership boundary.

## Invariants

1. Every helper lives in an ordinary `.go` file and is exported. A helper in a
   `_test.go` file here is unreachable from other packages' tests, which
   defeats the package's only purpose.
2. Leaf packages only — never root `internal/query`, never a handler family,
   never a graph driver. The root/family half is enforced by the compiler: root
   imports the families for its aliases, so importing either from here is an
   import cycle and `internal/query`'s tests stop building. The package itself
   still compiles -- the cycle only materializes in the test binary of a package
   whose tests import this one, so a newly split family with no such test yet
   would compile clean and break later. Leaf dependencies such as
   `internal/status`, `internal/governanceaudit`, or `queryauth` are fine.
3. No production behavior. If production code needs it, it belongs in
   `querycontract`.
4. No `Run` or `RunSingle` call in a non-test file. `internal/queryplan`'s
   `DiscoverQueryCallsites` walks this directory like every other one under
   `internal/query`, so such a call is an unregistered production query callsite
   and fails `TestHotCypherManifestCoversEveryProductionQueryCall`. Give a fake
   that needs both methods the `FakeGraphReader` shape: each routes through an
   unexported helper rather than one calling the other.

Invariants 3 and 4 are checked, not just asserted, and the same gate enforces
the direction that keeps invariant 3 honest — a non-test file under
`internal/query` importing this package fails it, naming the file and the two
legal exits.

Invariant 2 used to read "standard library only", enforced by the same gate,
because the inventory skipped this directory and wanted a proxy for "nothing
here can reach a backend". That rule was blocking real work — `fakeStatusReader`
needs `internal/status`, `fakeGovernanceAuditAppender` needs
`internal/governanceaudit`, `fakeScopedTokenResolver` needs `queryauth` — while
the whitelist it came bundled with let a genuine graph read pass the gate in
silence, so long as it wore the self-delegation shape. Dropping the skip
retired both. Do not reintroduce either.

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
