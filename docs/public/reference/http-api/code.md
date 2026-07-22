# HTTP Code Routes

Use these routes for source-backed code search, symbol lookup, dependency
questions, call-graph reads, and bounded diagnostics. Detailed request and
response schemas live in `GET /api/v0/openapi.json`; this page keeps the public
contract map short.

Repository-scoped requests accept `repo_id` as a repository ID, name, slug, or
indexed path. The server resolves that selector to the canonical repository ID
before querying. Treat returned file locations as `repo_id + relative_path`;
absolute server paths are not portable client identifiers.

## Route Index

| Route | Use it for |
| --- | --- |
| `POST /api/v0/code/search` | Text or entity-name search when the caller has terms but not a canonical symbol. |
| `POST /api/v0/code/symbols/search` | Definition-shaped symbol rows, exact or fuzzy, with paging metadata. |
| `POST /api/v0/code/structure/inventory` | Bounded structural inventory: functions, classes, top-level elements, dataclasses, documented/decorated symbols, classes with a method, super calls, and function counts by file. |
| `POST /api/v0/code/topics/investigate` | Broad behavior/topic exploration before exact symbol or relationship lookup. |
| `POST /api/v0/code/security/secrets/investigate` | Redacted hardcoded-secret findings, confidence, severity, suppression notes, source handles, and coverage. |
| `POST /api/v0/code/imports/investigate` | Importers, imports by file, package imports, module dependencies, direct Python file cycles, and cross-module calls. |
| `POST /api/v0/code/call-graph/metrics` | Hub functions and recursive functions for one repository. |
| `POST /api/v0/code/flow/taint-path` | Bounded taint-path evidence from active value-flow facts, labeled as derived reducer evidence. |
| `POST /api/v0/code/flow/reaching-def` | Bounded reaching-definition rows from exact parser-emitted `dataflow_functions` facts. |
| `POST /api/v0/code/flow/cfg-summary` | Bounded CFG summaries from exact parser-emitted `dataflow_functions` facts. |
| `POST /api/v0/code/flow/pdg-summary` | Bounded partial PDG summaries combining def-use and control-dependence facts. |
| `POST /api/v0/code/relationships` | Direct or bounded transitive relationships for a canonical entity or resolved name. |
| `POST /api/v0/code/relationships/story` | Narrative relationship packet with ambiguity handling and recommended follow-up calls. |
| `POST /api/v0/code/call-chain` | Bounded path between start and end symbols or entity IDs. |
| `POST /api/v0/code/dead-code` | Lower-level graph-backed dead-code candidate scan. |
| `POST /api/v0/code/dead-code/cross-repo` | Producer-repository dead-code candidates classified against deterministic consumer evidence. |
| `POST /api/v0/code/dead-code/investigate` | Dead-code investigation packet with cleanup-ready and ambiguous buckets. |
| `POST /api/v0/code/complexity` | Single-function relationship metrics or a bounded list of complex functions. |
| `POST /api/v0/code/quality/inspect` | Complexity, function length, argument count, or refactoring-candidate inspections. |
| `POST /api/v0/code/language-query` | Language/entity-type queries that do not fit the focused routes above. |
| `POST /api/v0/code/cypher` | Diagnostics-only bounded read-only Cypher. |
| `POST /api/v0/code/bundles` | Search the pre-indexed package registry catalog (package bundles) by name, namespace, or PURL. |

## Search And Discovery

`POST /api/v0/code/search` requires `query`. Optional filters include
`repo_id`, `language`, `limit`, `exact`, and `search_type`. Matching is
case-sensitive. Repository-selected requests use the indexed graph path.
Requests without `repo_id` use one authorization-aware query over the current
Postgres content-entity name index; authorization, language, and entity-name
filters are applied before `LIMIT`. Global substring searches require at least
three Unicode characters. Set `exact=true` for a complete name match; exact
global searches may use names shorter than three characters. The public page
limit defaults to 50 and is capped at 200. Every response includes `count`,
`limit`, and `truncated`; the handler reads one extra row internally so
`truncated=true` means at least one additional ordered match exists beyond the
returned page. `matches` is the compatibility alias for `results`.

