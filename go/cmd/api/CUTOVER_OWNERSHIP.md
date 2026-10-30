<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# eshu-api cutover: route/mutation-to-contract ownership map

Refs #8872, part of #6707. Status: preparation only — no cutover has
occurred, and no in-tree implementation moves in this artifact.

## What this is

One row per live HTTP route served by the `eshu` API binary: the handler
family that owns it, the OpenAPI contract fragment that publishes its
request/response shape, whether the route reads or mutates durable state,
which core-owned persistence backs a mutation, and which entrypoint mounts
it (`newRouter` vs `wireAPI`).

## Method

- Route census: the generated surface inventory (`api_route` surfaces), which
  is itself drift-gated against the served OpenAPI spec.
- Family attribution: every `Mount`/`*Routes` method on every handler plus
  the two `wireAPI`-direct mounts (`serviceintelhttp.ReportHandler`,
  `ask.Handler` via `mountAskAndNarration`); constant-built patterns
  resolved to their literals.
- Lock: `go/cmd/api/cutover_ownership_test.go` fails on any live route
  without a row, any row without a live route, any missing contract
  fragment, and any `newRouter` row that does not resolve on a fully-wired
  test router (OPTIONS probe: mounted paths answer 405, unmounted 404).

## Reading the table

- `contract` is relative to `go/internal/query/openapi/`. `<mux-health>`
  and `<doc-ui>` have no spec entry by design (liveness probe; spec
  browsers).
- `channel=mutation` means the handler persists durable state through a
  core-owned store. Query-shaped POSTs (search, investigate, resolve,
  query) are `read`: they compute over reader-backed stores and write
  nothing.
- `wireAPI` rows are composed after `newRouter` returns, once a live
  keyring or provider resolver exists. The OPTIONS probe excludes them
  (exclusion count pinned by `cutoverWireAPIProbeExclusions`); their mount
  attribution is code-inspected per Method above. Per-family mount proof
  will be cited in the destination plan's proof column when that plan
  lands — no such plan or column exists yet.
- The `/api/v1/` alias rewrite and the `/api/v0/` deprecation headers are
  applied outside `Mount` (`v1PrefixAliasMiddleware`,
  `deprecationHeadersMiddleware`); covered by `v1_alias_test.go` and
  `deprecation_headers_test.go`.
- `/.well-known/oauth-protected-resource*` is MCP-binary-only
  (`cmd/mcp-server`); it is not in the API spec and belongs to the #8873
  lane, not this map.

## Ownership summary

All `read` families are backed by reader ports only (`db.ReadStore`,
graph `GraphQuery`, `ContentStore`) and carry no durable writes. They move
to `eshu-api` as consumers of versioned core read contracts.

Every `mutation` row persists through core-owned admission storage that
stays with the core. `eshu-api` keeps the HTTP shape and calls a versioned
core admission contract; it never takes direct table or queue handles. The
in-tree seam exists for the single bounded suppression mutation:
`query.SuppressionAdmission` at `SuppressionAdmissionContractVersion`, bound
by `newSupplyChainHandler` to the core-owned Postgres store; reads consume
the `querycontract.CoreReadContractsVersion` port set.

| Mutation family | Core-owned persistence | eshu-api disposition |
| --- | --- | --- |
| Admin | Postgres recovery/reindex/dead-letter/queue stores | HTTP shape moves; recovery queue writes via admission contract |
| AdminRetirement | retirement marker/fencer/purger stores | trigger via admission contract |
| AdminTerminalFailure | terminal-failure ack store + replay ledger | ack via admission contract |
| AdminIdentityMutations | identity stores (roles, IdP mappings) | via admission contract |
| AdminProviderConfigMutations | provider-config stores | via admission contract |
| SignInPolicyMutations | sign-in-policy store | via admission contract |
| LocalIdentity | identity/session/token stores | session issuance via admission contract |
| BrowserSessions | browser-session store | session lifecycle via admission contract |
| Setup | bootstrap stores | first-run flow via admission contract |
| OIDCLogin | login-state/session stores | login flow via admission contract |
| SAML | login-state/session stores | login flow via admission contract |
| GitHubLogin | login-state/session stores | login flow via admission contract |
| SupplyChain (suppressions row only) | VulnerabilitySuppressionMutationStore | single bounded policy mutation via admission contract |

