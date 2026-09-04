# #5167 Code Family, Batch 2a — Two More Routes Off The Pending Ledger

`POST /api/v0/code/language-query` and `POST /api/v0/code/imports/investigate`
leave `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`) and join the
scoped-token allowlist. Both now bind the caller's repository grant inside the
queries they run, and both refuse a grantless scoped caller before touching a
backend.

The ledger goes from 12 pending routes to 10. It was 22 before batch 1 and 12
after it. The code family is not finished: `POST /api/v0/code/bundles`,
`POST /api/v0/code/call-chain`, `POST /api/v0/code/relationships` and
`POST /api/v0/code/relationships/story` are still on it, and the three
`/api/v0/impact/*` routes, `GET /api/v0/index-status`,
`GET /api/v0/freshness/services/changed-since` and
`GET /api/v0/images/tag-history` remain for their own workstreams. Batch 2a is
two routes, not the five the batch-2 research scoped; the other three each need
a live NornicDB measurement this batch did not run.

Both routes shared batch 1's root cause and added one of their own.
`applyRepositorySelectorForCapability` binds a grant only to a selector the
caller actually supplied, so a route that reads "no `repo_id`" as "search
everything" ran with no grant bound. On top of that:

- language-query is owned by `LanguageQueryHandler`, not `CodeHandler`, so none
  of the family's selector plumbing was reachable from it. `repo_id` was used
  raw — never resolved through `queryselector`, never checked against the grant,
  and an ungranted one was queried rather than refused.
- imports/investigate does not require `repo_id` at all. `validate` on
  `importDependencyRequest` accepts `source_file`, `target_file`,
  `source_module` or `target_module` on their own, so `{"source_file": "…"}` is
  a legitimate request that names no repository. There was no selector to fix;
  the grant had to reach the statement.

## What Moved

Each route makes the ledger header's five moves: a matcher, an advertised entry
classed `scopedRouteGrantBound`, the `x-scoped-token-support` marker and the
policy `403` on its OpenAPI operation, and removal from the pending map. Both
join `scopedCodeGraphGrantRoute`
(`go/internal/query/auth_scoped_routes_code_flow.go`) beside the dead-code
routes, the other entry in that list whose grant lands in two backends.

| Route | Binding | Symbol |
| --- | --- | --- |
| `POST /api/v0/code/language-query` (SQL) | `repo_id = ANY($n)` on every content read the route makes | `buildLanguageTypeEntityFilters` (`go/internal/query/content_reader_entity_search.go`) |
| `POST /api/v0/code/language-query` (Cypher) | grant in the `WHERE` of the required `MATCH` block that binds Repository, in all four builders | `buildRepositoryCypher`, `buildDirectoryCypher`, `buildFileCypher`, `buildEntityCypherWithSemanticFilter` (`go/internal/query/language_query_cypher.go`) |
| `POST /api/v0/code/language-query` (selector) | `repo_id` resolved as a selector for every caller class; ungranted rejected with 400 for a scoped one | `applyRepositorySelectorForAccess` (`go/internal/query/code_repository_selector.go`) |
| `POST /api/v0/code/imports/investigate` | grant per Repository alias in each of seven builders | `importDependencyGrantPredicates` (`go/internal/query/code_import_dependencies_queries.go`) |

## Language-Query Had Two Choke Points And Neither Was Bound

The SQL half is the batch-1 shape exactly: `buildLanguageTypeEntityFilters`
appended `repo_id = $n` only when `repoID != ""` and had no `else`, so a scoped
caller who omitted `repo_id` got a statement with no repository restriction and
read the whole content corpus. It gains the same `appendRepositoryGrantFilter`
branch (`go/internal/query/content_reader_code_topic.go`) the four batch-1
content builders emit. One edit covers three call sites, because every content
read this route makes goes through it: the content-only entity types, the
graphless and zero-row fallbacks, and the metadata enrichment pass.

