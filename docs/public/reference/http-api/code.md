# HTTP Code Routes

Use these routes when a caller needs code relationships and source-backed
answers without pulling the full code-to-cloud graph story.

## Route Index

- `POST /api/v0/code/search`
- `POST /api/v0/code/symbols/search`
- `POST /api/v0/code/structure/inventory`
- `POST /api/v0/code/topics/investigate`
- `POST /api/v0/code/security/secrets/investigate`
- `POST /api/v0/code/imports/investigate`
- `POST /api/v0/code/call-graph/metrics`
- `POST /api/v0/code/relationships`
- `POST /api/v0/code/relationships/story`
- `POST /api/v0/code/call-chain`
- `POST /api/v0/code/dead-code`
- `POST /api/v0/code/dead-code/investigate`
- `POST /api/v0/code/complexity`
- `POST /api/v0/code/quality/inspect`
- `POST /api/v0/code/language-query`
- `POST /api/v0/code/cypher`
- `POST /api/v0/code/bundles`

Public code-query requests accept a repository selector in `repo_id` when a
repository scope is part of the request. The selector may be the canonical
repository ID, repository name, repository slug, or indexed path. The server
resolves it to the canonical repository ID before querying.

Interpret results using canonical `repo_id + relative_path`, not absolute
server-local paths.

## Code Search

`POST /api/v0/code/search`

```json
{
  "query": "process_payment",
  "repo_id": "payments",
  "exact": false,
  "limit": 10
}
```

Use this for text and indexed code search when the caller has search terms but
not a known entity ID.

## Symbol Search

`POST /api/v0/code/symbols/search`

```json
{
  "symbol": "process_payment",
  "repo_id": "payments",
  "match_mode": "exact",
  "entity_types": ["function"],
  "limit": 25,
  "offset": 0
}
```

The symbol route returns definition-shaped rows with `source_handle`,
`classification=definition`, `match_kind`, `truncated`, and `ambiguity`.

## Structural Inventory

`POST /api/v0/code/structure/inventory`

```json
{
  "repo_id": "payments",
  "language": "python",
  "inventory_kind": "dataclass",
  "entity_kind": "class",
  "limit": 25,
  "offset": 0
}
```

Use this for prompts such as "list functions/classes", "show top-level
elements", "find dataclasses", "find documented functions", and "count
functions per file." Every request must include at least one scope filter:
`repo_id`, `file_path`, `language`, `entity_kind`, or `symbol`.

Responses are deterministic and paged with `truncated` and `next_offset`.

## Topic Investigation

`POST /api/v0/code/topics/investigate`

```json
{
  "topic": "repo sync authentication and GitHub App auth resolution",
  "repo_id": "eshu",
  "intent": "explain_auth_flow",
  "limit": 25,
  "offset": 0
}
```

Use this before exact symbol lookup when the caller names a behavior instead of
a known symbol. Responses include searched terms, matched files, matched
symbols, evidence groups, call graph handles, recommended next calls, coverage,
limit, offset, and truncation state.

## Hardcoded Secret Investigation

`POST /api/v0/code/security/secrets/investigate`

```json
{
  "repo_id": "payments",
  "finding_kinds": ["api_token", "password_literal"],
  "include_suppressed": false,
  "limit": 25,
  "offset": 0
}
```

Use this for prompts about potential hardcoded passwords, API keys, and
secrets. Responses return redacted excerpts, finding kind, confidence,
severity, suppression notes, source handles, coverage, limit, offset, and
truncation state.

## Import Dependencies

`POST /api/v0/code/imports/investigate`

```json
{
  "query_type": "imports_by_file",
  "repo_id": "payments",
  "source_file": "src/module_a.py",
  "language": "python",
  "limit": 25,
  "offset": 0
}
```

The route supports imports by file, importers, package imports, direct file
cycles, module dependencies, and cross-module calls. It requires at least one
scope anchor: `repo_id`, `source_file`, `target_file`, `source_module`, or
`target_module`.

## Call Graph Metrics

`POST /api/v0/code/call-graph/metrics`

```json
{
  "metric_type": "hub_functions",
  "repo_id": "payments",
  "language": "go",
  "limit": 25,
  "offset": 0
}
```

The route requires `repo_id` and supports `hub_functions` and
`recursive_functions`. Hub rows include incoming, outgoing, and total degree.
Recursive rows include recursion kind and evidence.

## Relationships And Call Chains

`POST /api/v0/code/relationships`

Use `entity_id` when the caller already has a canonical entity. The route also
accepts `name`, optional `direction`, and `relationship_type`. Set
`transitive=true` with `relationship_type=CALLS` for indirect callers or
callees, and use `max_depth` to cap traversal.

`POST /api/v0/code/relationships/story`

Use this when the caller needs a narrative relationship packet. The route
resolves one target first and returns bounded candidates if the target is
ambiguous. It supports direct relationships, bounded transitive CALLS
traversal, class hierarchy prompts, and override prompts.

`POST /api/v0/code/call-chain`

Use this when the caller needs a bounded path between a start and end symbol or
entity. Lightweight profiles that cannot answer authoritative graph traversal
return `unsupported_capability` instead of guessing.

## Dead Code

`POST /api/v0/code/dead-code/investigate`

```json
{
  "repo_id": "payments",
  "language": "typescript",
  "limit": 100,
  "offset": 0,
  "exclude_decorated_with": ["@route", "@app.route"]
}
```

Use investigation mode for prompts such as "What code is dead in this repo?"
It returns coverage, language maturity, exactness blockers, cleanup-ready and
ambiguous buckets, source handles, and recommended next calls.

`POST /api/v0/code/dead-code`

Use the lower-level candidate scan when a client needs raw candidate rows.
`repo_id` and `language` are optional. `limit` defaults to `100` and is capped
at `500`.

The dead-code response is intentionally `derived` until the broader framework,
public API, reflection, and user-configured root registry from
[Dead-Code Reachability Spec](../dead-code-reachability-spec.md) is implemented.
Language-specific root and blocker details live in that spec and the OpenAPI
description; do not duplicate the full language matrix here.

## Complexity And Quality

`POST /api/v0/code/complexity`

Accepts `entity_id` or `function_name` for a single function. Without a single
selector, it returns a bounded deterministic `results` list with `limit` and
`truncated`.

`POST /api/v0/code/quality/inspect`

Use this for prompts about complex functions, long functions, high argument
count, or combined refactoring candidates. Supported `check` values are
`complexity`, `function_length`, `argument_count`, and
`refactoring_candidates`.

## Language Query

`POST /api/v0/code/language-query`

Use this for language-specific query contracts that do not fit the generic
symbol, relationship, or structural inventory routes. Prefer the focused code
routes first when they can answer the request.

## Diagnostics-Only Code Routes

`POST /api/v0/code/cypher`

Diagnostics-only. It accepts `cypher_query` plus optional `limit`, rejects
writes, uses a request timeout, and appends or enforces bounded limits. Use
purpose-built code, story, impact, and content routes for normal prompt
contracts.

## Bundle Search

`POST /api/v0/code/bundles`

Searches indexed repositories as pre-indexed bundle candidates. It does not
upload files, mutate graph state, or import `.eshu` archives.
