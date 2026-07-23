# prod-service-catalog-correlations — production validation

Capability: `service_catalog.correlations.list` (tool
`list_service_catalog_correlations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: catalog_entity_repository_service_workload_or_owner_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer service catalog ownership and drift lookup anchored by scope,
entity, repository, service, workload, or owner.

## Committed reproducible evidence

**Bounded lookup, missing-evidence explanation, and local-descriptor evidence** —
`go/internal/query/service_catalog_correlations_test.go`:
`TestServiceCatalogListCorrelationsRequiresScopeAndLimit`,
`TestServiceCatalogListCorrelationsUsesBoundedStore`,
`TestServiceCatalogCorrelationsDecodeRequiredAnchorKeys`,
`TestServiceCatalogListCorrelationsReportsMissingEvidenceForRepositoryScope`,
`TestServiceCatalogListCorrelationsExplainsLocalOnlyDescriptorEvidence`, and
`TestServiceCatalogListCorrelationsBoundsLocalDescriptorEvidenceCount`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestServiceCatalogListCorrelations -count=1
cd go && go test ./internal/query -run TestServiceCatalogCorrelationsDecodeRequiredAnchorKeys -count=1
```

**Repository-scope and authorization boundaries** —
`go/internal/query/service_catalog_correlations_repository_scope_test.go` and
`go/internal/query/service_catalog_authz_test.go`. Reproduce:

```bash
cd go && go test ./internal/query -run 'TestServiceCatalog' -count=1
```

**Deployed-services target-story readback** —
`scripts/verify_remote_e2e_target_story.sh` (via
`scripts/lib/remote_e2e_service_catalog.sh`,
`target_story_check_service_catalog_correlations`) asserts
`service_catalog_correlations` counts and `evidence_summary` local/external
descriptor states against a live deployed stack over both the HTTP
`/service-catalog/correlations` route and the `list_service_catalog_correlations`
MCP tool. `scripts/test-verify-remote-e2e-target-story.sh` and
`scripts/test-verify-remote-e2e-target-story-canonical-ids.sh` are the script's
own local proofs, driven against the fixtures in
`scripts/lib/test-verify-remote-e2e-target-story-mcp-service-catalog.json` and
`scripts/lib/test-verify-remote-e2e-target-story-canonical-ids-mcp-service-catalog.json`
respectively, without live credentials. Reproduce the local proofs:

```bash
scripts/test-verify-remote-e2e-target-story.sh
scripts/test-verify-remote-e2e-target-story-canonical-ids.sh
```

## Notes

No private data: cited evidence covers repository/service/workload/owner
anchors and descriptor-state labels only.

Related: #5552 (burn-down).
