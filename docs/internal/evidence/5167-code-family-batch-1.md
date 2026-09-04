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
when nothing granted proves the symbol used, the candidate is kept, classified
`ambiguous`, and carries `permission_hidden_consumer` — the same reason the
cross-repo route answers with, in the same order. A strong granted edge beside
it still drops the candidate as reachable, because a consumer the caller may
read has already answered the question. An unscoped caller runs one statement,
the unchanged one, and gets the unchanged answer.

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
old behaviour on record beside the new one. All three live tests are
build-tagged and were run by hand against the pinned image; no CI job builds
that tag, so re-run them when the pin moves.

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
  `TestLiveNornicDBDeadCodeIncomingRejectsReturnDistinct` covers every row of
  that table. It is a manual control, not a CI one — nothing in `.github/` or
  the Makefile builds the `live_nornicdb_dead_code_incoming` tag — so it fires
  only when someone runs it against the pin.
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

The merged probe costs 44–50% less than the pair, and 2–14% more than a single
probe on its own. That overhead has the same sign in all four runs, so it is not
noise: it is the `OPTIONAL MATCH` the pair never paid for, and it buys the
per-row grant answer the pair could not give. One expansion instead of two is
what the 44–50% is.

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

A scoped caller that names no consumers now runs two statements for two
different questions. The evidence page binds the grant
(`buildCrossRepoDeadCodeConsumerEvidenceQuery` rendering
`AND row.repository_id = ANY($n)`) ahead of the `LIMIT`, so the cap falls on
consumers the caller may see. The second is
`crossRepoDeadCodeUngrantedConsumerProbeQuery`, a bounded existence probe that
returns the producer entities with a consumer outside the grant and nothing
else. `TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe` pins both: the
grant's position in the first, the shape of the second.

Filtering in SQL alone would destroy the signal the handler needs — a symbol
whose only consumers are out of grant must stay `unknown_needs_evidence` with
reason `permission_hidden_consumer`, not become `dead`. The probe carries it,
and carries only it: the entity ids it returns are the caller's own producer
candidates, so no ungranted consumer's repository, entity, or citation crosses
the reader boundary at all.

*Whose only consumers* is the whole of it. A hidden consumer decides the answer
only when nothing granted proves use — hidden-only, or hidden beside weak
granted evidence. A strong granted consumer settles the symbol `live_by_consumer`
even with a hidden one beside it, and the row still carries
`hidden_consumer_evidence_count`, so the caller is told both things. That is the
order `applyDeadCodeIncomingEdges` has always applied on `/dead-code` and
`/dead-code/investigate`; this route built its reasons before consulting
`crossRepoDeadCodeHasStrongLiveEvidence` and so answered `unknown` where the
other answered reachable, for the same mixed shape.
`TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer` pins all
three cases, and `applyDeadCodeIncomingEdges`'s doc comment names its
counterpart so the next change to either one finds the other. `consumer_evidence_truncated` is not outranked and still forces
unknown: a page the read never finished is incomplete evidence, not evidence
that is known and unreadable.

Keeping a request's `consumer_repo_ids` selector out of that count is the
correctness half, not a refinement. A caller granted producer P and consumer A,
asking about A alone, must get `live_by_consumer` from A's own strong evidence
even when an unrelated ungranted repository also consumes the symbol. Counting
that consumer buried A's evidence under `permission_hidden_consumer`;
`TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector` is the guard, and
`crossRepoDeadCodeConsumerReadPlan` makes it structural by not running the probe
at all for a request that named consumers.

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

That request also skips the ungranted-consumer probe entirely, which is a
removal of work rather than a relaxation. Every named consumer the grant admits
is inside the grant, so the only consumers the probe could report are ones the
request excluded, and those must not be counted.
`TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository` drives the
whole route over the shipped `ContentReader` against a driver that filters on
the repository array the statement actually binds, and asserts both halves: `B`
answers `live_by_consumer`, and exactly one consumer statement was sent.
`TestCrossRepoDeadCodeConsumerReadPlan` pins the other five shapes, including
the two that read nothing.

The truncation fail-safe is per entity, not per request. The page reports the
entities it finished — the ones it returned rows for and moved past before the
cap — and every other entity takes `consumer_evidence_truncated` and answers
unknown, never `dead`. The probe contributes no coverage gap of its own, because
it answers for every entity it is given;
`TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven` is the guard on that, and
`TestContentReaderCrossRepoDeadCodeEvidenceMarksMissingEntitiesUnknownWhenTruncated`
on the page half.

The boundary itself is the exception. The sentinel row is dropped, but its
entity id is already scanned, and the statement orders by entity id — so a
sentinel naming a different entity than the last one returned proves the read
stopped *between* two entities rather than inside one, and every entity it
returned rows for is complete. Throwing that id away marked a full page of
strong consumer evidence `consumer_evidence_truncated` and answered unknown for
a read that was never short.
`TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast` sits on exactly
row 1,001.

### The Signal Half Is Two Bounded Reads

The shipped signal read is the fourth shape of that half. Three were withdrawn
on measurements: an unbounded complement of the page, the page statement re-run
with no grant bound, and a probe seeking one `repository_id` range per gap in
the sorted grant. What ships walks the producer entity's own distinct
`(repository_id, scope_id)` pairs in index order and stops at the first pair the
grant does not contain, so its cost follows the answer rather than the entity's
fan-in, the caller's grant size, the generations retention still keeps, or the
number of ingestion scopes covering a repository.

Performance Evidence: the walk's buffer count does not move along any axis
measured. It reads `hit=4,886` at every grant size from 5 to 500 repositories,
where the range shape climbs from `hit=7,622` to `hit=626,377`; about
`hit=3,150` from 0 to 200 retained generations, where the walk on the
two-column index climbs to `hit=1,150,489`; and `hit=2,255` whether one or
fifty scopes cover a granted repository, where pair stepping read `hit=26,788`.
Across every point measured the walk reads between 2,255 and 6,081 buffers, the
larger figures belonging to the wider fan-out fixtures, never to the axis under
test. Every `EXPLAIN (ANALYZE, BUFFERS)` table
behind those numbers, the exactness differentials, and the bootstrap-replay
proof for migrations 101 and 102 are in
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md).

The truncation fail-safe gets simpler and stricter at once. Only the evidence
page can now stop short, so `markCrossRepoDeadCodeConsumerEvidenceTruncated`
takes one coverage set instead of two. The case it used to cover — one busy
producer entity spending the shared signal budget and leaving every later
candidate on the page `unknown_needs_evidence` whatever its own evidence said —
cannot happen, because the probe answers for every entity it is given.
`TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven` is the guard.

## Verification

Run after the last edit, exit codes captured directly:

```text
cd go && go test ./internal/query ./internal/query/querycontract \
  ./internal/mcp ./cmd/api ./internal/queryplan -count=1              # 0
cd go && go test ./internal/storage/postgres -count=1                 # 0
ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=... \
  go test ./internal/query -run ...UngrantedConsumerProbeLive -count=1 # 0
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

## Proof Ledger

The route-by-route red/green runs, the BITES mutation ledger — what was broken
on purpose, which guard judged it, and the exit code — the query-plan manifest
re-audit, and what this change costs on the read path live in [#5167 code family
batch 1 proofs](5167-code-family-batch-1-proofs.md). The measurement record for
the cross-repo hidden-consumer read is in [#5167 cross-repo hidden-consumer
walk](5167-cross-repo-hidden-consumer-walk.md). The three notes are split
because together they outgrow the repository's 500-line file cap.
