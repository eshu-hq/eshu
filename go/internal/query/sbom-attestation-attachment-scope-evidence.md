# SBOM Attachment Scope Evidence

Repository scope is not a supported SBOM/attestation attachment read anchor.
Reducer attachment facts expose subject and document identity, plus status and
artifact kind filters. They do not publish a canonical source repository field,
so accepting `repository_id` on list, count, or inventory routes would turn an
unscoped aggregate into false repository-specific evidence.

The remote target-story verifier already uses
`/supply-chain/sbom-attestations/attachments/count?subject_digest=<digest>` for
SBOM proof. Repository proof must come from repository-scoped routes such as
impact findings, service catalog correlations, CI/CD correlations, container
image identities, or security-alert reconciliations.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(SupplyChainListSBOMAttestationAttachmentsRejectsRepositoryScope|SBOMAttestationAttachmentAggregateRoutesRejectRepositoryScope|ResolveRouteForwardsSBOMRepositoryScopeToHTTPContract|DispatchSBOMAggregateRepositoryScopeReturnsHTTPContractError)' -count=1` proves repository-scoped SBOM attachment list, count, inventory, and MCP aggregate calls fail before any read-model call. The same tests prove MCP forwards `repository_id` to the HTTP handler instead of dropping it and reading an unscoped count.

No-Observability-Change: the fix adds no new read model, graph query, Postgres
query, reducer lane, worker, queue, metric, span, or log contract. Valid SBOM
attachment reads still use `query.sbom_attestation_attachments`,
`query.sbom_attestation_attachment_aggregate`, the existing
`eshu_dp_postgres_query_duration_seconds` instrumentation, and the existing
limit/truncation fields.