`POST /api/v0/code/symbols/search` accepts `symbol` or `query`, optional
`match_mode`, repository/language/entity filters, `limit`, and `offset`.
Responses include definition rows, `source_handle`, `classification=definition`,
`match_kind`, `truncated`, and `ambiguity`.

`POST /api/v0/code/topics/investigate` accepts `topic` or `query`, plus optional
`intent`, repository/language filters, `limit`, and `offset`. Responses include
searched terms, matched files and symbols, evidence groups, source handles,
relationship-story handles, coverage, and truncation state.

## Inventory And Dependencies

`POST /api/v0/code/structure/inventory` requires at least one scope filter:
`repo_id`, `file_path`, `language`, `entity_kind`, or `symbol`. Supported
`inventory_kind` values include `entity`, `top_level`, `dataclass`,
`documented`, `documented_function`, `decorated`, `class_with_method`,
`super_call`, and `function_count_by_file`. Responses are deterministic and
paged with `truncated` and `next_offset`.

`POST /api/v0/code/imports/investigate` requires at least one scope anchor:
`repo_id`, `source_file`, `target_file`, `source_module`, or `target_module`.
Supported `query_type` values are `imports_by_file`, `importers`,
`module_dependencies`, `package_imports`, `file_import_cycles`, and
`cross_module_calls`. `target_file` is accepted only for cycle and cross-module
queries. The response uses one canonical row key for the selected query:
`dependencies`, `modules`, `cycles`, or `cross_module_calls`. Package pages are
distinct by repository, module, and language before offset and limit are
applied.

Module, cycle, and call candidates use a 25,000-row internal ceiling. The
handler requests one extra sentinel row and returns HTTP 422 with an instruction
to narrow the repository, file, or module scope when that ceiling is exceeded.
This internal bound is separate from the caller's page limit.
`file_import_cycles` rows include `repo_id`, `repo_name`, `cycle_path`, and
`cycle_edges`, where each proof edge names the `IMPORTS` relationship plus
source/target files, source/target modules, and line numbers when available.
Empty cycle pages return `cycles=[]`; unavailable graph backends return a
service-unavailable error instead of pretending the repository is acyclic.

No-Regression Evidence: all 244 valid request shapes map to 140 hash-frozen
production query texts in the query-plan gate. Exactness tests cover empty
graphs, duplicate edges, dotted-module prefix collisions, language filters,
paging, repository-path collisions, directionally scoped cycles, truncation,
and overflow. Cold and immediate-repeat NornicDB timings are
recorded in `docs/internal/evidence/5561-import-investigation-bounds.md` against
the 1.5-second interactive SLO.

Observability: the route emits `query.import_dependency_investigation` with
`eshu.import_dependencies.query_type`, `result_count`, `truncated`, and
`scan_overflow` attributes. Responses keep the Eshu truth envelope, `coverage`,
`truncated`, and `next_offset`. The read path adds no graph write, queue, worker,
or runtime setting.

`POST /api/v0/code/call-graph/metrics` requires `repo_id`. It supports
`hub_functions` and `recursive_functions`, deterministic ordering, paging,
truncation metadata, source handles, and coverage. Both variants read one
repository-scoped `CALLS` edge stream, then compute distinct hub degree or
reverse-edge recursion before applying language filters and paging. Duplicate
edges do not inflate degree counts or repeat recursive pairs; a self-call counts
once in each hub direction and appears as one `self_call` row. Exact ties use
canonical `Function.uid` as the final stable sort key, with legacy `id` fallback
for older nodes. Distinct canonical functions are not collapsed when their
legacy IDs collide, and their source and recursive-partner entity handles use
the canonical UID so clients can follow each result. The edge read requests a
50,001st sentinel row. Repositories with at most 50,000 physical `CALLS` edges
return exact metrics; larger repositories receive HTTP 422 explaining that the
exact edge bound was exceeded, with no partial `functions` rows.

## Code Flow

The code-flow routes require `repo_id` and accept optional `language`, `symbol`,
`file_path`, `line`, and `limit` filters. They read only active-generation facts
that were already produced by the parser/collector/reducer path; they do not
call semantic providers and do not require provider keys.

