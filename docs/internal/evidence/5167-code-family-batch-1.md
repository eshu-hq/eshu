# #5167 Code Family, Batch 1 — Ten Routes Off The Pending Ledger

Ten `POST /api/v0/code/*` routes leave `pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`) and join the
scoped-token allowlist. Each one now binds the caller's repository grant inside
the query it runs, and refuses a grantless scoped caller before touching a
backend.

All ten shared one root cause. `applyRepositorySelectorForCapability` only binds
a grant to a selector the caller actually supplied:
`queryselector.ResolveExactForAccess`
(`go/internal/query/queryselector/selector.go`) returns `""` for an empty
selector without consulting the grant at all. So every route that reads "no
repo_id" as "search everything" ran its downstream query with no grant bound.
Two of the ten were worse than that: `code/complexity`'s entity_id branch
carried no repository predicate at all and ignored even a repo_id the caller did
supply, and `code/security/secrets/investigate` returns redacted secret line
text.

`code/call-graph/metrics` is the one exception: `repo_id` is mandatory there and
the selector always resolves it through the grant, so it was never exploitable.
It gains the predicate as defense in depth.

## What Moved

Each route moves by the ledger header's four steps: a matcher, an advertised
entry classed `scopedRouteGrantBound`, the `x-scoped-token-support` marker on
the OpenAPI path, and removal from the pending list. The two matchers are
`scopedCodeContentGrantRoute` and `scopedCodeGraphGrantRoute`, both added here
in `go/internal/query/auth_scoped_routes_code_flow.go` and wired into
`scopedHTTPRouteSupportsTenantFilter`. The pre-existing `scopedCodeFlowRoute`
still matches only the four `/api/v0/code/flow/*` routes and is untouched. The
ledger goes from 24 pending routes to 14.

| Route | Binding | Symbol |
| --- | --- | --- |
| `POST /api/v0/code/topics/investigate` | `repo_id = ANY($n)` | `codeTopicFilters` (`go/internal/query/content_reader_code_topic.go`) |
| `POST /api/v0/code/security/secrets/investigate` | `repo_id = ANY($n)` | `hardcodedSecretFilters` (`go/internal/query/content_reader_security_secrets.go`) |
| `POST /api/v0/code/symbols/search` | `repo_id = ANY($n)`; per-repo iteration on the name fallback | `symbolSearchFilters` (`go/internal/query/content_reader_symbol_search.go`), `symbolNameFallbackEntities` (`go/internal/query/code_symbol.go`) |
| `POST /api/v0/code/structure/inventory` | `repo_id = ANY($n)` | `structuralInventoryWhere` (`go/internal/query/content_reader_structural_inventory.go`) |
| `POST /api/v0/code/dead-code` | candidate choke point, both backends | `deadCodeCandidateRows` (`go/internal/query/code_dead_code_scan.go`) |
| `POST /api/v0/code/dead-code/investigate` | same choke point | `deadCodeCandidateRows` |
| `POST /api/v0/code/dead-code/cross-repo` | same choke point, plus the consumer-evidence read | `deadCodeCandidateRows`, `crossRepoDeadCodeConsumerScan` (`go/internal/query/content_reader_dead_code_cross_repo.go`) |
| `POST /api/v0/code/call-graph/metrics` | grant on both `CALLS` endpoints | `callGraphMetricsEdgesCypher` (`go/internal/query/code_call_graph_metrics.go`) |
| `POST /api/v0/code/quality/inspect` | grant in the MATCH-attached `WHERE` | `buildCodeQualityCypher` (`go/internal/query/code_quality.go`) |
| `POST /api/v0/code/complexity` | grant in all three builders; the list branch also changes anchor and the entity_id branch gains the supplied-repo anchor | `listMostComplexFunctions`, `lookupComplexityRowByName`, `lookupComplexityRowByID` (`go/internal/query/code_complexity_queries.go`) |

An eleventh route changes its emitted text without changing its rows.
`POST /api/v0/ecosystem/graph-summary` calls the same
`callGraphMetricsEdgesCypher` through `graphSummaryHotEntities`
(`go/internal/query/infra_graph_summary_packet.go`), so a scoped caller there
now runs the grant-bound edge pass. `handleGraphSummary` already answers
not-found for an out-of-grant `repo_id` before the read, so the predicate cannot
change that route's row set.
`TestGraphSummaryHotEntitiesRunTheGrantBoundEdgePass` pins its text in both
directions.

