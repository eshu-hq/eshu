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

Accepted here means the route can query indexed entities for that language; it
does not promote every framework, route, outbound-contract, dead-code, or
cross-repo relationship claim to full parity. The current feature-level contract
is the [Language Feature Parity Ledger](../languages/support-maturity.md#language-feature-parity-ledger),
which marks supported, partial, and derived features and links each row to
implementation, tests, docs, read surfaces, and follow-up issues. Features
marked partial or absent from the ledger remain unsupported for API/MCP parity
claims even when the parser can index source entities.

## Entity types

Entity types are resolved against three backing stores:

- **Graph-backed** — served from the canonical graph backend when available.
- **Graph-first with content fallback** — graph first, falls back to Postgres
  content store if graph is empty for the language/type.
- **Content-only** — served from the Postgres content store.

Accepted values in the current `entity_type` enum:

`annotation`, `class`, `component`,
`directory`, `enum`, `file`, `function`, `guard`, `impl_block`, `macro`,
`module`, `module_attribute`, `protocol`, `protocol_implementation`,
`repository`, `sql_column`, `sql_function`, `sql_index`, `sql_migration`, `sql_table`,
`sql_trigger`, `sql_view`, `struct`, `terraform_backend`, `terraform_check`,
`terraform_import`, `terraform_lock_provider`, `terraform_module`,
`terraform_moved_block`, `terraform_removed_block`, `terragrunt_config`,
`terragrunt_dependency`, `terragrunt_input`, `terragrunt_local`, `type_alias`,
`type_annotation`, `typedef`, `union`, `variable`.

`atlantis_project`/`atlantis_workflow` are **not** language-queryable and are
deliberately absent from the enum above: Atlantis entities carry language
`yaml`, which `language-query` does not accept, so no language/entity_type pair
could return rows. They are served instead by `resolve_entity` /
`get_entity_context` (which resolve the `AtlantisProject`/`AtlantisWorkflow`
graph labels and return their outgoing governance edges) and by
`list_relationship_edges` for the MANAGES / ATLANTIS_DEPENDS_ON / USES_WORKFLOW
verbs. See [Atlantis Parser](../languages/atlantis.md#query-surfacing).

`flux_kustomization`/`flux_git_repository`/`flux_oci_repository`/`flux_bucket`
are likewise **not** language-queryable and are deliberately absent from the enum
above: Flux CRs carry language `yaml`, which `language-query` does not accept.
They are served by `get_entity_context`, which resolves the `FluxKustomization` /
`FluxGitRepository` / `FluxOCIRepository` / `FluxBucket` graph labels and
surfaces their typed fields (`url`, `source_ref_*`, `ref_*`, `bucket_name`,
`endpoint`, `provider`, `source_path`, `target_namespace`, `generate_name`)
through content-metadata enrichment, and by `list_relationship_edges` for the
`RECONCILES_FROM` verb (issue #5360 PR B; a `FluxKustomization` reconciling
manifests from its resolved source CR). `resolve_entity` by name is a
separate, still-deferred follow-up. See [Flux Parser](../languages/flux.md).

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

## Adding Or Promoting Language Query Support

Parse-only behavior is not supported query behavior. A parser can emit rows for
a language or entity kind while `execute_language_query` still rejects that
language, omits that entity type, or returns only a lower-authority fallback.

When adding or promoting language-query support:

1. Update the Go registry or handler enum that accepts the `language` or
   `entity_type` value.
2. Add focused HTTP or MCP coverage for the accepted value, unsupported-value
   error behavior, limit handling, and deterministic result shape.
3. State whether the entity type is graph-backed, graph-first with content
   fallback, or content-only.
4. Update the affected language page with the query surface and proof path.
5. Update `specs/language-feature-parity-ledger.v1.yaml` so the promoted
   feature has implementation files, test files, docs, read surfaces, parser
   backing, and deterministic no-provider proof.
6. Update this page when accepted values, backing-store behavior, error
   semantics, truth ceilings, or HTTP/MCP parity changes.
7. Run `scripts/verify-parser-relationship-kit.sh`, focused query tests, the
   docs build, and `git diff --check`.

Guardrails:

- Dynamic imports, runtime plugin loading, reflection, generated code,
  framework discovery, and framework-specific roots remain unsupported query
  behavior until the query path has focused tests for that exact pattern.
- A language page can document source evidence or parser metadata without
  adding it to this DSL. Do not list a value here until the route accepts it.
- Unsupported languages and entity types must fail cheaply with the documented
  errors instead of returning empty supported-looking results.
- If the answer is profile-limited, stale, partial, or unsupported, preserve the
  normal Eshu truth and error conventions instead of silently downgrading.

## Related

- [HTTP API Reference](http-api.md)
- [MCP Guide](../guides/mcp-guide.md)
- [Capability Conformance Spec](capability-conformance-spec.md)
- [Truth Label Protocol](truth-label-protocol.md)
