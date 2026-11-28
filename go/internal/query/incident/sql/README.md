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
