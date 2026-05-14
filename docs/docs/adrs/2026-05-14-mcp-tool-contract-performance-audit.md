# ADR: MCP Tool Contract And No-Cache Performance Audit

**Date:** 2026-05-14
**Status:** Proposed
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Context

Issues #300 and #301 continue the MCP overhaul from issue #280. The service
story PR made `get_service_story` and `investigate_service` usable as one-call
story surfaces, but the broader MCP surface still had legacy slow-path risks:

- direct Cypher examples could run graph-wide scans without a server-added row
  cap;
- content search accepted multi-repo input but executed repository searches
  sequentially when more than one repo was supplied;
- several list/search tools advertised weak or missing paging metadata;
- prompt documentation still treated raw Cypher as an acceptable answer path for
  some code-quality and security prompts.

Eshu does not have a cache layer in front of MCP. Every prompt-contract tool
therefore needs to be accurate, bounded before execution, and observable on a
cold call.

## Decision

MCP tools stay transport-only. Performance rules live in the HTTP query layer
and its graph/Postgres read helpers, because MCP dispatch calls those handlers
directly.

This PR hardens the current broad-read risks that were visible in the existing
tool surface:

- `execute_cypher_query` is diagnostics-only. The HTTP handler rejects writes,
  keeps the 30 second timeout, accepts a `limit` argument, appends a bounded
  `LIMIT` probe when the caller omits one, rejects explicit query limits above
  the requested cap, trims the result window, returns `truncated`, and now emits
  the canonical `{data, truth, error}` envelope with capability
  `graph_query.read_only_cypher`.
- `search_file_content` and `search_entity_content` advertise `limit` and
  `offset` through MCP. Their backing HTTP handlers cap `limit` at 200, probe
  one extra row, cap `offset` at 10000, return `truncated`, and support
  deterministic paging.
- Explicit multi-repo content searches now use one scoped PostgreSQL query over
  the requested repo IDs instead of one sequential query per repo.
- `find_infra_resources` now advertises `limit` through MCP, caps it at 200,
  probes one extra graph row, and returns `truncated`.
- The MCP cookbook no longer recommends raw graph-wide source-code Cypher for
  hardcoded-secret prompts. Issue #292 remains the first-class security prompt
  work item.
- `find_symbol` is the first-class MCP symbol-definition lookup for issue #287.
  It routes to `POST /api/v0/code/symbols/search`, defaults to exact matching,
  accepts optional `repo_id`, `language`, and entity-type filters, caps `limit`
  at 200, caps `offset` at 10000, probes one extra row for `truncated`, returns
  `source_handle` and `ambiguity`, and uses the content index before graph
  fallback to avoid a graph-wide Cypher read on normal cold calls.
- `get_code_relationship_story` is the first-class MCP path for issue #288.
  It routes to `POST /api/v0/code/relationships/story`, resolves a target name
  before graph traversal, returns bounded ambiguity candidates instead of
  guessing, anchors graph reads by entity id, requires deterministic
  `ORDER BY` plus `SKIP`/`LIMIT`, probes one extra row for `truncated`, and uses
  a bounded breadth-first CALLS traversal when `include_transitive=true`. When
  callers request both direct directions, rows are interleaved and the response
  reports per-direction available, returned, and truncated counts so one busy
  direction cannot hide the other without a visible coverage signal.
- `investigate_code_topic` is the first-class MCP path for issue #286. It routes
  to `POST /api/v0/code/topics/investigate`, accepts a natural-language topic,
  optional intent, repository selector, language, limit, and offset, derives a
  bounded search-term set, and uses one scored content-index query to return
  ranked files, symbols, coverage, truncation, call-graph handles, and exact
  recommended next calls.

## Prompt-Family Audit

