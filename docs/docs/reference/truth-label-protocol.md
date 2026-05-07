# Truth Label Protocol

This document defines the wire-level truth contract for CLI, HTTP API, and MCP
responses.

The goal is simple: users should be able to tell whether a result is
authoritative, derived, or unavailable without guessing from wording.

## Truth Levels

- `exact`
  - authoritative graph truth or durable semantic truth
- `derived`
  - deterministic result computed from indexed entities, content, or other
    structured relational state
- `fallback`
  - exploratory result that is useful but not strong enough to claim full
    authority for the requested capability

High-authority capabilities such as transitive caller analysis, call-chain
paths, and dead-code detection must not silently downgrade to `fallback` in a
profile that cannot answer them correctly. Those return a structured
unsupported-capability error.

## Canonical Envelope

The canonical response envelope is:

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
- `truth` may still be present on partial failures if it adds useful state

## HTTP Response Shape

Successful responses should use the canonical envelope:

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

### Fields

- `level`
  - rollup level for the whole response
- `basis`
  - one of:
    - `authoritative_graph`
    - `semantic_facts`
    - `content_index`
    - `hybrid`
- `capability`
  - capability ID from the conformance matrix
- `profile`
  - `local_lightweight`, `local_authoritative`, `local_full_stack`, or
    `production`
- `backend`
  - optional graph-backend identity when the response was served through a
    graph adapter. Current values: `neo4j`, `nornicdb`. Absent when no
    graph adapter was exercised (for example on `local_lightweight`).
- `freshness`
  - object with:
    - `state`
      - `fresh`, `stale`, `building`, or `unavailable`
    - `observed_at`
      - optional RFC3339 timestamp
    - `detail`
      - optional operator-facing summary
- `reason`
  - human-readable explanation in English for logs, CLI rendering, and debugging

`authoritative` is not a canonical wire field. Clients should infer it from
`level == "exact"` together with capability semantics.

### Freshness States

- `fresh`
  - the runtime believes the answer reflects the current indexed truth for the
    requested scope
- `stale`
  - previously indexed truth exists, but backlog or lag means the answer may
    not reflect the latest source state
- `building`
  - initial or replacement indexing is still in progress and authoritative data
    is not ready yet
- `unavailable`
  - the required backend or authoritative source is currently unavailable

## Per-Item Truth

List responses may contain mixed-confidence entries. In those cases:

- each item may carry its own `truth` object
- the top-level `truth.level` is the worst item level or response-level level,
  whichever is less authoritative
- item truth uses the same schema shape, but `capability` and `profile` may be
  omitted when inherited from the response

Example:

```json
{
  "data": {
    "edges": [
      {
        "id": "e1",
        "truth": {
          "level": "exact",
          "basis": "authoritative_graph",
          "freshness": {
            "state": "fresh"
          }
        }
      },
      {
        "id": "e2",
        "truth": {
          "level": "derived",
          "basis": "hybrid",
          "freshness": {
            "state": "stale",
            "detail": "cross-repo enrichment is lagging"
          },
          "reason": "edge derived from partially converged hybrid evidence"
        }
      }
    ]
  },
  "truth": {
    "level": "derived",
    "capability": "platform_impact.blast_radius",
    "profile": "production",
    "basis": "hybrid",
    "freshness": {
      "state": "stale",
      "detail": "reducer backlog exceeds freshness threshold"
    },
    "reason": "response contains at least one derived edge"
  },
  "error": null
}
```

## Unsupported Capability Error

When a runtime cannot answer a high-authority question correctly, it should
return:

```json
{
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

## Error Codes

The initial structured error codes are:

- `unsupported_capability`
- `backend_unavailable`
- `index_building`
- `scope_not_found`
- `capability_degraded`
- `overloaded`

## MCP Contract

MCP tool results should include one `resource` content block whose resource
payload is the canonical envelope.

If a human-readable text block is also returned, the structured envelope remains
the canonical client contract.

Structured MCP results should therefore look like:

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

This follows MCP's embedded-resource model rather than inventing a custom
content block type.

## CLI Contract

The CLI should display:

- the normal result payload
- a concise truth summary when the result is not `exact`
- a stable JSON envelope when `--json` is used

Example:

```text
truth=derived basis=content_index capability=code_search.exact_symbol
```

For unsupported capabilities, the CLI should fail non-zero and print an
actionable explanation:

```text
unsupported capability: call_graph.transitive_callers
current profile: local_lightweight
required profile: local_authoritative
```

### CLI JSON Mode

`eshu ... --json` should emit the same canonical envelope shape used by the HTTP
API, adapted only for CLI serialization.

## Cache Guidance

If HTTP caching or client-side memoization is used, cached results must be
invalidated when truth level or freshness changes. ETags, cache keys, or
equivalent validators should therefore vary on:

- request payload
- `truth.level`
- `truth.freshness.state`

## Test Requirements

- one shared truth type in Go
- HTTP response tests for supported and unsupported cases
- MCP response tests preserving truth metadata
- CLI rendering tests for non-exact and unsupported cases
- CLI `--json` tests for the canonical envelope