`cfg-summary` and `reaching-def` rows are labeled `exact_parser_fact` because
they come from parser-emitted `dataflow_functions`. `taint-path` rows are
labeled `derived_reducer_evidence` because they expose reducer-ingested taint and
interprocedural evidence handles. `pdg-summary` rows are labeled
`partial_derived_summary`; they combine available def-use and
control-dependence facts and must not be treated as whole-program PDGs.

Responses include `coverage` and `bounds` so empty evidence, unsupported
languages, ambiguous symbols, stale generations, and truncation stay visible.
Unsupported languages return explicit unsupported coverage rather than claiming
no findings.

## Relationships And Paths

`POST /api/v0/code/relationships` accepts either `entity_id` or `name`. Optional
filters include `direction`, `relationship_type`, `transitive`, and
`max_depth`. Set `transitive=true` with `relationship_type=CALLS` for indirect
callers or callees; `max_depth` caps traversal.

`POST /api/v0/code/relationships/story` resolves one target first. If the target
is ambiguous, it returns bounded candidates instead of guessing. It supports
direct relationships, bounded transitive `CALLS`, class hierarchy prompts, and
override prompts.

Two optional, additive parameters help agents stay within a prompt budget:

- `relationship_types` (array): a multi-type filter that supersedes the singular
  `relationship_type`; each requested type is followed with the same bounded
  query and the results are merged. It is rejected with `include_transitive`,
  `class_hierarchy`, or `overrides`.
- `token_budget` (integer ≥ 0): caps the response by an estimated serialized
  token cost, applied after `limit`. When it forces a cut, the response reports
  `summary.token_budget` with `dropped`, `available_before_budget`, and
  `guidance` on how to narrow. Omitting it (or `0`) means no budget and the
  response is unchanged.

Relationship rows are ordered by bounded centrality: each row carries a
`centrality` integer — the neighbor's degree within the resolved result set (how
many returned edges reference that neighbor across all requested directions and
types) — and rows are sorted by it, descending, with deterministic tie-breaking
on the bounded query's name-then-id order. `coverage.ranked_by` is
`bounded_centrality`. Centrality is measured within the bounded result set, not
over the whole graph, so the most-connected neighbors survive a small `limit` or
`token_budget` first.

Direct relationship rows include source-backed symbol metadata for both ends of
the edge when the graph contains the owning file and repository:
`source_repo_id`, `source_repo_name`, `source_file_path`, `source_language`,
`source_type`, `source_start_line`, `source_end_line`, `target_repo_id`,
`target_repo_name`, `target_file_path`, `target_language`, `target_type`,
`target_start_line`, and `target_end_line`. Missing containment or language data
is surfaced as absent or null metadata; readers must not infer a file, repository,
or language from the symbol name alone. The source/target prefix always follows
the returned edge direction, so an `incoming` row has the caller under `source_*`
and the requested symbol under `target_*`.

Cross-repository relationship story and call-chain reads are explicit opt-in
only. `cross_repo=true` requires repository selectors before traversal; scoped
tokens resolve those selectors against their grant before graph reads. The
relationship-story graph shape joins both edge endpoints to repositories and
filters scoped results through the granted repository set, while call-chain
paths constrain every returned/intermediate node to the selected endpoint
repositories. Direct rows label code relationships as
`edge_origin=direct_code_edge`; package/module/service inference must use its own
relationship type and provenance instead of masquerading as a direct code edge.

No-Regression Evidence: cross-repo caller/callee/importer/inheritance/call-chain
transport and policy behavior are guarded by focused MCP and HTTP tests:

```bash
go test ./internal/query -run 'Test(BuildCallChainCypherCrossRepo|HandleCallChainRejects|HandleRelationshipStoryRejects|RelationshipStoryDataMarksCrossRepo|RelationshipStoryGraphCypherCrossRepo|OpenAPIDocumentsCrossRepo)' -count=1
go test ./internal/mcp -run 'Test(AnalyzeCodeRelationshipsSchemaAdvertisesCrossRepo|ResolveRouteMapsAnalyzeCodeRelationshipsCrossRepo|ResolveRouteMapsFindFunctionCallChainCrossRepo)' -count=1
```

The tests prove explicit MCP query types, endpoint selectors, path-wide
repository filters, scoped-token denial, response scope metadata, OpenAPI
fields, and source/target repository evidence for story rows.