## Route table

| route | family | contract | channel | persistence | mounted-by |
| --- | --- | --- | --- | --- | --- |
| `DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}` | AdminIdentityMutations | paths/auth/admin_mutations.go | mutation | writer: identity stores (roles, IdP mappings) | newRouter |
| `DELETE /api/v0/auth/browser-session` | BrowserSessions | paths/auth/routes.go | mutation | writer: browser-session store | newRouter |
| `GET /api/v0/admin/delta-union/completeness` | Admin | paths/status/delta_union_completeness.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `GET /api/v0/admin/reindex/progress` | Admin | paths/status/reindex_progress.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `GET /api/v0/admin/shared-projection/tuning-report` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `GET /api/v0/auth/admin/api-tokens` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/audit/events` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/audit/summary` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/idp-group-mappings` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/idp-providers` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/provider-configs` | AdminProviderConfigReads | paths/auth/admin_provider_configs.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}` | AdminProviderConfigReads | paths/auth/admin_provider_configs.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions` | AdminProviderConfigReads | paths/auth/admin_provider_configs.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/auth/admin/role-assignments` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/roles` | AdminIdentityReads | paths/auth/admin_reads.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/admin/sign-in-policy` | SignInPolicyReads | paths/auth/sign_in_policy.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/auth/browser-session` | BrowserSessions | paths/auth/routes.go | read | writer: browser-session store | newRouter |
| `GET /api/v0/auth/github/callback` | GitHubLogin | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/github/login` | GitHubLogin | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/local/api-tokens` | LocalIdentity | paths/auth/tokens.go | read | writer: identity/session/token stores | newRouter |
| `GET /api/v0/auth/local/invitations` | AdminIdentityReads | paths/auth/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/oidc/callback` | OIDCLogin | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/oidc/login` | OIDCLogin | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/profile` | Profile | paths/auth/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/providers` | AuthProviders | paths/auth/routes.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/auth/saml/providers/{provider_id}/login` | SAML | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/saml/providers/{provider_id}/metadata` | SAML | paths/auth/routes.go | read | writer: login-state/session stores | wireAPI |
| `GET /api/v0/auth/sessions` | SessionList | paths/auth/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/auth/setup-state` | Setup | paths/auth/setup.go | read | writer: bootstrap stores | wireAPI |
| `GET /api/v0/auth/sign-in-policy` | SignInPolicyReads | paths/auth/sign_in_policy.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/capabilities` | Capabilities | paths/catalog/capabilities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/catalog` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/ci-cd/run-correlations` | CICD | paths/cicd/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/ci-cd/run-correlations/count` | CICD | paths/cicd/run_correlation_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/ci-cd/run-correlations/inventory` | CICD | paths/cicd/run_correlation_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/cloud/inventory` | CloudInventory | paths/cloud/inventory.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/cloud/ssm-host-inventory` | SSMHostInventory | paths/cloud/ssm_host_inventory.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/cloud/resources` | Infra | paths/cloud/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/codeowners/ownership` | CodeownersOwnership | paths/code/owners.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/collector-extraction-readiness` | ExtractionReadiness | paths/status/collector_extraction_readiness.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/collector-extraction-readiness/{family}` | ExtractionReadiness | paths/status/collector_extraction_readiness.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/collector-readiness` | Status | paths/status/collector_readiness.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/collectors` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/component-extensions` | ComponentExtensions | paths/catalog/component_extensions.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/component-extensions/{component_id}/diagnostics` | ComponentExtensions | paths/catalog/component_extensions.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/dependencies` | Dependencies | paths/repository/dependencies.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` | Documentation | paths/evidence/documentation.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/facts` | Documentation | paths/evidence/documentation.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/findings` | Documentation | paths/evidence/documentation.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/findings/count` | Documentation | paths/evidence/documentation_finding_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/findings/inventory` | Documentation | paths/evidence/documentation_finding_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/documentation/findings/{finding_id}/evidence-packet` | Documentation | paths/evidence/documentation.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/ecosystem/overview` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/entities/{entity_id}/context` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/evidence/admission-decisions` | Evidence | paths/evidence/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/evidence/bundle` | Evidence | paths/evidence/bundle.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/evidence/relationships/{resolved_id}` | Evidence | paths/evidence/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/fact-schema-versions` | FactSchemaVersions | paths/status/fact_schema_version.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/fact-schema-versions/{fact_kind}` | FactSchemaVersions | paths/status/fact_schema_version.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/freshness/changed-since` | Freshness | paths/freshness/changed_since.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/freshness/generations` | Freshness | paths/freshness/generations.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/freshness/services/changed-since` | Freshness | paths/freshness/service_changed_since.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/graph/entities` | GraphEntityInventory | paths/search/graph_entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/iac/resources` | IaC | paths/iac/resources.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/images` | Images | paths/supply/chain/images.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/images/tag-history` | TagHistory | paths/supply/chain/tag_history.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/incidents/{incident_id}/context` | Incident | paths/evidence/incident_context.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/index-status` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/infra/resources/count` | Infra | paths/infrastructure/resource_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/infra/resources/inventory` | Infra | paths/infrastructure/resource_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/ingesters` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/ingesters/{ingester}` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/investigation-workflows` | InvestigationWorkflows | paths/evidence/investigation_workflows.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/investigations/deployable-unit/packet` | Evidence | paths/evidence/investigations.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/investigations/drift/packet` | CloudRuntimeDrift | paths/evidence/investigations.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/investigations/services/{service_name}` | Entities | paths/evidence/investigations.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/investigations/supply-chain/impact/packet` | SupplyChain | paths/evidence/investigations.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/kubernetes/correlations` | Kubernetes | paths/infrastructure/kubernetes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/metrics/timeseries` | Metrics | paths/status/metrics.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/observability/coverage/correlations` | ObservabilityCoverage | paths/infrastructure/observability_coverage.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/openapi.json` | APIRouter | paths/status/routes.go | read | n/a (mux-level) | newRouter |
| `GET /api/v0/package-registry/correlations` | PackageRegistry | paths/repository/package_registry.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/dependencies` | PackageRegistry | paths/repository/package_registry.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/dependency-chains` | PackageRegistry | paths/repository/package_registry.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/packages` | PackageRegistry | paths/repository/package_registry.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/packages/count` | PackageRegistry | paths/repository/package_registry_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/packages/inventory` | PackageRegistry | paths/repository/package_registry_aggregate.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/package-registry/versions` | PackageRegistry | paths/repository/package_registry.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/query-playbooks` | Playbooks | paths/catalog/playbooks.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/replatforming/selectors` | IaC | paths/iac/replatforming_selectors.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/by-language` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/language-inventory` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/branches` | Repositories | paths/repository/branches.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/content` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/context` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/coverage` | Repositories | paths/repository/stats_and_coverage.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/freshness` | Repositories | paths/repository/freshness.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/stats` | Repositories | paths/repository/stats_and_coverage.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/story` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/repositories/{repo_id}/tree` | Repositories | paths/repository/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/secrets-iam/identity-trust-chains` | SecretsIAM | paths/infrastructure/secrets_iam.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/secrets-iam/posture-gaps` | SecretsIAM | paths/infrastructure/secrets_iam.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/secrets-iam/posture-summary` | SecretsIAM | paths/infrastructure/secrets_iam.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/secrets-iam/privilege-posture-observations` | SecretsIAM | paths/infrastructure/secrets_iam.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/secrets-iam/secret-access-paths` | SecretsIAM | paths/infrastructure/secrets_iam.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/semantic/code-hints` | SemanticEvidence | paths/search/semantic_evidence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/semantic/documentation-observations` | SemanticEvidence | paths/search/semantic_evidence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/service-catalog/correlations` | ServiceCatalog | paths/service/catalog.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/services/{service_name}/context` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/services/{service_name}/intelligence-report` | ServiceIntelHTTP | paths/service/intelligence_report.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `GET /api/v0/services/{service_name}/story` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/status/answer-narration` | Status | paths/status/answer_narration.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/collector-readiness` | Status | paths/status/collector_readiness.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/collectors` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/freshness-causality` | Status | paths/freshness/causality.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/governance` | Status | paths/status/governance.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/hosted-readiness` | Status | paths/status/hosted_readiness.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/index` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/ingesters` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/ingesters/{ingester}` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/operations` | Status | paths/status/operations.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/operator-control-plane` | Status | paths/status/operator_control_plane.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/pipeline` | Status | paths/status/routes.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/status/semantic-extraction` | Status | paths/status/semantic.go | read | reader (StatusReader/ReadStore; writer handle held for governance audit reads) | newRouter |
| `GET /api/v0/supply-chain/advisories` | SupplyChain | paths/supply/chain/advisory_catalog.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/advisories/evidence` | SupplyChain | paths/supply/chain/advisory_evidence.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/container-images/identities` | SupplyChain | paths/supply/chain/container_images.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/container-images/identities/count` | SupplyChain | paths/supply/chain/container_image_identity_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/container-images/identities/inventory` | SupplyChain | paths/supply/chain/container_image_identity_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/impact/explain` | SupplyChain | paths/supply/chain/impact_explain.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/impact/findings` | SupplyChain | paths/supply/chain/impact_findings.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/impact/findings/count` | SupplyChain | paths/supply/chain/impact_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/impact/inventory` | SupplyChain | paths/supply/chain/impact_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/sbom-attestations/attachments` | SupplyChain | paths/supply/chain/sbom_attestations.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/sbom-attestations/attachments/count` | SupplyChain | paths/supply/chain/sbom_attestation_attachment_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/sbom-attestations/attachments/inventory` | SupplyChain | paths/supply/chain/sbom_attestation_attachment_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/security-alerts/reconciliations` | SupplyChain | paths/supply/chain/security_alerts.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/security-alerts/reconciliations/count` | SupplyChain | paths/supply/chain/security_alert_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/security-alerts/reconciliations/inventory` | SupplyChain | paths/supply/chain/security_alert_aggregate.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` | SupplyChain | paths/supply/chain/advisory_evidence.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/supply-chain/vulnerability-scanner/contract` | SupplyChain | paths/supply/chain/vulnerability_scanner_contract.go | read | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `GET /api/v0/surface-inventory` | SurfaceInventory | paths/catalog/surface_inventory.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/work-items/evidence` | WorkItems | paths/evidence/work_item.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/workloads/{workload_id}/context` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/workloads/{workload_id}/story` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /health` | APIRouter | <mux-health> | read | n/a (mux-level) | newRouter |
| `PATCH /api/v0/auth/admin/sign-in-policy` | SignInPolicyMutations | paths/auth/sign_in_policy.go | mutation | writer: sign-in-policy store | wireAPI |
| `PATCH /api/v0/auth/browser-session/context` | BrowserSessions | paths/auth/routes.go | mutation | writer: browser-session store | newRouter |
| `POST /api/v0/admin/backfill` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/changed-since/poisoned-links/query` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/collector-failures/acknowledge` | AdminTerminalFailure | paths/status/collector_terminal_failure_acknowledgement.go | mutation | writer: terminal-failure ack store + replay ledger | newRouter |
| `POST /api/v0/admin/collector-failures/acknowledgements/query` | AdminTerminalFailure | paths/status/collector_terminal_failure_acknowledgement.go | read | writer: terminal-failure ack store + replay ledger | newRouter |
| `POST /api/v0/admin/collector-failures/query` | AdminTerminalFailure | paths/status/collector_terminal_failure_acknowledgement.go | read | writer: terminal-failure ack store + replay ledger | newRouter |
| `POST /api/v0/admin/dead-letter` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/dead-letters/query` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/decisions/query` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/input-invalid-facts/query` | AdminInputInvalidFacts | paths/status/admin.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/admin/recover-generations` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/refinalize` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/reindex` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/reopen` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/replay` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/replay-events/query` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/repository-retirements` | AdminRetirement | paths/status/repository_retirement.go | mutation | writer: retirement marker/fencer/purger stores | newRouter |
| `POST /api/v0/admin/skip` | Admin | paths/status/admin.go | mutation | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/admin/work-items/query` | Admin | paths/status/admin.go | read | writer: Postgres recovery/reindex/dead-letter/queue stores | newRouter |
| `POST /api/v0/answer-quality/score` | CLISupport | paths/cli/score.go | read | none (pure function of the request body; no store reads or writes) | wireAPI |
| `POST /api/v0/ask` | Ask | paths/search/ask.go | read | reader (ReadStore/graph/content, no durable writes) | wireAPI |
| `POST /api/v0/auth/admin/idp-group-mappings` | AdminIdentityMutations | paths/auth/admin_reads.go | mutation | writer: identity stores (roles, IdP mappings) | newRouter |
| `POST /api/v0/auth/admin/provider-configs` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection` | AdminProviderConfigMutations | paths/auth/admin_provider_configs.go | mutation | writer: provider-config stores | wireAPI |
| `POST /api/v0/auth/admin/role-assignments` | AdminIdentityMutations | paths/auth/admin_reads.go | mutation | writer: identity stores (roles, IdP mappings) | newRouter |
| `POST /api/v0/auth/admin/role-assignments/revoke` | AdminIdentityMutations | paths/auth/admin_mutations.go | mutation | writer: identity stores (roles, IdP mappings) | newRouter |
| `POST /api/v0/auth/browser-session` | BrowserSessions | paths/auth/routes.go | mutation | writer: browser-session store | newRouter |
| `POST /api/v0/auth/local/api-tokens` | LocalIdentity | paths/auth/tokens.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/api-tokens/{token_id}/revoke` | LocalIdentity | paths/auth/tokens.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/api-tokens/{token_id}/rotate` | LocalIdentity | paths/auth/tokens.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/bootstrap` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/break-glass` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/break-glass/session` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/invitations` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/invitations/accept` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/invitations/{invite_id}/revoke` | AdminIdentityMutations | paths/auth/admin_mutations.go | mutation | writer: identity stores (roles, IdP mappings) | newRouter |
| `POST /api/v0/auth/local/login` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/mfa/totp/begin` | LocalIdentity | paths/auth/totp.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/mfa/totp/confirm` | LocalIdentity | paths/auth/totp.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/password/rotate` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/users/{user_id}/disable` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/users/{user_id}/mfa-reset` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/local/users/{user_id}/password` | LocalIdentity | paths/auth/routes.go | mutation | writer: identity/session/token stores | newRouter |
| `POST /api/v0/auth/saml/providers/{provider_id}/acs` | SAML | paths/auth/routes.go | mutation | writer: login-state/session stores | wireAPI |
| `POST /api/v0/auth/setup/admin` | Setup | paths/auth/setup.go | mutation | writer: bootstrap stores | wireAPI |
| `POST /api/v0/auth/setup/claim` | Setup | paths/auth/setup.go | mutation | writer: bootstrap stores | wireAPI |
| `POST /api/v0/auth/setup/mfa` | Setup | paths/auth/setup.go | mutation | writer: bootstrap stores | wireAPI |
| `POST /api/v0/aws/runtime-drift/findings` | IaC | paths/cloud/aws_runtime_drift.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/cloud/runtime-drift/findings` | CloudRuntimeDrift | paths/cloud/runtime_drift.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/bundles` | Code | paths/code/graph.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/call-chain` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/call-chain/compare` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/call-graph/metrics` | Code | paths/code/symbols.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/complexity` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/cypher` | Code | paths/code/graph.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/dead-code` | Code | paths/code/dead/scan.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/dead-code/cross-repo` | Code | paths/code/dead/cross_repo.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/dead-code/investigate` | Code | paths/code/dead/investigation.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/divergence/findings` | Code | paths/code/divergence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/divergence/investigate` | Code | paths/code/divergence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/divergence/report` | Code | paths/code/divergence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/flow/cfg-summary` | Code | paths/code/flow.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/flow/pdg-summary` | Code | paths/code/flow.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/flow/reaching-def` | Code | paths/code/flow.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/flow/taint-path` | Code | paths/code/flow.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/imports/investigate` | Code | paths/code/symbols.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/language-query` | Language | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/quality/inspect` | Code | paths/code/quality.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/relationships` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/relationships/story` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/routes/callers` | Code | paths/code/route_to_caller.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/search` | Code | paths/code/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/security/secrets/investigate` | Code | paths/code/security.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/structure/inventory` | Code | paths/code/symbols.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/symbols/search` | Code | paths/code/symbols.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/topics/investigate` | Code | paths/code/symbols.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/code/visualize` | Code | paths/code/graph.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/compare/environments` | Compare | paths/status/compare.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/dockerfile-installs/search` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/php-config/search` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/entities/read` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/entities/search` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/files/lines` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/files/read` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/content/files/search` | Content | paths/search/content.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/ecosystem/graph-summary` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/entities/resolve` | Entities | paths/search/entities.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/evidence/citations` | Evidence | paths/evidence/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/iac/dead` | IaC | paths/iac/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/iac/management-status` | IaC | paths/iac/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/iac/management-status/explain` | IaC | paths/iac/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/iac/terraform-import-plan/candidates` | IaC | paths/iac/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/iac/unmanaged-resources` | IaC | paths/iac/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/blast-radius` | Impact | paths/impact/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/change-surface` | Impact | paths/impact/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/change-surface/investigate` | Impact | paths/impact/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/contracts` | Impact | paths/impact/contract.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/deployment-config-influence` | Impact | paths/impact/deployment_config_influence.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/developer-change-plan` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/entity-map` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/explain-dependency-path` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/pre-change` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/resource-investigation` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/trace-deployment-chain` | Impact | paths/impact/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/trace-exposure-path` | Impact | paths/impact/exposure.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/impact/trace-resource-to-code` | Impact | paths/impact/rest.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/infra/relationships` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/infra/resources/search` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/investigation-workflows/resolve` | InvestigationWorkflows | paths/evidence/investigation_workflows.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/investigation/packet/render` | CLISupport | paths/cli/render.go | read | none (pure function of the request body; no store reads or writes) | wireAPI |
| `POST /api/v0/query-playbooks/resolve` | Playbooks | paths/catalog/playbooks.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/relationships/catalog` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/relationships/edges` | Infra | paths/infrastructure/routes.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/replatforming/ownership-packets` | IaC | paths/iac/replatforming_ownership.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/replatforming/plans` | IaC | paths/iac/replatforming.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/replatforming/rollups` | IaC | paths/iac/replatforming_rollups.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/report/bundle/capture` | CLISupport | paths/cli/capture.go | read | none (pure function of the request body; no store reads or writes) | wireAPI |
| `POST /api/v0/report/bundle/validate` | CLISupport | paths/cli/validate.go | read | none (pure function of the request body; no store reads or writes) | wireAPI |
| `POST /api/v0/runtime-declarations/query` | RuntimeDeclarations | paths/repository/runtime_declarations.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/search/semantic` | SemanticSearch | paths/search/semantic.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/service/report/compose` | CLISupport | paths/cli/compose.go | read | none (pure function of the request body; no store reads or writes) | wireAPI |
| `POST /api/v0/supply-chain/impact/suppressions` | SupplyChain | paths/supply/chain/suppression_mutation.go | mutation | writer: VulnerabilitySuppressionMutationStore (suppressions only) | newRouter |
| `POST /api/v0/terraform/config-state-drift/findings` | TerraformConfigStateDrift | paths/iac/terraform_config_state_drift.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `POST /api/v0/visualizations/derive` | Visualization | paths/evidence/visualization_packets.go | read | reader (ReadStore/graph/content, no durable writes) | newRouter |
| `GET /api/v0/docs` | APIRouter | <doc-ui> | read | n/a (mux-level) | newRouter |
| `GET /api/v0/redoc` | APIRouter | <doc-ui> | read | n/a (mux-level) | newRouter |
