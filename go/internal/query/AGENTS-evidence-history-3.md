# internal/query evidence and change history (part 3)

Part 3 of 3 of the dated, per-issue evidence and rationale entries for
`go/internal/query`, split out of [AGENTS.md](AGENTS.md) to keep the
read-first guidance under the repository's 500-line cap. Read the entry for a
specific issue number before touching the routes, helpers, or call sites that
issue names. Add new dated per-issue entries to whichever part is closest in
issue number, not in AGENTS.md.

This part covers issues #5761 and #5764, preserved with headings demoted one
level (`##` to `###`) from an earlier single-file AGENTS.md. See
[AGENTS-evidence-history.md](AGENTS-evidence-history.md) (part 1, issues
#2048-#4794/#4733) and
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) (part 2, issues
#3492-#5816).

## Evidence

### Language query gets a route-level capability, bounded-error mapping, and a span (#5761)

`POST /api/v0/code/language-query` (`language_queries.go`) previously ran with
no capability of its own, answered every bounded graph-read failure with a
generic HTTP 500 plus the raw driver error text, and opened no dedicated span.
`LanguageQueryHandler.handleLanguageQuery` now:

- Gates on the new `symbol_graph.language_entities` capability
  (`contract_capability_matrix_ext.go`) via `capabilityUnsupported` before any
  of its four guarded call sites (`entity_type=="guard"`,
  `graphBackedEntityTypes`, `graphFirstContentBackedEntityTypes`,
  `contentBackedEntityTypes`) touch `GraphQuery` or `ContentStore`, returning
  `501 unsupported_capability` through `WriteContractError` when the running
  profile's ceiling is nil -- the AGENTS.md "capability gate before any read"
  invariant, now honored by this route too. `local_lightweight` gets
  `derived` (content-only entity kinds are servable without the graph
  sidecar); `local_authoritative`/`local_full_stack`/`production` get
  `exact`. No profile committed in
  `specs/capability-matrix/language-entities.v1.yaml` is actually nil today,
  so this profile-level gate's 501 is NOT reachable via a real profile today
  (`TestHandleLanguageQueryCapabilityGateReturns501WhenUnsupported` proves the
  gate mechanism itself by temporarily forcing the matrix entry unsupported
  and restoring it). A second, distinct 501 IS reachable in a shipped
  configuration -- see "F1/F2: the graphless path and the graph-only 501
  residue" below.
- Opens span `query.language_query` (`telemetry.SpanQueryLanguageQuery`,
  registered in `contract_language_query.go` and pinned by
  `telemetry.TestSpanNames`) via `startQueryHandlerSpan`, carrying the stable
  `http.route` / `eshu.capability` attributes every other query-handler span
  in this package carries
  (`TestHandleLanguageQueryEmitsLanguageQuerySpan`).
- Maps every one of its four guarded call sites' bounded graph-read errors
  through the shared `WriteGraphReadError` (`graph_read_http_error.go`):
  `ErrGraphUnavailable` -> `503 backend_unavailable`, `ErrGraphReadDeadline`
  -> `504 backend_timeout`. Before #5761 all four sites answered any error,
  bounded or not, with a bare `500` and `err.Error()` in the body, leaking
  driver text and giving callers no way to distinguish "retry with backoff"
  from "your request was malformed"
  (`TestHandleLanguageQueryMapsGraphReadAvailabilityErrors`,
  `TestHandleLanguageQueryContentBackedBranchMapsGraphReadAvailabilityErrors`).
- Records the unmodified cause of a genuine (non-bounded) failure to the
  operator log, while the response body stays static, via the new
  `LanguageQueryHandler.Logger *slog.Logger` field and `logQueryFailure`
  before answering the now-static `500 language query failed` body. The
  bounded `failure_class` key (`language_query.guard`,
  `language_query.graph_backed`, `language_query.graph_first_content_backed`,
  `language_query.content_backed`) names which of the four call sites failed,
  alongside `language`, `entity_type`, and the error, so an operator can
  triage a degraded entity-type family without the response body carrying
  any backend detail. `CodeHandler.Mount` (`code.go`) forwards its own
  `Logger` field into the `LanguageQueryHandler` it constructs; a nil
  `Logger` on either struct is tolerated and simply skips logging.

#### F1/F2: the graphless path and the graph-only 501 residue

Landed in this same #5761 commit, alongside the capability gate, error
mapping, and span work above.

- **F1 -- a driverless `*Neo4jReader` is not the same as no reader.**
  `cmd/api/wiring_graph.go`'s `openQueryGraph` returns a nil `*neo4j.Driver`
  for `ProfileLocalLightweight` or `ESHU_DISABLE_NEO4J=true`, but
  `cmd/api/wiring.go` and `cmd/mcp-server/wiring.go` still call
  `NewNeo4jReader(nil, database)` unconditionally, so `LanguageQueryHandler.Neo4j`
  is always non-nil in production even under a graphless profile. Before F1,
  every call site that checked `h.Neo4j != nil` treated that driverless reader
  as configured and sent it a `Run` that always failed. `Neo4jReader.GraphConfigured()`
  (`neo4j.go`) now reports whether a driver or session factory is actually
  wired, and the package-level `languageQueryGraphConfigured` helper
  (`language_query_graph_configured.go`) calls it through the optional
  `graphConfiguredReader` interface -- falling back to a plain non-nil check
  for `GraphQuery` fakes that do not implement it, so existing test doubles
  keep working. `queryByLanguageWithSemanticFilter` and
  `queryGraphFirstContentByLanguageWithSemanticFilter` (`language_queries.go`)
  both gate on `languageQueryGraphConfigured(h.Neo4j)` instead of a nil check,
  so a graphless deployment now takes the Postgres content-store fallback
  instead of failing every graph-backed and graph-first read.
- **501 residue for graph-only entity types.** `Repository`, `Directory`, and
  `File` have no content-store equivalent (`graphLabelToContentEntityType`
  returns `""` for all three, `language_query_entities.go`), so under a
  graphless profile the content fallback has nothing to query.
  `queryByLanguageWithSemanticFilter` returns the sentinel
  `errLanguageQueryGraphOnlyEntityUnavailable` in that case, and
  `handleLanguageQuery`'s `graphBackedEntityTypes` branch maps it to
  `501 unsupported_capability` via `writeLanguageQueryUnsupportedCapability`
  rather than routing it through `WriteGraphReadError`'s 503/504 bounded-retry
  vocabulary -- no retry or backend recovery changes the answer for these
  three labels under a graphless profile, so it is a capability-shaped
  failure, not a transient one. This is a second, distinct 501 cause from the
  profile-level gate above, and (#5761 P2-5) it carries its own message --
  `entity type "<x>" requires a graph backend` -- naming the offending entity
  kind, instead of reusing the gate's "language query requires a supported
  query profile" wording (`TestHandleLanguageQueryUnconfiguredReaderReturns501ForGraphOnlyEntityType`).
  Unlike the profile-level gate, this residue 501 IS reachable in a shipped
  configuration: `local_lightweight` or `ESHU_DISABLE_NEO4J=true` plus
  `entity_type: repository|directory|file`.
- **F2 -- the `guard` content fallback must filter by entity type, not
  label.** `queryGraphFirstContentByLanguageWithSemanticFilter` takes a
  `contentEntityType` parameter distinct from the Neo4j `label` it uses for
  the graph read. Every `graphFirstContentBackedEntityTypes` caller passes the
  same value for both, but `entity_type=="guard"` diverges: its graph read
  uses `label="Function"` plus a `semantic_kind=guard` Cypher filter, while
  its content fallback must query `contentEntityType="guard"` so
  `contentEntityTypeFilter` (`elixir_semantic_types.go`) applies the matching
  `entity_type=Function AND metadata->>semantic_kind=guard` predicate. Before
  F2, the guard call site passed `label` ("Function") to the fallback too, so
  a graphless or zero-row guard read silently returned every `Function`
  instead of only guard clauses
  (`TestHandleLanguageQueryUnconfiguredReaderGuardEntityTypeServesGuardsOnlyFromContent`).

Proof for F1/F2 and the 501 residue: `go test ./internal/query -run
'TestHandleLanguageQueryUnconfiguredReader|TestHandleLanguageQueryCapabilityGateReturns501WhenUnsupported'
-v -count=1` (`language_query_graphless_reader_test.go`,
`language_query_graph_error_test.go`).

No-Regression Evidence: the one hot-path file this PR's
`scripts/verify-performance-evidence.sh` run actually flags is `code.go`, and
it is flagged because it already contains pre-existing Cypher
(`lookupComplexityRow`'s `MATCH (e) WHERE e.id = $entity_id` plus three
`OPTIONAL MATCH` lines, `code.go:445-448`) -- `is_hot_path_by_content`
(`scripts/verify-performance-evidence.sh`) matches the gate's own
hot-path-by-content patterns against a file's current content, not this PR's
diff, so any file carrying that Cypher is flagged regardless of what changed.
This PR's actual diff to `code.go` is a `Logger *slog.Logger` field on
`CodeHandler` plus one changed constructor line (`lq :=
&LanguageQueryHandler{Neo4j: h.Neo4j, Content: h.Content, Profile:
h.profile(), Logger: h.Logger}`) -- a struct field addition and a
pass-through, nothing else. This adds no hot-path query work: running the gate's own
hot-path-by-content patterns -- use them exactly as
`scripts/verify-performance-evidence.sh` defines them, since they are
word-anchored and dropping the anchors makes `chan` match prose in a comment
-- over `code.go`'s post-change content returns 4 matches, all pre-existing
Cypher in `lookupComplexityRow`, none of them touched by this PR's diff. Baseline = pre-#5761 `code.go` (`CodeHandler`
with no `Logger` field, `LanguageQueryHandler{Neo4j, Content, Profile}`
construction); after = the field plus the pass-through line above. No Cypher
statement, `MATCH` pattern, round trip, worker/lease/batch parameter, or
concurrency primitive is added or changed by this file, or by this PR at all:
the route's actual Cypher (`buildLanguageCypherWithSemanticFilter`,
`language_query_cypher.go`) is untouched by #5761 -- the Cypher text, its
`LIMIT $limit` shape, and its `ORDER BY` are unchanged.