The dead-code choke point's two backends: `ContentReader.DeadCodeCandidateRows`
(`go/internal/query/content_reader_dead_code_candidates.go`, SQL
`AND repo_id = ANY($4)` ahead of `LIMIT`/`OFFSET`) and
`buildDeadCodeGraphCypherForLabel` (`go/internal/query/code_dead_code.go`,
Cypher `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` on the
Repository anchor).

Two shared helpers keep the ten from drifting apart: `codeContentGrantScope`
(`go/internal/query/code_repository_selector.go`) resolves the grant and reports
the fail-closed case, and `appendRepositoryGrantFilter`
(`go/internal/query/content_reader_code_topic.go`) is the single SQL grant
predicate all four content builders emit.

## The Empty-Grant Trap

An empty id list reads as *unrestricted* to every `repo_id = ANY($n)` and
`id IN $allowed_repository_ids` predicate in this package, so a grantless scoped
caller would have seen the whole corpus through the very predicate meant to
protect it. `codeContentGrantScope` returns `blocked` for that caller and each
route returns its own empty page — an empty list, a not-found, zero
candidates — without touching a backend, so an empty grant is indistinguishable
from an empty index.

## The Complexity List Branch Needed A New Query, Not A New Predicate

The first version of this change appended the grant to `listMostComplexFunctions`
the way every other builder got it. That query binds its Repository anchor in an
`OPTIONAL MATCH`, and a `WHERE` attached to an `OPTIONAL MATCH` constrains the
optional pattern, not the driving row set. The predicate was in the text and
filtered nothing.

Measured against NornicDB v1.2.3
(`timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`,
self-reports 1.2.2) on a seeded two-tenant graph, a scoped caller granted only
`repo://tenant-a/granted-service` and sending no selector got all three seeded
functions back, including the other tenant's and one attached to no repository:

```text
"results":[
 {"complexity":11,"entity_id":"fn:orphan","name":"LiveOrphanComplexityProbe","repo_id":"repo.id",...},
 {"complexity":9,"entity_id":"fn:ungranted","name":"LiveUngrantedComplexityProbe","repo_id":"repo.id",...},
 {"complexity":7,"entity_id":"fn:granted","name":"LiveGrantedComplexityProbe","repo_id":"repo.id",...}]
```

Name, language, line span, complexity, and the semantic metadata projection all
reached the caller. The `repo_id` column came back as the literal string
`repo.id`, which is the separate multi-clause projection corruption recorded in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).

`complexityListAnchor` now gives a scoped caller a single anchoring
`MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)`
with the grant in that `MATCH`-attached `WHERE`, ahead of the `ORDER BY` and
`LIMIT`. A function the graph cannot attribute to a repository is dropped for a
scoped caller, which is the fail-closed answer for a row whose tenant is
unknown. An unscoped caller keeps the `OPTIONAL MATCH` form and its exact text.

## Red Then Green

Nine of the ten routes carry a response-body two-tenant proof: one granted
repository, one out-of-grant repository, and an assertion that the out-of-grant
id never appears in the body. `code/call-graph/metrics` is the tenth. Its
`repo_id` is mandatory and grant-resolved, so its proof is the pair "a granted
repo_id returns only its own functions" and "an ungranted repo_id is rejected
with 400 and never reaches the edge scan", rather than a corpus-wide leak
assertion.

The three graph-backed routes are driven by `evaluatingRepositoryGraph` and
`evaluatingCallGraphEdges`
(`go/internal/query/code_graph_grant_evaluating_fake_test.go`,
`go/internal/query/auth_scoped_code_graph_rows_grant_test.go`). Those fakes read
the emitted statement far enough to answer two questions — is the Repository
binding optional, and which repository predicates govern it — then apply
Cypher's clause semantics to seeded rows. That is what lets them fail on clause
attachment, which no substring assertion can see.
`TestEvaluatingRepositoryGraphKeepsOptionalMatchRows` feeds the fake the exact
statement shape this change replaced and asserts the out-of-grant row survives
with null repository columns, so a fake that quietly dropped every non-matching
row could not pass.

