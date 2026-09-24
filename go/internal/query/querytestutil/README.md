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

## Nested leaves

Two packages nest under this one (#6642):

- `content/` — the content-read test doubles (`FakePortContentStore`,
  `FakeDeadCodeContentStore`, and related fixtures). The fake `database/sql`
  driver peeled out to `internal/testutil/contentreader` for #6818 move 5.
  See `content/README.md` and `content/AGENTS.md`.
- `graph/` — the graph-read test doubles (`FakeGraphReader`,
  `FakeGraphReaderWithSingle`, `FakeRepoGraphReader`, `FakeWorkloadGraphReader`)
  and the #6786 NornicDB Cypher-shape guards
  (`AssertCypherHasNoBrokenAndOr`, `AssertCypherHasNoIgnoredLabelPredicate`).
  See `graph/README.md` and `graph/AGENTS.md`.

Every invariant in this file and in `AGENTS.md` applies to both leaves: no
production behavior, helpers live in ordinary (non-`_test.go`) files, no
`Run`/`RunSingle` call in a non-test file, and no non-test file under
`internal/query` may import this package or either leaf.
`internal/queryplan`'s production-import guard matches `querytestutil` as an
import-path element, so it covers `querytestutil/content` and
`querytestutil/graph` the same way it covers this package.

## Ownership boundary

Owns helpers used by more than one package's tests. A helper used by exactly
one package belongs in that package's own `_test.go` file, not here.

This package owns no production behavior and must not grow any. Anything that
production code needs belongs in `querycontract`.

## Exported surface

See `doc.go` for the godoc contract.

- `MustMapField` — walks a decoded JSON/OpenAPI document one key at a time,
  failing with the offending key name.
- `RecordingResourceInvestigationGraph` — answers the four directed reads from
  installed row sets and records every `Run` query in `RunCalls`.
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
- `CodeGrantGrantedRepo`/`CodeGrantOtherRepo`, `SearchString`, and
  `BoundCanonicalLanguage` — the #5167/#6642 grant-test repo ids, the
  Cypher/SQL-text substring check, and a Cypher builder's canonical bound
  `$languages` entry; package query forwards each under its pre-move name.
- `LanguageMetadataSharedPath/Name/Start`, `LanguageGrantGrantedEntity`/
  `LanguageGrantUngrantedEntity`, and `LanguageQueryGrantEntities` — the
  #6642 language-query merge-key and grant fixtures both package query's and
  package `language`'s tests share, so neither drifts from the other.
- `MockLanguageQueryGraphReader` — minimal `querycontract.GraphQuery` double
  answering `Run`/`RunSingle` from `Rows`; package query forwards it as
  `mockLanguageQueryGraphReader`.

The graph-read doubles, the content-read doubles, and the fake `database/sql`
driver moved into `graph/` and `content/` for #6642 (see Nested leaves above);
their exported-surface bullets live in those packages' own READMEs now.

### Prefer the adapter shape for the remaining shared fakes

`graph.FakeGraphReader`, `graph.FakeRepoGraphReader`,
`graph.FakeWorkloadGraphReader`, and `content.FakePortContentStore` are each
adapted from root through an unexported adapter with the original field names,
delegating to the leaf package rather than reimplementing. Renaming fields
across every consumer is the alternative, and it buys nothing the adapter does
not. Prefer this shape for `FakeStatusReader`, `FakeGovernanceAuditAppender`,
and `FakeScopedTokenResolver` below, and for any future promotion out of this
package.

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

`graph.FakeGraphReader` is adapted by rebuilding it from the adapter's funcs on
each call, which works because it holds nothing between calls. The other two do
hold state, and each needs a different answer.

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

## Dependencies

The standard library, plus any LEAF package a fake genuinely needs to name the
types it stands in for -- `internal/status`, `internal/governanceaudit` and
`auth` are the shape, not the list. Deliberately stated as a rule rather
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
`internal/governanceaudit`, or `auth`, while the self-delegation whitelist
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

No-Observability-Change: no metric, span, log, or status surface is touched. A
test double deliberately emits no telemetry -- one that produced spans would
pollute the traces of whatever it stands in for.
