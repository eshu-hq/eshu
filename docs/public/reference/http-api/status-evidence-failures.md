# Status And Evidence Route Failures

The status read routes, the evidence routes, and the investigation packet
routes answer a failed read or build step with a fixed message for that step,
never with the backend error text (#7674). A backend error can quote SQL,
Cypher, relation names, hosts, and connection settings. It goes on the request
span as an `exception` event, and the span status is Error with the fixed
message as its description. The [Status And Admin](status-admin.md) page
describes what each status route returns.

## Statuses

- A stale or timed-out PostgreSQL reader answers the retryable `503`
  `backend_unavailable` with `Retry-After` on every read below. The status
  snapshot, the live-activity board, the governance audit summary, the content
  store, the relationship evidence read model, the admission decision read
  model, and the drift finding store all read through the guarded reader.
- A `503` without `Retry-After` is a configuration state. On these routes it
  means the status reader, the live-activity reader, the graph backend, or the
  admission decision read model is not configured on this API instance. Its
  text is fixed, for example `status reader not configured`.
- Any other failure answers `500` with the fixed message in the table.
- A client that cancels its own request gets `499` with the same fixed
  message. The span status stays Unset and the span gets only the
  `eshu.request.client_canceled` event. `499` is not in the OpenAPI spec.
- The status snapshot read runs under its own internal time bound. A
  snapshot read that runs out of that bound is not a reader verdict and
  answers the route's `500`.
- The existing `400`, `404`, and `501` answers do not change.

## Capability field

A reader-verdict `503` names the route's capability-matrix key in
`error.capability` on the routes that have one. These routes have no
capability-matrix key yet, so their reader-verdict error envelope carries no
`capability` field: `GET /api/v0/status/pipeline`,
`GET /api/v0/status/collectors`, `GET /api/v0/collectors`,
`GET /api/v0/status/ingesters/{ingester}`, `GET /api/v0/ingesters/{ingester}`,
`GET /api/v0/status/index`, `GET /api/v0/index-status`,
`GET /api/v0/status/hosted-readiness`, `GET /api/v0/status/freshness-causality`,
`GET /api/v0/status/collector-readiness`, `GET /api/v0/collector-readiness`,
`GET /api/v0/evidence/bundle`, and the two admin audit reads below. The
status, `Retry-After`, code, and message are the same as on a keyed route.
#8227 registers their keys.

## Index status for a scoped caller

`GET /api/v0/status/index` and `GET /api/v0/index-status` make one of two
reads, chosen by the caller:

- An unscoped caller reads the status snapshot through the guarded
  PostgreSQL reader. A stale or timed-out reader answers the retryable `503`
  with `Retry-After`. Any other failure answers `500`
  `index status read failed`. No status reader answers `503`
  `status reader not configured` with no `Retry-After`.
- A scoped caller does not read the status snapshot. Its only read is the
  granted-repository count in the graph, and a caller with no grant makes no
  read. A graph outage answers the retryable `503` with `Retry-After`. A graph
  read that exceeds its deadline answers `504`. A count that fails any other
  way, or that returns no row, answers `500`
  `scoped index status repository count failed`. No graph backend answers
  `503` `graph backend not configured` with no `Retry-After`.

## Fixed messages

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `GET /api/v0/status/pipeline` | status snapshot read | `pipeline status read failed` |
| `GET /api/v0/status/collectors`, `GET /api/v0/collectors` | status snapshot read | `collector status read failed` |
| `GET /api/v0/status/ingesters/{ingester}`, `GET /api/v0/ingesters/{ingester}` | status snapshot read | `ingester status read failed` |
| `GET /api/v0/status/index`, `GET /api/v0/index-status` | status snapshot read (unscoped caller) | `index status read failed` |
| `GET /api/v0/status/index`, `GET /api/v0/index-status` | graph repository count (scoped caller) | `scoped index status repository count failed` |
| `GET /api/v0/status/hosted-readiness` | status snapshot read | `hosted readiness status read failed` |
| `GET /api/v0/status/freshness-causality` | status snapshot read | `freshness causality status read failed` |
| `GET /api/v0/status/collector-readiness`, `GET /api/v0/collector-readiness` | status snapshot read | `collector readiness status read failed` |
| `GET /api/v0/status/semantic-extraction` | status snapshot read | `semantic extraction status read failed` |
| `GET /api/v0/status/operations` | status snapshot read | `operations status read failed` |
| `GET /api/v0/status/operations` | live-activity read | `operations live activity read failed` |
| `GET /api/v0/status/governance` | status snapshot read | `governance status read failed` |
| `GET /api/v0/status/governance` | governance audit summary read | `governance audit summary read failed` |
| `GET /api/v0/status/answer-narration` | status snapshot read | `answer narration status read failed` |
| `GET /api/v0/evidence/bundle` | status snapshot read | `live evidence bundle status read failed` |
| `GET /api/v0/evidence/bundle` | bundle validation | `live evidence bundle composition failed` |
| `GET /api/v0/evidence/relationships/{resolved_id}` | relationship evidence read | `relationship evidence read failed` |
| `POST /api/v0/evidence/citations` | file content read | `evidence citation file read failed` |
| `POST /api/v0/evidence/citations` | entity content read | `evidence citation entity read failed` |
| `GET /api/v0/investigations/deployable-unit/packet` | admission decision read | `deployable-unit packet admission decision read failed` |
| `GET /api/v0/investigations/deployable-unit/packet` | packet build | `deployable-unit packet build failed` |
| `GET /api/v0/investigations/deployable-unit/packet` | refusal packet build | `deployable-unit refusal packet build failed` |
| `GET /api/v0/investigations/drift/packet` | drift finding read | `drift packet readback failed` |
| `GET /api/v0/investigations/drift/packet` | packet build | `drift packet build failed` |
| `GET /api/v0/investigations/drift/packet` | refusal packet build | `drift refusal packet build failed` |
| `GET /api/v0/investigations/supply-chain/impact/packet` | packet build | `supply-chain impact packet build failed` |
| `GET /api/v0/investigations/supply-chain/impact/packet` | refusal packet build | `supply-chain impact refusal packet build failed` |

The packet build and bundle validation steps do not read a store. They
answer `500` with the fixed message, because a build error can quote packet
contents. The drift packet readback keeps its `internal_error` contract
envelope with the profile block.

## Admin audit reads

`GET /api/v0/auth/admin/audit/events` and `GET /api/v0/auth/admin/audit/summary`
read the governance audit store through the guarded PostgreSQL reader. A stale
or timed-out reader answers the retryable `503` with `Retry-After`, and the
envelope carries no `capability` field. Any other failure answers `500` with
`failed to list audit events` or `failed to summarize audit events`, and a
client cancel answers `499` with the same text and no error log line. No audit
reader answers `503` `admin audit reader is unavailable` with no
`Retry-After`.

## Related

- [Selector And Read Failures](selector-and-read-failures.md) for the shared
  client-cancel contract.
- [Admin Route Failures](admin-failures.md) for the same contract on the
  `/api/v0/admin/*` routes.
