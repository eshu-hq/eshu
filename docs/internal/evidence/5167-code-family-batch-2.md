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
| `POST /api/v0/code/language-query` (Cypher) | grant in the anchoring `MATCH`'s own `WHERE`, in all four builders | `buildRepositoryCypher`, `buildDirectoryCypher`, `buildFileCypher`, `buildEntityCypherWithSemanticFilter` (`go/internal/query/language_query_cypher.go`) |
| `POST /api/v0/code/language-query` (selector) | `repo_id` resolved against the grant; ungranted rejected with 400 | `applyRepositorySelectorForAccess` (`go/internal/query/code_repository_selector.go`) |
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
place: appended to the `WHERE` attached to the single anchoring `MATCH`, beside
the optional `r.id = $repo_id` the builder already emitted, ahead of every
`WITH`, `ORDER BY` and `LIMIT`.

Clause attachment is settled by construction here, not by argument. The
Repository binding is a required `MATCH` in all four patterns, so an entity the
graph cannot attribute to a repository is already dropped today and the grant
condition decides row membership rather than nulling a projection. That is the
opposite of the `complexityListAnchor` defect batch 1 measured, and it is why
this route needs no live NornicDB run — see "Why No Live Run" below.

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

This is a user-visible change on an existing route, and it is documented in the
route's OpenAPI description and in
[HTTP API — Code](../../public/reference/http-api/code.md): a `repo_id` outside
a scoped caller's grant used to be queried and now returns `400`.

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

## Why No Live Run

Batch 1 ran a live NornicDB proof for `complexityListAnchor` because its
Repository binding sat on an `OPTIONAL MATCH`, where a `WHERE` constrains the
optional pattern rather than the driving row set, and no amount of reading the
statement settles that. Neither route here has that shape:

- language-query's four builders each bind Repository in a required `MATCH`
  chain and carry one `WHERE` attached to it.
- imports/investigate's seven builders each emit a single anchoring `MATCH` and
  route every predicate through `writeCypherPredicates`, which can only produce
  a `WHERE` attached to that `MATCH`. There is no `OPTIONAL MATCH` and no `WITH`
  anywhere in the file.

Clause attachment is therefore decided by construction rather than by
measurement, and `TestLanguageQueryBuildersBindTheGrantInTheShippedCypher` and
`TestImportDependencyBuildersBindTheGrantInTheShippedCypher` assert the grant is
inside the anchoring binding's own predicate list, not merely present in the
text. `evaluatingRepositoryGraph`
(`go/internal/query/code_graph_grant_evaluating_fake_test.go`) applies Cypher's
clause semantics to the emitted statement for the language-query route tests, so
moving the grant onto an optional pattern would still be caught. What a live run
would add here is plan-operator evidence, and no plan claim changed.

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

`specs/capability-matrix.v1.yaml` records `execute_language_query`'s production
row as `experimental` because the only committed deployed proof for the sibling
capability went through `/api/v0/code/imports/investigate`, not this route.
Nothing in this batch changes that. This is a tenancy fix with unit-level and
statement-level proof; it is not deployed validation, and no capability-matrix
row is raised on the strength of it.

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
already matched, and it lands in the anchoring `MATCH`'s own `WHERE` (Cypher) or
the statement's `WHERE` (SQL) — ahead of `SKIP`/`LIMIT`, ahead of
`LIMIT $scan_limit`, and ahead of `LIMIT`/`OFFSET`. A scoped caller therefore
reads no more rows than before and, on these two routes, strictly fewer: both
were corpus-wide for a caller who named no repository. The SQL grant column is
`content_entities.repo_id`, the same column that statement's single-repository
branch already filters on. No query gains a clause, a hop, a `WITH`, or a second
statement, and no builder's row count for an unscoped caller changes. Nothing
here puts a filter in a `WITH`-attached `WHERE` or guards a disjunct with
`$param <> ''`, the two NornicDB shapes recorded in
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