Row counts are unchanged **or lower**, not simply unchanged. Alongside the
control-flow and truth-basis bookkeeping in `language_queries.go`,
`language_query_metadata.go`, and `language_query_reasons.go`, this PR adds
`languageQueryMaxLimit = 200` and clamps `req.Limit` to it. The builders splice
`params["limit"]` verbatim into `LIMIT $limit` with no ceiling of their own, so
before this change a caller could pass an arbitrarily large `limit` and have
every matching row collected, enriched, and serialized, held back only by the
read policy's time budget. For `limit <= 200` the terminal row count for every
entity-type family is identical to before; above it the count is bounded at
200. The clamp only ever reduces work, so the no-regression conclusion holds in
both directions. That bound is what the `label_inventory` disposition for
`(*LanguageQueryHandler).queryByLanguageWithSemanticFilter` asserts as
`max_results: 200` in `go/internal/queryplan/testdata/query-source-coverage.yaml`,
replacing the grandfathered prose `non_hot_reason` that the threading change
invalidated.

Backend NornicDB (canonical, default per AGENTS.md); Neo4j compatibility mode
unaffected for the same reason -- no Cypher changed. Input shape unchanged: the
same `{language, entity_type, query, repo_id, limit}` request body, with
`limit` now documented as capped at 200 in the OpenAPI schema and the
`execute_language_query` MCP tool schema. Proof: `go
test ./internal/query ./internal/mcp ./internal/telemetry -count=1` and, for
the specific behavior this entry documents, `go test ./internal/query -run
'TestHandleLanguageQuery|TestLanguageQueryCarriesLanguageEntitiesCapability|TestOpenAPILanguageQueryDocuments501'
-v -count=1`.

