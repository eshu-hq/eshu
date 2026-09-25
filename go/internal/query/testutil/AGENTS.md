# testutil — agent instructions

## Read first

- `doc.go` — why this package exists at all.
- `README.md` — the ownership boundary and the Nested leaves section.
- For the content-read or graph-read fakes specifically:
  `content/AGENTS.md` / `graph/AGENTS.md`.

## Invariants

1. Every helper lives in an ordinary `.go` file and is exported. A helper in a
   `_test.go` file here is unreachable from other packages' tests, which
   defeats the package's only purpose.
2. Leaf packages only — never root `internal/query`, never a handler family,
   never a graph driver. Leaf dependencies such as `internal/status`,
   `internal/governanceaudit`, or `auth` are fine.

   Only ONE of those three bans has a compiler backstop, and knowing which
   matters more than the rule itself:

   - **Root** is caught, but only in a test binary. Root's own tests import
     this package, so importing root from here is a cycle and
     `internal/query`'s tests stop building. This package still compiles on
     its own -- `go build ./internal/query/testutil` succeeds -- so a
     `go build` will not tell you.
   - **A handler family is NOT caught**, and is caught even less often than
     it first appears. Importing `registry` (the package-registry family,
     `internal/query/package/registry`) from here compiles fine: root
     and this package merely share that dependency, which is not a cycle. A
     cycle needs that family's tests to import this package back, and only
     the INTERNAL test package triggers it. Measured, all three legs planted:

     | plant | result |
     | --- | --- |
     | this package imports `registry` | exit 0, no cycle |
     | plus `package registry` (internal) importing this one | exit 1, `import cycle not allowed in test` |
     | plus `package registry_test` (external) importing this one | exit 0, no cycle |

     So whether the ban bites depends on how the family writes its tests, and
     on whether they use this package at all:

     - Family tests are INTERNAL and import this package -> caught. This is
       the normal case for a family that needs the shared fakes, which is the
       reason this package exists. The `semanticsearch` move hit exactly this:
       its in-package tests import `testutil`, so promoting its
       index-store fake here was rejected by the compiler, not by review.
     - Family tests are INTERNAL and do not import this package -> not
       caught. The package-registry family (`registry`) is in this state
       today: 22 of 22 test files are `package registry`, none importing
       this one.
     - Family tests are EXTERNAL (`package registry_test`) -> never caught,
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
   `DiscoverQueryCallsites` walks this directory and its nested leaves like
   every other one under `internal/query`, so such a call is an unregistered
   production query callsite and fails
   `TestHotCypherManifestCoversEveryProductionQueryCall`. The rule is the
   absence of that call expression, not any particular shape. See
   `testutil/graph/AGENTS.md` for how the three graph-read fakes there
   satisfy it two different ways.

Invariants 3 and 4 are checked, not just asserted, and the same gate enforces
the direction that keeps invariant 3 honest — a non-test file under
`internal/query` importing this package fails it, naming the file and the two
legal exits.

Invariant 2 used to read "standard library only", enforced by the same gate,
because the inventory skipped this directory and wanted a proxy for "nothing
here can reach a backend". That rule was blocking real work — `fakeStatusReader`
needs `internal/status`, `fakeGovernanceAuditAppender` needs
`internal/governanceaudit`, `fakeScopedTokenResolver` needs `auth` — while
the whitelist it came bundled with let a genuine graph read pass the gate in
silence, so long as it wore the self-delegation shape. Dropping the skip
retired both. Do not reintroduce either. The latter two fakes landed here as
soon as it did, and their imports are the proof the rule was the blocker.

## Two consumers, as designed

