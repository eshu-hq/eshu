# Incident Store

The Postgres reads behind the incident-context response: anchor selection,
timeline and change candidates, routing, runtime, and review evidence, plus
the durable owning-repository authorizer.

Layout:

- `context.go` — `PostgresIncidentContextStore`, the bounded read, anchor
  selection, and the timeline and change reads.
- `review.go`, `routing.go`, `runtime.go` — the per-topic reads.
- `review_evidence.go`, `routing_evidence.go`, `runtime_evidence.go`,
  `candidates.go`, `commit.go` — the pure evidence-edge assembly over
  decoded rows.
- `decode.go`, `factschema_decode_incident.go` — the incident row decoders
  and the incident-specific factschema decode wrappers.
- `authorizer.go` — the durable owning-repository authorizer.
- Work-item decoding comes from the shared
  `internal/query/decode/workitem` leaf (#6623), imported as
  `workitemdecode`.

The response types live in `incident/model/`, the query text in
`incident/sql/`, the HTTP surface in `incident/`. The service-catalog,
CI/CD run, and container image sub-reads behind the runtime evidence arrive
as injected ports (`WithCatalog`, `WithCICD`, `WithImages`); the query
root's `NewPostgresIncidentContextStore` forwarder builds the production
concretes, and a nil port fails its read loudly instead of thinning the
evidence path silently.

## PostgreSQL read access

`NewStoreWithReadStore(db.Queryer)` and
`NewPostgresIncidentRepositoryAuthorizerWithReadStore(db.Queryer)` accept a
guarded business reader. Anchor, timeline, routing, review, and durable owning
repository reads use this port. Injected catalog, CI/CD, and image readers must
also use guarded ports. Freshness errors propagate without querying the writer.
Legacy constructors and all SQL, result shaping, tenant/repository scope gates,
limits, and stage telemetry remain unchanged.

## Move evidence

The family moved here verbatim from the query root (`incident_context_*`
sources plus the incident-owned `factschema_decode_incident.go` wrappers);
only the package clause, the `model` / `incidentsql` / `querycontract`
qualifications, the destuttered file and constant names, and the
constructor-to-ports change differ. The work-item decode substrate the
review reads need was forked verbatim into `decode_workitem.go` with source
citations (shared with the work-item lane, which owned it then); #6623
then deleted that fork and repointed this package at the shared
`internal/query/decode/workitem` leaf with no logic change.
