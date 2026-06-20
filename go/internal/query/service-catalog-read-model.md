# Service Catalog Read Model

`ServiceCatalogHandler` reads reducer-owned
`reducer_service_catalog_correlation` facts from Postgres. Repository-scoped
requests first resolve human repository selectors through the shared repository
catalog resolver, then match reducer rows by either admitted `repository_id` or
ambiguous `candidate_repository_ids`.

Direct service evidence is returned as `service_id`. Workload-only evidence is
returned as `workload_id` without fabricating a service identifier. Ambiguous
rows expose `candidate_repository_ids[]`, and ambiguous, unresolved, stale, or
rejected rows can expose `required_anchor_keys[]` so API and MCP callers can see
which exact proof anchors would promote the declaration. Empty anchored pages
carry `missing_evidence[]` classes such as
`repository_service_catalog_correlation` so zero rows means the scoped
correlation evidence is missing rather than hidden by an unsupported selector
shape.

No-Regression Evidence: `go test ./internal/query ./internal/mcp ./internal/storage/postgres -run 'Test(ServiceCatalogListCorrelationsExplainsRepositoryScopedEvidence|ServiceCatalogListCorrelationsReportsMissingEvidenceForRepositoryScope|ServiceCatalogCorrelationsDecodeRequiredAnchorKeys|PostgresServiceCatalogCorrelationsResolveCandidateRepositoryIDs|ServiceCatalogCorrelationQueryUsesActiveFactReadModel|OpenAPISpecIncludesServiceCatalogCorrelations|ResolveRouteMapsServiceCatalogCorrelationsToBoundedQuery|ServiceCatalogToolSchemaAdvertisesRepositorySelectors|BootstrapDefinitionsIncludeServiceCatalogCorrelationFactIndexes)' -count=1` proves direct service rows, workload-only rows, explicit missing-evidence classes, ambiguous candidate repository and required-anchor readback, API/OpenAPI shape, MCP route/schema agreement, and the Postgres candidate repository index.

No-Observability-Change: the change stays inside the existing `query.service_catalog_correlations` HTTP/MCP handler span and the Postgres read-model query path. It adds no graph write, queue, worker, reducer domain, runtime knob, metric instrument, or metric label; operators still diagnose the route through the existing query span, HTTP status/error body, truth envelope, count/limit/truncated fields, and Postgres query duration instrumentation.
