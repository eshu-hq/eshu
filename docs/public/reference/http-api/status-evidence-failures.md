# Status And Evidence Route Failures

The status read routes, the evidence routes, and the investigation packet
routes answer a failed read or build step with a fixed message for that step,
never with the backend error text (#7674). A backend error can quote SQL,
Cypher, relation names, hosts, and connection settings. It goes on the span as
an `exception` event, and the span status is Error with the fixed message as
its description. The evidence and investigation packet routes record this on
their own handler span. The status routes and the live evidence bundle open no
handler span, so they record it on the request span, where `otelhttp` replaces
the description for a `5xx`. The [Status And Admin](status-admin.md) page
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
- The status snapshot read runs under its own internal time bound. If the
  bound expires while the reader borrows a connection, runs its identity
  check, or waits for replay, the error is a reader verdict and answers the
  retryable `503` with `Retry-After`, as
  [HTTP API](../http-api.md) describes. If it expires after the snapshot
  transaction begins, the route answers its `500`.
- The existing `400`, `404`, and `501` answers do not change.

## Capability field

A reader-verdict `503` names the route's capability-matrix key in
`error.capability`, so an operator can tell which route a stale or timed-out
PostgreSQL reader fenced. The status, `Retry-After`, code, and message are the
same on every route. #8227 registered the keys below for the routes that had
none; the other routes on this page already carried theirs.

| Route | `error.capability` |
| --- | --- |
| `GET /api/v0/status/pipeline` | `operations.pipeline_status` |
| `GET /api/v0/status/collectors`, `GET /api/v0/collectors` | `operations.collector_status` |
| `GET /api/v0/status/ingesters/{ingester}`, `GET /api/v0/ingesters/{ingester}` | `operations.ingester_status` |
| `GET /api/v0/status/index`, `GET /api/v0/index-status` | `operations.index_status` (also on the scoped graph-count `503`) |
| `GET /api/v0/status/hosted-readiness` | `operations.hosted_readiness_status` |
| `GET /api/v0/status/freshness-causality` | `operations.freshness_causality_status` |
| `GET /api/v0/status/collector-readiness`, `GET /api/v0/collector-readiness` | `operations.collector_readiness_status` |
| `GET /api/v0/evidence/bundle` | `operations.evidence_bundle_live` |
| `GET /api/v0/auth/admin/audit/events` | `audit.events.list` |
| `GET /api/v0/auth/admin/audit/summary` | `audit.summary.read` |

Each key is `experimental` in the `production` profile until a deployed run
proves the route end to end.

## Index status for a scoped caller

`GET /api/v0/status/index` and `GET /api/v0/index-status` make one of two
reads, chosen by the caller:

- An unscoped caller reads the status snapshot through the guarded
  PostgreSQL reader. A stale or timed-out reader answers the retryable `503`
  with `Retry-After`. Any other failure answers `500`
  `index status read failed`. An unconfigured status reader answers `503`
  `status reader not configured` with no `Retry-After`.
- A scoped caller does not read the status snapshot. Its only read is the
  granted-repository count in the graph, and a caller with no grant makes no
  read. A graph outage answers the retryable `503` with `Retry-After`. A graph
  read that exceeds its deadline answers `504`. A count that fails any other
  way, or that returns no row, answers `500`
  `scoped index status repository count failed`. An unconfigured graph backend
  answers `503` `graph backend not configured` with no `Retry-After`.

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
envelope names `audit.events.list` or `audit.summary.read` in `capability`.
Any other failure answers `500` with
`failed to list audit events` or `failed to summarize audit events`, and a
client cancel answers `499` with the same text and no error log line. An
unconfigured audit reader answers `503` `admin audit reader is unavailable`
with no `Retry-After`.

## Related

- [Selector And Read Failures](selector-and-read-failures.md) for the shared
  client-cancel contract.
- [Admin Route Failures](admin-failures.md) for the same contract on the
  `/api/v0/admin/*` routes.
