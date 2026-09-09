# Incident SQL

The Postgres query text behind the incident-context reads: the shared
active-generation fact-row projection and the bounded list queries per
topic (`context.go`, `review.go`, `routing.go`, `runtime.go`). Pure text —
no Go logic; the `incident/store/` reads compose these queries with their
filters.

## Move evidence

The text moved here verbatim from the query root's
`incident_context_*_sql.go` files; only the constant names lost their
`incident_` prefix. The authorizer's resolve query stayed with its reader
in `incident/store/authorizer.go`.

No-Regression Evidence: baseline `459b83f4c` vs this branch — the
normalized old-vs-new SQL diff over these consts is empty (same text, new
homes), and the SQL-shape tests plus the route-serves-data registry
evidence markers pin this text from the new paths.

No-Observability-Change: query text has no telemetry surface; see the
store README for the route-level signals, which are unchanged.
