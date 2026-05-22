# Truth Label Protocol

The truth label protocol is the wire-level contract for CLI, HTTP API, and MCP
responses. It tells clients whether a result is authoritative, derived, or an
explicitly bounded fallback.

## Truth Levels

| Level | Meaning |
| --- | --- |
| `exact` | Authoritative graph truth or durable semantic truth. |
| `derived` | Deterministic result computed from indexed entities, content, or structured relational state. |
| `fallback` | Exploratory result that is useful but not strong enough to claim authority for the requested capability. |

High-authority capabilities, such as transitive caller analysis, call-chain
paths, and dead-code detection, must not silently downgrade to `fallback` when
the current profile cannot answer them correctly. They return
`unsupported_capability`.

## Canonical Envelope

```json
{
  "data": {},
  "truth": {},
  "error": null
}
```

Rules:

- successful responses set `data` and `truth`, with `error: null`
- failed responses set `error`, with `data: null`
- failed responses may carry bounded machine-readable diagnostics under
  `error.details`
- `truth` may be present on partial failures when it adds useful state

The envelope MIME type is `application/eshu.envelope+json`.

## Truth Fields

| Field | Contract |
| --- | --- |
| `level` | Rollup truth level for the whole response. |
| `basis` | `authoritative_graph`, `semantic_facts`, `content_index`, or `hybrid`. |
| `capability` | Capability ID from the conformance matrix. |
| `profile` | `local_lightweight`, `local_authoritative`, `local_full_stack`, or `production`. |
| `backend` | Optional graph backend identity, currently `neo4j` or `nornicdb`; absent when no graph adapter was exercised. |
| `freshness` | Object with `state`, optional `observed_at`, and optional `detail`. |
| `reason` | Human-readable explanation for logs, CLI rendering, and debugging. |

`authoritative` is not a canonical wire field. Clients infer authority from
`level == "exact"` plus the capability semantics.

Freshness states:

- `fresh`: indexed truth is current for the requested scope
- `stale`: indexed truth exists, but lag or backlog may hide newer source state
- `building`: initial or replacement indexing is still in progress
- `unavailable`: the required backend or authoritative source is unavailable

Example:

```json
{
  "data": {
    "matches": []
  },
  "truth": {
    "level": "derived",
    "capability": "code_search.exact_symbol",
    "profile": "local_lightweight",
    "basis": "content_index",
    "freshness": {
      "state": "fresh"
    },
    "reason": "resolved from indexed entity and content tables"
  },
  "error": null
}
```

## Per-Item Truth

List responses may contain mixed-confidence entries. In those cases:

- each item may carry its own `truth` object
- top-level `truth.level` is the worst item level or response-level level,
  whichever is less authoritative
- item truth uses the same schema shape, but may omit inherited `capability`
  and `profile`

## Errors

Unsupported high-authority requests use a structured error:

```json
{
  "data": null,
  "truth": null,
  "error": {
    "code": "unsupported_capability",
    "message": "transitive callers require authoritative graph mode",
    "capability": "call_graph.transitive_callers",
    "profiles": {
      "current": "local_lightweight",
      "required": "local_authoritative"
    }
  }
}
```

Current error codes from `go/internal/query/contract.go`:

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

## MCP Contract

MCP tool results should include one `resource` content block whose payload is
the canonical envelope. A human-readable `text` block may also be returned, but
the embedded envelope remains the client contract.

```json
{
  "content": [
    {
      "type": "text",
      "text": "Found 3 matches."
    },
    {
      "type": "resource",
      "resource": {
        "uri": "eshu://tool-result/envelope",
        "mimeType": "application/eshu.envelope+json",
        "text": "{\"data\":{},\"truth\":{},\"error\":null}"
      }
    }
  ]
}
```

## CLI Contract

The CLI should display the normal result payload and a concise truth summary
when the result is not `exact`.

```text
truth=derived basis=content_index capability=code_search.exact_symbol
```

For unsupported capabilities, the CLI should fail non-zero and print the
current and required profiles. In `--json` mode, CLI commands should emit the
canonical envelope shape.

## Cache Guidance

Any HTTP cache, local memoization, ETag, or equivalent validator must vary on:

- request payload
- `truth.level`
- `truth.freshness.state`

Do not reuse a cached result across truth-level or freshness changes.

## Verification

Contract changes must update the shared Go truth types, the capability matrix,
and tests for the affected HTTP, MCP, or CLI response path. New capability IDs
must be registered before handlers call `BuildTruthEnvelope`; unknown
capabilities panic in the Go contract tests.
