# Incident SQL — Agent Instructions

Scope: `go/internal/query/incident/sql/` (package `sql`).

## Ownership

This leaf owns the incident-context Postgres query text (#6060, lane B S2):
the shared active-generation fact-row projection (`context.go`) and the
bounded list queries per topic (`review.go`, `routing.go`, `runtime.go`).
The authorizer's resolve query stays with its reader in
`incident/store/authorizer.go` (it moved with that file, verbatim).

- Pure query text: this package imports nothing. Never add Go logic here;
  filter composition lives in `incident/store/`.
- A query-text change is a behavior change: the companion SQL-shape tests
  (in `incident/handler_test.go` and
  `incident/store/routing_evidence_test.go`) and the route-serves-data
  registry evidence markers pin this text. Update all three together.
- `queryplan` manifests: the incident family has no entries. Keep it zero.

## Naming

`docs/internal/naming.md` is law: no `incident_` file prefixes, no
`sql/sql.go`. Files are named by topic; constants are exported without the
family stutter (`ListIncidentsQuery`, not
`listIncidentContextIncidentsQuery`).
