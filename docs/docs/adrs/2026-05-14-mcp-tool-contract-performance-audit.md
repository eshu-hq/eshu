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

## Prompt-Family Audit

| Prompt family from docs | Primary current MCP path | Status after this PR | Remaining tracked work |
| --- | --- | --- | --- |
| Cross-repo service story, onboarding, runbooks | `get_service_story`, `investigate_service` | One-call dossier path from #284; keep using story first | #285 parent epic |
| Exact file/source evidence | `get_file_content`, `get_file_lines`, `get_entity_content` | Already scoped by repo/path or entity ID | None from this PR |
| Content evidence search | `search_file_content`, `search_entity_content` | Bounded, paged, multi-repo query is single PostgreSQL call | None from this PR |
| Symbol discovery and implementation lookup | `find_code`, `execute_language_query` | Existing bounded routes; raw Cypher examples now diagnostics-only | #287 |
| Callers, callees, imports, call chains | `analyze_code_relationships`, `find_function_call_chain` | Existing depth caps and profile gates; direct relationship pagination still belongs in story-grade tooling | #288 |
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
- infrastructure search has a max limit and truncation probe.

No-Regression Evidence: focused tests cover MCP search-content paging schema and
dispatch, server-added Cypher limits plus envelope truth, the capability matrix
YAML sync, single-query explicit multi-repo content search with offset, and
infrastructure search limit/truncation behavior. Review follow-up regressions
also cover max-offset rejection, client-side paging contract errors as HTTP
400, token-scanned Cypher LIMIT detection, OpenAPI response paging metadata,
and MCP offset schema bounds:
`go test ./internal/mcp ./internal/query -run 'TestResolveRouteMapsSearchFileContentPatternAndRepoIDs|TestResolveRouteMapsSearchEntityContentSingleRepoID|TestSearchContentToolsAdvertisePagingContract|TestSearchContentToolsAdvertiseMaxOffset|TestContentHandlerSearchFilesUsesSinglePagedQueryForExplicitRepoIDs|TestContentHandlerSearchFilesRejectsUnsupportedPagedFallbackAsBadRequest|TestContentHandlerSearchEntitiesRejectsUnsupportedPagedFallbackAsBadRequest|TestContentHandlerSearchRejectsOffsetAboveBound|TestHandleCypherQueryAddsBoundedLimitAndEnvelope|TestHandleCypherQueryRejectsExplicitLimitAboveRequestedLimit|TestHandleCypherQueryIgnoresLimitInsideStringLiteral|TestOpenAPISpec_ContentEntitySchemasExposeMetadata|TestCapabilityMatrixMatchesYAMLContract|TestSearchInfraResourcesProbesOneExtraRowForTruncation' -count=1`.

Observability Evidence: the changed paths continue through existing
`postgres.query` and `neo4j.query` spans with `db.operation` values
`search_file_content_in_repos`, `search_entity_content_in_repos`,
`search_file_content_page`, `search_entity_content_page`, and
`searchInfraResources`' existing graph query span. Direct Cypher now uses the
canonical response envelope with `truth.capability=graph_query.read_only_cypher`
so MCP callers can distinguish diagnostics from first-class prompt tools.

## Consequences

The documented prompt path is stricter: story and focused tools are the primary
contracts, while raw Cypher is an inspected diagnostics escape hatch. MCP
clients can now page content search without guessing, and explicit multi-repo
search no longer serializes repository queries. Security prompts remain
deliberately unsolved by raw Cypher and are tracked in #292.
