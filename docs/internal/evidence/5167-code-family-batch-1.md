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
selector without consulting the grant at all, so every route that reads "no
repo_id" as "search everything" ran its query with no grant bound. Two were
worse: `code/complexity`'s entity_id branch carried no repository predicate at
all and ignored even a repo_id the caller supplied, and
`code/security/secrets/investigate` returns redacted secret line text.

`code/call-graph/metrics` is the one exception: `repo_id` is mandatory there,
and the selector resolves it against the grant before the read and rejects an
ungranted one with 400. It was never exploitable and needs no predicate.

## What Moved

Each route moves by the ledger header's five steps: a matcher, an advertised
entry classed `scopedRouteGrantBound`, the `x-scoped-token-support` marker and
the policy `403` on the OpenAPI path, and removal from the pending list. The two
matchers, `scopedCodeContentGrantRoute` and `scopedCodeGraphGrantRoute`, are
added in `go/internal/query/auth_scoped_routes_code_flow.go` and wired into
`scopedHTTPRouteSupportsTenantFilter`; `scopedCodeFlowRoute` still matches only
the four `/api/v0/code/flow/*` routes. The ledger goes from 22 pending routes to
12; it was 24 before the freshness pair left it in #6472.

| Route | Binding | Symbol |
| --- | --- | --- |
| `POST /api/v0/code/topics/investigate` | `repo_id = ANY($n)` | `codeTopicFilters` (`go/internal/query/content_reader_code_topic.go`) |
| `POST /api/v0/code/security/secrets/investigate` | `repo_id = ANY($n)` | `hardcodedSecretFilters` (`go/internal/query/content_reader_security_secrets.go`) |
| `POST /api/v0/code/symbols/search` | `repo_id = ANY($n)`; per-repo iteration on the name fallback | `symbolSearchFilters` (`go/internal/query/content_reader_symbol_search.go`), `symbolNameFallbackEntities` (`go/internal/query/code_symbol.go`) |
| `POST /api/v0/code/structure/inventory` | `repo_id = ANY($n)` | `structuralInventoryWhere` (`go/internal/query/content_reader_structural_inventory.go`) |
| `POST /api/v0/code/dead-code` | candidate choke point, both backends, plus the incoming-edge probe on the consumer side | `deadCodeCandidateRows` (`go/internal/query/code_dead_code_scan.go`), `CodeReachabilityIncomingEntityIDs` (`go/internal/query/content_reader_dead_code.go`), `buildDeadCodeGrantedIncomingBatchProbeCypher` (`go/internal/query/code_dead_code_candidate_entity.go`) |
| `POST /api/v0/code/dead-code/investigate` | same choke point, same probe | `deadCodeCandidateRows`, `CodeReachabilityIncomingEntityIDs` |
| `POST /api/v0/code/dead-code/cross-repo` | same choke point, plus the consumer-evidence read | `deadCodeCandidateRows`, `buildCrossRepoDeadCodeConsumerEvidenceQuery` (`go/internal/query/content_reader_dead_code_cross_repo.go`) |
| `POST /api/v0/code/call-graph/metrics` | mandatory `repo_id` resolved against the grant before the read; Cypher unchanged | `applyRepositorySelectorForCapability` (`go/internal/query/code_repository_selector.go`) |
| `POST /api/v0/code/quality/inspect` | grant in the MATCH-attached `WHERE` | `buildCodeQualityCypher` (`go/internal/query/code_quality.go`) |
| `POST /api/v0/code/complexity` | grant in all three builders; the list branch also changes anchor and the entity_id branch gains the supplied-repo anchor | `listMostComplexFunctions`, `lookupComplexityRowByName`, `lookupComplexityRowByID` (`go/internal/query/code_complexity_queries.go`) |

`POST /api/v0/ecosystem/graph-summary` shares that edge pass through
`graphSummaryHotEntities` (`go/internal/query/infra_graph_summary_packet.go`)
and is bound the same way, by a check ahead of the read: `getGraphSummaryPacket`
answers not-found for an out-of-grant `repo_id`.
`TestGraphSummaryHotEntitiesEdgePassIsUnchanged` pins that the scoped and
shared-key edge pass are one text, and that an out-of-grant `repo_id` never
reaches it.

The dead-code choke point's two backends: `ContentReader.DeadCodeCandidateRows`
(SQL `AND repo_id = ANY($4)` ahead of `LIMIT`/`OFFSET`) and
`buildDeadCodeGraphCypherForLabel` (Cypher
`r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` on the
Repository anchor).

