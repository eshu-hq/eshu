# Incident HTTP Surface — Agent Instructions

Scope: `go/internal/query/incident/` (package `incident`, top-level files).

## Ownership

This leaf owns the incident-context HTTP surface (#6060, lane B S2):
`handler.go` (the `IncidentHandler` behind the incident-context route),
`scope.go` (the scoped-token authorization boundary over the durable
incident→repository edge), `answer.go` (the answer-packet companion), and
`capabilities.go` (the family's capability registration, which must live
with the routes so the leaf's own test binary observes the same profile
gates production does).

- The handler depends on the `IncidentContextStore` and
  `IncidentRepositoryAuthorizer` interfaces from `incident/model/`, never
  on `incident/store/` concretes. Wiring builds concretes through the root
  `NewPostgres*` forwarders.
- Spans start through `queryspan` with the package-local
  `incidentHandlerTracer` seam (see the `queryspan` package docs on why the
  tracer is package-local). The span name, route, and capability
  attributes are unchanged from the root handler.
- This package imports `incident/model`, `queryauth`, `querycontract`,
  `queryspan`, and `telemetry`. It MUST NOT import the query root or
  `incident/store/`.
- `queryplan` manifests: the incident family has no entries. Keep it zero.

## Naming

`docs/internal/naming.md` is law: no `incident_` file prefixes, no
`incident/incident.go`, exported identifiers lose the family stutter
(`IncidentHandler`, not `IncidentContextHandler`). The root
`incident_alias.go` keeps every old exported spelling for staying callers.
