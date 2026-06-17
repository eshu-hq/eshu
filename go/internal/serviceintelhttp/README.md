# serviceintelhttp

## Purpose

`serviceintelhttp` is the HTTP adapter that serves the **service intelligence
report** route:

```
GET /api/v0/services/{service_name}/intelligence-report
```

It is the one place that joins the report's two lower layers: the `query`
service-story seam (which builds the dossier and truth) and the pure
`serviceintel` composer (which arranges them into a report). It is also the route
the `get_service_intelligence_report` MCP tool dispatches to.

## Ownership boundary

This package owns the report HTTP route only. It does not:

- read a graph, content, or relational store directly (it reuses
  `query.EntityHandler` via the `BuildServiceStoryEnvelope` seam),
- re-derive or reclassify truth (the report truth is the service-story truth),
- compose the report itself (that is `serviceintel.Compose`), or
- run any LLM path.

## Import direction (no cycle)

`serviceintelhttp` imports `query` and `serviceintel`. `serviceintel` imports
`query`. `query` imports neither. The route is mounted by `cmd/api` (which
already imports `query`), not by the `query` router, so `query` never depends on
this package.

## Behaviour

- Resolution failures — capability gate (501), no repository access / missing
  service (404), ambiguous selector (409) — return the **same error envelope** as
  the service-story route (the seam returns the canonical `ErrorEnvelope` and
  status; the handler writes it with `query.WriteErrorEnvelope`).
- On success it returns the composed `serviceintel.Report` as a response
  envelope, with the report's anchor truth.
- The `supply_chain` and `incidents_support` sections are emitted `unsupported`
  with their fallback next calls until their evidence lanes gain a seam; the
  composer keeps them visible rather than hiding them.

## Verification

```bash
(cd go && go test ./internal/serviceintelhttp -count=1)
(cd go && go test ./internal/mcp -run ServiceIntelligenceReport -count=1)  # API/MCP parity
scripts/verify-package-docs.sh
```
