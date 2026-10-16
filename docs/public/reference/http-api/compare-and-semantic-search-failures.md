# Compare And Semantic Search Failures

`POST /api/v0/compare/environments` and `POST /api/v0/search/semantic` answer
a failed read with a fixed message for that read, never with the backend
error text (#7674). A graph, content-store, scope-resolver, or search-index
error can quote Cypher, SQL, relation names, hosts, credentials, and stored
document ids. The error goes on a span as an `exception` event, and the span
status is Error with the fixed message as its description. The compare route
opens no handler span, so it marks the request span. The semantic search
route marks its own `query.semantic_search` handler span.

MCP `compare_environments` and `search_semantic_context` call the same
handlers, so they get the same answers.

## Statuses

- A stale or timed-out PostgreSQL reader answers the retryable `503`
  `backend_unavailable` with `Retry-After`. On the compare route, the service
  evidence read goes through the guarded reader, and an unavailable graph
  backend answers the same retryable `503`. On the semantic search route, the
  scope resolution and the index read go through the guarded reader.
- A compare graph read that exceeds its deadline answers `504`, as before.
- Any other failed read answers `500` with the fixed message in the tables
  below.
- A client that cancels its own request gets `499` with the same fixed
  message. The span status stays Unset and the span gets only the
  `eshu.request.client_canceled` event, with no exception. A cancel counts
  only when the error wraps `context.Canceled` and the request's own context
  was canceled; a cancel from an inner context on a live request is still a
  `500`. `499` is not in the OpenAPI spec, because the client has already
  gone. See
  [Failed and canceled query reads](../telemetry/traces.md#failed-and-canceled-query-reads-7626).
- A reader-verdict `503` names the route's capability in `error.capability`:
  `platform_impact.environment_compare` or
  `semantic_search.curated_retrieval`.

## Environment comparison

| Failed read | Fixed message |
| --- | --- |
| workload lookup | `environment comparison workload lookup failed` |
| service evidence read | `environment comparison service evidence read failed` |
| left environment snapshot | `environment comparison left environment snapshot read failed` |
| right environment snapshot | `environment comparison right environment snapshot read failed` |

The `500` and `499` bodies keep the route's plain
`{"error": ..., "detail": ...}` shape. The compare route has no
configuration `503`, so every `503` it answers carries `Retry-After`.

## Semantic search

| Failed read | Fixed message |
| --- | --- |
| scope resolution | `semantic search scope resolution failed` |
| index or local hybrid retrieval, or a stored result that fails normalization | `semantic search retrieval failed` |

The `500` and `499` bodies keep the route's own error shape: the
`internal_error` envelope when the caller asks for
`application/eshu.envelope+json`, else
`{"error_code": ..., "message": ..., "capability": ...}`. A reader-verdict
`503` uses the shared verdict shape: the envelope when negotiated, else
`{"error": ..., "detail": ...}`.

Two configuration states answer `503` `backend_unavailable` with fixed text
and no `Retry-After`, because a retry does not help until an operator wires
the missing part:

- `semantic search requires the persisted search index`: no persisted search
  index is configured on this API instance.
- `semantic mode requires an embedder`: the request asked for mode `semantic`
  and no embedder is configured. Before #7674 the text carried a
  `search retrieval backend: ` prefix.

A canonical repository id that maps to several active ingestion scopes
answers `409` with the fixed text
`semantic search repository scope is ambiguous`, never the text of an error
that wraps it.

### Status changes

Two failures changed status in #7674:

- A scope-resolver fault that is neither a reader verdict nor an ambiguity
  answered `503` `backend_unavailable` with the resolver's error text. It now
  answers `500` `internal_error`.
- A stored result that fails response normalization (for example, a document
  with no graph handles) answered `400` `invalid_argument` and quoted the
  stored document id. The document comes from the server's own index, not
  from the request. It now answers `500` `internal_error`.

A server-side fault must not read as an availability signal or as a caller
error. The HTTP reference, the OpenAPI spec, and the MCP tool descriptions
named neither old status for these failures.

## What does not change

- The request-validation `400` answers on both routes. Their text describes
  only the caller's own input, such as `workload_id is required`,
  `limit is required`, or the JSON decoder's view of a malformed body.
- The semantic search `403` permission answers, the `404` for a repository
  outside the caller's grant, and the `501` for a profile without the
  capability.
- Both success shapes.

## Related

- [Semantic Search](semantic-search.md) for the semantic search contract.
- [Selector And Read Failures](selector-and-read-failures.md) for the shared
  client-cancel contract.
