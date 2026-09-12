# Evidence OpenAPI Fragments

The OpenAPI 3.0 path documentation for Eshu's evidence-surface reads: the
evidence bundle and citation reads, the documentation facts/findings reads,
the incident-context read, the investigation workflows and investigation
packets, the visualization-derive read, and the evidence work-item count.

Layout:

- `routes.go` — `Routes`: `GET /api/v0/documentation/facts`,
  `GET /api/v0/documentation/findings`,
  `GET /api/v0/documentation/findings/{finding_id}/evidence-packet`,
  `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness`,
  `GET /api/v0/evidence/admission-decisions`,
  `GET /api/v0/evidence/citations`, and
  `GET /api/v0/evidence/relationships/{resolved_id}`.
- `documentation_finding_aggregate.go` — `DocumentationFindingAggregate`:
  `GET /api/v0/documentation/findings/count` and
  `GET /api/v0/documentation/findings/inventory`.
- `bundle.go` — `Bundle`: `POST /api/v0/evidence/bundle`.
- `incident_context.go` — `IncidentContext`: the doc for
  `GET /api/v0/incidents/{incident_id}/context` (the handler lives in
  `query/incident`).
- `investigation_workflows.go` — `InvestigationWorkflows`:
  `GET /api/v0/investigation-workflows` and
  `POST /api/v0/investigation-workflows/resolve`.
- `investigations.go` — `Investigations`: the deployable-unit, drift,
  services, and supply-chain-impact investigation packet reads.
- `visualization_packets.go` — `VisualizationPackets`:
  `POST /api/v0/visualizations/derive`.
- `work_item.go` — `WorkItem`: `GET /api/v0/work-items/evidence`.

## Why its own package

`routes.go` was already the largest fragment file in the family before the
move (493 of the 500-line cap); splitting each additional evidence surface
into its own file, in its own package, keeps every file well under the cap
and lets an agent touch one evidence route without re-reading the rest.

## Adding a fragment

New evidence route documentation gets its own file here exporting one JSON
string constant, following the existing files' shape. Register the new
constant in `openapi/spec.go`'s concatenation and add the route to
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR. A fragment left out of `spec.go` never reaches the published
spec even though the route itself still works — the spec silently
under-documents a real route.

## Move evidence

This tree is destination-only for the #6642 path split: these eight
constants and their route sets were confirmed against the pre-move
`openapi_paths_*.go` files (`rg -o '"/api/v0[^"]*"'`) with no route added,
removed, or reworded.