Two shared helpers keep the ten from drifting apart: `codeContentGrantScope`
(`go/internal/query/code_repository_selector.go`) resolves the grant and reports
the fail-closed case, and `appendRepositoryGrantFilter` is the single SQL grant
predicate all four content builders emit.

## The Empty-Grant Trap

An empty grant is a fail-open on the SQL half of this package and a fail-closed
on the Cypher half. Both get the same gate for different reasons, and the
difference is easy to state backwards.

On the SQL side `appendRepositoryGrantFilter`
(`go/internal/query/content_reader_code_topic.go`) returns early for an empty id
list and never appends `repo_id = ANY($n)` at all, so a grantless scoped caller's
statement carries no repository restriction and reads the whole corpus. The
fail-open is the builder's *omission*, not the predicate's semantics:
`repo_id = ANY('{}')` is false for every row. On the Cypher side there is no
such hole: `GraphPredicate` and `GraphCondition`
(`go/internal/query/querycontract/repository_authz.go`) gate on `Scoped()` alone
and never on emptiness, so the same caller renders
`(repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)` against
two empty arrays, which matches nothing. Every scoped graph builder here follows
that shape.

`codeContentGrantScope` returns `blocked` ahead of both, and each route answers
with its own empty page — an empty list, a not-found, zero candidates — without
touching a backend. On the content routes that closes the corpus-wide read; on
the graph routes it is defense in depth keeping an empty grant indistinguishable
from an empty index, so index existence cannot be probed.

## A Scope Grant Is Not A Repository Id