Observability Evidence: the route now opens span `query.language_query`
(`telemetry.SpanQueryLanguageQuery`) carrying `http.route` /
`eshu.capability` attributes, proven emitted exactly once per request by
`TestHandleLanguageQueryEmitsLanguageQuerySpan`
(`language_query_span_test.go`). The new `logQueryFailure` structured log
(`slog.WarnContext`, message `"language query failed"`) carries the bounded
`failure_class` key described above plus `language`, `entity_type`, and
`error`, mirroring the existing `neo4j_read_policy.go` decode-drop /
read-attempt log shape this package already uses for operator triage. No new
metric instrument or metric label is added; the span and log are the only new
observability signals, both scoped to this route.

### Repository context/story auxiliary graph reads: propagate vs attributed-degrade (#5764)

The template for this section is #5761 above; read it first for the shared
`WriteGraphReadError` mechanics. #5761 fixed the ROUTE-level guard on
`language-query`; #5764 fixes a different, narrower defect: several helpers
called *underneath* an already-guarded route folded a bounded graph-read
error into the same silent path as a genuine empty result, so a deadlined or
unavailable read answered a fabricated zero/empty value instead of the
guarded route's own 503/504 contract. The fix is not uniform -- some sites
propagate the sentinel like a base lookup would, others attribute the
degradation instead of failing the whole response -- because the two failure
modes are not interchangeable for every field a response carries.