No-Observability-Change: this change adds no graph write, queue, worker,
runtime knob, metric instrument, metric label, or new span. Operators continue
to diagnose these read paths through the existing HTTP status/error body,
truth envelope, bounded `coverage`, `truncated`/`limit` fields, and query
handler spans/metrics.

Each returned relationship carries a `provenance` block so API and MCP clients
can compare code and correlation edges without reading several optional scalar
fields. The block includes `confidence` when numeric confidence exists,
`confidence_state`, `method`, `source_family`, `reason`, `truth_state`, and the
boolean `derived`, `heuristic`, and `unsupported` flags. Older scalar fields
such as `confidence`, `resolution_method`, `confidence_basis`, and
`resolution_source` remain in place for compatibility.

For `CALLS`/`REFERENCES` code edges, `method` is the code `resolution_method`
when available. `resolution_method` values include `scip`, `declared`,
`same_file`, `import_binding`, `type_inferred`, `scope_unique_name`,
`cross_repo_export_package`, and
`repo_unique_name`, with confidence derived from that method (see the
[graph model](../../concepts/graph-model.md)). An edge projected before this
contract can still miss scalar provenance; its `provenance` block reports
`confidence_state=unsupported`, `source_family=unsupported`, and
`truth_state=unsupported` rather than inventing confidence. Per-edge provenance
is independent of the answer-level truth envelope; a low-confidence edge does
not lower the answer's truth level.

`TAINT_FLOWS_TO` rows are reducer-owned value-flow evidence edges. When the
solver emitted a finding trail, `provenance.why_trail` contains a bounded,
ordered list of source, intermediate, and sink port steps; if the cap was hit,
`provenance.why_trail_truncated=true`. These rows report
`source_family=value_flow_edge` and `truth_state=derived`. The trail is
provenance for explaining an existing finding and does not promote value-flow
evidence to canonical graph truth.

Repository and cross-system correlation edges use `confidence_basis` instead of
code `resolution_method`. Treat `evidence_constant`, `evidence_aggregate`, and
`assertion_override` as the correlation-side explanation for the same numeric
confidence field; the `provenance` block reports these as `method` with
`source_family=correlation_edge` and `truth_state=heuristic`. Do not map them
onto code resolution tiers.

### Confidence Floor Contract

`min_confidence` is the confidence-floor request parameter for relationship
read routes that return code or correlation edges. Runtime support is available
only where the specific route's OpenAPI schema advertises the field. The
relationship-story route (`POST /api/v0/code/relationships/story`) implements
this contract:

- omitted means no confidence floor and preserves current edge visibility,
  including ambiguous, stale, conflicting, and missing-confidence rows;
- accepted values are JSON numbers from `0` through `1`, inclusive;
- invalid, non-numeric, negative, or greater-than-`1` values return a request
  validation error instead of silently broadening or narrowing results;
- the floor filters only returned relationship rows after canonical scope,
  type, direction, limit, and pagination validation; it must not change reducer
  admission, graph writes, evidence drilldowns, or answer truth envelopes;
- rows without numeric `confidence` do not satisfy a positive floor, but remain
  visible when the floor is omitted or `0`;
- empty results caused by the floor keep the normal success envelope and must
  not imply that no underlying relationship or evidence exists.

MCP tools mirror the HTTP spelling as `min_confidence`; camelCase aliases such
as `minConfidence` are not part of the Eshu wire contract.

No-Regression Evidence: `go test ./internal/query -run
'TestHandleRelationshipStory(SurfacesRelationshipProvenanceBlock|ProvenanceSurvivesMinConfidenceAndEmptyResults|SurfacesEdgeProvenance|AppliesMinConfidenceFloor|MinConfidenceValidation)|TestOpenAPIRelationship(StoryDocumentsMinConfidence|SchemaDocumentsProvenanceBlock)'
-count=1` covers relationship-story filtering, default visibility, provenance
shape, validation, empty results, and OpenAPI schema support. The filter runs
after the bounded relationship-story read and before centrality ranking,
truncation, and token-budget cuts; it adds no Cypher predicate, traversal,
ordering, or graph write.

No-Observability-Change: relationship-story confidence filtering adds no graph
query, graph write, queue, worker, route, metric, span, log field, or runtime
knob. Operators still diagnose the read through the existing graph query spans,
query-duration metrics, HTTP route attribution, and answer-level truth envelope.