That enrichment pass is worth naming separately.
`enrichLanguageResultsWithContentMetadata`
(`go/internal/query/language_query_metadata.go`) is a SECOND content read,
issued after the graph has already answered, and it builds a merge-key map from
whatever it reads. Unbound, it read every tenant's rows to build that map, so a
key collision on file path, label, name and start line would merge another
tenant's metadata into a granted row. It is bound now, and
`TestLanguageQueryMetadataEnrichmentCannotWidenTheAnswer` pins it.

The Cypher half is four builders behind one dispatcher,
`buildLanguageCypherWithSemanticFilter`. Each assembles its own `WHERE`, so the
grant is four small edits rather than one — but all four put it in the same
place: appended to the `WHERE` of the required `MATCH` block that binds
Repository, beside the optional `r.id = $repo_id` the builder already emitted,
ahead of every `WITH`, `ORDER BY` and `LIMIT`.

"`MATCH` block" is the accurate phrase, not "the anchoring `MATCH`".
`buildDirectoryCypher` is written as two `MATCH` clauses and hangs its `WHERE`
on the second, while `r` is bound in the first. Both are required, so the
predicate constrains the joined row set and `r` is genuinely filtered — but a
reader checking the clause-attachment argument against that builder would find
the tighter sentence false, and the argument is what the whole "no live run"
case rests on.

Clause attachment is settled by construction here, not by argument. The
Repository binding is required in all four patterns, so an entity the graph
cannot attribute to a repository is already dropped today and the grant
condition decides row membership rather than nulling a projection. That is the
opposite of the `complexityListAnchor` defect batch 1 measured. Construction is
not the whole answer, though — it says nothing about what the pinned backend
does with the statement — so the argument is now backed by a measurement: see
"The Live NornicDB Run" below.

### The Selector Question, Settled By Extraction

The research raised a design choice: give `LanguageQueryHandler` its own
selector plumbing, or make the family's helpers reachable from both handlers.
The second, and by extraction rather than duplication.
`applyRepositorySelectorForCapability`'s body moves to the free function
`applyRepositorySelectorForAccess`, and the `CodeHandler` method now delegates
to it. `codeContentGrantScope` and `codeGrantAccessFilter` were already free
functions. One implementation of "resolve the selector, map a transient graph
failure to the bounded-read contract, reject anything else with 400" now serves
every route in the family, so the two handlers cannot drift apart on it.

Two user-visible changes fall out of that, one per caller class, and both are
documented in the route's OpenAPI description and in
[HTTP API — Code](../../public/reference/http-api/code.md).

For a **scoped** caller: a `repo_id` outside the grant used to be queried and
now returns `400`. "Ungranted" is only meaningful for that caller class —
`AllowsRepositoryID` returns true for every id when `AllScopes` is set, so an
unscoped caller has no ungranted ids at all.

For an **unscoped** shared-key, admin or local caller: `req.RepoID` now goes
through `queryselector.ResolveExactForAccess` like every other code route's,
and that lands differently on three cases. A canonical id (`repo://…`,
`repo-…`, `repository:…`) passes through untouched, so nothing changes,
including for a typo'd one. A non-canonical selector — a name, slug, path or
remote URL — is now resolved against the catalog and the graph, so one that
resolves returns that repository's rows where it used to return an empty page.
One that resolves to nothing returns `400` where it used to return `200` with
`results: []`.

The mitigation is worth stating rather than leaving a reviewer to find it: the
OpenAPI operation has advertised `repo_id` as "Optional repository selector
(canonical ID, name, slug, or path)" all along, and a `400` response was already
declared on it. This change makes the handler match its published contract
rather than inventing a new one.
`TestLanguageQuerySharedKeyRepoIDGoesThroughTheSelector` covers all three cases;
the sibling unscoped tests pass no `repo_id` at all, which is exactly the case
the selector never touches, so on their own they proved nothing about it.

## Imports/Investigate Had No Selector To Fix

