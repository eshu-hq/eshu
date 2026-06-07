# SBOM Attachment Scope Evidence

Repository, workload, and service scopes are supported SBOM/attestation
attachment read anchors when they are applied to reducer-owned attachment facts.
Reducer attachment facts expose subject and document identity, plus status,
artifact kind, repository, workload, service, and warning-preview fields. Source
anchors do not make parse-only rows canonical image evidence; callers still must
inspect `attachment_scope`, `canonical_writes`, and `missing_evidence`.

The list route uses repository/workload/service source anchors to return
matching attachment rows and missing-hop diagnostics. Count and inventory routes
apply the same anchors to their aggregate predicates and echo the applied scope,
so a scoped request with no matching evidence returns scoped zero instead of
falling back to global totals.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(SupplyChainListSBOMAttestationAttachmentsAcceptsRepositoryScope|SBOMAttestationAttachmentAggregateRoutesForwardSourceScopes|SBOMAttestationAttachmentAggregateQueriesFilterSourceScopes|SBOMAttestationAttachmentAggregateRoutesDoNotDropServiceScope|ResolveRouteForwardsSBOMRepositoryScopeToHTTPContract|DispatchSBOMAggregateRepositoryScopeReturnsScopedCount)' -count=1` proves repository-scoped SBOM attachment list, count, inventory, and MCP aggregate calls keep the source scope instead of dropping it and reading an unscoped count.

No-Observability-Change: the fix adds no new read model, graph query, Postgres
query, reducer lane, worker, queue, metric, span, or log contract. Valid SBOM
attachment reads still use `query.sbom_attestation_attachments`,
`query.sbom_attestation_attachment_aggregate`, the existing
`eshu_dp_postgres_query_duration_seconds` instrumentation, and the existing
limit/truncation fields.
