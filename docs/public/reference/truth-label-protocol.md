# Truth Label Protocol

Truth labels are the wire-level authority contract for HTTP API, MCP, and CLI
responses. They tell clients whether a result is authoritative, derived from
indexed state, or an explicitly bounded fallback.

The query-response implementation lives in `go/internal/query/contract.go`.
Reducer materialization layer names live in `go/internal/truth`.

## Truth Levels

| Level | Meaning |
| --- | --- |
| `exact` | Authoritative graph truth or durable semantic truth. |
| `derived` | Deterministic result from indexed entities, content, or structured relational state. |
| `fallback` | Exploratory result that is useful but not authoritative for the capability. |

High-authority capabilities such as transitive call graphs, call-chain paths,
and dead-code cleanup must return `unsupported_capability` when the active
profile cannot answer them correctly. They must not silently downgrade to
`fallback`.

## Canonical Envelope

Programmatic HTTP clients opt in with:

```http
Accept: application/eshu.envelope+json
```

The canonical envelope is:

```json
{
  "data": {},
  "truth": {},
  "error": null
}
```

Successful responses set `data` and `truth`, with `error: null`. Failed
responses set `error`, usually with `data: null`. Error details may carry
bounded machine-readable diagnostics.

## Truth Fields

| Field | Contract |
| --- | --- |
| `level` | Rollup truth level for the response. |
| `capability` | Capability ID from the conformance matrix. |
| `profile` | `local_lightweight`, `local_authoritative`, `local_full_stack`, or `production`. |
| `basis` | `authoritative_graph`, `semantic_facts`, `content_index`, or `hybrid`. |
| `backend` | Optional graph backend identity, currently `neo4j` or `nornicdb`. |
| `freshness` | Object with `state`, optional `observed_at`, and optional `detail`. |
| `reason` | Human-readable explanation for logs, CLI output, and debugging. |

`authoritative` is not a canonical wire field. Clients infer authority from
`level == "exact"` plus capability semantics.

Freshness states are:

- `fresh`
- `stale`
- `building`
- `unavailable`

## Error Codes

Current query error codes are:

- `unsupported_capability`
- `ambiguous`
- `unauthenticated`
- `invalid_argument`
- `not_found`
- `permission_denied`
- `backend_unavailable`
- `index_building`
- `scope_not_found`
- `capability_degraded`
- `overloaded`
- `internal_error`
- `documentation_read_model_unavailable`

Unsupported capability errors include the capability and current/required
profiles when the handler has that information.

## Per-Item Truth

List responses may contain mixed-confidence entries. Individual items may carry
their own `truth` object. The top-level `truth.level` should be the worst item
level or the response-level level, whichever is less authoritative.

## MCP Contract

MCP tool results should include a resource content block whose payload is the
canonical envelope:

```json
{
  "type": "resource",
  "resource": {
    "uri": "eshu://tool-result/envelope",
    "mimeType": "application/eshu.envelope+json",
    "text": "{\"data\":{},\"truth\":{},\"error\":null}"
  }
}
```

A text block may summarize the result, but the embedded envelope remains the
client contract.

## CLI Contract

CLI JSON mode should emit the canonical envelope shape. Human output may render
the result payload plus a concise truth summary when the result is not exact:

```text
truth=derived basis=content_index capability=code_search.exact_symbol
```

For unsupported capabilities, CLI commands should fail non-zero and report the
current and required profiles.

## Truth Layers

`go/internal/truth` defines reducer materialization layers:

- `source_declaration`
- `applied_declaration`
- `observed_resource`
- `canonical_asset`

`canonical_asset` is reducer output, not an accepted source input for
`truth.Contract.SourceLayers`.

## Cache Guidance

HTTP caches, local memoization, ETags, or equivalent validators must vary on:

- request payload
- `truth.level`
- `truth.freshness.state`

Do not reuse a cached result across truth-level or freshness changes.