**The decision rule**: a primary scalar or narrative source (a top-level
count, or a narrative sentence built directly from a row list) PROPAGATES,
because a fabricated wrong answer there is worse than a 503/504. A genuine
auxiliary detail/breakdown panel layered on top of an already-truthful
headline fact ATTRIBUTED-DEGRADES: the response stays 200, but the
degradation is surfaced through an existing shape (`limitations`/
`partial_reasons`, or a stage-log `failure_class`) rather than silently
disappearing. Under genuine ambiguity, attribute-degrade -- never leave a
site silent.

**PROPAGATE (bounded-read envelope, `WriteGraphReadError`):**

- `queryRepositoryContextCount` (`repository_context_counts.go`) -- the
  `file_count`/`workload_count`/`platform_count`/`dependency_count`
  top-level scalars on `GET /api/v0/repositories/{repo_id}/context`. Changed
  to `(int, error)`; `queryRepositoryContextCounts` aborts the whole
  aggregate on the first error instead of continuing with a fabricated
  fallback. `getRepositoryContext`'s `summary_counts` stage answers the error
  via `WriteGraphReadError` first, falling back to a generic 500 only for an
  unmapped (non-bounded) error, matching the route's existing
  `repository_lookup` stage.
- `queryRepositoryStoryStringRows` (`repository_story_counts.go`) -- the
  workload/platform/language narrative rows `GET
  /api/v0/repositories/{repo_id}/story` builds its headline sentence from
  (`"Repository X defines N workload(s): ..."`). Changed to `([]string,
  error)`; `queryRepositoryStoryGraphSummary` aborts on the first error the
  same way. `getRepositoryStory`'s `graph_summary` stage answers through
  `WriteGraphReadError`.
- `queryRepoDeploymentEvidenceDirection` (`repository_deployment_evidence.go`)
  -- changed to `([]map[string]any, bool, error)`; `queryRepoDeploymentEvidence`
  propagates the first direction's error immediately instead of trying the
  second direction and silently returning `nil, nil`. Both
  `getRepositoryContext`'s and `getRepositoryStory`'s `deployment_evidence`
  stages (the latter via `loadRepositoryDeploymentEvidenceForOverview`) were
  ALSO fixed at the call site: before #5764 both answered this error with a
  bare `500` carrying `err.Error()` in the body -- so even after
  `queryRepoDeploymentEvidenceDirection` started propagating, the caller
  still leaked driver text instead of the stable 503/504 contract. Both now
  call `WriteGraphReadError` first, exactly the #5761 pattern.
