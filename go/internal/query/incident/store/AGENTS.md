# Incident Store — Agent Instructions

Scope: `go/internal/query/incident/store/` (package `store`).

## Ownership

This leaf owns the incident-context Postgres reads (#6060, lane B S2):
`context.go` (the store, the anchor selection, the timeline and change
reads), `review.go` / `routing.go` / `runtime.go` (the per-topic reads),
`review_evidence.go` / `routing_evidence.go` / `runtime_evidence.go` (the
pure evidence-edge assembly over decoded rows), `candidates.go` and
`commit.go` (routing-candidate and build-commit assembly), `decode.go` (the
incident row decoders), `factschema_decode_incident.go` (the
incident-specific factschema decode wrappers), `decode_workitem.go` (the
forked work-item decode substrate — see below), and `authorizer.go` (the
durable owning-repository authorizer).

- The service-catalog, CI/CD run, and container image sub-reads behind the
  runtime evidence arrive as injected ports (`WithCatalog`, `WithCICD`,
  `WithImages`). A nil port fails its read with a required-store error;
  never default one silently, and never construct the owning families'
  concretes here (their homes live outside this package).
- `decode_workitem.go` forks shared root decode helpers with per-symbol
  source citations. Do not extend the forks: when the work-item lane
  promotes that substrate to a shared home, this file adopts it and the
  forks go away.
- This package imports `incident/model`, `incident/sql`, `querycontract`,
  `supplychain` (image port only), and the factschema SDKs. It MUST NOT
  import the query root or `incident/`.
- `queryplan` manifests: the incident family has no entries. Keep it zero.

## Naming

`docs/internal/naming.md` is law: no `incident_` file prefixes, no
`store/store.go`, exported identifiers lose the family stutter. Files are
named by topic (`review.go`), with the `_evidence` suffix marking the pure
edge assembly beside each topic's reads. The root `incident_alias.go` keeps
every old exported spelling for staying callers.