| Test | Red | Green |
| --- | --- | --- |
| `TestCodeTopicInvestigation*` (3) | `AllowedRepositoryIDs = []string(nil), want [...]`; `queried = true, want false` | `ok internal/query 1.789s` |
| `TestCodeTopicFiltersBindTheGrantInTheShippedSQL` | `want a repo_id = ANY($1) grant predicate` | `ok internal/query 1.706s` |
| `TestCodeContentRoutes*` (3 × 4 route cases) | build failure: `AllowedRepositoryIDs undefined` on all three request types | `ok internal/query 1.802s` |
| `TestCodeContentFiltersBindTheGrantInTheShippedSQL` (3) | same build failure | `ok internal/query 1.802s` |
| `TestSymbolNameFallback*` (3) | `SearchEntitiesByName repositories = []string{""}, want ["repo://tenant-a/granted-service"]` | `ok internal/query 1.171s` |
| `TestDeadCodeRoutes*`, `TestCrossRepoDeadCodeProducerScanCarriesTheGrant` | build failure: `undefined: deadCodeCandidateQuery` | `ok internal/query 2.074s` |
| `TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher` | same build failure | `ok internal/query 2.074s` |
| `TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL` (2) | `candidate SQL is missing "AND repo_id = ANY($4)"` | `ok internal/query 1.747s` |
| `TestCrossRepoDeadCodeConsumerEvidence*` (2), `TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal` | build failure: the reader took no grant argument and returned no hidden counts | `ok internal/query 1.074s` |
| `TestCallGraphMetricsBindsTheGrantOnBothCallEndpoints` | `missing "(source.repo_id IN $allowed_repository_ids OR ...)" on the source endpoint` | `ok internal/query 1.826s` |
| `TestCallGraphMetricsEmptyGrantSkipsTheEdgeScan` (2) | `read` sub-test reached the graph | `ok internal/query 1.826s` |
| `TestCallGraphMetricsBodyCarriesOnlyGrantedFunctions`, `TestUngrantedRepositorySelectorIsRejectedWith400` | new coverage, no prior red | `ok internal/query 1.225s` |
| `TestCodeQualityAndComplexityBuildersBindTheGrant` (4) | all four builders `missing "(repo.id IN $allowed_repository_ids OR ...)"` | `ok internal/query 1.799s` |
| `TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead` (4) | all four reached the graph | `ok internal/query 1.799s` |
| `TestComplexityByEntityIDHonoursASuppliedRepoID` | `entity_id lookup ignores the supplied repo_id` | `ok internal/query 1.799s` |
| `TestComplexityListDoesNotLeakUngrantedFunctions` | `scoped complexity list leaked "UngrantedComplexityProbe"` and `"OrphanComplexityProbe"`, both with `"repo_id":""` | `ok internal/query 1.295s` |
| `TestLiveNornicDBComplexityListFiltersUngrantedFunctions` (live) | `scoped complexity list leaked "LiveUngrantedComplexityProbe"`, exit `1` | `ok internal/query 1.112s`, exit `0` |

Unscoped counterparts (`TestCodeTopicInvestigationSharedKeyReadIsUnchanged`,
`TestCodeContentRoutesSharedKeyReadIsUnchanged`,
`TestSymbolNameFallbackSharedKeySearchIsUnchanged`,
`TestDeadCodeRoutesSharedKeyScanIsUnchanged`,
`TestCallGraphMetricsUnscopedCypherIsUnchanged`,
`TestCodeQualityAndComplexityUnscopedCypherCarriesNoGrant`,
`TestComplexityListUnscopedAnswerIsUnchanged`,
`TestCodeQualityInspectUnscopedAnswerIsUnchanged`,
`TestLiveNornicDBComplexityListKeepsTheUnscopedAnswer`) pin the other direction:
a shared-key caller's query text and row set are unchanged.

## The Backend-Required Half

The graph half of this change is a clause-attachment question, and clause
attachment is backend behaviour, not a text property. Reusing the
`relationshipStoryRepoPredicates` predicate string is an argument about the
string, not about where the string sits, and trusting it is what let the first
version of the complexity list branch look proven while filtering nothing.

Two things close that gap. The live NornicDB v1.2.3 run executes the real route
against a real backend, red before the change and green after. The evaluating
fakes then keep it closed in the credential-free suite, because they fail when a
grant predicate moves back onto an optional pattern. The other four graph
builders (`buildDeadCodeGraphCypherForLabel`, `buildCodeQualityCypher`,
`lookupComplexityRowByName`, `lookupComplexityRowByID`) are single-clause
`MATCH … WHERE … RETURN` reads whose grant already sits in the `MATCH`-attached
`WHERE`; `TestBuildDeadCodeGraphCypherKeepsTheScopedVariantSimple` and the
evaluating-fake route tests assert that shape instead of assuming it.

```bash
docker run -d --name nornic-5167-p0 -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_NO_AUTH=true -p 17687:7687 \
  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
cd go && ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query \
  -tags live_nornicdb_complexity_grant -run TestLiveNornicDBComplexityList -count=1
```

