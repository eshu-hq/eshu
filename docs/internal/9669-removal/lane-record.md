<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 lane record (#9669 removal, arbiter-authorized prep only)

Lane branch `drive/9669-d13-removal`. Ruling: AUTHORIZED-WITH-CONDITIONS for
consumer-drain and removal preparation only — no deletion, no deletion PR,
no traffic shift; merge of any removal PR needs a separate cutover-good ruling.
First deliverable: refresh the stale removal-tail narrative at this base.

## 51 unmounted GETs (fallthrough-401, enumerated live 2026-11-07)

### APIRouter
- `GET /api/v0/openapi.json`
### Admin
- `GET /api/v0/admin/delta-union/completeness`
- `GET /api/v0/admin/reindex/progress`
- `GET /api/v0/admin/shared-projection/tuning-report`
### CloudInventory
- `GET /api/v0/cloud/inventory`
### CloudRuntimeDrift
- `GET /api/v0/investigations/drift/packet`
### CodeownersOwnership
- `GET /api/v0/codeowners/ownership`
### ComponentExtensions
- `GET /api/v0/component-extensions`
- `GET /api/v0/component-extensions/{component_id}/diagnostics`
### Dependencies
- `GET /api/v0/dependencies`
### Entities
- `GET /api/v0/entities/{entity_id}/context`
- `GET /api/v0/investigations/services/{service_name}`
- `GET /api/v0/services/{service_name}/context`
- `GET /api/v0/services/{service_name}/story`
- `GET /api/v0/workloads/{workload_id}/context`
- `GET /api/v0/workloads/{workload_id}/story`
### ExtractionReadiness
- `GET /api/v0/collector-extraction-readiness`
- `GET /api/v0/collector-extraction-readiness/{family}`
### FactSchemaVersions
- `GET /api/v0/fact-schema-versions`
- `GET /api/v0/fact-schema-versions/{fact_kind}`
### Freshness
- `GET /api/v0/freshness/changed-since`
- `GET /api/v0/freshness/generations`
- `GET /api/v0/freshness/services/changed-since`
### GraphEntityInventory
- `GET /api/v0/graph/entities`
### IaC
- `GET /api/v0/iac/resources`
- `GET /api/v0/replatforming/selectors`
### Images
- `GET /api/v0/images`
### Incident
- `GET /api/v0/incidents/{incident_id}/context`
### Kubernetes
- `GET /api/v0/kubernetes/correlations`
### Metrics
- `GET /api/v0/metrics/timeseries`
### ObservabilityCoverage
- `GET /api/v0/observability/coverage/correlations`
### SSMHostInventory
- `GET /api/v0/cloud/ssm-host-inventory`
### SecretsIAM
- `GET /api/v0/secrets-iam/identity-trust-chains`
- `GET /api/v0/secrets-iam/posture-gaps`
- `GET /api/v0/secrets-iam/posture-summary`
- `GET /api/v0/secrets-iam/privilege-posture-observations`
- `GET /api/v0/secrets-iam/secret-access-paths`
### SemanticEvidence
- `GET /api/v0/semantic/code-hints`
- `GET /api/v0/semantic/documentation-observations`
### ServiceCatalog
- `GET /api/v0/service-catalog/correlations`
### SessionList
- `GET /api/v0/auth/sessions`
### Status
- `GET /api/v0/index-status`
- `GET /api/v0/status/answer-narration`
- `GET /api/v0/status/governance`
- `GET /api/v0/status/hosted-readiness`
- `GET /api/v0/status/index`
- `GET /api/v0/status/operations`
### SupplyChain
- `GET /api/v0/investigations/supply-chain/impact/packet`
### SurfaceInventory
- `GET /api/v0/surface-inventory`
### TagHistory
- `GET /api/v0/images/tag-history`
### WorkItems
- `GET /api/v0/work-items/evidence`
