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
| `freshness` | Object with `state`, optional `observed_at`, optional `detail`, optional `cause`, and optional `next_check`. |
| `reason` | Human-readable explanation for logs, CLI output, and debugging. |

`authoritative` is not a canonical wire field. Clients infer authority from
`level == "exact"` plus capability semantics.

Freshness states are:

- `fresh`
- `stale`
- `building`
- `unavailable`

## Freshness Causality

When an answer is not `fresh`, the freshness object may explain **why** with a
bounded `cause` and a recommended `next_check`. Causality is **additive and
optional**: a handler attaches a cause only when it holds the evidence for it
(for example a readiness verdict showing a dead-lettered domain, or a
generation-pending signal). A handler that cannot prove a cause leaves it unset
and never guesses.

`cause` is a closed enumeration:

| Cause | Meaning |
| --- | --- |
| `pending_repo_generation` | A repo's graph generation has not yet completed. |
| `reducer_backlog` | Queued reducer projection has not yet drained. |
| `dead_lettered_domain` | A domain's projection failed and is parked for repair. |
| `missing_collector_completion` | A collector has not reported a completed run for the coverage. |
| `content_coverage_unavailable` | Content coverage is not yet indexed for the scope. |
| `unsupported_profile` | The active profile cannot serve authoritative truth for the capability. |

`next_check` is a bounded follow-up call in the `recommended_next_calls` shape
(a `tool` or `route`, an optional `reason`, and optional bounded `params`). It
points at a status, generation, coverage, citation, or queue surface a consumer
can call to learn when the answer will catch up.

### Stale is not wrong

Freshness causality is **distinct from answer correctness**. A `stale`,
`building`, or `unavailable` answer is not an incorrect answer: it reflects
truth that was correct at `freshness.observed_at` and has a known, named reason
for lagging. `cause` explains the lag and `next_check` says where to look for
the catch-up; neither implies the data is wrong. Correctness is governed by
`level` and `basis`. A consumer should present a stale answer with its cause and
next check, not discard it as false.

The cause enumeration and the cause→`next_check` mapping live in
`go/internal/query/freshness_causality.go`. Causes are wired into handlers
incrementally; the metrics time-series handler is the first proof-of-contract
(it attaches `missing_collector_completion` when no collector is reporting and
`content_coverage_unavailable` when a metric has no indexed history yet).

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