| Prompt family from docs | Primary current MCP path | Status after this PR | Remaining tracked work |
| --- | --- | --- | --- |
| Cross-repo service story, onboarding, runbooks | `get_service_story`, `investigate_service` | One-call dossier path from #284; keep using story first | #285 parent epic |
| Exact file/source evidence | `get_file_content`, `get_file_lines`, `get_entity_content` | Already scoped by repo/path or entity ID | None from this PR |
| Content evidence search | `search_file_content`, `search_entity_content` | Bounded, paged, multi-repo query is single PostgreSQL call | None from this PR |
| Symbol discovery and implementation lookup | `find_symbol`, `find_code`, `execute_language_query` | First-class definition lookup is bounded, paged, source-handle backed, and no longer requires raw Cypher for "where is this implemented?" prompts | #287 |
| Broad code-topic and implementation investigation | `investigate_code_topic` | First-class content-index investigation returns ranked files, symbols, searched terms, coverage, truncation, and source/relationship follow-up handles without raw Cypher or client-side term guessing | #286 |
| Callers, callees, imports, call chains | `get_code_relationship_story`, `find_function_call_chain` | Relationship story is bounded, ambiguity-aware, entity-anchored, paged, and exposes optional bounded transitive CALLS traversal; call-chain keeps the dedicated endpoint | #288 |
| Dead code and code quality | `find_dead_code`, `find_most_complex_functions` | Existing bounded routes; raw Cypher examples now show limits | #289, #290 |
| Class hierarchy and overrides | `analyze_code_relationships`, `execute_language_query` | Current fallback remains diagnostics-heavy for some shapes | #291 |
| Security hardcoded secrets | none first-class | Raw graph-wide Cypher removed from the recommended prompt path | #292 |
| Deployment, GitOps, and resource tracing | `trace_deployment_chain`, `trace_resource_to_code`, story tools | Service story is one-call; low-level trace paths keep existing caps | #293, #294, #295 |
| Environment comparison | `compare_environments` | Existing scoped workload/environment route | #296 |
| Package and registry prompts | `list_package_registry_packages`, `list_package_registry_versions` | Already require/cap `limit` and deterministic ordering | #297 |
| Documentation/confluence prompts | story routes plus content evidence | Story-first guidance remains; exact docs use paged content search | #298 |
| Raw Cypher cookbook prompts | `execute_cypher_query` | Diagnostics-only, timeout-bound, server-capped, envelope-backed | #299 |

## Bounds And Observability

The changed read paths are cold-call bounded:

- direct Cypher has a timeout, query length limit, write-keyword rejection, row
  cap, and canonical truth envelope;
- content search has limit/offset, deterministic ordering, one-row truncation
  probes, a max offset to keep broad cold calls bounded, and a single
  multi-repo SQL query for explicit repo sets;
- symbol lookup has exact/fuzzy match modes, deterministic ordering,
  limit/offset, one-row truncation probes, source handles, and ambiguity
  metadata from one content-index query;
- relationship story resolves names with a bounded content-index lookup before
  touching the graph, returns ambiguity candidates without guessing, reads
  direct edges with entity-anchored ordered pagination, and limits transitive
  CALLS traversal by depth plus result-window size;
- topic investigation derives at most 16 search terms from `topic` and `intent`,
  pushes repository and language scope into a single scored PostgreSQL query,
  orders by score and stable repo-relative path, probes one extra row for
  `truncated`, and returns exact follow-up calls instead of expanding
  relationships or source bodies in the first response;
- infrastructure search has a max limit and truncation probe.

No-Regression Evidence: focused tests cover MCP search-content paging schema and
dispatch, server-added Cypher limits plus envelope truth, the capability matrix
YAML sync, single-query explicit multi-repo content search with offset, and
infrastructure search limit/truncation behavior. Review follow-up regressions
also cover max-offset rejection, client-side paging contract errors as HTTP
400, token-scanned Cypher LIMIT detection, OpenAPI response paging metadata,
MCP offset schema bounds, and the first-class symbol lookup route/tool contract:
`go test ./internal/mcp ./internal/query -run 'TestResolveRouteMapsSearchFileContentPatternAndRepoIDs|TestResolveRouteMapsSearchEntityContentSingleRepoID|TestSearchContentToolsAdvertisePagingContract|TestSearchContentToolsAdvertiseMaxOffset|TestContentHandlerSearchFilesUsesSinglePagedQueryForExplicitRepoIDs|TestContentHandlerSearchFilesRejectsUnsupportedPagedFallbackAsBadRequest|TestContentHandlerSearchEntitiesRejectsUnsupportedPagedFallbackAsBadRequest|TestContentHandlerSearchRejectsOffsetAboveBound|TestHandleCypherQueryAddsBoundedLimitAndEnvelope|TestHandleCypherQueryRejectsExplicitLimitAboveRequestedLimit|TestHandleCypherQueryIgnoresLimitInsideStringLiteral|TestOpenAPISpec_ContentEntitySchemasExposeMetadata|TestCapabilityMatrixMatchesYAMLContract|TestSearchInfraResourcesProbesOneExtraRowForTruncation' -count=1`.

No-Regression Evidence: symbol lookup focused proof:
`go test ./internal/query -run 'TestCodeHandlerFindSymbolReturnsBoundedContentDefinitions|TestCodeHandlerFindSymbolRejectsHugeOffset|TestCodeHandlerFindSymbolRejectsGraphOnlyOffset|TestCodeHandlerFindSymbolRejectsMissingBackends|TestOpenAPI' -count=1` and
`go test ./internal/mcp -run 'TestFindSymbolToolAdvertisesBoundedLookupContract|TestResolveRouteMapsFindSymbol|TestReadOnlyTools|TestCodebaseTools' -count=1`.

