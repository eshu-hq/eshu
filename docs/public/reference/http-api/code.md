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
| `POST /api/v0/code/relationships` | Direct or bounded transitive relationships for a canonical entity or resolved name. |
| `POST /api/v0/code/relationships/story` | Narrative relationship packet with ambiguity handling and recommended follow-up calls. |
| `POST /api/v0/code/call-chain` | Bounded path between start and end symbols or entity IDs. |
| `POST /api/v0/code/dead-code` | Lower-level graph-backed dead-code candidate scan. |
| `POST /api/v0/code/dead-code/investigate` | Dead-code investigation packet with cleanup-ready and ambiguous buckets. |
| `POST /api/v0/code/complexity` | Single-function relationship metrics or a bounded list of complex functions. |
| `POST /api/v0/code/quality/inspect` | Complexity, function length, argument count, or refactoring-candidate inspections. |
| `POST /api/v0/code/language-query` | Language/entity-type queries that do not fit the focused routes above. |
| `POST /api/v0/code/cypher` | Diagnostics-only bounded read-only Cypher. |
| `POST /api/v0/code/bundles` | Search indexed repositories as pre-indexed bundle candidates. |

## Search And Discovery

`POST /api/v0/code/search` requires `query`. Optional filters include
`repo_id`, `language`, `limit`, `exact`, and `search_type`. The handler searches
graph entities first and falls back to the content index when graph search has
no rows.

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
`cross_module_calls`. The response uses one canonical row key for the selected
query: `dependencies`, `modules`, `cycles`, or `cross_module_calls`.

`POST /api/v0/code/call-graph/metrics` requires `repo_id`. It supports
`hub_functions` and `recursive_functions`, deterministic ordering, paging,
truncation metadata, source handles, and coverage.

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

Each `CALLS`/`REFERENCES` relationship in the response carries per-edge
provenance: `confidence` (a number) and `resolution_method` (how the callee was
resolved). `resolution_method` is a closed value — `scip`, `declared`,
`same_file`, `import_binding`, `type_inferred`, `scope_unique_name`, or
`repo_unique_name` — and the confidence is derived from it (see the
[graph model](../../concepts/graph-model.md)).
The fields are additive: an edge projected before this contract omits both, and
readers must treat a missing `resolution_method` as unspecified. Per-edge
provenance is independent of the answer-level truth envelope; a low-confidence
edge does not lower the answer's truth level.

`POST /api/v0/code/call-chain` finds a bounded path between `start` and `end`,
or between `start_entity_id` and `end_entity_id`. `repo_id` scopes both
endpoints when provided. Lightweight profiles that cannot answer authoritative
graph traversal return `unsupported_capability` rather than fallback prose.

## Dead Code

`POST /api/v0/code/dead-code/investigate` is the normal prompt-facing dead-code
route. It returns coverage, language maturity, exactness blockers,
cleanup-ready and ambiguous buckets, suppressed modeled roots, source handles,
recommended next calls, paging, and truncation state.

`POST /api/v0/code/dead-code` is the lower-level candidate scan. `repo_id` and
`language` are optional; `limit` defaults to `100` and is capped at `500`.

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

## Diagnostics And Bundles

`POST /api/v0/code/cypher` is diagnostics-only. It accepts `cypher_query` plus
optional `limit`, rejects mutation keywords, caps query length, uses a request
timeout, and appends or enforces bounded `LIMIT` values. Use purpose-built
code, story, impact, and content routes for normal client workflows.

`POST /api/v0/code/bundles` searches indexed repositories as bundle candidates.
It does not upload files, import `.eshu` archives, or mutate graph state.
