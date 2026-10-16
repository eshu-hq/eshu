# Infra And Inventory Route Failures

The infrastructure, relationship catalog, cloud resource, graph entity,
container image, admission decision, runtime drift, operator control plane,
and surface inventory routes answer a failed read with a fixed message for
that step, never with the backend error text (#7674). A backend error can
quote SQL, Cypher, relation names, hosts, and connection settings. It goes on
the span as an `exception` event, and the span status is Error with the fixed
message as its description. A route that opens its own handler span records
it there. The ecosystem overview, the relationship catalog and edge routes,
the operator control plane, and the surface inventory open no handler span, so
they record it on the request span, where `otelhttp` replaces the description
for a `5xx`.

## Statuses

- A graph backend outage answers the retryable `503` `backend_unavailable`
  with `Retry-After` on every route that reads the graph. A graph read that
  exceeds its deadline answers `504`.
- A stale or timed-out PostgreSQL reader answers the retryable `503` with
  `Retry-After` on the routes that read through the guarded reader: the cloud
  resource page selection, the admission decision reads, the runtime drift
  count and page reads, and the operator control plane status snapshot.
- The two infra aggregate routes read the PostgreSQL read model through the
  guarded reader beside the graph. A stale or timed-out reader on the read
  model readiness check answers the retryable `503`. A read model leg that
  fails during the read falls back to the graph, so there a stale reader
  surfaces only when the graph leg failed too.
- A `503` without `Retry-After` is a configuration state, for example no graph
  backend, no aggregate store, no admission decision read model, or no status
  reader on this API instance, or a grant-filtered tag-history cursor on a
  deployment with no cursor sealing key. Its text is fixed, and these routes
  write it without marking the span.
- Any other failure answers `500` with the fixed message in the table.
- A client that cancels its own request gets `499` with the same fixed
  message. The span status stays Unset and the span gets only the
  `eshu.request.client_canceled` event. A `context.Canceled` from an inner
  context while the request is still live is a server fault and answers
  `500`. `499` is not in the OpenAPI spec.
- The existing `400`, `404`, `422`, and `501` answers do not change.

The runtime drift readback keeps its `internal_error` contract envelope with
the profile block. The graph entity inventory also records the error of a
graph `503` or `504` verdict on its handler span, as it did before.

## Capability field

A graph or reader verdict `503` or `504` names the route's capability-matrix
key in `error.capability`. `GET /api/v0/status/operator-control-plane` has no
capability-matrix key yet, so its reader-verdict error envelope carries no
`capability` field. The status, `Retry-After`, code, and message are the same
as on a keyed route. #8227 registers its key. The status snapshot read on that
route runs under the same internal time bound as the status routes in
[Status And Evidence Route Failures](status-evidence-failures.md).

## Fixed messages

| Route | Failed step | Fixed message |
| --- | --- | --- |
| `POST /api/v0/infra/resources/search` | graph read | `infra resource search failed` |
| `POST /api/v0/infra/relationships` | graph read | `infra relationships read failed` |
| `GET /api/v0/ecosystem/overview` | graph counts | `ecosystem overview read failed` |
| `POST /api/v0/ecosystem/graph-summary` | ecosystem packet read (no `repo_id`) | `graph summary ecosystem read failed` |
| `POST /api/v0/ecosystem/graph-summary` | repository packet read | `graph summary repository read failed` |
| `GET /api/v0/infra/resources/count` | aggregate read | `infra resource count failed` |
| `GET /api/v0/infra/resources/inventory` | aggregate read | `infra resource inventory read failed` |
| `GET /api/v0/cloud/resources` | owner-ledger page selection | `cloud resource page selection failed` |
| `GET /api/v0/cloud/resources` | graph hydration read | `cloud resource graph hydration failed` |
| `GET /api/v0/cloud/resources` | ledger and graph parity check | `cloud resource graph projection is inconsistent` |
| `POST /api/v0/relationships/catalog` | graph counts | `relationships catalog read failed` |
| `POST /api/v0/relationships/edges` | graph edge slice | `relationship edges read failed` |
| `GET /api/v0/graph/entities` | kind count read | `graph entity kind count read failed` |
| `GET /api/v0/graph/entities` | kind count decode | `graph entity kind count decode failed` |
| `GET /api/v0/graph/entities` | entity list read (`kind` set) | `graph entity list read failed` |
| `GET /api/v0/images` | graph read | `container image list read failed` |
| `GET /api/v0/images/tag-history` | graph read (tag read or scoped lookup) | `container image tag history read failed` |
| `GET /api/v0/images/tag-history` | continuation seal | `query failed: the tag-history continuation could not be sealed` |
| `GET /api/v0/evidence/admission-decisions` | decision read | `admission decision read failed` |
| `GET /api/v0/evidence/admission-decisions` | evidence read (`include_evidence`) | `admission decision evidence read failed` |
| `POST /api/v0/cloud/runtime-drift/findings` | count or page read | `cloud runtime drift readback failed` |
| `GET /api/v0/status/operator-control-plane` | status snapshot read | `operator control plane status read failed` |
| `GET /api/v0/surface-inventory` | embedded inventory load | `surface inventory unavailable` |

The parity check, the count decode, the continuation seal, and the surface
inventory load read no store. They answer `500` with the fixed message. Their
error text is internal detail: the count decode quotes graph keys, and the
seal error describes the deployment's key state.

## Metrics

No instrument was added, removed, or renamed. The route counters keep their
outcome labels: a client cancel on the image list and tag-history routes
still counts under `query_error`, and on the cloud resource list under
`store_error` or `graph_error`, the same as a server fault on that step. The
`499` status and the `eshu.request.client_canceled` span event tell the two
apart.

## Related

- [Selector And Read Failures](selector-and-read-failures.md) for the shared
  client-cancel contract.
- [Status And Evidence Route Failures](status-evidence-failures.md) for the
  same contract on the status, evidence, and investigation packet routes.