Host: MacBook Pro, arm64, macOS. The run is a correctness proof on a three-node
seeded graph, not a latency measurement, and no timing from it is cited
anywhere.

## Cross-Repo Consumer Evidence

`POST /api/v0/code/dead-code/cross-repo` reads consumer evidence for its
producer candidates. That read used to reach Postgres with no grant: it fetched
every tenant's consumer rows, capped them at 1000, and dropped the out-of-grant
ones in Go afterwards. No consumer identity ever left the process — hidden rows
are counted, never projected — but the cap fell on a mixed set, so another
tenant's rows could push a granted consumer off the page.

The grant is now in the statement (`crossRepoDeadCodeConsumerScan`, rendering
`AND row.repository_id = ANY($n)`), ahead of the `LIMIT`. Filtering there alone
would have destroyed the signal the handler needs: a symbol whose only consumers
are out of grant must stay `unknown_needs_evidence` with reason
`permission_hidden_consumer`, not become `dead`. A second bounded statement
(`buildCrossRepoDeadCodeHiddenConsumerQuery`) returns the count of excluded
consumers per producer entity and nothing else — no id, name, or citation. It
runs only for a scoped caller. `filterCrossRepoDeadCodeEvidence`
(`go/internal/query/code_dead_code_cross_repo_filter.go`) stays as the Go-side
check for the repository-boundary fallback, which is not grant-bound, and for a
content store that does not bind the grant itself.

The truncation fail-safe is unchanged and still carries the case the cap can
still lose: an entity left with zero rows is marked
`consumer_evidence_truncated` by
`markCrossRepoDeadCodeConsumerEvidenceTruncated`, so a short page reads as
"unknown", never as "dead".

## Query-Plan Source Coverage

`go test ./internal/queryplan` was red on this branch before this pass, and
neither the earlier verification list nor the first review ran that package. Six
callsites failed `TestHotCypherManifestCoversEveryProductionQueryCall`, because
adding a grant predicate changes the enclosing symbol's `source_sha256` and the
manifest freezes it:

```text
code_call_graph_metrics.go:(*CodeHandler).callGraphMetricsData: hot callsite source_sha256 does not match production symbol
code_complexity_queries.go:(*CodeHandler).listMostComplexFunctions:  grandfathered source_sha256 does not match production symbol
code_complexity_queries.go:(*CodeHandler).lookupComplexityRowByName: grandfathered source_sha256 does not match production symbol
code_dead_code_scan.go:(*CodeHandler).deadCodeCandidateRows:         grandfathered source_sha256 does not match production symbol
code_quality.go:(*CodeHandler).inspectCodeQuality:                   grandfathered source_sha256 does not match production symbol
infra_graph_summary_packet.go:(*InfraHandler).graphSummaryHotEntities: grandfathered source_sha256 does not match production symbol
```

That is the gate working as designed: a changed source digest forces the owning
callsite through a typed non-hot audit rather than letting a prose
`non_hot_reason` carry forward. The hot callsite keeps its registration and its
digest is refreshed; `cypher_sha256` for `QP-CALL-GRAPH-HUBS` and
`QP-CALL-GRAPH-RECURSIVE` is unchanged, so the accepted plan claim still stands.
The five grandfathered prose entries are replaced with typed dispositions
carrying the bound each read already enforces, and their keys are removed from
`grandfatheredNonHotSourceDigests`:

| Callsite | Class | Bound |
| --- | --- | --- |
| `listMostComplexFunctions` | `label_inventory` | `Function`, 101 rows (`complexityMaxListLimit` + 1) |
| `lookupComplexityRowByName` | `keyed_support` | single key `$entity_name`, 3 rows (`complexityNameCandidateLimit` + 1) |
| `deadCodeCandidateRows` | `label_inventory` | one candidate label per page from the closed `deadCodeCandidateLabels` set, 250 rows (`deadCodeCandidateQueryMax`) |
| `inspectCodeQuality` | `label_inventory` | `Function`, 101 rows (`codeQualityMaxLimit` + 1) |
| `graphSummaryHotEntities` | `keyed_support` | single key `$repo_id`, 50001 rows (`callGraphMetricsEdgeScanLimit` + 1) |

## BITES — Each Choke Point Proved To Bite

Each row breaks one production binding, runs the guard, restores the file, and
records the exit code directly (`cmd; echo $?`, never after a pipe). Every
mutation was restored and its guard rerun at exit `0`.

