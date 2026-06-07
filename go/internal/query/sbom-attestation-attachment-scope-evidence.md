# SBOM Attachment Scope Evidence

Repository, workload, and service scopes are supported SBOM/attestation
attachment read anchors when they are applied to reducer-owned attachment facts.
The repository-first proof path is direct `repository_id` support, not a
separate resolver: API and MCP callers may pass a canonical source repository
id or the same human repository selectors used by repository context routes.
Reducer attachment facts expose subject and document identity, plus status,
artifact kind, repository, workload, service, and warning-preview fields. Source
anchors do not make parse-only rows canonical image evidence; callers still must
inspect `attachment_scope`, `canonical_writes`, and `missing_evidence`.

The list route resolves repository selectors before it uses
repository/workload/service source anchors to return matching attachment rows
and missing-hop diagnostics. Count and inventory routes apply the same resolved
anchors to their aggregate predicates and echo the applied scope, so a scoped
request with no matching evidence returns scoped zero instead of falling back to
global totals. Unknown or ambiguous repository selectors fail before the
attachment store runs.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(SupplyChainListSBOMAttestationAttachments(AcceptsRepositoryScope|ResolvesRepositorySelectors|RejectsInvalidRepositorySelectors|AcceptsWorkloadServiceAnchors|UsesBoundedStore)|SBOMAttestationAttachmentAggregateRoutes(ForwardSourceScopes|ResolveRepositorySelectors|RejectInvalidRepositorySelectors|DoNotDropServiceScope)|SBOMAttestationAttachmentAggregateQueriesFilterSourceScopes|ResolveRouteForwardsSBOMRepositoryScopeToHTTPContract|DispatchSBOMAggregateRepositoryScopeReturnsScopedCount)' -count=1` proves repo-only, service/workload-scoped, digest-scoped, ambiguous selector, missing-evidence, API aggregate, and MCP aggregate SBOM attachment readbacks stay scoped and bounded. `scripts/test-verify-remote-e2e-target-story.sh` proves the remote target-story checklist starts SBOM proof from `target_repository_id` and only adds subject digest as a narrowing predicate.

No-Observability-Change: the fix adds no new SBOM read model, graph query,
reducer lane, worker, queue, metric, span, or log contract. Human repository
selectors use the existing content-catalog lookup before the existing bounded
SBOM attachment read; canonical repository ids skip that lookup. Valid SBOM
attachment reads still use `query.sbom_attestation_attachments`,
`query.sbom_attestation_attachment_aggregate`, the existing
`eshu_dp_postgres_query_duration_seconds` instrumentation, repository selector
errors, and the existing limit/truncation fields.