No-Regression Evidence: `go test ./internal/query -run
'TestHandleRelationshipsSurfaces(RelatedSymbolSourceMetadata|GraphEdgeProvenance)|TestNornicDBOneHopRelationshipsCypherProjects(RelatedSymbolSourceMetadata|EdgeProvenance)|TestRelationshipGraphRowCypherProjectsEdgeProvenance|TestNormalizeNornicDBRelationshipRowsDropsMissingEdgeProvenance|TestOpenAPIRelationshipDocumentsSourceMetadata'
-count=1` covers response preservation, Neo4j and NornicDB query projection, and
OpenAPI schema drift for related symbol source metadata and edge provenance.

No-Observability-Change: this is a bounded one-hop read projection on the
existing `/api/v0/code/relationships` route. It adds no graph write, queue,
worker, metric instrument, metric label, runtime knob, or new route; operators
continue diagnosing this path through the existing query route spans and HTTP
request metrics.

`POST /api/v0/code/call-chain` finds a bounded path between `start` and `end`,
or between `start_entity_id` and `end_entity_id`. `repo_id` scopes both
endpoints when provided. Lightweight profiles that cannot answer authoritative
graph traversal return `unsupported_capability` rather than fallback prose.

## Dead Code

`POST /api/v0/code/dead-code/investigate` is the normal prompt-facing dead-code
route. It returns coverage, language maturity, exactness blockers,
cleanup-ready and ambiguous buckets, suppressed modeled roots, source handles,
recommended next calls, paging, and truncation state.

`POST /api/v0/code/dead-code` is the lower-level candidate scan. `repo_id`,
`language`, and `candidate_kind` are optional; `limit` defaults to `100` and is
capped at `500`. `candidate_kind` accepts the exact advertised labels
`Function`, `Class`, `Struct`, `Interface`, `Trait`, and `SqlFunction`. A
selected kind narrows the raw scan and its reported bound to that kind;
unsupported values return `400` instead of silently scanning functions.

Both routes remain `derived` until the broader framework, public API,
reflection, and user-configured root registry from
[Dead-Code Reachability Spec](../dead-code-reachability-spec.md) is complete.
Language-specific root and blocker details belong in that spec and the OpenAPI
description, not in this route map.

## Complexity, Quality, And Language Queries

`POST /api/v0/code/complexity` accepts `entity_id` or `function_name` for a
single function. Without a single selector, it returns a bounded deterministic
`results` list with `limit` and `truncated`.

`POST /api/v0/code/quality/inspect` supports `complexity`, `function_length`,
`argument_count`, and `refactoring_candidates`, with threshold fields and
recommended next calls in the response.

`POST /api/v0/code/language-query` requires `language` and `entity_type`.
Use focused routes first when they answer the question; use language-query for
language/entity-type contracts that do not fit symbol, relationship,
inventory, dependency, or dead-code routes.
Accepted language/entity pairs are not a blanket framework, route, outbound
contract, dead-code, or cross-repo parity claim. The feature-level contract is
the [Language Feature Parity Ledger](../../languages/support-maturity.md#language-feature-parity-ledger);
features marked partial or absent from that ledger remain unsupported for
API/MCP parity claims.

## Diagnostics And Bundles

`POST /api/v0/code/cypher` is diagnostics-only. It accepts `cypher_query` plus
optional `limit`, rejects mutation keywords, caps query length, uses a request
timeout, and appends or enforces bounded `LIMIT` values. Use purpose-built
code, story, impact, and content routes for normal client workflows.
`POST /api/v0/code/visualize` shares the same read-only Cypher path and
projects the result into a bounded, renderable graph visualization packet
instead of raw rows. Both routes are shared-key/all-scope callers only (#5167
Group C): the query text is caller-supplied and unbounded, so there is no
selector to intersect against a scoped-token or browser-session tenant grant,
and both are rejected before the handler runs.

`POST /api/v0/code/bundles` searches the pre-indexed package registry catalog
(package bundles) by name, namespace, or PURL. It requires a non-empty `query`
or `ecosystem` scope and rejects unscoped requests. It does not upload files,
import `.eshu` archives, or mutate graph state.