`repo_id` is one of five anchors, so a scoped caller who anchors on a file or
module names no repository at all and there is nothing for a selector to
resolve. The grant had to land in the statements.

All seven builders share a shape that makes that one edit each rather than seven
different ones: every one writes its repository node through
`writeRepositoryNode` and its predicates through `writeCypherPredicates`, which
always attaches its `WHERE` to the single anchoring `MATCH`.
`importDependencyGrantPredicates` renders the condition per Repository alias and
`importDependencyParams` merges the grant arrays.

The ordering is the substance. Four builders page with `SKIP`/`LIMIT`; the
other three scan to `LIMIT $scan_limit` (25,000 rows plus a sentinel) and page
in Go afterwards. A grant applied after that bound — in the Go pass rather than
the statement — would let an out-of-grant repository fill the scan budget and
push a granted repository's rows past the `422` the overflow raises. That is the
filter-before-limit rule (#5167 W3 P1) at its sharpest on this family, and
`TestImportDependencyScanBoundIsSpentOnGrantedRowsOnly` seeds 25,001
out-of-grant edges beside one granted cycle and asserts the granted answer still
comes back.

`crossModuleCallRowsCypher` binds `source_repo` and `target_repo`
independently. What that adds needs stating precisely, because it is not a new
row-set filter: `crossModuleCallRowMatches`
(`go/internal/query/code_import_dependencies_rows.go`) already drops a row whose
two endpoints disagree, so a mixed-tenant pair never reached the response. What
it adds is scan-budget protection — that Go pass runs after `$scan_limit`, so
binding only the caller side would still spend the budget on callees the caller
may not see — and the batch-1 consumer-side rule stated in the query text rather
than only in a post-filter. The leak this route did have on that query type was
a pair with BOTH endpoints in another tenant, which the Go pass admits and the
grant now removes.

## The Grant Travels On The Request, And Its Zero Value Is Scoped

Seven builders take an `importDependencyRequest`, so the grant rides there as an
unexported `access` field rather than a seventh parameter on each signature.
Being unexported, `encoding/json` cannot reach it from a request body.

That choice has one sharp edge, and it bit immediately.
`querycontract.RepositoryAccessFilter`'s zero value has `AllScopes` false, so
`Scoped()` reports **true**: a request built without naming an access renders
the grant condition. The queryplan manifest bindings built their proof requests
exactly that way, so regenerating them naively would have repinned every
`cypher_sha256` to the SCOPED statement while each entry's committed `plan`
operators describe the repository-anchored shape a shared-key caller runs. That
is batch 1's `callGraphMetricsEdgesCypher` lesson inverted — there, the pinned
shape became the one only an unscoped caller ran.

The bindings now set an explicitly all-scopes filter, the way the workload,
cloud-resource and resource-selector bindings already do, so the pinned Cypher
and every plan claim are unchanged. The scoped shapes are covered where the
other families cover theirs: the import-dependency variant family gains an
access dimension and enumerates both caller classes.

## Query-Plan Manifests

Eight source digests moved because eight function bodies changed — the seven
import-dependency builders and language-query's
`queryByLanguageWithSemanticFilter` callsite in `query-source-coverage.yaml`.
No `cypher_sha256` moved and no `plan` block changed. They are regenerated in
their own commit, separate from the semantic change, so the mechanical churn
stays out of the reviewable diff.

The import-dependency variant family goes from 244 reachable request shapes over
140 frozen query texts to 488 over 280. Exactly double, in both numbers, and
that is itself a check: it says every one of the seven builders renders the
grant for a scoped caller, and that none of them collapse the two caller classes
into one statement.

## The Live NornicDB Run

Batch 1 ran a live NornicDB proof for `complexityListAnchor` because its
Repository binding sat on an `OPTIONAL MATCH`, where a `WHERE` constrains the
optional pattern rather than the driving row set. Neither route here has that
shape: language-query's four builders each bind Repository in a required
`MATCH`, and imports/investigate's seven route every predicate through
`writeCypherPredicates`, which can only attach a `WHERE` to the single anchoring
`MATCH`.

That settles clause attachment by construction, and this batch first shipped
with it as the whole argument. It is not enough. Clause attachment says nothing
about whether the pinned executor parses the statement the builders now emit,
applies the added predicate at all, or applies it before the row bound rather
than after. Those are backend questions, and only the backend answers them.

`TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder` and
`TestLiveNornicDBImportDependencyGrantBindsEveryBuilder` (build tag
`live_nornicdb_language_imports_grant`) run all fourteen shipped statement
shapes against the pinned image, scoped and unscoped, on a two-repository graph
seeded through the labels, relationship types and properties the canonical
projector writes. The out-of-grant repository gets six rows to the granted
repository's one, sorts first under every builder's `ORDER BY`, and every page
bound is set below six. The unscoped control comes back entirely out-of-grant;
the scoped run returns the granted rows and nothing else. A predicate applied to
the statement's output, after the bound, would have left the scoped run empty.
The run record and per-shape row counts are in the proof ledger.

That ordering control stands in for a plan read because the pinned build cannot
give one. `TestLiveNornicDBGrantPlanShapeIsNotReportable` measures it: `EXPLAIN`
and `PROFILE` are both accepted, both return zero rows, and the driver summary
carries neither `Plan()` nor `Profile()`. A `PROFILE` prefix therefore turns a
row-returning statement into an empty one silently, which is its own reason
never to leave one in shipped Cypher.

### The Directory Builder Answered Nothing On This Backend

The live run turned up a defect the grant work did not cause.
`buildDirectoryCypher` returned zero rows on the pinned build — scoped and
unscoped alike, on a graph seeded the way the projector writes directories — so
`entity_type: "directory"` on this route answered an empty `results` list on the
default graph backend for every caller, and said nothing about why.

The cause is a backend row drop, bisected in
`TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing` and recorded in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md):
a read with two `MATCH` clauses followed by a `WITH … count(…)` aggregation
drops every row as soon as the `RETURN` projects anything richer than a plain
property or a literal. `labels(d)` triggers it, and so do `coalesce(…)` and a
list construction; a plain property, a null property and a string literal are
all fine. One `MATCH` clause evaluates the same join correctly.

