# Language Query DSL

Eshu exposes a small structured DSL for language-specific entity queries over
indexed code. It is the canonical surface for "give me decorators on this
class," "list methods of this struct," "which imports reference this symbol,"
and similar structured-code questions.

Two transport paths expose the same DSL:

- MCP tool: `execute_language_query`
- HTTP route: `POST /api/v0/code/language-query`

Both accept the same JSON payload. The MCP dispatcher requests the canonical
Eshu envelope from the mounted HTTP route; the direct HTTP handler currently
returns the data object itself.

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

Accepted names:

`c`, `cpp`, `csharp`, `dart`, `elixir`, `go`, `haskell`, `hcl`, `java`,
`javascript`, `jsx`, `kotlin`, `perl`, `php`, `python`, `ruby`, `rust`,
`scala`, `sql`, `swift`, `tsx`, `typescript`.

`jsx` normalizes to `javascript`; `tsx` normalizes to `typescript`.
Unsupported languages return HTTP 400 with the valid values.

## Entity types

Entity types are resolved against three backing stores:

- **Graph-backed** — served from the canonical graph backend when available.
- **Graph-first with content fallback** — graph first, falls back to Postgres
  content store if graph is empty for the language/type.
- **Content-only** — served from the Postgres content store.

Accepted values in the current `entity_type` enum:

`annotation`, `class`, `component`, `directory`, `enum`, `file`, `function`,
`guard`, `impl_block`, `macro`, `module`, `module_attribute`, `protocol`,
`protocol_implementation`, `repository`, `sql_column`, `sql_function`,
`sql_index`, `sql_table`, `sql_trigger`, `sql_view`, `struct`,
`terraform_backend`, `terraform_check`, `terraform_import`,
`terraform_lock_provider`, `terraform_module`, `terraform_moved_block`,
`terraform_removed_block`, `terragrunt_config`, `terragrunt_dependency`,
`terragrunt_input`, `terragrunt_local`, `type_alias`, `type_annotation`,
`typedef`, `union`, `variable`.

`guard` is a semantic filter over `function` entities and returns
guard-classified functions only.

## Capability Mapping

Selected semantic filters answer symbol-graph capabilities from the
[capability matrix](capability-conformance-spec.md). Truth ceilings for those
capabilities are:

| Capability | `local_lightweight` | `local_authoritative` | `local_full_stack` | `production` |
| --- | --- | --- | --- | --- |
| `symbol_graph.class_methods` | `derived` | `exact` | `exact` | `exact` |
| `symbol_graph.decorators` | `derived` | `exact` | `exact` | `exact` |
| `symbol_graph.argument_names` | `derived` | `exact` | `exact` | `exact` |
| `symbol_graph.imports` | `derived` | `exact` | `exact` | `exact` |
| `symbol_graph.inheritance` | `derived` | `exact` | `exact` | `exact` |

Under `local_lightweight`, answers are served from indexed entities and
relational content without the authoritative graph. Higher profiles serve the
authoritative graph and upgrade to `exact`.

## Example request

HTTP:

```http
POST /api/v0/code/language-query HTTP/1.1
Content-Type: application/json

{
  "language": "python",
  "entity_type": "class",
  "query": "User",
  "repo_id": "eshu",
  "limit": 10
}
```

Direct HTTP response:

```json
{
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
