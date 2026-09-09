# Incident Read Model — Agent Instructions

Scope: `go/internal/query/incident/model/` (package `model`).

## Ownership

This leaf owns the incident-context read-model contract (#6060, lane B S2):
the `IncidentContext*` row, filter, snapshot, and response types, the
`IncidentTruth*` / `IncidentSlot*` vocabularies, the `Capability` /
`DefaultLimit` / `MaxLimit` bounds, and the `BuildIncidentContextResponse` assembly that
applies the public contract to a store snapshot. The shared
`MissingEdge` constructor lives here because the response assembly and the
`incident/store` routing builder must never disagree on what "missing" means
for a slot.

- Do not add routes, SQL, or Postgres reads here. Reads live in
  `incident/store/`, query text in `incident/sql/`, the HTTP surface in
  `incident/`.
- This package imports `querycontract` only. It MUST NOT import the query
  root, `incident`, or `incident/store` (cycle through `incident_alias.go`).
- `queryplan` manifests: the incident family has no entries. Keep it zero.

## Naming

`docs/internal/naming.md` is law: no `incident_` file prefixes, no
`model/model.go`, exported identifiers lose the family stutter (`Capability`,
not `IncidentContextCapability`; `MissingEdge`, not
`MissingIncidentContextEdge`). The root `incident_alias.go` keeps every old
exported spelling for staying callers.