| # | Mutation | Guard run | Exit |
| --- | --- | --- | --- |
| 1 | `appendRepositoryGrantFilter` emits `true /* $n */` instead of `repo_id = ANY($n)` | `go test ./internal/query -run BindTheGrantInTheShippedSQL -count=1` | `1` (4 failures: topic, secrets, symbol_search, structural_inventory) |
| 2 | `codeContentGrantScope` returns `blocked=false` on `access.Empty()` | `go test ./internal/query -run 'EmptyGrant' -count=1` | `1` (topic, secrets, symbols, structure ×2, dead-code ×2) |
| 3 | `buildDeadCodeGraphCypherForLabel` drops `access.GraphCondition("r")` | `go test ./internal/query -run TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher -count=1` | `1` |
| 4 | `ContentReader.DeadCodeCandidateRows` emits `AND true /* $n */` | `go test ./internal/query -run TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL -count=1` | `1` |
| 5 | `buildCodeQualityCypher` and all three complexity builders drop their grant | `go test ./internal/query -run TestCodeQualityAndComplexityBuildersBindTheGrant -count=1` | `1` (4 failures) |
| 6 | `callGraphMetricsEdgesCypher` renders an empty grant clause | `go test ./internal/query -run TestCallGraphMetricsBindsTheGrantOnBothCallEndpoints -count=1` | `1` |
| 7 | complexity and quality drop their `access.Empty()` refusal | `go test ./internal/query -run TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead -count=1` | `1` (4 failures) |
| 8 | `symbolNameFallbackEntities` always takes the single-lookup branch (`if true`), so it asks for repository `""` | `go test ./internal/query -run TestSymbolNameFallback -count=1` | `1` (`repositories = []string{""}`) |
| 9 | `complexityListAnchor` returns the `OPTIONAL MATCH` form for every caller (`if false`) | `go test ./internal/query -run TestComplexityListDoesNotLeakUngrantedFunctions -count=1` | `1` |
| 10 | `crossRepoDeadCodeConsumerScan` emits `AND true /* $n */` instead of `AND row.repository_id = ANY($n)` | `go test ./internal/query -run TestCrossRepoDeadCode -count=1` | `1` |
| 11 | the same mutation as #9, run against the live backend instead of the fake | `ESHU_NEO4J_URI=bolt://localhost:17787 go test ./internal/query -tags live_nornicdb_complexity_grant -run TestLiveNornicDBComplexityListFiltersUngrantedFunctions -count=1` | `1` (leaked `LiveUngrantedComplexityProbe` and `LiveOrphanComplexityProbe`) |

An earlier attempt at #1 deleted the whole helper body and failed as an unused
import rather than an assertion, which proves nothing. The mutations above keep
the package compiling so the failure is the assertion's.

Rows 9 and 11 are the same one-token mutation judged by two different guards.
Row 9 is the credential-free guard that runs in CI; row 11 is the live NornicDB
one, and it is the only row in this table that settles clause attachment against
a real backend. A second engineer reran both directions of row 11 on a fresh
container from the same pinned digest (NornicDB self-reporting 1.2.2, bolt on
port 17787): mutated exit `1` with the leak body quoted above, restored exit `0`.

## Verification

Run after the last edit, exit codes captured directly:

```text
cd go && go test ./internal/query ./internal/mcp ./cmd/api -count=1   # 0
cd go && go test ./internal/queryplan -count=1                        # 0
cd go && go vet ./internal/query ./internal/mcp                       # 0
scripts/dev/precommit-go.sh fmt   <changed .go>                       # 0
scripts/dev/precommit-go.sh lint  <changed .go>                       # 0 (2 packages, 0 issues)
scripts/dev/precommit-go.sh filecap <changed .go>                     # 0
scripts/verify-package-docs.sh                                        # 0
scripts/verify-openapi.sh                                             # 0 (255 routes, 255 path entries)
scripts/verify-doc-citations.sh                                       # 0
scripts/verify-markdown-line-cap.sh --all                             # 0
git diff --check                                                      # 0
```

On origin/main `code_dead_code.go` was 496 lines and `code_dead_code_scan.go`
was 468; this change pushed both over the 500-line cap, and
`code_dead_code_cross_repo.go` followed later. The candidate-page request type,
the scan budget helpers, the candidate-label predicate, and the cross-repo
consumer-evidence filter moved to the sibling files that already own those
families (`code_dead_code_candidate_schedule.go`,
`code_dead_code_candidate_entity.go`, `code_dead_code_cross_repo_filter.go`)
rather than into new files, because `internal/query`'s non-test file set is
pinned by the dirgate grandfather ledger.

