# Language Query DSL

Eshu exposes a small structured DSL for language-specific entity queries over
indexed code. It is the canonical surface for "give me decorators on this
class," "list methods of this struct," "which imports reference this symbol,"
and similar structured-code questions.

Two transport equivalents expose the same DSL:

- MCP tool: `execute_language_query`
- HTTP route: `POST /api/v0/code/language-query`

Both accept the same JSON payload and return the same result shape. When the
client opts into the canonical envelope via
`Accept: application/eshu.envelope+json`, results arrive as a
[`{data, truth, error}` envelope](truth-label-protocol.md).

## Payload

```json
{
  "language": "python",
  "entity_type": "class",
  "query": "User",
  "repo_id": "eshu",
  "limit": 25
}
```

### Fields

| Field | Required | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `language` | yes | string | — | Canonical language name. See [Supported languages](#supported-languages). |
| `entity_type` | yes | string | — | Entity kind to search for. See [Entity types](#entity-types). |
| `query` | no | string | empty | Optional name-substring filter applied to the entity. Empty = list all matching entities. |
| `repo_id` | no | string | empty | Optional canonical repository id to scope the search. |
| `limit` | no | integer | 50 | Maximum number of results. |

## Supported languages

Canonical names accepted by the `language` field:

`c`, `cpp`, `csharp`, `dart`, `go`, `haskell`, `java`, `javascript`, `perl`,
`python`, `ruby`, `rust`, `scala`, `sql`, `swift`, `typescript`.

Common aliases are normalized at request time. Unsupported languages return
HTTP 400 with a list of valid values.

## Entity types

Entity types are resolved against three backing stores:

- **Graph-backed** — served from the canonical graph backend when available.
- **Graph-first with content fallback** — graph first, falls back to Postgres
  content store if graph is empty for the language/type.
- **Content-only** — served from the Postgres content store.

Accepted values in the `entity_type` enum:

`repository`, `directory`, `file`, `module`, `function`, `class`, `struct`,
`enum`, `union`, `macro`, `variable`, `sql_table`, `sql_view`,
`sql_function`, `sql_trigger`, `sql_index`, `sql_column`.

The surface also accepts `guard` as a semantic filter over `function`
entities (returns guard-classified functions only).

## Capability mapping

Each `entity_type` answers one capability from the
[capability matrix](capability-conformance-spec.md). Truth level per profile
follows the matrix:

| `entity_type` | Capability | `local_lightweight` | `local_full_stack` | `production` |
| --- | --- | --- | --- | --- |
| `class` | `symbol_graph.class_methods` | `derived` | `exact` | `exact` |
| `function` (decorators filter) | `symbol_graph.decorators` | `derived` | `exact` | `exact` |
| `function` (arguments filter) | `symbol_graph.argument_names` | `derived` | `exact` | `exact` |
| `module` / `file` (import filter) | `symbol_graph.imports` | `derived` | `exact` | `exact` |
| `class` / `struct` (inheritance) | `symbol_graph.inheritance` | `derived` | `exact` | `exact` |

Under `local_lightweight`, answers are served from indexed entities and
relational content without the authoritative graph. Higher profiles serve the
authoritative graph and upgrade to `exact`.

## Example request

HTTP:

```http
POST /api/v0/code/language-query HTTP/1.1
Accept: application/eshu.envelope+json
Content-Type: application/json

{
  "language": "python",
  "entity_type": "class",
  "query": "User",
  "repo_id": "eshu",
  "limit": 10
}
```

Envelope response:

```json
{
  "data": {
    "language": "python",
    "entity_type": "class",
    "query": "User",
    "results": [
      {
        "entity_id": "py-user-class-1",
        "name": "User",
        "labels": ["Class"],
        "file_path": "src/models/user.py",
        "repo_id": "eshu",
        "language": "python",
        "start_line": 12,
        "end_line": 87,
        "metadata": { "semantic_kind": "data_class" }
      }
    ]
  },
  "truth": {
    "level": "derived",
    "capability": "symbol_graph.class_methods",
    "profile": "local_lightweight",
    "basis": "content_index",
    "freshness": { "state": "fresh" },
    "reason": "resolved from indexed entity and content tables"
  },
  "error": null
}
```

## Errors

| Error | Cause |
| --- | --- |
| HTTP 400 `language is required` | `language` missing or empty. |
| HTTP 400 `entity_type is required` | `entity_type` missing or empty. |
| HTTP 400 `unsupported language "<x>"` | `language` not in the canonical set. |
| HTTP 400 `unsupported entity_type "<x>"` | `entity_type` not in the enum. |

## Related

- [HTTP API Reference](http-api.md)
- [MCP Guide](../guides/mcp-guide.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Truth Label Protocol](truth-label-protocol.md)