It is fixed here rather than deferred. The builder now emits a single linear
pattern, and the direction is load-bearing: the forward form
`(r:Repository)-[:REPO_CONTAINS|CONTAINS*]->(d:Directory)-[:CONTAINS]->(f:File)`
was measured on the same build and returns rows that are *wrong* — a nested
directory's file is folded into its parent's `file_count` and the nested
directory disappears. Anchoring at `File` keeps the last `CONTAINS` hop out of
the variable-length chain, so `d` binds to the directory that directly holds
each file. A comma-separated pattern behaves like two clauses and is not a fix.

Two consequences worth stating plainly. This is a **behaviour change for every
caller**, not only a scoped one: a request that returned `results: []` on
NornicDB now returns directories. And it is the one place on this route where
the unscoped query text deliberately changes — every other builder's unscoped
text is byte-identical to before, and the tests pin that. No queryplan digest
moves with it: the manifest's only language-query entry records a
`source_sha256` of `(*LanguageQueryHandler).queryByLanguageWithSemanticFilter`
in `language_queries.go`, a function this change does not touch, and this route
has no `cypher_sha256` anywhere.

The live seed grew a directory one level below the granted repository's own so
the rewrite is judged on the depth-N `CONTAINS` chain the projector actually
writes, and the proof asserts each directory's `file_count`, not merely that
rows came back — presence alone would have passed the wrong rewrite.

## Two Answers The Grant Made Wrong

Review found two places where adding the grant changed an answer it had no
business changing. Both are fixed here.

