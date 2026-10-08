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
incident-specific factschema decode wrappers), and `authorizer.go` (the
durable owning-repository authorizer). Work-item decoding comes from the
shared `internal/query/decode/workitem` leaf (#6623), imported as
`workitemdecode`.

- The service-catalog, CI/CD run, and container image sub-reads behind the
  runtime evidence arrive as injected ports (`WithCatalog`, `WithCICD`,
  `WithImages`). A nil port fails its read with a required-store error;
  never default one silently, and never construct the owning families'
  concretes here (their homes live outside this package).
- Do not re-fork the shared work-item decode substrate
  (`internal/query/decode/workitem`, imported as `workitemdecode`): the
  `#6623` move deleted this package's verbatim fork.
- This package imports `incident/model`, `incident/sql`, `querycontract`,
  `supplychain` (image port only), the narrow `storage/postgres/db` read port, `decode` (the classified
  decode-error type), the shared `decode/workitem` leaf, and the factschema SDKs. It MUST NOT
  import the query root or `incident/`.
- `queryplan` manifests: the incident family has no entries. Keep it zero.

## Naming

`docs/internal/naming.md` is law: no `incident_` file prefixes, no
`store/store.go`, exported identifiers lose the family stutter. Files are
named by topic (`review.go`), with the `_evidence` suffix marking the pure
edge assembly beside each topic's reads. The root `incident_alias.go` keeps
every old exported spelling for staying callers.

## Reader access

API/MCP business wiring uses `NewStoreWithReadStore` and
`NewPostgresIncidentRepositoryAuthorizerWithReadStore` with `db.Queryer`.
Every direct incident SQL read uses the selected reader; injected catalog,
CI/CD, and image ports must use the same guarded read boundary. A guard error
propagates; never retry it on the writer. Legacy constructors remain compatible.
