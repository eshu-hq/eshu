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
- `decode_workitem.go` — the forked work-item decode substrate (cited
  per symbol; adopted from the shared home once the work-item lane lands
  it).
- `authorizer.go` — the durable owning-repository authorizer.

The response types live in `incident/model/`, the query text in
`incident/sql/`, the HTTP surface in `incident/`. The service-catalog,
CI/CD run, and container image sub-reads behind the runtime evidence arrive
as injected ports (`WithCatalog`, `WithCICD`, `WithImages`); the query
root's `NewPostgresIncidentContextStore` forwarder builds the production
concretes, and a nil port fails its read loudly instead of thinning the
evidence path silently.

## Move evidence

The family moved here verbatim from the query root (`incident_context_*`
sources plus the incident-owned `factschema_decode_incident.go` wrappers);
only the package clause, the `model` / `incidentsql` / `querycontract`
qualifications, the destuttered file and constant names, and the
constructor-to-ports change differ. The work-item decode substrate the
review reads need is forked verbatim into `decode_workitem.go` with source
citations (shared with the work-item lane, which still owns it).

No-Regression Evidence: baseline `459b83f4c` vs this branch —
`go test ./internal/query/...` passes with 0 failures (counts in the lane
handoff); the store behavior is pinned by the companion tests
(`context_test.go` with the fake-driver harness, `anchor_selection_test.go`,
`truncation_test.go`, `authorizer_test.go`, the `*_evidence_test.go` files),
all moved with their subjects. The normalized old-vs-new SQL diff over the
store query consts is empty (same text, new homes); the route-serves-data
registry entry for the incident route points at the new files with
unchanged evidence markers, and its gate passes.

No-Observability-Change: this package emits no metric or span of its own;
the only log writes are the pre-existing decode-drop debug logs, unchanged.
The incident route keeps `eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` via the unchanged query-surface
middleware, and the handler span name is unchanged.
