# prod-service-catalog-correlations — production validation

Validation-Slug: prod-service-catalog-correlations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: service_catalog.correlations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