Root `query`'s tests and `internal/query/semanticsearch`'s both import this
package. The first family move (#6060) landed, so the earlier
"one consumer, on purpose" exception is spent — apply the two-consumer rule
below as written.

`MustMapField` now has both consumers -- `semanticsearch`'s
`semantic_search_language_test.go` calls it. `graph.FakeGraphReader` still has
only root. That is fine: it was landed as a precursor to the split, not as
precedent for moving a single-consumer helper here for tidiness.

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
(`semantic_search_language_wire_contract_test.go`) -- matching the registry
family's precedent (`package_registry_family_test_doubles_test.go`, #6399). Count it with
`rg -n 'stubSemanticSearchIndex|runSemanticSearch\(' go/internal/query/*_test.go`
rather than from this sentence; the scoped-token admission test is NOT one of
them, since it uses `fakeScopedTokenResolver` and never touches the stub.

Check this before promising a promotion: the split is decided by whether the
fixture's signature can avoid the family's types, not by how much duplication
you would like to remove.

## Adapting a fake without churning its callers

`graph.FakeGraphReader` arrived with 155 root files already constructing the
helper it replaced, using keyed literals over unexported fields; one of the
155 declares the adapter, so 154 are callers. Exporting those fields would
have meant renaming every one of them.

It did not. Root keeps an unexported adapter with the original field names and
delegates its methods here, so the callers are untouched and the dispatch rules
exist once. Use that shape for the remaining shared fakes.

`fakeGovernanceAuditAppender` (19 root files build it) and
`fakeScopedTokenResolver` (52) followed the same shape, with one wrinkle each.
Both counts include `auth_test.go`, which declares the two adapters. Both hold state between
calls, where `graph.FakeGraphReader` holds none, so neither adapter can be
rebuilt from its fields on every call the way `fakeGraphReader` is. The
appender copies its slice into the delegate and takes back what was recorded.
The resolver cannot copy at all — a mutex guards the call it records — so it
holds a `FakeScopedTokenResolver` and passes its answer to `ResolveAnswering`
per call. That entry point exists for exactly this: an adapter that assigned
the answer into the delegate would introduce the concurrent write the mutex
prevents.

Reading state through a lock is the one thing an adapter cannot hand back as a
field. Three root files now say `resolver.called()` where they said
`resolver.called`. That is the whole consumer cost of both promotions.

For the content-read fakes (`FakePortContentStore`, `FakeDeadCodeContentStore`)
and the content-reader SQL driver, see `content/AGENTS.md` — each needed its
own extra step before it could move.

If you find yourself editing consuming test files while moving a fake, the
shape is wrong. Go back to the adapter.

One narrow exception, and only this one: state the adapter cannot hand back as
a plain field. `fakeScopedTokenResolver.called` became `resolver.called()` in
three files because reading the recorded call has to take the lock that guards
the recording, and a method is the only way to do that. The test for whether an
edit qualifies is that no adapter shape could avoid it -- not that it was
inconvenient. Renaming a field, changing a keyed literal, or touching a call
site to fit a signature you chose does not qualify, and that is the churn the
rule above exists to stop.

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
  (8324 tests run, 0 failing at baseline).

## Common changes

Adding a helper: confirm it is used by at least two packages' tests, OR that it
is blocking a specific family move the way `MustMapField` was. A helper that is
neither belongs in its consumer's own `_test.go` file.

### Re-measuring the mutation proof

The delegation evidence across this file and the leaves cites failure counts
with two different units and two different stabilities — reproduce them
exactly or the numbers will look drifted:

- Every failure count is TOP-LEVEL test functions (`rg -c '^--- FAIL'`),
  while the run totals are `=== RUN` lines, which include subtests. The gap
  is not small: the workload mutation is 40 top-level failures and 50 once
  failing subtests are counted. Re-derive with the anchored pattern.
- Failure counts are stable across rebases (they measure the set of tests
  that depend on the fake, which does not change); run totals are volatile
  and move on almost every rebase. Totals stay pinned to a named
  `origin/main` commit, never to a moving ref.
- Measure the dependent set with a full `go test ./internal/query/`, never
  with `-run` naming the tests you expect: `-run` measures your own filter
  (the first attempt here named four tests, saw four failures, and reported
  four as though it were the dependent set).

## Anti-patterns

- Moving a helper here "for tidiness" when only one package uses it and no
  family move is waiting on it.
- Adding a dependency on a handler family to make one helper more convenient.
- Merging `graph.FakeRepoGraphReader` and `graph.FakeWorkloadGraphReader` into
  one type with a flag for the difference — see
  `testutil/graph/AGENTS.md`'s "Near-duplicate fakes are not one type".