The empty-grant short-circuit sat ahead of the route's only entity-type check,
which was the tail of the dispatch chain. So a scoped caller with no repository
grants got the route's empty `200` for an `entity_type` no branch serves, while
every other caller got the documented `400`. Two callers disagreeing about
whether a request is well-formed is a contract bug on its own, and the
difference also signals the caller's grant state through a response the empty
page exists to keep uninformative. `acceptLanguageQueryEntityType` now answers
it with the rest of the request validation, ahead of the selector's own graph
read. The dispatch tail keeps the same call as a backstop for the day the gate
and the three dispatch maps drift.

`enrichLanguageResultsWithContentMetadata` keyed its merge map on file path,
label, entity name and start line. Those four are not unique across
repositories — a fork, a vendored copy, or a generated file two services both
carry gives two repositories the same values — so rows collided, the last
content row written won, and both graph rows were enriched from it. The grant
does not cover this: it is reachable with BOTH repositories inside one caller's
own grant. `languageResultRepositoryMatchKey` puts the repository on both sides
of the key, reading it from `repo_id` on a graph row (falling back to `id`,
which is where `buildRepositoryCypher` projects it) and from `RepoID` on a
content row. An empty repository is a key of its own rather than a wildcard, so
a row the graph could not attribute can only match a content row that carries
none either.

It is a wrapper around the existing `languageResultMatchKey` rather than a
change to it, because that helper is shared with the entity and code-search
enrichments (`entity_metadata.go`, `code_search_metadata.go`) and those two
cannot reach this defect. Both read through `ContentReader.SearchEntityContent`,
whose `WHERE repo_id = $1` is unconditional, and all six of their call sites
pass a single non-empty repository: `searchGraphEntitiesWithExact` refuses an
empty `repo_id` outright with `errGlobalGraphEntitySearchUnsupported`, the
entity route returns through `resolveGlobalContentEntities` before its graph
path when `repo_id` is empty, and the remaining four enrich exactly one row
using that row's own `repo_id`. One repository in the content read means no
second repository to collide with. Language-query is the only one of the three
whose content read deliberately spans a SET of repositories -- that is what
`searchLanguageEntities` binding the grant does -- which is why the key needed
the repository here and nowhere else.

## A Batch-1 Fake Was Hiding Predicates

`evaluatingRepositoryGraph` reads the `WHERE` attached to the Repository binding
and cut that block at the first clause keyword it found. Its scan for `" WITH "`
matched the `WITH` inside `f.name ENDS WITH '.go'` — the extension filter the
language-query builders splice into the same `WHERE` — so every predicate after
the extension filter, including the grant, was invisible to it and every seeded
row was admitted. `clauseTerminatorIndex` now skips a `WITH` that is the second
word of `ENDS WITH` or `STARTS WITH`. The fake also learns the Repository alias
the statement binds, because the batch-1 builders write `repo` and the
language-query builders write `r`.

The batch-1 routes' assertions are unaffected: their statements carry no
`ENDS WITH`, so the block they parsed was already complete.

## Capability Matrix

`execute_language_query` is a tool, not a capability row.
`specs/capability-matrix.v1.yaml` attaches it to five rows —
`symbol_graph.decorators`, `symbol_graph.argument_names`,
`symbol_graph.class_methods`, `symbol_graph.imports` and
`symbol_graph.inheritance` — and most are already `production: supported`. Two
are `experimental`: `symbol_graph.argument_names`, whose note records a deployed
readback dropping module-level function parameters, and `symbol_graph.imports`,
whose note records that its only deployed proof went through
`/api/v0/code/imports/investigate` — the sibling capability
`symbol_graph.import_dependencies` — rather than through this tool.

Nothing in this batch changes any of them, and the file is not in the diff. This
is a tenancy fix with unit-level and statement-level proof; it is not deployed
validation, and no capability-matrix row is raised on the strength of it.

## Proof Ledger

