# Graph-read safety

Eshu applies one 10-second budget to each logical NornicDB or Neo4j read made
through `Neo4jReader`. An earlier caller deadline wins. The reader passes the
remaining budget to the backend transaction, so collection and backend work
share the same clock. A typed retryable connectivity failure may open one fresh
session, but both attempts remain inside the original budget.

After execution, Eshu closes the driver session with a separate one-second
cleanup context. This lets the driver return connections to its pool even when
the read budget expired. A cleanup failure emits a sanitized
`query.graph_read.session_close_failed` warning; it does not expose driver
text, query text, or backend addresses.

The policy also covers graph reads performed during API and MCP startup,
including the cloud-resource owner-ledger backfill. Each backfill page uses the
same bounded reader rather than a raw driver session.

Route budgets still bound non-graph work. For example, the read-only Cypher
route has a 30-second outer budget, while its graph execution is limited by the
tighter 10-second reader budget. The reader deadline is a safety net; it does
not replace query-shape fixes.

## Caller responses

Every graph-backed HTTP route maps these sentinels onto the same stable status
and error envelope, so a bounded-availability failure is never reported as a
generic HTTP 500 — or, for a route whose graph read happens during repository
selector resolution, a misleading HTTP 400. This spans the read-only Cypher API
and its `execute_cypher_query` MCP tool, repository inventory/context/story, the
code search, relationships, call-graph, dead-code, flow, and quality routes,
entity and service resolution, the impact and change-surface family,
infrastructure and image reads, package-registry and service-catalog
correlations, the supply-chain evidence and security-alert-reconciliation
routes, the secrets-IAM grant-posture summary, and the service-story seam.
Their MCP tools therefore surface `backend_timeout` / `backend_unavailable`
rather than a generic transport failure:

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Reader budget expired | `504` | `backend_timeout` | `graph query exceeded its deadline` |
| Graph unavailable | `503` | `backend_unavailable` | `graph temporarily unavailable; retry after graph health is restored` |

Responses do not expose Bolt addresses, Cypher text, or raw driver errors.

Two known gaps, both tracked separately:

- `POST /api/v0/code/language-query` still returns HTTP 500 for a bounded
  graph-read failure. The envelope carries a `capability`, and that route has
  never been assigned one in the capability catalog. Nothing technically
  prevents emitting the envelope — the field is free-form and unvalidated — but
  putting an invented capability in front of operators is worse than the honest
  gap, so the route waits on a real capability assignment.
- `POST /api/v0/code/visualize` does follow the contract at runtime, but has no
  OpenAPI path entry at all — a gap that predates this contract — so it cannot
  advertise `503`/`504` until that entry exists.

Every route in `boundedGraphReadRoutes` follows the table above and advertises
both statuses in the OpenAPI spec. That list is the enforced set — read it as
"these routes are proven to map", not as a proof that no other graph-backed
route exists.

`TestOpenAPIDocumentsBoundedGraphReadFailuresOnEveryGuardedRoute` keeps the spec
from drifting away from that set, but note what it can and cannot do: it asserts
that every route **on its list** documents `503` and `504`. It cannot detect a
graph-backed route that was never added to the list, nor a handler whose
guard covers only some of its branches. Adding a graph-backed route therefore
means adding it to `boundedGraphReadRoutes` as well — derive that list from the
call graph rather than by inspection, since a guard often sits in a helper
several frames below the registered handler.

Routes that reach Postgres or the content store rather than the graph are
unaffected — their failures are not graph-read sentinels and keep their existing
status.

## Operator signals

Use `eshu_dp_neo4j_query_duration_seconds{operation="read"}` with its closed
`outcome` label: `success`, `slow`, `recovered`, `deadline`, `unavailable`,
`caller_deadline`, `canceled`, or `error`. `deadline` means the graph policy or
backend transaction deadline expired while the caller was still live.
`caller_deadline` means the enclosing request deadline expired first; it is not
counted as a graph-policy deadline.

The `neo4j.query` span records the same outcome plus
`eshu.graph_read.attempts` and
`eshu.graph_read.configured_deadline_ms`. Slow, deadline, and unavailable
reads also emit `query.graph_read.warning` with `pipeline_phase="query"`, a
bounded `failure_class`, and `duration_seconds`.

Session-close failures emit `query.graph_read.session_close_failed` with
`pipeline_phase="query"` and `failure_class="session_close_error"`. Because
cleanup has its own one-second bound, total request wall time may extend beyond
the graph-execution budget by up to that cleanup allowance.

Treat `slow` as completed work that remained inside the budget. Treat
`deadline` as exhausted graph-read work and investigate the query plan. Treat
`unavailable` as a health or connectivity event and inspect graph backend
health before retrying.
