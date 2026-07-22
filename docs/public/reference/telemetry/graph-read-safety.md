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

The read-only Cypher API and its `execute_cypher_query` MCP tool receive the
same stable error envelope. Repository inventory uses the same codes. Other
existing reader consumers still receive the sanitized sentinel message even
when their legacy route-specific HTTP status remains unchanged:

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Reader budget expired | `504` | `backend_timeout` | `graph query exceeded its deadline` |
| Graph unavailable | `503` | `backend_unavailable` | `graph temporarily unavailable; retry after graph health is restored` |

Responses do not expose Bolt addresses, Cypher text, or raw driver errors.

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
