# AppSync API key disclosure remediation

[CodeQL alert 20](https://github.com/eshu-hq/eshu/security/code-scanning/20)
reports `go/weak-sensitive-data-hashing` at the stable-ID hashing sink. Its
recorded dataflow originates in the AppSync API-key observation. Investigation
of that path found that the adapter and scanner copied `ApiKey.Id` into
resource identities and durable facts.

The scanner now drops IDs and descriptions at the SDK boundary, keeps only
API-level key count and lifecycle timestamps, and emits no per-key graph
resource. Legacy `aws_appsync_api_key` identities are filtered from cloud inventory
lists and other graph-backed identity reads. Fixed facet totals remain stored-node
counts and may include historical records, but expose no key identity fields. SQL and Cypher readers
apply the filter before ordering, pagination, candidate resolution, or result
hydration where those operations could otherwise expose a legacy key.

## Verification evidence

- RED regression tests failed before the read filters were added, including the
  Postgres identity page, graph list/count, infra search, and resource selector.
- Focused Go tests pass for the affected query, impact, deployment, compare,
  entity, and query-plan packages.
- A disposable PostgreSQL 18 fixture verifies that a seeded historical key is
  omitted from a full identity page while healthy resources remain. The same
  suite exercises 20,000 owner rows across 64 production SQL variants against a
  2-second per-query budget.
- An isolated Neo4j fixture verifies ten HTTP read paths: infra search, graph
  listing, entity context, infra relationships, resource investigation, change
  surface, entity map, deployment trace, environment comparison, and dependency-path tracing. A synthetic dependency
path whose only route crosses the hidden key is returned as no path. The
  synthetic key's stored name, resource ID, and ARN do not appear in responses;
  explicit selector requests return no match.
- Query source digests, emitted query hashes, and the 64-variant SQL family
  registry match the final production builders.
- The live regressions are enrolled in their matching runners: the untagged
  PostgreSQL proof is selected by generated live-readiness markers and the
  Neo4j proof is a `live_nornicdb_answer_truth` CI target pinned to Neo4j. The
  shared temporary PostgreSQL corpus helper is in
  `cloud_resource_legacy_keys_fixtures_test.go` so the untagged proof compiles.
  The PostgreSQL test passed on disposable PostgreSQL 18 in 1.083 s; the graph
  test passed on a fresh pinned Neo4j fixture in 6.562 s. Both test names are
  bound to their runner inventories and exact selectors.
- Enrollment checks passed: `python3 scripts/generate-live-postgres-readiness.py
  --check`, `bash scripts/test-generate-live-postgres-readiness.sh`,
  `bash scripts/verify-live-tests-ledger.sh`, and
  `python3 scripts/test-live-tests-registry.py`.

The historical-key filters change reads only. The scanner also stops emitting
per-key facts, so future graph projections no longer receive those key records.
No schema, queue behavior, or runtime instrumentation changed. Existing
`postgres.query` tracing and error recording remain in use.

Performance Evidence: on the isolated `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` fixture, an alternating warm Bolt comparison over 80 paired rounds used the exact pre-change query (SHA-256 `8e54e6fa6e5507f072062b6257802a748bde0cbfa5ca364d9dbe3fb73852d033`) and production candidate (SHA-256 `a9e94e4d1d12491e54404e511e17e36b2988356269973a77ea6a325cc386b27d`). The 1,000-edge graph included 20 historical keys; both paths returned the same 980 public relationships after `extractRelationships`. From Bolt `Session.Run` through result consumption and relationship filtering, baseline median/p95 was 4.651/7.718 ms and candidate was 5.184/7.224 ms. This synthetic result shows +0.534 ms median and -0.494 ms p95; it is not a production latency claim.

No-Regression Evidence (takeover fix): the shipped candidate is SHA-256 `42cae3d157d5e2638fd575ba68f4ac41d37ad78e9d53f4ad63c79f7b2da0624b`, which differs from the measured candidate above by exactly one line: the bare `WITH e` passthrough before the `OPTIONAL MATCH` pair is removed. On Neo4j `WITH e` binds no new variables and changes no cardinality, so the paired filtering measurement above still proves the filtering behavior; no new bench was run for the one-line removal (correctness fix, not an optimization). The NornicDB leg is the reason: on the pinned NornicDB executor that bare `WITH` empties the trailing relationship match (answer-truth regression `TestLiveNornicDBEntityContextAnswerTruth` failed before, passes after), and the same executor zeroes any `labels()`-in-`WHERE` result while silently ignoring property-only predicates — so legacy-key suppression on NornicDB-served paths is enforced in Go from projected labels/types, with identical output on Neo4j (live proof `TestHistoricalAppSyncKeyReadRoutesSuppressLegacyGraphNodeLive` passes on the shipped statement). No-Observability-Change: no spans, metrics, or log lines were added or removed; existing query traces and counters remain available.

Paired Graph-Read Cost Evidence: on the same pinned Neo4j image, exact source-built query statements from `origin/main` (`5d026f27`) and candidate `a279605f` were alternated over one disposable fixture containing 1,000 query-matched CloudResources, 20 historical API-key rows ordered before the page limit, one anchor, and one workload/instance path. Each row reports the median and nearest-rank p95 of 80 warm Bolt `Session.Run`-through-result-consumption samples, plus the sum of `DbHits` across PROFILE operators. For each family, ordered candidate IDs matched the baseline IDs after removing API-key rows and applying the same limit; the measured page/traversal outputs contained 20 API-key rows on baseline and zero on candidate.

| Query family | Median ms, base → candidate | p95 ms, base → candidate | Sum of operator DbHits, base → candidate |
| --- | ---: | ---: | ---: |
| CloudResource search page | 0.860 → 1.180 | 1.097 → 1.416 | 2,156 → 4,136 |
| Entity-map direct neighbor | 1.712 → 2.184 | 2.462 → 2.665 | 6,217 → 10,137 |
| Entity-map variable traversal | 1.846 → 2.333 | 2.116 → 2.624 | 7,115 → 11,035 |
| Materialized workload dependencies | 3.360 → 3.329 | 3.750 → 3.990 | 40,014 → 39,254 |

A second run used the same exact statements, key ordering, and paired method with 17,000 query-matched CloudResources (17,001 including the separate anchor). Ordered filtered-result parity held for all four families.

| Query family | Median ms, base → candidate | p95 ms, base → candidate | Sum of operator DbHits, base → candidate |
| --- | ---: | ---: | ---: |
| CloudResource search page | 6.890 → 13.367 | 10.131 → 18.711 | 34,156 → 68,136 |
| Entity-map direct neighbor | 18.277 → 28.007 | 22.808 → 38.213 | 86,217 → 154,137 |
| Entity-map variable traversal | 22.504 → 31.617 | 25.429 → 35.802 | 103,115 → 171,035 |
| Materialized workload dependencies | 50.442 → 50.237 | 54.906 → 55.793 | 680,014 → 679,254 |

The two-sided infra-relationship query was then measured with the exact source-built base/candidate statements and both incoming and outgoing collections populated. On the same 17k-resource graph, 500 neighbors in each direction (250k joined combinations) used 20 alternating warm pairs: median/p95 was 323.924/339.569 ms base and 549.582/565.760 ms candidate; PROFILE operator `DbHits` was 2,036,750 → 3,522,473. At 1,000 neighbors in each direction (1M joined combinations), 10 alternating pairs yielded median/p95 1,327.804/1,337.236 ms base and 2,294.447/2,314.385 ms candidate; `DbHits` was 8,039,250 → 14,505,973. Complete aggregate rows were consumed, and the sorted neighbor identities matched after removing 40 historical-key edges (20 in each direction). The exact statement hashes were base `9b3edcb272a0deab9f96169ab65338bcd05965b2dc20bec59f179775939f461d` and candidate `8038626c95bf4c9d1a6bd6a8db1cb0502134aed290f666c241b75522aef31763`. These runs measure Bolt request through full result consumption; they are not HTTP-handler timings. The 500-neighbor run met the 20-pair sampling shape; the 1,000-neighbor scale run used 10 pairs and its p95 is the maximum of those ten samples.

Change-surface depth-loop proof: the temporary live test `TestLiveChangeSurfaceAppSyncDepthLoopProof` passed on the pinned Neo4j image with `ESHU_OCI_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:28987 ESHU_LIVE_GRAPH_BACKEND=neo4j go test ./internal/query/impact -tags live_nornicdb_label_predicates -run '^TestLiveChangeSurfaceAppSyncDepthLoopProof$' -count=1 -v` (exit 0). The test file was removed after capturing the results. The source-built comparison exercised `ChangeSurfaceImpactRows` using one fixture and the same `WorkloadInstance` anchor for base and candidate. The graph had four depth-one historical AppSync keys (including `resource_type`-only, `data_type`-only, and conflicting-property shapes), one healthy EC2 instance at depth one, its IAM profile at depth two, and a healthy S3 resource at depth four. A separate 63×63×63 layered `File` subgraph generated 250,047 depth-four paths from the same anchor to a non-impact `File` endpoint, so the candidate's later-depth statements ran against a high-fanout shape. The independent oracle enumerated paths 1–4, excluded historical keys before ordering and paging, and returned each impacted node at minimum depth. Both scoped and unscoped candidate results matched `[instance depth 1, profile depth 2, S3 resource depth 4]` with `truncated=false`. The base query ran only depth one, returned three key rows from its four-row probe page, and reported `truncated=true`.

The production loop issued one base read (`depth=1, limit=4`) and four candidate reads (`depth/limit = 1/4, 2/3, 3/2, 4/2`). `PROFILE` summed operator `DbHits` to 507 for the base depth-one statement and 187, 8,098, 508,179, and 1,008,292 for candidate depths one through four (1,524,756 total). The exact candidate/base statement SHA-256 pairs by depth were: depth one `2dac9892a13e8fa8c0f83d7e297f11c272adf1e73a61f6b8f82ed73d414f484d` / `047e3d33e27067a16821b9bf36794ae0ad546fb845e96800bfb5d35910720263`; depth two `5768617489804e020c50105cc1cbe2b43c3aaec30818f2d8acdf5fec390f7b81` / `d5bff65a58dde240f5168a8c2500a042e08c44efb5e80c1d5f7c337a78c69bbb`; depth three `f001d41d1fb819c9ea669330711da058fd20f0a4bcddcf2004a94791488d7515` / `0413633e85d42da8d0c3c22f81ad67092996180f723bde8c4d1248193f7828c1`; depth four `b4879d124b17c7031a26dde444e5db9acc5d187d8f857683acf64ed5f67e7e19` / `752a21ae4377a48d7c37c87bff8e0e39971dd6f380c1e8ed0dbca59a774059b6`.

Across 20 alternating warm pairs of the unscoped route-method call, complete `ChangeSurfaceImpactRows` operation time was 0.683208 ms median / 1.103041 ms nearest-rank p95 for base and 78.192292 ms / 80.008250 ms for candidate. The scoped candidate run separately verified the same result and per-depth call limits; it is not included in these timing or PROFILE figures. The base result is intentionally incomplete and leaks key identities; this timing reports the synthetic cost of filtering before page limits and continuing through later depths, not an equivalent-output speed comparison. The proof was first run against candidate source at `a279605f35b76fa344cf826ff9950ce14b2c9e9a`. The rebased candidate is `dce911700649399b1becd87bf06149071236af69`; its nine commits are patch-identical to the earlier candidate and intervening `main` changes did not touch the measured query builders. This is local Bolt evidence on a synthetic high-fanout fixture, not an HTTP-handler or deployed-data latency claim.

At 1k resources, the filter adds approximately 0.3–0.5 ms of median latency in the three measured page/traversal families; dependency-read median was effectively unchanged. At 17k resources, the corresponding median increases are 6.477 ms, 9.730 ms, and 9.113 ms; dependency-read median was effectively unchanged. Operator `DbHits` increased in the page and entity-map cases at both sizes. These bounded measurements expose the cost of filtering historical records and do not establish production latency, workload representativeness, or a general no-regression claim. The exact base/candidate query SHA-256 pairs are: page `31f1f41a67723622445c54a9212e0ad95b98c32341f77babf90ce61b51e8f1b4` / `c041c8b89fe91a581f440f0cbfc380cc71a534dd1c60beb0a377d11bf7950539`; direct entity-map `3e977803ac8a6e268e25acfc1638ab723ad911c4452803961949c41abf7bc1fb` / `991f51069e3de1639fe3b037aa7d4db1cf939db55ed40334e7df759e336da145`; variable entity-map `e021c79212206f376c8fa2932fbc419aa4735340979d18faadbba46166d8c509` / `55828362fd8961720d3393dae4af56b5b48ce46d19cd9cce0d7313bd6d693c68`; workload dependencies `79e62402ecebbf1fc97dfa8ad89dd37a11b5c298ef3597f4f87269a0c6868a08` / `943c7b22e565912d547932232ff2579c8763da12d06c37a21242e2d82df79085`. At both fixture sizes, the page and traversal inputs returned 20 API-key rows on base and zero on candidate. The 17k synthetic page latency remains below the documented 2-second cloud-resource HTTP-handler SLO, but these measurements do not establish performance on deployed data.

Query-Plan Evidence: at candidate HEAD `a279605f35b76fa344cf826ff9950ce14b2c9e9a`, `scripts/verify-query-plan-profile.sh` passed against its disposable Neo4j image pinned by digest. It bound production-owned query bytes and ran live `PROFILE` checks for 22 handler entries, 30 legacy entries, and 875 safe production variants (927 shapes); the gate confirmed plans were present and met the bounded-anchor and forbidden-operator policies. The database had 25 indexes after the run (a fresh database starts with 2), confirming profiles executed. This checks plan shape across registered production queries; it is not a before/after latency measurement or a production workload claim.

No-Observability-Change: existing entity-context resolution counters and span attributes continue to report resolution and statement counts; this read filter adds no new runtime signal.

Historical facts and graph nodes are **NOT_PURGED**. No deployed API scopes,
persisted production values, graph state, or key rotation were inspected or
changed. This PR does not purge records or rotate keys. The data owner must
assess deployed exposure and rotation separately; any targeted production
cleanup requires its own reviewed procedure.

Traversal suppression moved to Go (differential fix): the `differential
nornicdb vs neo4j` gate failed on `changeSurfaceScopedOutgoingCypher` arm 1 —
the `AND NOT ('CloudResource' IN labels(impacted) AND ...)` conjunct shared
the repo-scope WHERE and emptied the whole traversal on the pinned NornicDB
executor (quorum reproduced `nornicdb=0 rows` against `neo4j=1 row` in both
pairings; the depth 2–4 presence divergences were downstream of the depth-1
empty page). The conjunct is removed from the statement; the arms now project
`impacted.resource_type`/`impacted.data_type` and
`changeSurfaceFilterTraversalRows` suppresses via
`changeSurfaceRowIsLegacyAppSyncKey` (CloudResource label plus either type
field, the same rule as the entity-map and infra-relationship paths). The
legacy unscoped statement is untouched (gate-green there; shape pinned by
`TestChangeSurfaceLegacyCypherGuardsWhitelistWithLabelTest`). The same gate
run also flagged two stale read-exemption anchors (the probe split out of
`getRelationships`; the suppression prefix added to the candidates WHERE);
both anchors now track the emitted text and the `statements_executed` phase
passes on the recorded CI captures.

Live parity proof (throwaway probe, deleted after): one Repository with a
Workload impact and an AppSync-key CloudResource impact at depth 1, new
scoped statement run raw on both proof backends —
`neo4j raw=[xgo8729:key xgo8729:ok] filtered=[xgo8729:ok]`,
`nornicdb raw=[xgo8729:key xgo8729:ok] filtered=[xgo8729:ok]` — identical row
sets and identical suppression. Committed unit tests pin the suppression
matrix and the Cypher shape (`TestChangeSurfaceFilterTraversalRowsSuppressesLegacyAppSyncKey`,
`TestChangeSurfaceScopedOutgoingCypherSuppressesKeysInGo`).

No-Regression Evidence: pure correctness fix restoring the pre-PR agreed
read shape — the new statement is the old minus the zeroing conjunct plus
two scalar projections, with the same `(start:Repository {id})` anchor, the
same bounded exact-depth expansion, and the same ORDER/LIMIT; no full bench
was run by explicit decision, the live parity probe above is the
same-input-shape check. No-Observability-Change: no spans, metrics, or log
lines added or removed; the `platform_impact.change_surface.outgoing`
graph-read name and existing counters are unchanged.