The red/green runs and the BITES mutation ledger — what was broken on purpose,
which guard judged it, and the exit code — live in
[#5167 code family batch 2a proofs](5167-code-family-batch-2-proofs.md), split
out because the two together outgrow the repository's 500-line Markdown cap.

## Verification

Run after the last edit, exit codes captured directly (`cmd; echo $?`, never
after a pipe):

```text
cd go && go test ./internal/query ./internal/mcp ./internal/queryplan -count=1   # 0
cd go && go vet ./internal/query ./internal/mcp ./internal/queryplan             # 0
scripts/dev/precommit-go.sh fmt   <changed .go>                                  # 0
scripts/dev/precommit-go.sh lint  <changed .go>                                  # 0
scripts/dev/precommit-go.sh filecap <changed .go>                                # 0
scripts/verify-package-docs.sh                                                   # 0
scripts/verify-openapi.sh                                                        # 0
scripts/verify-doc-citations.sh                                                  # 0
scripts/verify-markdown-line-cap.sh --all                                        # 0
scripts/verify-performance-evidence.sh                                           # 0
mkdocs build --strict --clean --config-file docs/mkdocs.yml                       # 0
git diff --check                                                                 # 0
```

No-Regression Evidence: this is a correctness change with no latency claim
attached; no benchmark was run and no speedup is asserted, so the claim being
made is no-regression, not a win. Every predicate it adds is an `IN`/`ANY()`
membership test against the caller's grant, on a node or column the query
already matched, and it lands in the `WHERE` of the required `MATCH` block that
binds Repository (Cypher) or the statement's `WHERE` (SQL) — ahead of
`SKIP`/`LIMIT`, ahead of `LIMIT $scan_limit`, and ahead of `LIMIT`/`OFFSET`. A
scoped caller therefore reads no more rows than before and, on these two
routes, strictly fewer: both were corpus-wide for a caller who named no
repository. The SQL grant column is `content_entities.repo_id`, the same column
that statement's single-repository branch already filters on. No query gains a
clause, a hop, a `WITH`, or a second statement, and no builder's row count for
an unscoped caller changes. Nothing here puts a filter in a `WITH`-attached
`WHERE` or guards a disjunct with `$param <> ''`, the two NornicDB shapes
recorded in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).
The one cost that is real and undeclared by a measurement: a scoped caller's
statement carries two extra bound arrays and one extra disjunct per Repository
alias, on queries whose anchors and limits are otherwise identical. That cost
falls only on scoped callers, who could not reach either route at all before
this change.

For an unscoped shared, admin, or local caller every grant predicate renders
empty and every grant parameter is unbound, so the query text those callers
execute is byte-identical to before. That is pinned two ways: by
`TestLanguageQuerySharedKeyReadIsUnchanged`,
`TestImportDependenciesSharedKeyReadIsUnchanged` and the builders' own
`…CarryNoGrantForAnUnscopedCaller` assertions, and by the queryplan manifests,
whose `cypher_sha256` values and plan blocks are unchanged for all seven
import-dependency entries.

Observability Evidence: no metric instrument, metric label, span, log event,
route, worker, queue, lease, or runtime knob is added or renamed. Operators
diagnose both routes through the governance-audit read-authorization events in
`go/internal/query/auth_audit.go` — `recordScopedReadAuthorized` and
`recordScopedRouteAuthorizationDeniedWithReason`, each stamped with tenant,
workspace, actor hash, and correlation id — plus the existing per-capability
handler spans `SpanQueryLanguageQuery` and
`SpanQueryImportDependencyInvestigation` and the
`eshu_dp_postgres_query_duration_seconds` /
`eshu_dp_neo4j_query_duration_seconds` histograms. The import route's existing
span attributes still report what an operator needs at 3 AM:
`eshu.import_dependencies.query_type`, `result_count`, `truncated` and
`scan_overflow`. A scoped caller that now reads fewer rows shows up as a smaller
`count` and a `truncated` that flips false in the same response envelope; a
grantless one shows up as the empty page plus a `scoped_read_allowed` audit
event with no backend span beneath it.
