# querytestutil

## Purpose

Test helpers reachable from `internal/query` and its handler-family
subpackages. Split out during the #6060 family moves.

Two packages consume it: root `query`'s tests and
`internal/query/semanticsearch`'s, the first family to move out (#6060).

The content-reader SQL driver is the largest helper promoted so far (#6060).
Measured on the base tree with
`git grep -l '<name>' <base> -- 'go/internal/query/*.go'`, 85 root files name
`openContentReaderTestDB` and 81 name `contentReaderQueryResult`. One file in
each set is `content_reader_driver_test.go`, which declares them, so the
consumers are 84 and 80. Counts elsewhere in this file follow the same rule:
they say whether the declaring file is included.

## Ownership boundary

Owns helpers used by more than one package's tests. A helper used by exactly
one package belongs in that package's own `_test.go` file, not here.

This package owns no production behavior and must not grow any. Anything that
production code needs belongs in `querycontract`.

## Exported surface

See `doc.go` for the godoc contract.

- `MustMapField` — walks a decoded JSON/OpenAPI document one key at a time,
  failing with the offending key name.
- `FakeGraphReader` — a graph-read double satisfying the two-method read port
  handlers depend on. Dispatches on query text: incoming-edge traversals go to
  `RunIncomingFn`, the dead-code scanner's paged candidate probe is answered
  with no rows, everything else goes to `RunFn`. The zero value is usable.
- `FakeGraphReaderWithSingle` — the same port with plain dispatch (`RunFn` /
  `RunSingleFn`, no query-text routing). Do not repoint its users at
  `FakeGraphReader`; the routing would silently change what they assert.
- `RecordingResourceInvestigationGraph` — answers the four directed reads from
  installed row sets and records every `Run` query in `RunCalls`.
- `FakeRepoGraphReader` — a graph-read double for `getRepositoryContext`
  tests. Dispatches on the longest matching Cypher fragment in `RunByMatch` /
  `RunSingleByMatch`; `RunFn` / `RunSingleFn` override that entirely.
  `RunSingle` has a single-entry fallback: an unmatched narrow
  single-repository lookup (`MATCH (r:Repository {id: $repo_id})`) returns the
  sole registered row when exactly one is registered.
- `FakeWorkloadGraphReader` — the same dispatch shape for `getWorkloadContext`
  tests, deliberately without the single-entry fallback. See "Two near-duplicate
  fakes, not one type" below before touching either.
- `ScriptedRows` — canned rows satisfying the `pgstatus.Rows` surface a
  Postgres-backed store scans. `Scan` fails on an arity or type mismatch rather
  than leaving destinations zeroed, so a drifted SELECT reads as a code problem
  instead of a data problem. Not safe for concurrent use: it is a cursor.
- `WithPackageMetricReader` — installs a process-global manual-reader meter
  provider for one test and returns the reader, burning the OTel global
  delegate-once on a throwaway provider first so a handler that wrongly caches
  its meter fails deterministically instead of passing by test-file ordering.
  Callers must not call `t.Parallel()`.
- `SemanticSearchDocumentFixture` and `SemanticSearchHTTPRequest` — the curated
  search-document fixture and the envelope-Accept request builder the
  semantic-search family's tests and root's session-permission and OpenAPI
  wire-contract tests both use.
- `FakeStatusReader` — a `status.Reader` double. Returns `Err` when set,
  otherwise `Snapshot`; `ReadStatusSnapshotFiltered` ignores the selection and
  delegates to `ReadStatusSnapshot`. The zero value is usable.
- `FakeGovernanceAuditAppender` — an audit-sink double satisfying the
  single-method appender port. Records every event of every batch in call order
  into `Events`, accumulating across calls, and always reports success. A test
  covering the audit-write failure path needs its own failing double.
- `FakeScopedTokenResolver` — a scoped-token resolver double. Answers from
  `Context`, `OK`, and `Err`, and records the presented credential behind
  `Called` and `Token`. `ResolveAnswering` is the same recording with the answer
  supplied per call; root's adapter is its only caller.
- `OpenContentReaderTestDB` / `ContentReaderQueryResult` — the shared fake
  `database/sql` driver. A test queues results; each query takes the head of the
  queue, runs that result's SQL-text and bind-value assertions, and answers with
  its rows.
- `ContentReaderQueryContainsInOrder` / `ContentReaderCheckArgs` — the same two
  assertions, callable directly for a test that holds a recorded query string
  rather than a queued result.
- `ContentReaderRelationshipReadModelColumns`,
  `ContentReaderDeploymentEvidenceColumns`,
  `ContentReaderRelationshipEvidenceColumns`,
  `ContentReaderDeadCodeCandidateColumns` — the column sets of four relational
  read models, used on both sides: a test declares one to say which read it is
  answering, and the driver answers that same read with the same helper when no
  result was queued.
- `FakePortContentStore` — the content-read double. Satisfies
  `querycontract.ContentStore` plus the narrow optional ports package `query`
  type-asserts a store against (documentation read models, repository entry
  points and deployment evidence, relationship evidence, service-story target
  support). It answers from fixture slices; the zero value is usable.
- `SortEntityContentByLocation` and `FilterLanguageRepos` — the two ordering
  and grant-filtering helpers `FakePortContentStore` shares across its reads,
  exported because a family's own double needs the same ordering and the same
  grant predicate to stay consistent with production.
- `FakeDeadCodeContentStore` — the dead-code content-read double. Embeds
  `FakePortContentStore` and overrides `GetEntityContent` and
  `DeadCodeIncomingEntityIDs`. An absent key in the incoming-edge answer means
  unreachable, so omission is the contract, not a zero value. Zero value usable.

`FakePortContentStore`'s entity reads filter before they limit, matching the
production SQL's predicate order. A double that limited first would hand a test
rows the real query would not return, which is the failure mode a double exists
to avoid.

### The content-reader driver's two-tier answer

The fake answers most queries from the queue, but a handler issues incidental
reads on the way to the query under test — a readiness probe, a language rollup,
a relationship count. Those are answered with an empty row set of the right
shape and leave the queue untouched; consuming the queue for them would misalign
every later expectation.

A test that genuinely asserts on one of those reads queues a result declaring
that read's own columns, and the queued rows then win. Matching on the column
set rather than the SQL text keeps the choice in the test's hands. An empty
queue with no matching default is an error, not an empty answer, so a handler
issuing a read nobody declared fails instead of passing.

`FakeGraphReader`'s `Run` routes through an unexported `rows` helper, and its
`RunSingle` falls back to that same helper rather than calling `Run` (it still
prefers `RunSingleFn` when one is set). `FakeRepoGraphReader` and
`FakeWorkloadGraphReader` reach the same end differently, by inlining their
dispatch in each method; neither has a `rows` helper. What matters is the
result, not the shape: the package stays free of any `Run`/`RunSingle`
call expression, which is what lets `internal/queryplan`'s callsite inventory
walk this directory instead of skipping it — see the invariants below.

### Why FakeGraphReader's fields are exported

An unexported field cannot be set from another package, so a type alias would
carry the type without the ability to fill it in. The `Fn` suffix keeps the
fields from colliding with the `Run` and `RunSingle` methods.

### How root uses it without touching 154 files

Root keeps an unexported `fakeGraphReader` adapter whose fields have the old
lowercase names, and whose methods delegate to `FakeGraphReader`. 155 root files
build it with keyed literals; one of them,
`code_relationships_graph_test.go`, is where the adapter is declared, so 154
consume it and none of those 154 changed.

`FakePortContentStore` works the same way at a larger scale: root's
`fakePortContentStore` keeps the original lowercase fields for the 93 root files
that build one with a composite literal, out of 125 that name it and 124 that
consume it. Every method forwards through one `promoted()` converter. Adding a fixture field means touching that converter once rather
than each of the 41 methods.

Its move needed something `fakeGraphReader`'s did not. Twenty read models had
to reach `querycontract` first: sixteen the fake named directly or reached
through a struct field, plus four that were already exported from package
`query` but still unreachable, since a double importing package `query` back is
an import cycle against that package's own internal test files. Package `query`
keeps an alias for each, so its call sites did not change.

The dispatch rules are not duplicated in that adapter, and that is the point.
Two copies drift, and a fake that no longer matches the real port keeps passing
while guarding nothing.

The delegation is proven rather than assumed. Deleting the incoming-edge
dispatch from this package fails **10** root tests; a narrower mutation that
keeps the branch but ignores `RunIncomingFn` fails 5. Both measured by running
the whole root suite, 8324 tests, against a baseline of 0 failures.

Those two mutations are close enough to be worth distinguishing. Deleting the
branch lets an incoming-edge query fall through to `RunFn`; keeping the branch
but dropping the `RunIncomingFn` call leaves it answering no rows. Collapsing
the branch to `return nil, nil` is the SAME edit as the second, not the first,
and reports 5 rather than 10 -- two mutations that are secretly one mutation
read as corroboration and are not.

Every failure count in this file counts TOP-LEVEL test functions
(`rg -c '^--- FAIL'`), while the 8324 baseline counts `=== RUN` lines, which
include subtests. The two units differ and the gap is not small: the workload
mutation below is 40 top-level failures and 50 once failing subtests are
counted. Re-derive a failure count with the anchored pattern or it will look
like the number drifted.

The two kinds of number age differently, which is worth knowing before you
re-measure anything here. The failure counts are stable: all seven survived a
rebase that added roughly 569 tests to the root suite, because they measure the
set of tests that depend on the fake, and that set did not change. The TOTAL is
volatile and moves on almost every rebase. So the totals here are pinned to a
named `origin/main` commit rather than to "this branch's HEAD" -- a sentence
anchored to a moving ref is only true at the moment it is written, and it
falsifies itself on the next rebase, which is how three different totals ended
up in this file at once.

`FakePortContentStore` was proven the same way. Zeroing `RepositoryCoverage`
fails **12** root tests, and zeroing `DocumentationFindings` fails **4**.

The 12 spread across repository stats, story, context, branches and dead-code
investigation rather than clustering on the coverage route, which is what makes
them evidence of delegation rather than of one handler.

The `DocumentationFindings` mutation is the one worth keeping even though it is
the smaller number: those four reach the double through a type assertion onto an
unexported port declared in package `query`, so had the assertion gone false
across the new package boundary, the handlers would have taken their fallback
path and every one of them would still have passed.

One mutation found a gap rather than proving anything, which is the only kind
of evidence that finds an unguarded predicate. Dropping the `entity_type`
filter from `ListRepoEntitiesByType` failed no root test: the tests covering
that predicate build their own doubles (`boundedK8sFakeContentStore`,
`truncationFakeContentStore`, `entityContextFakeContentStore`) rather than this
one.

That gap is closed. `portcontentstore_test.go` now pins both halves of the
predicate, and each half has a bite control:

| mutation | test that goes red |
| --- | --- |
| drop the `entity_type` filter | `…ListRepoEntitiesByTypeFiltersBeforeLimit` (returns `Service` rows) |
| apply `LIMIT` to the input before filtering | same test (returns 0 rows) |
| drop the repo filter | `…ListRepoEntitiesByTypeScopesToRepo` (leaks `repo-2`) |

The ordering half is worth keeping separate from the filtering half. A double
that limited first would spend its budget on rows of the wrong type and report
truncation the database would not produce, so a caller sizing a limit against
it would draw the wrong conclusion while every type in its result still looked
correct.

Beware a mutation that only looks like it changes the order. Moving the
`len(filtered) >= limit` break above the type check is semantically identical,
because that counter still only counts rows that passed the filter — it stays
green, and it should. The mutation that actually tests the ordering truncates
the input slice before filtering.

### One port rests on a single test

`RepositoryEntryPoints` has exactly one consuming test,
`TestQueryRepoEntryPointsUsesContentRowsBeforeGraph` — `entryPoints:` is set in
exactly one root file, `repository_entry_points_test.go`.

This is not a gap in the sense the `entity_type` one was — that predicate had no
coverage through this double at all, and this has a real test that genuinely
exercises the port. It is a note about what a green suite means. A regression in
the entry-points path fails one assertion rather than a spread, so if you are
changing that path, do not read green as broad agreement; read it as one test
agreeing. Only a panic-style mutation hides this, which is why the mutation
proof used sentinel returns: a panic aborts at the first failure and reports one
either way.

Measure that set with a full `go test ./internal/query/`, never with `-run`
naming the tests you expect. `-run` measures your own filter: the first attempt
here named four tests, saw four failures, and reported four as though it were
the dependent set.

Prefer this shape for the remaining shared fakes. Renaming fields across every
consumer is the alternative, and it buys nothing the adapter does not.

### Two near-duplicate fakes, not one type

`FakeRepoGraphReader` and `FakeWorkloadGraphReader` were promoted the same
way, from `repository_context_test.go` and `workload_context_test.go`. Root
keeps the same kind of unexported adapter (`fakeRepoGraphReader`,
`fakeWorkloadGraphReader`) for each. Only the file that used to declare each
fake changed; the other 42 and 29 consuming test files are untouched.

Both counts are measured on `.go` files only, and with the `{` of a composite
literal rather than the bare name. That distinction is worth keeping: a bare
`rg fakeRepoGraphReader` returns 44 where `fakeRepoGraphReader{` returns 43,
because `workload_context_test.go` names the fake in two doc comments without
ever building one. Counting mentions instead of constructions invents a
consumer, which has produced a wrong number here more than once.

The two fakes look alike -- same fields, same longest-fragment dispatch -- but
they are separate types on purpose. `FakeRepoGraphReader.RunSingle` falls back
to a sole registered row when the narrow single-repository lookup
(`MATCH (r:Repository {id: $repo_id})`) does not match anything registered.
`FakeWorkloadGraphReader.RunSingle` has no such fallback: `getWorkloadContext`
has no equivalent single-entity lookup, and adding one would hand a workload
test a row it never registered. Unifying the two behind a shared type -- even
one gated by a flag -- would give every workload test the repository
fallback's behavior. Workload tests would keep compiling, and most would keep
passing, because their `RunSingleByMatch` maps happen to have more than one
entry or their fragments happen to match. The fallback would misfire silently
for the ones that do not.

Both delegations are proven the same way `FakeGraphReader`'s is: break the
rule in querytestutil, run the whole root suite, restore, confirm green again.

- Deleting `FakeRepoGraphReader`'s single-entry fallback fails **16** root
  tests.
- Short-circuiting `FakeWorkloadGraphReader.RunSingle`'s `RunSingleByMatch`
  dispatch to `nil, nil` fails **40** root tests.

Both measured against the same 8324-run, 0-failure baseline as every
other proof in this file, on this branch rebased onto `origin/main` 460c59481. That total is not a portable
constant: it grows as tests are added and shrinks when a family moves out of
root, as `semanticsearch` did. Re-measure rather than carrying one forward --
the counts in this file were carried forward three separate times before anyone
noticed the sections disagreed.

### FakeStatusReader follows the same shape

Root keeps an unexported `fakeStatusReader` adapter with the original lowercase
`snapshot`/`err` field names 19 test files already build with keyed literals,
and both of its methods delegate to `FakeStatusReader`. Eighteen of those 19 are
untouched. The nineteenth is `status_handler_test.go`, which is where the
adapter itself lives, so it changes by definition; measure with
`git grep -l 'fakeStatusReader{' <base> -- 'go/internal/query/*.go'`.

The delegation is proven the same way: replacing `ReadStatusSnapshot`'s
delegation with an unconditional zero-value return fails **35** root tests;
restoring it returns to 0 failures. Both measured against the same 8324-test
run of `go test ./internal/query/ -count=1 -v`, never `-run`, with the mutation
applied through `go test -overlay=` so no tracked file changed. The mutation is
built before it is run, so the failures are the guard reacting rather than a
tree that does not compile.

Earlier drafts of this section carried a 20-failure, 6539-test pair, and later a
7864-test one. Both are gone: every count in this file is now measured on the
same run, and the rule the graph-read fakes above state applies here too.

### Adapting a fake that holds state

`FakeGraphReader` is adapted by rebuilding it from the adapter's funcs on each
call, which works because it holds nothing between calls. The other two do hold
state, and each needs a different answer.

`fakeGovernanceAuditAppender` keeps its `events` slice, which root tests read by
that name (`git grep -c '\.events' <base> -- 'go/internal/query/*.go'` sums to
182 occurrences), and its `Append` copies the
slice into the shared double, delegates, and takes back what was recorded. The
adapter decides nothing about which events land or whether the write succeeds.

`fakeScopedTokenResolver` cannot copy: its recorded call sits behind a mutex,
and one resolver instance is shared across parallel subtests. So the adapter
holds a `FakeScopedTokenResolver` and routes through `ResolveAnswering`, passing
its own `context`/`ok`/`err` as arguments. Writing them into the delegate per
call would be the concurrent write the mutex exists to prevent. On the base tree
52 root files build the adapter with keyed literals
(`git grep -l 'fakeScopedTokenResolver{' <base> -- 'go/internal/query/*.go'`).
Forty-nine are untouched. The other three -- `auth_test.go`, which also declares
the adapter, plus `auth_denial_reason_audit_test.go` and
`auth_headerless_bypass_test.go` -- read the recorded call and gained
parentheses, `resolver.called` becoming `resolver.called()`, because reading it
takes the same lock the recording does. Those five call sites are the whole
consumer cost.

### The same shape, applied to the content-reader driver

`ContentReaderQueryResult` needed one extra step. Callers pass a **slice** of it
to `openContentReaderTestDB`, and 80 root test files build those elements with
keyed literals over lowercase field names. A type alias cannot help there
either: the shared fields have to be exported to be settable from another
package, so an alias would rename every one of those literals.

Root keeps its own unexported `contentReaderQueryResult` with the original field
names, converts the slice element by element, and delegates. No consuming test
file changed. The dispatch — the default answers, the queue, the assertions —
lives only here.

Proven the same way: deleting the default-answer dispatch from this package
fails **16** root tests; keeping the defaults but not consuming the queue head
fails **26**. Both measured with a full `go test ./internal/query/ -count=1 -v`,
8324 tests run, against a baseline of 0 failures -- the same 8324-test run the
`FakeStatusReader` proof above cites, so every count in this file comes from one
base rather than from whichever base its section was written on.

One test leaves package `query` here rather than failing:
`TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking` moves into
this package alongside the function it covers, so root declares one fewer test
than it did before this change.

## Dependencies

The standard library, plus any LEAF package a fake genuinely needs to name the
types it stands in for -- `internal/status`, `internal/governanceaudit` and
`queryauth` are the shape, not the list. Deliberately stated as a rule rather
than an inventory: each fake promoted here turns another leaf from allowed into
taken, and a sentence enumerating today's imports is stale the next time one
lands. Run `rg -l 'eshu-hq/eshu' --glob '*.go' --glob '!*_test.go' .` if you
want the current set; do not transcribe it here.

Root `internal/query` and the handler families are not. Both bans are real,
but they are not enforced the same way and neither stops `go build`:

- Root cycles as soon as root's own tests build, because root's tests import
  this package. `go build ./internal/query/querytestutil` still succeeds on
  its own, so a green build proves nothing here.
- A handler family compiles clean. A cycle appears only if that family's
  INTERNAL test package imports this one back; an external `_test` package
  never cycles at all.

`AGENTS.md` invariant 2 has the measured breakdown of all three shapes. Do
not settle the question with a green build.

A graph driver has no business here either. A fake answers from funcs a test
installs; if it needs a driver it is not a fake.

## Telemetry

None. Test-only package.

## Gotchas / invariants

Helpers here MUST live in ordinary `.go` files and be exported. Moving one into
a `_test.go` file makes it unreachable from every other package's tests and
silently undoes the split — the compiler reports it as an ordinary undefined
symbol in the consuming package, not as a packaging mistake.

This package is intended for tests only, and part of that is enforced rather
than observed. `internal/queryplan`'s `DiscoverQueryCallsites` walks this
directory like every other one under `internal/query`, so a `Run` or `RunSingle`
call landing in a non-test file here is an unregistered production query
callsite and fails
`TestHotCypherManifestCoversEveryProductionQueryCall` with the file named. The
fakes stay clear of that by routing `Run` and `RunSingle` through an unexported
helper, so the package holds no such call at all.

It did not always work that way. The inventory used to skip this directory,
because a fake whose `RunSingle` answers by calling `Run` looks exactly like a
production read to a syntactic walk. Paying for that skip meant a second set of
rules — standard-library imports only, and `Run`/`RunSingle` reached only from a
fake's own `Run`/`RunSingle` delegating to its receiver. Both were proxies for
"this cannot reach a backend", and both were wrong in a way that mattered: the
stdlib rule blocked fakes that legitimately need `internal/status`,
`internal/governanceaudit`, or `queryauth`, while the self-delegation whitelist
let a genuine graph read wearing that exact shape pass the gate in silence.
Removing the call removed the need for either.

The other enforced half is direction: no non-test file under `internal/query`
may import this package, because production code must not depend on test
doubles. Note its scope. It covers the tree the inventory walks, which is where
an importer would realistically appear, but this package sits under
`go/internal`, so anything under `go/` could import it and only convention stops
a package outside `internal/query` from doing so.

While that holds, the linker drops this package from production binaries. That
is a consequence of the invariant rather than a guarantee on its own: a
production import would quietly pull `testing` into a shipped binary, so it is
a defect to fix, not a fact to document.

## Related docs

`go/internal/query/README.md` for the family-split contract.

## Performance and observability

No-Regression Evidence: this package is a test double and runs only inside test
binaries -- `internal/queryplan`'s callsite inventory walks it and records zero
`Run`/`RunSingle` call expressions, so nothing here is on a production query or
graph path. `FakeRepoGraphReader` and `FakeWorkloadGraphReader` trip the
perf-evidence gate on the word "Cypher" in their doc comments, which describe
which Cypher FRAGMENT a caller registers, not a query this code issues. The
promotion is a move: the dispatch bodies came across from
`repository_context_test.go` and `workload_context_test.go` unchanged, so the
dispatch work per call is identical; the adapter adds one struct construction
per call, in test binaries only. Root suite: 8324 `=== RUN`, 0 `--- FAIL`, on
this branch rebased onto `origin/main` 460c59481.

The base-side figure that used to sit here was measured against 94197f893, five
commits earlier, one of which moved a test between packages. Only the
measurement that reproduces at the stated commit is kept. The delta it described,
`TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking` moving
into this package with the function it covers, as above. That total is not a
portable constant -- it moves in both directions as tests are added and as
families move out of root.

No-Regression Evidence: for the governance-audit and scoped-token fakes
promoted alongside them, this package is a test double and runs only inside test
binaries, so nothing here sits on a production query, graph, queue or HTTP path.
`FakeGovernanceAuditAppender` and `FakeScopedTokenResolver` trip the
perf-evidence gate on their doc comments, which name the governance-audit and
scoped-token surfaces they stand in for rather than any work this code performs.
The promotion is a move: the append and resolve bodies came across from
`auth_test.go` unchanged and the root package keeps an unexported adapter that
delegates rather than reimplements, so the work per call is identical to before
and every consumer call site is untouched.

No-Regression Evidence: the flagged hot file is `go/internal/query/catalog.go`,
which does issue a real Cypher `MATCH` against the graph. This change does not
touch that query. The only edit there turns `CatalogWorkloadIdentityEntry` from
a struct declaration into a type ALIAS onto `querycontract`, so the shared
`FakePortContentStore` can name it from outside the root package. An alias
preserves type identity, so no conversion, copy, or extra allocation is
introduced on the row-decoding path; the query text, its parameters, and the
decode loop are byte-identical. The same shape applies to the other read models
promoted to `querycontract` in this change.

`FakePortContentStore` itself is a test double and runs only inside test
binaries, so it sits on no production path at all. Its promotion is a move: the
method bodies came across from `ports_test.go` unchanged and the root package
keeps an unexported adapter that delegates rather than reimplements, so the work
per call is identical.

Measured rather than asserted: 126 files under `internal/query` name
`fakePortContentStore`, and this change touches 4 of them -- `ports_test.go`
plus the two files its method bodies were split into, and one delegating method
in `service_story_target_support_test.go`. Those are the fake's DEFINITION
sites. The remaining 122, which are the call sites, are untouched.

No-Observability-Change: no metric, span, log, or status surface is touched. A
test double deliberately emits no telemetry -- one that produced spans would
pollute the traces of whatever it stands in for.