No-Regression Evidence: relationship story focused proof:
`go test ./internal/query -run 'TestHandleRelationshipStory|TestNornicDBRelationshipStory|TestCapability|TestOpenAPI' -count=1` and
`go test ./internal/mcp -run 'TestResolveRouteMapsCodeRelationshipStory|TestReadOnlyTools|TestCodebaseTools|TestEveryRegisteredToolHasDispatchRoute' -count=1`.

No-Regression Evidence: code topic investigation focused proof:
`go test ./internal/query -run 'TestHandleCodeTopicInvestigation|TestContentReaderInvestigateCodeTopic|TestOpenAPI|TestCapabilityMatrix' -count=1` and
`go test ./internal/mcp -run 'TestInvestigateCodeTopic|TestResolveRouteMapsInvestigateCodeTopic|TestReadOnlyTools|TestCodebaseTools|TestEveryRegisteredToolHasDispatchRoute' -count=1`.

Observability Evidence: the changed paths continue through existing
`postgres.query` and `neo4j.query` spans with `db.operation` values
`search_file_content_in_repos`, `search_entity_content_in_repos`,
`search_file_content_page`, `search_entity_content_page`, and
`search_symbols`; infrastructure lookup continues through `searchInfraResources`'
existing graph query span. Direct Cypher now uses the canonical response
envelope with `truth.capability=graph_query.read_only_cypher`; symbol lookup
uses `truth.capability=code_search.symbol_lookup` so MCP callers can distinguish
diagnostics from first-class prompt tools.

No-Observability-Change: relationship story uses the existing content-store and
graph query instrumentation rather than adding a new worker or storage path.
The response envelope reports `truth.capability=call_graph.relationship_story`,
`source_backend`, `coverage.query_shape`, `coverage.truncated`, and the scoped
`limit`/`offset`/`max_depth` values so MCP callers and operators can identify
whether a slow answer came from target resolution, direct graph reads, or
bounded transitive traversal.

Observability Evidence: code topic investigation emits
`query.code_topic_investigation` with `http.route` and `eshu.capability`, then
one child `postgres.query` span with `db.operation=investigate_code_topic`. The
response envelope reports `truth.capability=code_search.topic_investigation`,
`source_backend=postgres_content_store`, `coverage.query_shape`,
`coverage.searched_term_count`, `limit`, `offset`, and `truncated`, so a slow or
empty MCP answer can be diagnosed by scope, term expansion, and result window.

No-Regression Evidence: change-surface investigation focused proof:
`go test ./internal/query -run 'TestInvestigateChangeSurface|TestOpenAPI' -count=1`
and
`go test ./internal/mcp -run 'Test(ChangeSurfaceInvestigation|ResolveRouteMapsChangeSurface|ReadOnlyTools|EcosystemTools|EveryRegistered)' -count=1`.
The new path keeps target resolution separate from traversal: ambiguous service
or module names return candidates without running the graph expansion, resolved
targets use exact label-scoped resolver templates, and the traversal uses a
literal bounded `*1..max_depth`, deterministic `ORDER BY depth, impacted.name,
impacted.id`, `LIMIT limit+1`, and a `truncated` response flag.

Observability Evidence: change-surface investigation emits
`query.change_surface_investigation` with `http.route` and `eshu.capability`.
Graph work continues through `neo4j.query` spans; code-topic and changed-path
work continue through existing Postgres content-store reads. The response
envelope reports `truth.capability=platform_impact.change_surface`,
`target_resolution.status`, `code_surface.coverage.query_shape`,
`coverage.query_shape`, `max_depth`, `limit`, `offset`, and `truncated`, so MCP
latency can be classified as target resolution, content lookup, or bounded
graph traversal.

No-Regression Evidence: PR #325 is a staticcheck-only rewrite of
`resolveChangeSurfaceTarget` from a condition switch to a tagged
`switch totalCandidates`. Baseline failed with
`golangci-lint run ./internal/query` on QF1002; after the rewrite,
`golangci-lint run ./internal/query`, `golangci-lint run ./...`,
`go test ./internal/query -run TestInvestigateChangeSurface -count=1`, and
`go test ./...` passed against the same `origin/main` input shape. Candidate
row counts and terminal queue counts are unchanged because the Cypher query,
candidate slice, truncation check, and return branches for 0, 1, and many
candidates are byte-equivalent in behavior.

No-Observability-Change: PR #325 does not alter query execution, response
fields, spans, metrics, logs, or status surfaces. The existing
`query.change_surface_investigation` span, `neo4j.query` child spans, response
`target_resolution.status`, `coverage.query_shape`, `limit`, `offset`,
`max_depth`, and `truncated` fields remain the operator diagnosis path.

## Consequences

The documented prompt path is stricter: story and focused tools are the primary
contracts, while raw Cypher is an inspected diagnostics escape hatch. MCP
clients can now page content search without guessing, and explicit multi-repo
search no longer serializes repository queries. Security prompts remain
deliberately unsolved by raw Cypher and are tracked in #292.