No-Regression Evidence: every predicate this change adds is an indexed equality
or an `ANY()`/`IN` membership test against the caller's grant, on a node or
column the query already matched, and it lands ahead of the existing
`SKIP`/`LIMIT` (Cypher) or `LIMIT`/`OFFSET` (SQL), so a scoped page is drawn
from the granted set instead of a cross-tenant-polluted one. A scoped caller
reads no more rows than before, and on the routes that were corpus-wide it reads
fewer. On the SQL side the grant column is `content_entities.repo_id` /
`content_files.repo_id`, plus `code_reachability_rows.repository_id` for the
cross-repo consumer read — the same columns those queries' existing
single-repository branches already filter on.

Two shapes do change for scoped callers, and both are declared.
`listMostComplexFunctions` swaps its `OPTIONAL MATCH` for a required `MATCH`
over the same `CONTAINS`/`REPO_CONTAINS` path, which removes a clause between
the anchor and the `RETURN` rather than adding one. The cross-repo consumer read
gains one extra bounded statement per scoped request: an indexed `GROUP BY` over
the same joined rows the first statement already scans, capped by the same
entity-id list, on a route that already issues a paged candidate scan plus
per-entity probes. Nothing here puts a filter in a `WITH`-attached `WHERE` (not
evaluated as a filter on NornicDB) or guards a disjunct with `$param <> ''`
(poisons the enclosing `OR` on NornicDB) — see
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).
No benchmark was run and no speedup is claimed; this is a correctness change
with no latency claim attached.

For an unscoped shared, admin, or local caller every grant predicate renders
empty and every grant parameter is unbound, so the query text those callers
execute is byte-identical to before — with one deliberate exception,
`lookupComplexityRowByID`. That branch now emits `WHERE repo.id = $repo_id`
whenever `repo_id` is supplied, for scoped and unscoped callers alike, because
it previously ignored the repository the caller named. That is a user-visible
row-set change on an existing route: `{"entity_id":"X","repo_id":"A"}` used to
return X's row from repository B and now returns not-found. It is documented in
the route's OpenAPI description and in
[HTTP API — Code](../../public/reference/http-api/code.md), and pinned by
`TestComplexityByEntityIDHonoursASuppliedRepoID`.

Byte-identity is pinned for the one hot read carrying committed plan evidence.
`callGraphMetricsEdgesCypher`'s `cypher_sha256` for `QP-CALL-GRAPH-HUBS` and
`QP-CALL-GRAPH-RECURSIVE`
(`go/internal/queryplan/testdata/handler-hot-cypher.yaml`) is unchanged, so its
accepted plan block (`NodeIndexSeek`, `Expand`; forbidden `AllNodesScan`,
`CartesianProduct`, `UnboundedExpand`) still describes what production emits.
Only the builder's `source_sha256` moved, and
`TestCallGraphMetricsUnscopedCypherIsUnchanged` keeps the unscoped text from
drifting. That plan claim covers the unscoped text only; the scoped variant has
no fixture of its own. It is row-set-neutral on both of its callers, because
`$repo_id` is mandatory and grant-resolved there, so `source.repo_id = $repo_id`
already implies the membership test.

No-Observability-Change: no metric instrument, metric label, span, log event,
route, worker, queue, lease, or runtime knob is added or renamed. The cross-repo
consumer read's existing `postgres.query` span gains one attribute,
`db.rows.hidden_consumer_entities`. Operators keep diagnosing these ten routes
through the signals that already exist: the governance-audit read-authorization
events in `go/internal/query/auth_audit.go` — `DecisionAllowed` with reason code
`scoped_read_allowed` (`recordScopedReadAuthorized`) and `DecisionDenied` with
the route's reason code (`recordScopedRouteAuthorizationDeniedWithReason`), both
stamped with tenant, workspace, actor hash, and correlation id — plus the
existing per-capability handler spans (`SpanQueryCodeTopicInvestigation`,
`SpanQueryDeadCodeInvestigation`, `SpanQueryCallGraphMetrics`,
`SpanQueryCodeStructuralInventory`, `SpanQueryHardcodedSecretInvestigation`) and
the `eshu_dp_postgres_query_duration_seconds` /
`eshu_dp_neo4j_query_duration_seconds` histograms. A scoped caller that now
reads fewer rows shows up as a smaller `count`/`truncated` in the same response
envelope these routes already return.