A token can be granted a repository two ways, and only one of them is the id
these reads compare against. The grant may name the canonical repository id, or
the id of the ingestion scope that owns it — `git-repository-scope:` followed by
that same canonical id, which is how the git collector builds a repository
scope. Content rows carry the canonical id in `repo_id` and graph `Repository`
nodes carry it in `id`, so a scope-only grant pushed into `repo_id = ANY($n)` or
into `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` matches
nothing: the caller reads an empty page from a repository they were granted.
That is the same identity mismatch #5052 fixed in keyword search
([#5052 evidence](5052-keyword-search-scope-id.md)), reappearing at a new
predicate.

`RepositoryAccessFilter.WithCanonicalScopeRepositories`
(`go/internal/query/querycontract/repository_authz.go`) reads each granted git
repository scope back as the repository id it names, and `codeGrantAccessFilter`
(`go/internal/query/code_repository_selector.go`) is the one place this family
picks it up — the selector, `codeContentGrantScope`, the dead-code candidate
scan, quality, complexity, and the entity searches all take it from there. The
routes where `repo_id` is mandatory are fixed by the selector alone.

The resolution only ever adds ids, deliberately: resolving in place would empty
the id list for a caller whose grants are all non-repository scopes, which is
exactly the fail-open the section above exists to close, and growing a list
cannot reach that state. A repository-ref scope (`...@<ref>`) resolves to
nothing rather than to the repository — it names one ref and the rows carry no
ref to check it against — so that caller reads nothing, the fail-closed side.

## The Complexity List Branch Needed A New Query, Not A New Predicate

The first version of this change appended the grant to `listMostComplexFunctions`
the way every other builder got it. That query binds its Repository anchor in an
`OPTIONAL MATCH`, whose `WHERE` constrains the optional pattern, not the driving
row set. The predicate was in the text and filtered nothing.

Measured against NornicDB v1.2.3
(`timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`,
self-reports 1.2.2) on a seeded two-tenant graph, a scoped caller granted only
`repo://tenant-a/granted-service` and sending no selector got all three seeded
functions, including the other tenant's and one attached to no repository:

```text
"results":[
 {"complexity":11,"entity_id":"fn:orphan","name":"LiveOrphanComplexityProbe","repo_id":"repo.id",...},
 {"complexity":9,"entity_id":"fn:ungranted","name":"LiveUngrantedComplexityProbe","repo_id":"repo.id",...},
 {"complexity":7,"entity_id":"fn:granted","name":"LiveGrantedComplexityProbe","repo_id":"repo.id",...}]
```

Name, language, line span, complexity and the semantic metadata all reached the
caller. `repo_id` came back as the literal string `repo.id`, the separate
projection corruption recorded in [NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).

`complexityListAnchor` now gives a single anchoring
`MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)`
to every caller whose answer is restricted to some set of repositories, with the
grant and any supplied `repo_id` in that `MATCH`-attached `WHERE`, ahead of the
`ORDER BY` and `LIMIT`. A function the graph cannot attribute to a repository is
dropped there — fail-closed for a row whose repository is unknown.

The unscoped caller that names a repository is the second class the old shape
broke. Its `AND repo.id = $repo_id` was appended to that same
`OPTIONAL MATCH`-attached `WHERE`, so a shared-key, admin, or local caller
sending `{"repo_id":"A"}` ranked every `Function` in the corpus with the
repository columns nulled on everyone else's rows — a pre-existing bug, the
shape being on `origin/main`, that the live run above exposed rather than
introduced. It is fixed here rather than deferred, pinned by
`TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository`. Only an
unscoped caller that names no repository keeps the `OPTIONAL MATCH` form and its
exact text, so a corpus-wide ranking still carries a function the graph
attributes to no repository.

## The Backend-Required Half

The graph half is a clause-attachment question, and clause attachment is backend
behaviour, not a text property. Reusing the `relationshipStoryRepoPredicates`
string argues about the string, not where it sits, which is what let the first
version of the complexity list branch look proven while filtering nothing. The
live NornicDB v1.2.3 run closes that against a real backend, red before and
green after, and the evaluating fakes keep it closed credential-free.

The other four graph builders do not need the same backend run, but not because
they are all single-clause reads — three are not. The load-bearing property is
narrower: in all four the grant sits in the *anchoring* `MATCH`'s own `WHERE`, so
which rows exist is decided at the anchor. A later clause can change what a
surviving row projects; it cannot re-admit a row the anchor excluded.

| Builder | Between anchor and `RETURN` | Grant sits in |
| --- | --- | --- |
| `buildDeadCodeGraphCypherForLabel` | nothing — `MATCH … WHERE … RETURN … SKIP/LIMIT` | anchoring `MATCH`'s `WHERE` |
| `buildCodeQualityCypher` | one `WITH` plus a `WITH`-attached `WHERE` | anchoring `MATCH`'s `WHERE` |
| `lookupComplexityRowByName` | two `OPTIONAL MATCH` clauses, then `count(DISTINCT …)` aggregates | anchoring `MATCH`'s `WHERE` |
| `lookupComplexityRowByID` | the same two `OPTIONAL MATCH` clauses and aggregates | anchoring `MATCH`'s `WHERE` |

Only `buildDeadCodeGraphCypherForLabel` is genuinely single-clause, and it is the
only one asserted to be, by
`TestBuildDeadCodeGraphCypherKeepsTheScopedVariantSimple`. The quality builder's
`WITH`-attached `WHERE` is the pre-existing `codeQualityMetricFilter` on
complexity, line count and argument count, never on a repository; both
complexity lookups' `OPTIONAL MATCH` clauses come from the pre-existing
`complexityCandidateProjection`, which counts relationships. This change adds no
clause to any of the four and moves no predicate out of an anchor.

`nornicdb-query-pitfalls.md` records both shapes as risky on this backend
family, and the risk is to projected *values*, not row membership. Membership is
the tenancy question and the anchor settles it. That projection risk is real,
out of scope, and named so a later reader does not read these four as audited
clean end to end; the evaluating-fake route tests check the Repository binding's
clause attachment, not intervening clauses.

```bash
docker run -d --name nornic-5167-p0 -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_NO_AUTH=true -p 17687:7687 \
  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
cd go && ESHU_NEO4J_URI=bolt://localhost:17687 go test ./internal/query \
  -tags live_nornicdb_complexity_grant -run TestLiveNornicDBComplexityList -count=1
```

Host: MacBook Pro, arm64, macOS. The run is a correctness proof on a three-node
seeded graph, not a latency measurement; no timing from it is cited anywhere.

## The Incoming-Edge Probe Was Reading Another Tenant

The candidate scan is only half of what `/dead-code` and
`/dead-code/investigate` answer. The other half is the incoming-edge probe that
decides whether a candidate is still called, and both of its reads looked
outside the grant. `ContentReader.CodeReachabilityIncomingEntityIDs` is
deliberately not repo-scoped — a library symbol is kept alive by the service
repositories that call it — and `buildDeadCodeIncomingBatchProbeCypher` accepted
a source from any repository at all. So a candidate in a granted repository
vanished from the answer when a repository the caller was never granted called
it. Wrong twice: the caller is told a symbol is reachable on evidence they may
not read, and the gap left in the page says a hidden consumer exists.

Both reads now take the caller's grant, and neither drops a row for it. The SQL
projects `(row.repository_id = ANY($n)) AS consumer_in_grant` rather than
filtering in the `WHERE`: filtering would leave the symbol looking
unreferenced, which is a wrong answer rather than a safe one. The graph half
does the same thing in Cypher. `buildDeadCodeScopedIncomingBatchProbeCypher`
expands the candidate's incoming edges once, optionally matches the source's
repository, and projects the grant per row as `in_grant`.

An edge from outside the grant reaches the answer as
`deadCodeIncomingEdge.HiddenConsumer` with no confidence, no repository, and no
citation. It cannot filter the candidate out and cannot be reported as evidence:
the candidate is kept, classified `ambiguous`, and carries
`permission_hidden_consumer` — the same reason the cross-repo route already
answers with. Never live, never dead. An unscoped caller runs one statement, the
unchanged one, and gets the unchanged answer.

### One Probe, Because Two Could Not See A Same-Method Source

The graph half first shipped as a pair: a grant-bound probe whose rows were
evidence, and the unrestricted probe run after it, with the rows only the second
returned taken as hidden consumers. Both `RETURN DISTINCT`ed the
`(entity, resolution_method)` pair, and that is where it failed. When an
out-of-grant source calls the candidate with a resolution method a granted
source also carries, its row is byte for byte the granted row: the difference
between the two probes is empty, `HiddenConsumer` is never set, and the caller
is told the symbol is plainly reachable. The SQL half never had this hole,
because `consumer_in_grant` decides the grant per row. The two backends
disagreed on the same candidate.

Measured on the pinned NornicDB v1.2.3 replay image against a graph seeded with
one target and three sources sharing one resolution method — one inside the
grant, one outside it, one attached to no repository — the withdrawn pair
returned one identical row each, so the diff was empty. The shipped probe
returns two groups on the same graph: `in_grant=true` with one edge, and
`in_grant=false` with two, the out-of-grant source and the unattributed one
together. `TestLiveNornicDBDeadCodeIncomingWithdrawnPairCollapses` keeps the
old behaviour on record beside the new one.

Two clauses of the shipped statement are load-bearing on that backend, and both
were chosen by measurement rather than taste:

- It groups with `count(*)`, not `RETURN DISTINCT`. On the pinned backend,
  `DISTINCT` after a trailing `OPTIONAL MATCH` on the relationship-seeded
  traversal branch is absorbed into the first projection's source text:
  `incoming_entity_id` came back as the literal string
  `"DISTINCT coalesce(e.uid, e.id)"` and nothing was deduplicated. Moving the
  projections behind a `WITH` is worse — every other column came back null. The
  variant table and the executor boundary are in
  [NornicDB Pitfalls](../../public/reference/nornicdb-pitfalls.md);
  `TestLiveNornicDBDeadCodeIncomingRejectsReturnDistinct` is the negative
  control that goes red when the pin stops corrupting it.
- The source repository is an `OPTIONAL MATCH` here, which is the opposite of
  the complexity list's fix above and for the opposite reason. There the grant
  was a filter, so an optional binding filtered nothing. Here the grant is a
  projection, so a required binding would drop the two row classes this probe
  exists to report: a source in an ungranted repository, and a source the graph
  cannot attribute to any repository. Both project `in_grant=false` and become
  the hidden marker.

Performance Evidence: expanding once is also the cheaper shape, which is the
half the pair shipped without a number. Worst case for this probe is a
high-fan-in symbol, because each statement expands every incoming edge before
its own grouping reduces anything. Seeded on the pinned NornicDB v1.2.3 image
(`timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`,
self-reporting 1.2.2, `NORNICDB_EMBEDDING_ENABLED=false`, bolt via
`neo4j-go-driver/v5`, host MacBook Pro arm64 macOS): one `Function` with 5,000
incoming `CALLS` edges, 2,500 sourced from a granted repository and 2,500 from
an ungranted one, five resolution methods spread across them; the caller is
granted one of the two. Four runs of 15 iterations, one discarded warm-up round
per run, candidates measured in alternating order within each iteration so drift
cannot favour whichever went first. Medians:

| Run | Withdrawn pair | Shipped merged probe | One unrestricted probe |
| ---: | ---: | ---: | ---: |
| 1 | 529.3 µs | 279.7 µs | 247.5 µs |
| 2 | 547.4 µs | 273.8 µs | 258.5 µs |
| 3 | 497.3 µs | 278.9 µs | 244.1 µs |
| 4 | 583.1 µs | 303.0 µs | 297.2 µs |

The merged probe costs about what a single probe costs, and 44–49% less than the
pair — which is what one expansion instead of two should look like.

A first pass at this measurement said the opposite, and is recorded because the
mistake is easy to repeat: measuring each candidate as a block of nine
consecutive runs, immediately after the 5,000-edge write, put the merged probe
at a 3.07 ms median against 1.75 ms for the pair. The blocks were not comparable
— the store was still settling under the first one — and the interleaved,
warmed-up runs above reverse it four times out of four.

No-Observability-Change: the probe keeps the span and statement attributes every
graph read in this package carries through `Neo4jReader.Run`
(`eshu_dp_neo4j_query_duration_seconds`, labelled by statement), and there is now
one statement per candidate page where there were two, so the same signal is
what shows the change in production.

## Cross-Repo Consumer Evidence

`POST /api/v0/code/dead-code/cross-repo` reads consumer evidence for its
producer candidates. That read used to reach Postgres with no grant: it fetched
every tenant's consumer rows, capped them at 1000, and dropped the out-of-grant
ones in Go afterwards. No consumer identity ever left the process — hidden rows
are counted, never projected — but the cap fell on a mixed set, so another
tenant's rows could push a granted consumer off the page.

A scoped caller that names no consumers now runs one statement shape twice, for
two different questions. The evidence page binds the grant
(`buildCrossRepoDeadCodeConsumerEvidenceQuery` rendering
`AND row.repository_id = ANY($n)`) ahead of the `LIMIT`, so the cap falls on
consumers the caller may see. The signal read is the same builder with no
grant, which makes its text byte for byte the statement this route already
shipped. `TestCrossRepoDeadCodeSignalReadRepeatsTheUngrantedStatement` pins
both: the grant's position in the first, the exact text of the second.

Filtering in SQL alone would destroy the signal the handler needs — a symbol
whose only consumers are out of grant must stay `unknown_needs_evidence` with
reason `permission_hidden_consumer`, not become `dead`. The signal read carries
it: `filterCrossRepoDeadCodeEvidence` runs over its rows exactly as over the
page's — the request's `consumer_repo_ids` selector first, then the grant — and
only what is left counts. The count is all that crosses.

Applying that selector before counting is the correctness half, not a
refinement. A caller granted producer P and consumer A, asking about A alone,
must get `live_by_consumer` from A's own strong evidence even when an unrelated
ungranted repository also consumes the symbol. Counting that consumer buried
A's evidence under `permission_hidden_consumer`;
`TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector` is the guard.

The selector belongs in the page read as well, not only in the Go filter after
it. A caller granted P, A and B, asking about B alone, had the page cut from the
whole grant: a thousand rows from A filled it, B's own row fell off the end, and
B came back `unknown_needs_evidence` with `consumer_evidence_truncated` for a
symbol B proves live. `crossRepoDeadCodeConsumerReadPlan` now decides which list
the page binds — the request's consumers when it named any, the grant when it
did not, nothing only for an unscoped caller who named neither — so the row cap
falls where the question is. The named consumers are intersected with the grant
again on the way in, and a scoped caller left with an empty list reads nothing
rather than rendering the unbounded statement an empty list would produce; the
candidates then stay unknown.

That request also skips the signal read entirely, which is a removal of work
rather than a relaxation. `filterCrossRepoDeadCodeEvidence` drops every signal
row outside the named consumers before anything is counted, and every named
consumer the grant admits is inside the grant, so the count that read
contributes is empty by construction.
`TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository` drives the
whole route over the shipped `ContentReader` against a driver that filters on
the repository array the statement actually binds, and asserts both halves: `B`
answers `live_by_consumer`, and exactly one consumer statement was sent.
`TestCrossRepoDeadCodeConsumerReadPlan` pins the other five shapes, including
the two that read nothing.

The truncation fail-safe is per entity, not per request. Each read reports the
entities it finished — the ones it returned rows for and moved past before the
cap — and every other entity takes `consumer_evidence_truncated` and answers
unknown, never `dead`. Page rows are not coverage: an entity with strong granted
evidence still takes the marker when the signal read stopped before reaching it,
because that unread half is where an ungranted consumer would be. Both halves
are pinned: `TestCrossRepoDeadCodeSignalTruncationKeepsCandidatesUnknown` and
`TestCrossRepoDeadCodeSignalTruncationMarksEntitiesTheSignalNeverReached`.

The boundary itself is the exception. The sentinel row is dropped, but its
entity id is already scanned, and the statement orders by entity id — so a
sentinel naming a different entity than the last one returned proves the read
stopped *between* two entities rather than inside one, and every entity it
returned rows for is complete. Throwing that id away marked a full page of
strong consumer evidence `consumer_evidence_truncated` and answered unknown for
a read that was never short.
`TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast` sits on exactly
row 1,001.

### Two Bounded Reads, Not An Unbounded Complement

The first version of that signal was a statement of its own, counting the
*complement* of the page with one `LATERAL` arm per producer entity, each capped
at 100 rows. That cap bounds rows returned, not rows scanned, and it misses the
common case: when the grant covers most consumers, every arm inspects all of its
entity's reachability rows to prove none are outside the grant.

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` on the withdrawn `LATERAL`
statement and on the shipped signal read, in a throwaway PostgreSQL 16.15
container, data-plane schema applied from `schema/data-plane/postgres`
(`001_ingestion_scopes.sql`, `002_scope_generations.sql`,
`027_code_reachability.sql`) in filename order, synthetic rows only,
`VACUUM ANALYZE` after seeding, `SET jit = off` on both, warm (second) run
reported. Host: MacBook Pro, arm64, macOS. 1,300,000 `code_reachability_rows`;
one active scope and generation; a 50-entity producer page; 2,000 consumer rows
per page entity, all of them *inside* the grant across five granted repositories
— the no-hidden worst case — plus 1.2M rows on entity ids off the page.

| Metric | Withdrawn `LATERAL` complement | Shipped signal read |
| --- | ---: | ---: |
| Execution time | 18.962 ms | 5.958 ms |
| Rows read under the driving scan | 100,000 | 2,001 |
| Rows returned | 0 | 1,001 |
| Shared buffers | hit=3558 | hit=73 |
| Driving access | `Index Scan`, `Rows Removed by Filter: 2000`, `loops=50` | `Index Scan` under an `Incremental Sort` presorted on `entity_id`, under `Limit` |

The `LATERAL` plan says it in its own line: each of the 50 arms removed all
2,000 of its entity's rows on the grant-complement filter and returned none, so
the per-arm `LIMIT 100` never engaged. The shipped read does stop at its cap —
`Limit` takes 1,001 rows and the loop beneath produces 2,001 of 100,000
candidates — because `code_reachability_entity_lookup_idx` leads with
`entity_id`, the `ORDER BY` leads with `entity_id`, and the `Incremental Sort`
above therefore sorts one entity group at a time.

That is why the marking is load-bearing rather than defensive: a page whose
first entity carries more than 1,001 consumer rows spends the sentinel there,
leaving every later entity unproven whatever its own page rows say.
`hidden_consumer_evidence_count` is in no OpenAPI schema or public reference.

## Query-Plan Source Coverage

`go test ./internal/queryplan` was red on this branch before this pass, and no
earlier verification list ran that package. Six callsites failed
`TestHotCypherManifestCoversEveryProductionQueryCall`, because adding a grant
predicate changes the enclosing symbol's `source_sha256` and the manifest
freezes it:

```text
code_call_graph_metrics.go:(*CodeHandler).callGraphMetricsData: hot callsite source_sha256 does not match production symbol
code_complexity_queries.go:(*CodeHandler).listMostComplexFunctions: grandfathered source_sha256 does not match production symbol
```

The other four — `lookupComplexityRowByName`, `deadCodeCandidateRows`,
`inspectCodeQuality`, `graphSummaryHotEntities` — printed the same
grandfathered-digest line. That is the gate working as designed: a changed
digest forces the owning callsite through a typed non-hot audit rather than
letting a prose `non_hot_reason` carry forward. The five grandfathered prose
entries become typed dispositions carrying the bound each read already enforces,
and leave `grandfatheredNonHotSourceDigests`. Later passes move three digests
again — `listMostComplexFunctions` for the anchor fix, `callGraphMetricsData`
for its grantless-caller refusal, `graphSummaryHotEntities` for a corrected
comment — each re-recorded against the production symbol with its disposition
and bound re-audited unchanged. `handler-hot-cypher.yaml` ends this branch
untouched: `callGraphMetricsEdgesCypher` carries no grant, so its
`source_sha256` and the `cypher_sha256` for `QP-CALL-GRAPH-HUBS` and
`QP-CALL-GRAPH-RECURSIVE` are the values already committed:

| Callsite | Class | Bound |
| --- | --- | --- |
| `listMostComplexFunctions` | `label_inventory` | `Function`, 101 rows (`complexityMaxListLimit` + 1) |
| `lookupComplexityRowByName` | `keyed_support` | single key `$entity_name`, 3 rows (`complexityNameCandidateLimit` + 1) |
| `deadCodeCandidateRows` | `label_inventory` | one candidate label per page from the closed `deadCodeCandidateLabels` set, 250 rows (`deadCodeCandidateQueryMax`) |
| `inspectCodeQuality` | `label_inventory` | `Function`, 101 rows (`codeQualityMaxLimit` + 1) |
| `graphSummaryHotEntities` | `keyed_support` | single key `$repo_id`, 50001 rows (`callGraphMetricsEdgeScanLimit` + 1) |
| `deadCodeResultsWithGraphIncomingEdges` | `keyed_support` | bounded key batch of one candidate page, 250 keys (`deadCodeCandidateQueryMax`), 2500 rows (one per key per resolution method) |

The sixth entry is the incoming-edge probe. It kept its prose disposition until
this pass; moving it to `code_dead_code_candidate_entity.go` and giving it a
second statement forced the same audit, and a new callsite may not use
`non_hot_reason` at all, so it left the grandfather ledger for the typed row
above.

## Proof Ledger

The route-by-route red/green runs and the BITES mutation ledger — what was
broken on purpose, which guard judged it, and the exit code — live in [#5167
code family batch 1 proofs](5167-code-family-batch-1-proofs.md), split out
because the two together outgrow the repository's 500-line Markdown cap.

## Verification

Run after the last edit, exit codes captured directly:

```text
cd go && go test ./internal/query ./internal/query/querycontract \
  ./internal/mcp ./cmd/api ./internal/queryplan -count=1              # 0
cd go && go vet ./internal/query/... ./internal/mcp ./internal/queryplan  # 0
scripts/dev/precommit-go.sh fmt   <changed .go>                       # 0
scripts/dev/precommit-go.sh lint  <changed .go>                       # 0 (4 packages from 75 paths, 0 issues)
scripts/dev/precommit-go.sh filecap <changed .go>                     # 0
scripts/verify-package-docs.sh                                        # 0
scripts/verify-openapi.sh                                             # 0 (255 routes, 255 path entries)
scripts/verify-doc-citations.sh                                       # 0
scripts/verify-markdown-line-cap.sh --all                             # 0
scripts/verify-performance-evidence.sh                                # 0
mkdocs build --strict --clean --config-file docs/mkdocs.yml            # 0
git diff --check                                                      # 0
```

The lint list is the full three-dot changed `.go` set, so the queryplan
re-audit this PR leans on is inside the gate it cites.

On origin/main `code_dead_code.go` was 496 lines and `code_dead_code_scan.go`
was 468; this change pushed both over the 500-line cap, and
`code_dead_code_cross_repo.go` followed later. The candidate-page request type,
the scan budget helpers, the candidate-label predicate, the cross-repo
consumer-evidence filter and, in the round-7 pass, the whole incoming-edge probe
family moved to sibling files that already own those families rather than to new
ones, because `internal/query`'s non-test file set is pinned by the dirgate
grandfather ledger.

No-Regression Evidence: every predicate this change adds is an indexed equality
or an `ANY()`/`IN` membership test against the caller's grant, on a node or
column the query already matched, and it lands ahead of the existing
`SKIP`/`LIMIT` (Cypher) or `LIMIT`/`OFFSET` (SQL), so a scoped page is drawn
from the granted set instead of a cross-tenant-polluted one. A scoped caller
reads no more rows than before, save the one widened `DISTINCT` key declared
below, and on the routes that were corpus-wide it reads fewer. On the SQL side
the grant column is `content_entities.repo_id` / `content_files.repo_id`, plus
`code_reachability_rows.repository_id` — the same columns those queries'
single-repository branches already filter on.

Two shapes do change, and both are declared. `listMostComplexFunctions` swaps
its `OPTIONAL MATCH` for a required `MATCH` over the same
`CONTAINS`/`REPO_CONTAINS` path for a scoped caller or a supplied `repo_id`,
which removes a clause between the anchor and the `RETURN` rather than adding
one. The cross-repo consumer read runs one extra statement per scoped request,
on a route that already issues a paged candidate scan plus per-entity probes.
That statement is the ungranted read this route shipped before the grant landed,
unchanged and capped at the same 1001 rows, and it is measured rather than
asserted — see "Two Bounded Reads, Not An Unbounded Complement".

The round-7 incoming-edge probe adds a third such shape, declared without a
measurement. A scoped caller now runs two graph statements where it ran one:
the grant-bound probe, which adds a `CONTAINS`/`REPO_CONTAINS` hop from the
source to its repository, and the unrestricted probe unchanged. Both are the
same bounded `UNWIND $entity_ids` shape over one candidate page. The SQL half
adds no predicate and no scan: the grant is a projected boolean over
`code_reachability_rows.repository_id`, a column of the table the read already
scans, not one the statement returned before. It widens the `SELECT DISTINCT`
key from two columns to three, so an entity and method reachable from both a
granted and an ungranted consumer returns two rows where it returned one — at
most 2x over one candidate page. Every one of these costs falls only on scoped
callers, who could not reach these routes before this PR. Nothing here
puts a filter in a `WITH`-attached `WHERE` (not evaluated as a filter on
NornicDB) or guards a disjunct with `$param <> ''` (poisons the enclosing `OR`
on NornicDB) — see
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).
No benchmark was run and no speedup is claimed; this is a correctness change
with no latency claim attached.

For an unscoped shared, admin, or local caller every grant predicate renders
empty and every grant parameter is unbound, so the query text those callers
execute is byte-identical to before — with two deliberate exceptions, both on
`POST /api/v0/code/complexity`, and both about a `repo_id` the caller supplied
and the query then ignored. `lookupComplexityRowByID` now emits
`WHERE repo.id = $repo_id` whenever `repo_id` is supplied, so
`{"entity_id":"X","repo_id":"A"}` used to return X's row from repository B and
now returns not-found, and a `function_name` sent with it no longer softens
that: the name fallback runs only for an id lookup bound to no repository, the
one case where an empty result proves the id stale rather than held elsewhere
(`complexityIDLookupIsRepositoryBound`). `listMostComplexFunctions` takes the
required Repository
anchor on the same condition, so `{"repo_id":"A"}` ranks A's functions instead
of the whole corpus with other repositories' rows nulled. Both are user-visible
row-set fixes, documented in the route's OpenAPI description and in
[HTTP API — Code](../../public/reference/http-api/code.md), and pinned by
`TestComplexityByEntityIDHonoursASuppliedRepoID` and
`TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository`.

Byte-identity is pinned for the one hot read carrying committed plan evidence.
`callGraphMetricsEdgesCypher` is untouched, so its whole manifest entry
(`go/internal/queryplan/testdata/handler-hot-cypher.yaml`) holds the digests it
already held, and its accepted plan block (`NodeIndexSeek`, `Expand`; forbidden
`AllNodesScan`, `CartesianProduct`, `UnboundedExpand`) describes what every
caller emits rather than only an unscoped one.
`TestCallGraphMetricsCypherIsTheSameForEveryCaller` and
`TestCallGraphMetricsUnscopedCypherIsUnchanged` keep it that way.

No-Observability-Change: no metric instrument, metric label, span, log event,
route, worker, queue, lease, or runtime knob is added or renamed. The cross-repo
consumer read's existing `postgres.query` span gains one attribute,
`db.rows.consumer_signal_entities`. Operators keep diagnosing these ten routes
through the governance-audit read-authorization events in
`go/internal/query/auth_audit.go` — `DecisionAllowed` / `scoped_read_allowed`
(`recordScopedReadAuthorized`) and `DecisionDenied` with the route's reason code
(`recordScopedRouteAuthorizationDeniedWithReason`), both stamped with tenant,
workspace, actor hash, and correlation id — plus the existing per-capability
handler spans (`SpanQueryCodeTopicInvestigation`,
`SpanQueryDeadCodeInvestigation`, `SpanQueryCallGraphMetrics`,
`SpanQueryCodeStructuralInventory`, `SpanQueryHardcodedSecretInvestigation`) and
the `eshu_dp_postgres_query_duration_seconds` /
`eshu_dp_neo4j_query_duration_seconds` histograms. A caller that now reads fewer
rows shows up as a smaller `count`/`truncated` in the same response envelope.