- `queryRepositoryStoryStringRows` is also bounded at
  `repositoryStoryStringRowLimit` (500, `LIMIT $limit` on a copied params
  map). `queryRepositoryStoryPlatformTypes`'s Cypher (the only row-per-path
  caller among the three -- it traverses two extra hops past the repository
  anchor, `INSTANCE_OF` then `RUNS_ON`, so it emits one row per
  `WorkloadInstance`, not one row per platform) was changed to
  `RETURN DISTINCT p.type` so the LIMIT bounds distinct platform values
  instead of raw path rows; without DISTINCT, a repository with many
  instances of one platform (alphabetically first) could starve a different,
  real platform out of the story entirely under the new LIMIT -- a WRONG
  story, not merely a truncated one.
  `TestQueryRepositoryStoryPlatformTypesDistinctPreventsStarvation`
  (`repository_story_platform_types_test.go`) is the mutation-proven
  regression for this. The `workload_names` and `languages` callers were left
  unchanged: `workload_names`'s Cypher is a single-hop `DEFINES` match (one
  row per `Workload` node, no path fan-out), and `languages`'s Cypher is
  already grouped (`count(DISTINCT f)`), so neither has the row-per-path
  shape that requires DISTINCT. `RETURN DISTINCT p.type` is a plain-property
  projection, not a function call, matching the already-safe
  `RETURN DISTINCT repo.id` shape in `code_import_dependencies_queries.go`;
  see `docs/public/reference/nornicdb-pitfalls.md` for the function-call-
  projection DISTINCT pitfall this avoids. 500 mirrors the repo's
  established defensive-backstop tier (`nornicDBOneHopRelationships` /
  `enrichNornicDBRelationshipRows` use 501). **Corrected by the P1 review
  follow-up below**: this bound is in fact reachable for real repositories,
  the story's `workload_count`/`platform_count` are `len()` of these bounded
  lists rather than separate `count()` queries, and `workload_names` gained
  its own `RETURN DISTINCT` for a duplicate-name starvation risk analogous to
  `platform_types`'s. See "Repository story row bound reintroduced the
  fabricated-value defect" in
  [AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) for the
  corrected behavior and its evidence.

**ATTRIBUTED-DEGRADE (200, degradation surfaced, never silent):**

