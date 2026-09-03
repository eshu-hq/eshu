# querytestutil — agent instructions

## Read first

- `doc.go` — why this package exists at all.
- `README.md` — the ownership boundary.

## Invariants

1. Every helper lives in an ordinary `.go` file and is exported. A helper in a
   `_test.go` file here is unreachable from other packages' tests, which
   defeats the package's only purpose.
2. Leaf packages only — never root `internal/query`, never a handler family,
   never a graph driver. Leaf dependencies such as `internal/status`,
   `internal/governanceaudit`, or `queryauth` are fine.

   Only ONE of those three bans has a compiler backstop, and knowing which
   matters more than the rule itself:

   - **Root** is caught, but only in a test binary. Root's own tests import
     this package, so importing root from here is a cycle and
     `internal/query`'s tests stop building. This package still compiles on
     its own -- `go build ./internal/query/querytestutil` succeeds -- so a
     `go build` will not tell you.
   - **A handler family is NOT caught**, and is caught even less often than
     it first appears. Importing `packagereg` from here compiles fine: root
     and this package merely share that dependency, which is not a cycle. A
     cycle needs that family's tests to import this package back, and only
     the INTERNAL test package triggers it. Measured, all three legs planted:

     | plant | result |
     | --- | --- |
     | this package imports `packagereg` | exit 0, no cycle |
     | plus `package packagereg` (internal) importing this one | exit 1, `import cycle not allowed in test` |
     | plus `package packagereg_test` (external) importing this one | exit 0, no cycle |

     So whether the ban bites depends on how the family writes its tests, and
     on whether they use this package at all:

     - Family tests are INTERNAL and import this package -> caught. This is
       the normal case for a family that needs the shared fakes, which is the
       reason this package exists. The `semanticsearch` move hit exactly this:
       its in-package tests import `querytestutil`, so promoting its
       index-store fake here was rejected by the compiler, not by review.
     - Family tests are INTERNAL and do not import this package -> not
       caught. `packagereg` is in this state today: 22 of 22 test files are
       `package packagereg`, none importing this one.
     - Family tests are EXTERNAL (`package packagereg_test`) -> never caught,
       even when they do import this package.

     Do not read the first case as the rule. Two of the three shapes compile
     clean, and the one that catches you does so only because a family
     happened to need a fake from here.
   - **A graph driver is NOT caught.** It was, transitively, while the
     stdlib-only import rule stood; that rule is gone. `countQueryCalls`
     matches the selector names `Run` and `RunSingle` only, so a real read
     issued through a differently-named method is invisible to it.

   Two of the three are enforced by review, not by tooling. Treat them as
   rules you have to hold yourself to.
3. No production behavior. If production code needs it, it belongs in
   `querycontract`.
4. No `Run` or `RunSingle` call in a non-test file. `internal/queryplan`'s
   `DiscoverQueryCallsites` walks this directory like every other one under
   `internal/query`, so such a call is an unregistered production query callsite
   and fails `TestHotCypherManifestCoversEveryProductionQueryCall`. The rule is
   the absence of that call expression, not any particular shape, and the three
   fakes here satisfy it two different ways: `FakeGraphReader` routes both
   methods through an unexported `rows` helper, while `FakeRepoGraphReader` and
   `FakeWorkloadGraphReader` inline their dispatch in each method. Either is
   fine. What a new fake must not do is have one of the two methods call the
   other.

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

## Two consumers, as designed

Root `query`'s tests and `internal/query/semanticsearch`'s both import this
package. The first family move (#6060) landed, so the earlier
"one consumer, on purpose" exception is spent — apply the two-consumer rule
below as written.

`MustMapField` and `FakeGraphReader` still have only root as a consumer. That
is fine: they were landed as precursors to the split, not as precedent for
moving a single-consumer helper here for tidiness.

## A fixture that names a family type cannot live here

This is the practical consequence of invariant 2's first case above, stated as
a decision rule. Where that case applies, a fixture whose type signature names
a family type cannot be shared at all, and the consuming package declares its
own double instead.

That is why the semantic-search move promoted `SemanticSearchDocumentFixture`
and `SemanticSearchHTTPRequest` — which name only `internal/searchdocs` and
`querycontract` — but left the index-store fake behind: it implements
`semanticsearch.SemanticSearchIndexStore`. Root declares
`stubSemanticSearchIndex` in its own `_test.go` for the five root tests that
drive that route -- the four in the session-permission sweep
(`session_permission_enforcement_test.go`, all reaching it through
`runSemanticSearch`) and the OpenAPI `languages` wire-contract test
(`semantic_search_language_wire_contract_test.go`) -- matching the `packagereg`
precedent (`package_registry_family_test_doubles_test.go`, #6399). Count it with
`rg -n 'stubSemanticSearchIndex|runSemanticSearch\(' go/internal/query/*_test.go`
rather than from this sentence; the scoped-token admission test is NOT one of
them, since it uses `fakeScopedTokenResolver` and never touches the stub.

Check this before promising a promotion: the split is decided by whether the
fixture's signature can avoid the family's types, not by how much duplication
you would like to remove.

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

## Near-duplicate fakes are not one type

`FakeRepoGraphReader` and `FakeWorkloadGraphReader` promoted the same way
`FakeGraphReader` did, from `repository_context_test.go` and
`workload_context_test.go`. They dispatch the same way — longest matching
Cypher fragment wins in `RunByMatch`/`RunSingleByMatch`, `RunFn`/`RunSingleFn`
override everything — and it is tempting to fold them into one type, maybe
with a bool field for the difference.

Do not. `FakeRepoGraphReader.RunSingle` has a fallback the workload fake does
not: when no fragment matches, the cypher is the narrow single-repository
lookup (`MATCH (r:Repository {id: $repo_id})`), and exactly one row is
registered, it returns that row. `FakeWorkloadGraphReader.RunSingle` returns
nil in the same situation, on purpose — `getWorkloadContext` has no
single-entity lookup for a fallback to stand in for. Unifying the two types
would give every workload test the repository fallback too. Workload tests
would keep compiling, and most would keep passing, because most workload
`RunSingleByMatch` maps have more than one entry or their fragments actually
match — the fallback would only misfire for the ones that do not, silently
handing back an unregistered row instead of nil.

Both delegations are proven the same way `FakeGraphReader`'s was: break the
rule in querytestutil, run the whole root suite, restore it, confirm green
again.

- Deleting the single-entry `RunSingle` fallback from `FakeRepoGraphReader`
  fails **16** root tests.
- Deleting the `RunSingleByMatch` dispatch from `FakeWorkloadGraphReader`'s
  `RunSingle` (short-circuiting to `nil, nil`) fails **40** root tests.

Both measured at this branch's HEAD: 7792 tests run, 0 failing. That is not the
6539 of `FakeGraphReader`'s earlier proof -- the rebase moved it, and neither
number is portable. Restore the file and re-run the baseline before trusting
either number — a proof that leaves the break in place is not a proof of
anything else in the suite.

## Common changes

Adding a helper: confirm it is used by at least two packages' tests, OR that it
is blocking a specific family move the way `MustMapField` was. A helper that is
neither belongs in its consumer's own `_test.go`.

## Anti-patterns

- Moving a helper here "for tidiness" when only one package uses it and no
  family move is waiting on it.
- Adding a dependency on a handler family to make one helper more convenient.
- Merging two near-identical fakes into one type with a flag for the
  difference. If you find yourself doing this to `FakeRepoGraphReader` and
  `FakeWorkloadGraphReader`, stop — see "Near-duplicate fakes are not one
  type" above.
