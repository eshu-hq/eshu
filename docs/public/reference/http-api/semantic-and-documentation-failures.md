# Semantic Evidence And Documentation Route Failures

The two semantic evidence routes and the six documentation routes answer a
failed store read with a fixed message, never with the backend error text
(#7674). A backend error can quote SQL, relation names, hosts, and connection
settings. It goes on the route's handler span as an `exception` event, and
the span status is Error with the fixed message as its description. The
[Semantic Evidence](semantic-evidence.md) page and
[Evidence And Supply Chain](evidence-and-supply-chain.md) describe what each
route returns.

## Routes

| Route | Store read | Capability | Fixed message |
| --- | --- | --- | --- |
| `GET /api/v0/semantic/documentation-observations` | semantic evidence facts | `semantic_evidence.documentation_observations.list` | `semantic evidence request failed` |
| `GET /api/v0/semantic/code-hints` | semantic evidence facts | `semantic_evidence.code_hints.list` | `semantic evidence request failed` |
| `GET /api/v0/documentation/findings` | documentation findings | `documentation_findings.list` | `documentation evidence request failed` |
| `GET /api/v0/documentation/facts` | documentation facts | `documentation_facts.list` | `documentation evidence request failed` |
| `GET /api/v0/documentation/findings/{finding_id}/evidence-packet` | evidence packet | `documentation_evidence_packet.read` | `documentation evidence request failed` |
| `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` | packet freshness | `documentation_evidence_packet.freshness` | `documentation evidence request failed` |
| `GET /api/v0/documentation/findings/count` | finding aggregates | `documentation_findings.aggregate` | `documentation evidence request failed` |
| `GET /api/v0/documentation/findings/inventory` | finding aggregates | `documentation_findings.aggregate` | `documentation evidence request failed` |

The first six routes read through the content reader. The count and
inventory routes read through the documentation finding aggregate store.
Both are built on the guarded PostgreSQL reader in the API and in the MCP
server.

## Statuses

- A stale or timed-out PostgreSQL reader answers the retryable `503`
  `backend_unavailable` with `Retry-After` on every route above. The error
  envelope names the route's capability in `error.capability`.
- An unconfigured aggregate store answers `503` `backend_unavailable` with
  no `Retry-After` on the count and inventory routes. This is a
  configuration state and retrying does not help. The other six routes have
  no configuration `503`, so every `503` they answer carries `Retry-After`.
- Any other failure answers `500` with the fixed message in the table.
- A client that cancels its own request gets `499` with the same fixed
  message. The span status stays Unset and the span gets only the
  `eshu.request.client_canceled` event. A `context.Canceled` from an inner
  context while the request is still live is a server fault and answers
  `500`. `499` is not in the OpenAPI spec.

## Body shape

The `500` and `499` bodies keep each family's error shape. Only the status
and the span changed.

- The documentation routes answer `error_code` `internal_error`, the fixed
  `message`, and `correlation_id`, with no `capability` field. The
  correlation id is the `X-Correlation-ID` or `X-Request-ID` request header,
  or a generated id.
- The semantic evidence routes answer `error_code` `internal_error`, the
  fixed `message`, and `capability`.
- A caller that asks for the Eshu envelope gets the same fields under
  `error`, with `code` in place of `error_code`.

The reader-fence `503` uses the shared `backend_unavailable` envelope, the
same as on the [status and evidence routes](status-evidence-failures.md).

## Not implemented

The OpenAPI spec now declares the `501` these routes already answered. The
bodies do not change.

- The two semantic evidence routes answer `501`
  `documentation_read_model_unavailable` when no Postgres fact read model is
  wired. The plain body carries `error_code`, `message`, and `capability`.
- They answer `501` `unsupported_capability` on a runtime profile that does
  not support the capability. The plain body carries `error` and `detail`,
  and the envelope adds the current and required profiles. Every runtime
  profile supports both capabilities today.
- The count and inventory routes answer `501` `unsupported_capability` on a
  runtime profile that does not support `documentation_findings.aggregate`,
  such as `local_lightweight`. The plain body carries `error_code`, `message`,
  `capability`, and `correlation_id`, and the envelope adds the current and
  required profiles.
- The four other documentation routes already declared their `501`.

## Ask SSE without a flusher

`POST /api/v0/ask` with `Accept: text/event-stream` needs a response writer
that can flush. When the writer cannot, the route answers `500` with the
plain JSON body
`{"error":"internal_error","detail":"streaming not supported by this server configuration"}`.
This happens before any event-stream header is written and before any engine
call. The request span now gets status Error with that message and one
`exception` event. No store or engine read runs before this step, so it has
no cancel error to detect: a canceled request still answers `500` here, never
`499`. The
status, the body, and the headers do not change.

## What does not change

- The existing `400`, `403`, `404`, and `501` answers on these routes; the
  `501` is now declared on all eight.
- The fixed messages: these routes already answered these exact strings.
- The rank-cursor errors on the search routes that accept `rank_cursor`.
  They keep their statuses and bodies byte for byte: `400` for
  `rank_cursor_invalid` and `rank_cursor_unsupported`, `409` for
  `rank_cursor_basis_mismatch`. See [Rank Walk](rank-walk.md).

## Related

- [Selector And Read Failures](selector-and-read-failures.md) for the shared
  client-cancel contract.
- [Status And Evidence Route Failures](status-evidence-failures.md) for the
  same contract on the status and evidence routes.
