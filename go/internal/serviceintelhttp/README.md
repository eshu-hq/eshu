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

- read a graph or content store for the service story directly (it reuses
  `query.EntityHandler` via the `BuildServiceStoryEnvelope` seam),
- re-derive or reclassify the service-story truth (the report's anchor truth is
  the service-story truth),
- compose the report itself (that is `serviceintel.Compose`), or
- run any LLM path.

It owns the durable evidence lanes for the report:
`DurableSupplyChainEvidenceSource` loads bounded reducer-owned supply-chain
impact inventory through `query.SupplyChainImpactAggregateStore`, and
`DurableIncidentEvidenceSource` resolves the workload's catalog service id
(`postgres.ServiceCatalogIDResolver`) and loads that service's incident routing
evidence (`postgres.ServiceIncidentEvidenceLoader`). Both sit behind seams so
the handler stays testable without a database.

## Import direction (no cycle)

`serviceintelhttp` imports `query`, `serviceintel`, `reducer`, and
`storage/postgres`. `serviceintel` imports `query`. `query` imports none of them.
The route is mounted by `cmd/api` and `cmd/mcp-server` (which already import
`query`), not by the `query` router, so `query` never depends on this package.

## Behaviour

- Resolution failures — capability gate (501), no repository access / missing
  service (404), ambiguous selector (409) — return the **same error envelope** as
  the service-story route (the seam returns the canonical `ErrorEnvelope` and
  status; the handler writes it with `query.WriteErrorEnvelope`).
- On success it returns the composed `serviceintel.Report` as a response
  envelope, with the report's anchor truth.
- The `incidents_support` section is sourced from durable incident-routing
  evidence and carries **incident-context** truth (`incident.context.read`), not
  the service-story platform truth. It is attributed only when the workload
  resolves to exactly one catalog service. An unresolved or ambiguous workload, a
  load error (logged via `serviceintel.incident_load_error` /
  `serviceintel.incident_ambiguous_catalog_service`, never report-fatal), or no
  routed incidents all leave the section `unsupported` with its fallback rather
  than fabricating a false "no incidents".
- The `supply_chain` section is sourced from reducer-owned supply-chain impact
  inventory and carries supply-chain-impact truth
  (`supply_chain.impact_findings.aggregate`), not the service-story platform
  truth. The read is scoped to the resolved workload id, uses the same precise
  impact profile as the inventory route default, and stays bounded. No inventory
  or a load error leaves the section `unsupported` with its fallback rather than
  fabricating an empty supported section.

No-Regression Evidence: the supply-chain source reads one bounded
`impact_status` inventory page through the existing aggregate read model, scoped
by resolved workload id and precise detection profile. `TestBuildReportInput*`
proves sourced, empty, load-error, and blank-workload report composition;
`TestDurableSupplyChainEvidenceSource*` proves the store filter, limit
(`SupplyChainImpactAggregateMaxLimit+1`), count rollup, nil-empty behavior, and
operator log on load failure; `TestDispatchServiceIntelligenceReportParity`
proves the MCP tool receives the sourced section through the same HTTP route.

No-Observability-Change: this adds no graph write, queue, worker, lease, metric,
span name, deployment knob, or provider call. The new read uses the existing
Postgres aggregate query path and request context; operators diagnose it through
the existing service intelligence request route metrics/spans, existing
supply-chain aggregate Postgres instrumentation, and the structured
`serviceintel.supply_chain_load_error` log.

## Verification

```bash
(cd go && go test ./internal/serviceintelhttp -count=1)
(cd go && go test ./internal/mcp -run ServiceIntelligenceReport -count=1)  # API/MCP parity
scripts/verify-package-docs.sh
```