- `queryRepoInfrastructureFromGraph` (`repository_infrastructure.go`) --
  changed to `([]map[string]any, bool, error)`. Reasoning it differs from the
  PROPAGATE sites: it is a genuine auxiliary panel (a supplementary
  infrastructure-entity list, not a headline count or narrative sentence),
  it is the OR-heavy multi-label scan most likely to be the one thing that
  deadlines on a large repository, and `entity_workload_context.go`'s
  `fetchServiceReadModelWorkloadContext` calls it on a read-model-only path
  that exists specifically to serve repositories with no materialized graph
  -- propagating there would 504 a request Postgres can fully answer. This
  read is also bounded at `repositoryInfrastructureEntityLimit` (5000,
  `LIMIT $limit` on a copied params map): the preferred content path in the
  same file already truncates silently at this exact constant
  (`queryRepoInfrastructureFromContent`'s `ListRepoEntities` call), so
  capping the graph fallback here is parity with already-shipped behavior in
  terms of the bound's existence, not a new bound. **A healthy graph read
  landing PAST the bound IS disclosed** (P2-2 review follow-up, corrected: an
  earlier draft of this entry said "exactly on" the bound with
  `len(rows) == limit`, which cannot distinguish "exactly limit rows exist"
  from "more rows exist past the bound" -- see the accurate statement at
  "The two reasons are mutually exclusive per read" below): the bool return
  is `len(rows) > limit`, threaded through `queryRepoInfrastructure` as a
  third return value and surfaced via the new `infrastructureTruncatedReason`
  (`"infrastructure_truncated"`, `repository_infrastructure_degrade.go`)
  alongside the existing degraded reason -- distinct because a truncated read
  returned real rows (just possibly not all of them) where a degraded read
  returned none. The `partial_reasons: []` / `limitations: []` a repo with
  fewer than 5000 infrastructure entities receives is therefore an honest
  "nothing partial" answer, not merely an unexamined default.
  `queryRepoInfrastructure` (`repository_context_helpers.go`) wraps it as
  `([]map[string]any, bool, bool)`, converting the error into a `degraded`
  signal and forwarding the graph read's own `truncated` signal unchanged
  so every caller answers 200 either way. Callers attribute the degradation
  through the shape each response already has:
  - `getRepositoryContext` adds a NEW `partial_reasons` top-level field
    (`repository_context.go`) carrying `infrastructureReadDegradedReason`
    (`"infrastructure_read_degraded"`, `repository_infrastructure_degrade.go`)
    -- following the sibling precedent on the same family
    (`repository_stats.go` + `repository_stats_limits.go`,
    `entity.go:contextPartialReasons`) rather than inventing a new shape.
  - `getRepositoryStory` appends the same reason to its EXISTING
    `limitations` slice (`repository_story.go`'s new `extraLimitations`
    parameter on `buildRepositoryStoryResponseWithCoverage`), which
    `attachAnswerMetadata` already promotes into
    `answer_metadata.partial_reasons` -- no new field needed there.
  - `fetchServiceReadModelWorkloadContext`
    (`entity_workload_context.go`) appends the same reason to its own
    existing `limitations` slice.
  - `fetchWorkloadContextForOperation` (`entity_workload_context.go`) -- the
    shared core behind `fetchWorkloadContext`, `fetchServiceWorkloadContext`,
    and `fetchServiceWorkloadContextWithSelector` -- also appends the reason
    to a `limitations` slice on the result map it returns. This map is the
    literal response body for `GET /api/v0/workloads/{workload_id}/context`
    and `GET /api/v0/services/{service_name}/context` (no intermediate
    response builder copies fields selectively, so the key must be
    `limitations` to be visible at all), and is read into `partial_reasons`
    by `contextPartialReasons` for
    `GET /api/v0/workloads/{workload_id}/{context,story}`
    (`entity_workload_handlers.go`) and into `response["limitations"]` for
    `GET /api/v0/services/{service_name}/story`
    (`service_story_dossier.go`'s existing `limitations` whitelist copy),
    which `attachAnswerMetadata` promotes the same way as the repository
    story. `trace_deployment_chain` (`impact_trace_deployment.go`) reaches
    the same fixed helper but its own response builder
    (`buildDeploymentTraceResponse`) does not currently surface
    `limitations` -- a known, unwidened gap on that one route, matching this
    issue's stated scope discipline.
  - Every one of the four `queryRepoInfrastructure`/
    `queryRepoInfrastructureFromGraph` call sites above also appends
    `infrastructureTruncatedReason` (`"infrastructure_truncated"`) to the same
    `partial_reasons`/`limitations` slot, independently of
    `infrastructureReadDegradedReason`, whenever a HEALTHY graph read landed
    past `repositoryInfrastructureEntityLimit` -- more rows exist beyond it
    (P2-2 review follow-up). The two reasons are mutually exclusive per read:
    a failed read has no rows to bound, and a bounded read did not fail.
  - **P2-3 review follow-up:** the content path was the unqualified gap the
    public contract (`openapi_paths_repositories.go`,
    `docs/public/reference/telemetry/graph-read-safety.md`) did not disclose:
    `queryRepoInfrastructureRows` tries `queryRepoInfrastructureFromContent`
    first, and on a normally-wired deployment (content store configured) that
    path answers before the graph fallback ever runs, so a truncation signal
    that fired only on the graph path could never surface in production.
    `queryRepoInfrastructureFromContent` now requests
    `repositoryInfrastructureEntityLimit+1` from `ListRepoEntities`, caps in
    Go, and returns its own `truncated` bool (`len(entities) > limit`) that
    `queryRepoInfrastructureRows` forwards unchanged instead of the prior
    hardcoded `false`. **This fix was itself wrong** (round-6 P1 review
    follow-up, corrected in
    [AGENTS-evidence-history-4.md](AGENTS-evidence-history-4.md)):
    `ListRepoEntities` has no `entity_type` predicate, so the bool it
    produced meant "this repo has more than 5000 content entities of ANY
    type" (true for nearly every real repository), not "the infrastructure
    panel was clipped." Read part 4 for the type-filtered fix
    (`ContentStore.ListRepoEntitiesByTypes`) before touching this call site.
  - Every infrastructure-read call site that runs under a
    `repositoryQueryStageTimer`/`serviceQueryStageTimer` also passes both
    signals through that stage's existing `Done` call: `slog.String(
    "failure_class", infrastructureReadDegradedReason)` only when degraded
    (reusing the #5761 `failure_class` convention), and
    `slog.Bool("truncated", ...)` unconditionally (`infrastructureDegradeLogAttrs`,
    `repository_infrastructure_degrade.go`), matching this package's existing
    `slog.Bool("truncated", ...)` stage-log convention
    (`service_workload_resolution.go`, `repository_stats.go`) instead of a new
    metric or log shape. This is `getRepositoryContext`, `getRepositoryStory`,
    and `fetchWorkloadContextForOperation`'s `"repo_infrastructure"` stage --
    NOT `fetchServiceReadModelWorkloadContext`, whose read-model-only path
    (above) has no stage timer around its direct
    `queryRepoInfrastructureFromGraph` call at all; that call site's
    degraded/truncated signals reach only the response `limitations` slice,
    with no per-stage log line to also carry them.
  - A negative test proves a HEALTHY zero-row read (no error, not truncated)
    does not add either reason -- that is what separates "no infrastructure"
    from "couldn't read infrastructure" and from "read landed on the bound".
**Six sibling helpers stay OUT of scope** -- `queryRepoEntryPoints`,
`queryRepoLanguageDistribution` (the `repository_context.go`/
`repository_context_helpers.go` graph-fallback variant; the STORY route's
languages are part of the PROPAGATE `queryRepositoryStoryStringRows` above, a
different function on a different route), `queryRepoDependencies`,
`queryRepoConsumers`, `queryRepoRelationshipOverviewDirection` (and its
`queryRepoRelationshipOverview` wrapper), and `queryRepoSourceToolBreakdown`
(all `repository_context_helpers.go`) still fold a bounded graph-read error
into the same silent "no rows" path as a genuine empty result, exactly as
before #5764. A prior pass on this issue widened all six to
`([]map[string]any, bool)` and classified them `non_hot: {class:
keyed_support, key_bound: single_key, max_results: 5001}` in
`query-source-coverage.yaml` -- but `5001` was fabricated: none of the six
has any `LIMIT` in its Cypher, `5001` never appeared anywhere in their source,
and `validateNonHotDisposition` (`source_coverage.go`) only checks
`MaxResults > 0`, so nothing caught the placeholder. That work was reverted;
all six are back to their `origin/main` shape (single-return, legacy
`non_hot_reason` prose, grandfathered digests restored in
`grandfathered_non_hot.go`).

The reason this is a narrower scope, not a shortcut: this repo's non-hot
taxonomy (`source_coverage.go`) has no "keyed-but-unbounded" class by design
-- `keyed_support` requires a real `max_results` bound (`validateNonHotDisposition`
enforces `MaxResults > 0`), and the only other non-hot classes
(`label_inventory`, `delegated`, `operator_query`, `backend_metadata`) don't
fit an ad hoc repository-relationship or tech-fingerprint read either. So
touching one of these six helpers' *source* -- as the widened-scope pass
did, changing every signature to add a `degraded bool` -- forces a real
choice: either bound the underlying Cypher with a genuine `LIMIT` (a
user-visible truncation change with its own truth story, requiring the same
row-per-path DISTINCT analysis `queryRepositoryStoryPlatformTypes` needed
above, for six queries instead of one) or leave the disposition as legacy
prose and the source untouched. #5764's actual defect -- silently swallowing
a bounded read error -- does not require touching these six at all, since
none of them sits under a PROPAGATE headline the way `infrastructure` does;
bounding six more queries to manufacture a `keyed_support` disposition would
have been scope creep dressed up as thoroughness, and the fabricated `5001`
is exactly what happens when that pressure produces a bound with no evidence
behind it. `queryRepoInfrastructureFromGraph` and
`queryRepositoryStoryStringRows` are the two helpers in this issue that
genuinely needed a bound (documented above, with real `LIMIT` values and,
for the platform-types shape, the DISTINCT-before-LIMIT correctness fix); the
six siblings' pre-existing silent swallow is reported as a known, unwidened
gap rather than "fixed" with an invented number.

**Why the `Content` nil trap matters here specifically**: `getRepositoryContext`
short-circuits every auxiliary graph read whose read-model equivalent is
available (`repositoryReadModelDependencies`, `relationshipReadModel`,
`readModelSummary`, `contentCoverage`) at `repository_context_counts.go`'s
`queryRepositoryFileCount`/`queryRepositoryWorkloadCount`/etc. A test with a
non-nil `Content` whose read model is silently "available" never reaches the
graph helper under test and passes vacuously. Every regression test for this
fix either leaves `Content` nil or uses a stub whose read model reports
unavailable (`fakePortContentStore{}`'s zero value).

**Mutation-proof pitfall specific to this fix**: a `fakeGraphReader` mock that
fails EVERY `Run` call unconditionally can produce a false green when
mutating one propagate site, because a DIFFERENT, still-correctly-fixed site
further down the same handler (for example `deployment_evidence`, which runs
after `summary_counts` in `getRepositoryContext`) also sees the same
unconditional error and independently produces the same 503/504 -- passing
even though the mutated site itself silently swallowed the error. Every test
in this fix therefore matches the mock's failure to a specific Cypher
fragment (`repositoryContextCountCypherFragment`,
`infrastructureGraphReadCypherFragment`, the `HAS_DEPLOYMENT_EVIDENCE`
fragment) so only the site under test can fail, and every other `Run` call
succeeds with an empty, healthy result.

No-Regression Evidence: pure correctness fix for the PROPAGATE sites (a
bounded read failure that previously fabricated a value now reports it
honestly) and a pure observability fix for the ATTRIBUTED-DEGRADE
`infrastructure` site (the degradation is now visible; the response shape and
status class are unchanged for the healthy and the degraded case alike --
still 200, still the same fields, with `partial_reasons`/`limitations`/
`failure_class` newly present only when a read actually failed). No Cypher
anchor changed on any site; the Go control flow around an existing
`reader.Run`/`RunSingle` call's error changed on every site, and two sites
also gained a genuine `LIMIT` bound: `queryRepoInfrastructureFromGraph`
(`LIMIT $limit` = `repositoryInfrastructureEntityLimit`, 5000, parity with
the existing content-path truncation) and `queryRepositoryStoryStringRows`
(`LIMIT $limit` = `repositoryStoryStringRowLimit`, 500, plus `RETURN DISTINCT`
on the one row-per-path caller, `queryRepositoryStoryPlatformTypes`, to keep
the LIMIT from starving a real platform). Backend NornicDB (Neo4j
compatibility unaffected); the error-handling change is backend-agnostic Go
control flow, and both new `LIMIT`s are unreachable for real per-repo
cardinality (see the bound-specific reasoning above). Proof: `go test
./internal/query -run
'TestGetRepositoryContext.*MapsGraphReadAvailabilityErrors|TestGetRepositoryStory.*MapsGraphReadAvailabilityErrors|TestGetRepositoryContextInfrastructureDegradeAttributesFailure|TestGetRepositoryContextInfrastructureHealthyEmptyDoesNotDegrade|TestGetRepositoryStoryInfrastructureDegradeAttributesFailure|TestQueryRepositoryStoryPlatformTypesDistinctPreventsStarvation'
-v -count=1` (failing-first for every site, mutation-proven by temporarily
reverting each guard -- and, for the platform-types starvation test, by
temporarily dropping `RETURN DISTINCT` from production -- confirming the
mutated test failed for the fabricated-value/leaked-500/starved-platform
reason, then restoring) and `go test ./internal/query ./internal/mcp
-count=1`.

Observability Evidence: `infrastructureDegradeLogAttrs` /
`repositoryContextDegradeLogAttrs` (`repository_infrastructure_degrade.go`)
add a `failure_class` attribute to the existing
`repository_query.stage_completed` / `service_query.stage_completed` log
event for the stage that degraded; no new span, metric instrument, or metric
label is added. The PROPAGATE sites emit no new observability signal beyond
what `WriteGraphReadError` and the reader's own
`query.graph_read.warning`/`eshu_dp_neo4j_query_duration_seconds{outcome=...}`
signals (`neo4j_read_policy.go`) already carry.
