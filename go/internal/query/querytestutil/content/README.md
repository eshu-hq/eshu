# querytestutil/content

## Purpose

The content half of `querytestutil`, nested under it for #6642: the
content-read test doubles. The fake `database/sql` driver
(`OpenReaderTestDB`, `ReaderQueryResult`, the query assertions, and the column
helpers) peeled out to `internal/testutil/contentreader` for #6818 move 5.
See `doc.go` for the godoc contract and `../README.md` for the consumer counts
and split history that stay with the parent.

## Ownership boundary

Same as the parent: helpers used by more than one package's tests, and no
production behavior. A helper used by one package belongs in that package's own
`_test.go` file.

## Exported surface

See `doc.go` for the godoc contract. The fake `database/sql` driver and its
column helpers now live in `internal/testutil/contentreader` (see its README);
what remains here are the store doubles.

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

### How root uses FakePortContentStore without touching 124 files

`FakePortContentStore` works the same way `graph.FakeGraphReader` does, at a
larger scale: root's `fakePortContentStore` keeps the original lowercase fields
for the 93 root files that build one with a composite literal, out of 125 that
name it and 124 that consume it. Every method forwards through one `promoted()`
converter. Adding a fixture field means touching that converter once rather
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

Failure counts below are top-level test functions; run totals are `=== RUN`
lines pinned to a named `origin/main` commit. The re-measurement protocol is
in AGENTS.md under Common changes, and in the parent `../AGENTS.md`.

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

That gap is closed. `content/port_store_test.go` now pins both halves of the
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

Measure that set with a full `go test ./internal/query/` per the parent
AGENTS.md re-measurement protocol, never with `-run` naming the tests you
expect.

### The same shape, applied to the content-reader driver

`ReaderQueryResult` needed one extra step. Callers pass a **slice** of it to
`openContentReaderTestDB`, and 80 root test files build those elements with
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
`FakeStatusReader` proof in the parent README cites, so every count in this
file comes from one base rather than from whichever base its section was
written on.

One test leaves package `query` here rather than failing:
`TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking` moves into
this package alongside the function it covers, so root declares one fewer test
than it did before this change.

## Dependencies

Leaf packages only, as the parent's invariant 2 states. This package does not
import the parent `querytestutil` or its sibling leaf.

## Telemetry

None. A test double deliberately emits no telemetry.

## Gotchas / invariants

A non-test file under `internal/query` importing this package fails
`internal/queryplan`'s callsite inventory, the same as an import of the parent.

## Related docs

`../README.md`, `../AGENTS.md`.

## Performance and observability

No-Regression Evidence: the base-side figure that used to sit in the parent
README was measured against 94197f893, five commits earlier, one of which
moved a test between packages. Only the measurement that reproduces at the
stated commit is kept. The delta it described,
`TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking` moving
into this package with the function it covers, is recorded above. That total is
not a portable constant -- it moves in both directions as tests are added and
as families move out of root.

No-Regression Evidence: the flagged hot file is
`go/internal/query/repository/catalog.go` (moved from
`go/internal/query/catalog.go` for #6060 lane-B B3), which does issue a real
Cypher `MATCH` against the graph. This change does not touch that query. The
only edit there turns `CatalogWorkloadIdentityEntry` from a struct declaration
into a type ALIAS onto `querycontract`, so `FakePortContentStore` can name it
from outside the root package. An alias preserves type identity, so no
conversion, copy, or extra allocation is introduced on the row-decoding path;
the query text, its parameters, and the decode loop are byte-identical. The
same shape applies to the other read models promoted to `querycontract`
alongside it.

`FakePortContentStore` itself is a test double and runs only inside test
binaries, so it sits on no production path at all. Its promotion is a move: the
method bodies came across from `ports_test.go` unchanged and the root package
keeps an unexported adapter that delegates rather than reimplements, so the
work per call is identical.

Measured rather than asserted: 126 files under `internal/query` name
`fakePortContentStore`, and this change touches 4 of them -- `ports_test.go`
plus the two files its method bodies were split into, and one delegating method
in `service_story_target_support_test.go`. Those are the fake's DEFINITION
sites. The remaining 122, which are the call sites, are untouched.

No-Observability-Change: no metric, span, log, or status surface is touched. A
test double deliberately emits no telemetry -- one that produced spans would
pollute the traces of whatever it stands in for.
