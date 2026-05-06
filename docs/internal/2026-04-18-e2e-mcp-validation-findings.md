# E2E MCP Validation Findings — 2026-04-18

## Context

Validated the Go MCP server on the E2E instance (`eshu-e2e.example.test`, 896 repos)
by querying `sample-service-api`. Auth forwarding bug fixed and deployed.
Compared results against the QA Python instance (`eshu-qa.example.test`).

## Target Service

- **Name:** sample-service-api
- **Workload ID:** workload:sample-service-api
- **Kind:** service (Hapi.js Node API, Backstage catalog type: API/openapi)
- **Note:** Dual deployment (migration in progress) — should show in both QA and production environments

---

## Side-by-Side Comparison: Go E2E vs QA Python

### Service Context

| Feature | Go E2E | QA Python | Delta |
|---------|--------|-----------|-------|
| Workload detected | Yes | Yes | OK |
| Environments | 0 | 2 (qa, production) | **MISSING** |
| Instances | 0 | 2 (qa, production) | **MISSING** |
| Hostnames/Entrypoints | 0 | 2 (`sample-service-api.qa.example.com`, `sample-service-api.production.example.com`) | **MISSING** |
| API Surface | None | 21 endpoints with paths, methods, operation_ids | **MISSING** |
| API Versions | None | v3 | **MISSING** |
| Docs Routes | None | /_specs | **MISSING** |
| Spec Files | None | specs/index.yaml (from server/init/plugins/spec.js) | **MISSING** |
| Dependencies (outbound) | 2 (DEPLOYS_FROM, DISCOVERS_CONFIG_IN) | 0 | Different model |
| Entry points | 3 (script mains) | 2 (hostnames) | Different semantics |
| K8s Resource kind | NULL | "API" | **BUG** |

### Deployment Chain / trace_deployment_chain

| Feature | Go E2E | QA Python | Delta |
|---------|--------|-----------|-------|
| ArgoCD ApplicationSets | 0 | 2 (`sample-service-api`, `dashboard-sample-service-api`) | **MISSING** |
| K8s Resources | 1 (catalog-info.yaml only) | 6 (API, ConfigMap, GrafanaDashboard, GrafanaFolder, 2x XIRSARole) | **MISSING** |
| Terraform Modules | 0 | 100 | **MISSING** |
| Provisioning Source Chains | 0 | 11 repos | **MISSING** |
| Consumer Repositories | 7 (terraform-stack-* only) | 12 (actual service consumers) | **WRONG** |
| Hostnames | 0 | 2 | **MISSING** |
| Observed Config Environments | 0 | 2 (qa, production) | **MISSING** |
| Documentation Overview | None | Full (audiences, summaries, key artifacts, drilldowns) | **MISSING** |
| Support Overview | None | Full | **MISSING** |
| Story (narrative) | Minimal | Rich multi-section | **MISSING** |
| Deployment Facts | None | 4 (OBSERVED_IN_ENVIRONMENT x2, EXPOSES_ENTRYPOINT x2) | **MISSING** |
| Mapping Mode | "none" | "evidence_only" | **MISSING** |

### Consumer Repositories (Completely Different)

**Go E2E consumers (7)** — All terraform-stack-* repos via `PROVISIONS_DEPENDENCY_FOR`:
- terraform-stack-conversation
- terraform-stack-external-search
- terraform-stack-service-a
- terraform-stack-poc-nlp-search
- terraform-stack-provisioning
- terraform-stack-sitemaps
- terraform-stack-wordsmith

**QA Python consumers (12)** — Actual service consumers via hostname/repo references:
- sample-saved-search-api (repo_reference + hostname_reference)
- marketplace-api (repo_reference)
- traffic-reporter-api (repo_reference)
- sample-search-api (hostname_reference)
- provisioning-indexer-api (repo_reference)
- marketplace-automation (hostname_reference)
- sample-conversation-api (hostname_reference)
- config-service (hostname_reference)
- php-common-lib (hostname_reference)
- product-description-api (hostname_reference)
- sample-service-api-canary (hostname_reference) ← the dual deployment!
- content-indexer (hostname_reference)

### ArgoCD (E2E has zero, QA has full picture)

QA shows the deployment pipeline:
```
deployment-charts repo → argocd/sample-service-api/ directory:
  ├── base/dashboards/dashboard-overview-configmap.yaml (ConfigMap)
  ├── base/dashboards/dashboard-overview.yaml (GrafanaDashboard)
  ├── base/dashboards/folder.yaml (GrafanaFolder)
  ├── base/xirsarole.yaml (XIRSARole)
  └── overlays/env-qa/xirsarole-patch.yaml (XIRSARole)
```

Two ApplicationSets:
1. `sample-service-api` → project: argocd, deploys to helm.namespace
2. `dashboard-sample-service-api` → project: monitoring, deploys to monitoring ns

### Provisioning Source Chains (QA has, E2E has none)

QA correctly identifies 11 repos with their terraform module configurations:
- api-node-ai-provider: ecs_service, github_actions_role
- api-node-make-model-indexer: ecs_service, github_actions_role
- api-node-provisioning-indexer: ecs_service, github_actions_role
- api-node-template: ecs_service, github_actions_role
- dap-data-framework: s3, dynamodb, lambda, secrets_manager
- dap-dataplatform-infrastructure: networking, security (VPC, NACLs, GuardDuty)
- dap-general-purpose: s3, athena
- dap-sharedservices-infrastructure: networking, security
- dap-terraform-modules: s3 bucket
- github-settings: team configs
- deployment-charts: envoy gateway, API gateway, IRSA, load balancers

---

## Root Cause Analysis

### Why the Go E2E is missing so much

The Go data plane has these capabilities built and working:
1. Code parsing (functions, variables, files, directories)
2. Relationship evidence (DEPLOYS_FROM, DISCOVERS_CONFIG_IN, PROVISIONS_DEPENDENCY_FOR)
3. Basic workload/service detection (from catalog-info.yaml)
4. K8sResource extraction (but without `kind` field)

The QA Python instance has additional **query-time enrichment layers** that the Go
instance lacks:

| Missing Capability | What QA Does | Go Status |
|---|---|---|
| **Hostname extraction** | Parses config/*.json for hostname patterns | Not implemented |
| **API surface discovery** | Parses OpenAPI specs + server/init/plugins/spec.js references | Not implemented |
| **Cross-repo consumer search** | Searches ALL repos for hostname/name references | Not implemented |
| **ArgoCD ApplicationSet parsing** | Parses deployment-charts repo for ArgoCD manifests | Not implemented (or not in graph) |
| **Multi-repo K8s resource aggregation** | Collects K8s resources from deployment-charts + source repo | Only source repo |
| **Terraform module extraction (from provisioning_source_chains)** | Parses terraform in consumer repos | Relationship exists but no module detail |
| **Deployment fact synthesis** | Synthesizes OBSERVED_IN_ENVIRONMENT from config evidence | Not implemented |
| **Documentation/support overview** | Generates structured documentation from content store | Not implemented |
| **Story generation** | Multi-section narrative from all evidence | Minimal story only |
| **Provisioning source chain construction** | Follows IaC repos to find what modules they use | Not implemented |

### Key architectural difference

The QA Python instance does **query-time content search + synthesis**:
- When you ask for service context, it searches the content store across ALL repos
  for hostname matches, repository name references, etc.
- It reads config files to extract hostnames and environment patterns
- It parses OpenAPI/Swagger specs at query time
- It constructs provisioning chains by following terraform module declarations

The Go E2E instance relies **only on graph relationships pre-computed at ingestion time**:
- Whatever the projector/reducer put in Neo4j is all that's available
- No query-time content search
- No cross-repo hostname/reference scanning
- No OpenAPI parsing

---

## Bugs (Code Defects in Go)

### 1-3. Cypher Duplicate Column Name (BLOCKING)

Affects: `find_code`, `execute_language_query`, `analyze_code_relationships`
```
Neo.ClientError.Statement.SyntaxError: Multiple result columns with the same name
```

### 4. get_repo_summary returns 404 for valid repo names

`repo_name: "sample-service-api"` → 404 despite repo existing.

### 5. K8sResource kind is NULL

Graph stores `kind: null` but catalog-info.yaml declares `kind: API`.

---

## What Needs To Be Built (Priority Order)

### P0 — Fix blocking bugs (1-5)

Quick wins, pure code fixes.

### P1 — Hostname/Entrypoint extraction

Parse `config/*.json` for hostname patterns at ingestion time.
Store as graph properties on the Workload or as WorkloadInstance nodes.
This unlocks: environments, entrypoints, consumer hostname search.

### P2 — API Surface Discovery

Parse OpenAPI specs (referenced in catalog-info.yaml `$text: ./catalog-specs.yaml`)
and/or scan for spec plugin registration (server/init/plugins/spec.js).
Store endpoint paths, methods, operation_ids as graph entities.

### P3 — Cross-repo consumer discovery

At query time OR at backfill time, search content store for:
- Hostname references (`sample-service-api.qa.example.com` in any repo's files)
- Repository name references (`sample-service-api` in package.json, configs)
Store as CONSUMES or REFERENCES_HOSTNAME relationship evidence.

### P4 — ArgoCD resource aggregation from deployment-charts repo

The deployment-charts repo contains ArgoCD ApplicationSets and K8s resources for all services.
Need to:
- Parse ApplicationSet manifests and link to service workloads
- Collect K8s resources (ConfigMap, GrafanaDashboard, XIRSARole) from argocd/{service}/ paths
- Store cross-repo K8s resources with `deployed_by` attribution

### P5 — Provisioning source chain construction

Follow terraform module declarations in consumer repos to build the full IAC chain.
Currently we have the PROVISIONS_DEPENDENCY_FOR edge but no module-level detail.

### P6 — Deployment fact synthesis + story generation

Synthesize deployment facts (OBSERVED_IN_ENVIRONMENT, EXPOSES_ENTRYPOINT) from
hostname evidence and config file analysis. Generate rich multi-section stories.

### P7 — Documentation/support overview generation

Query-time feature that pulls together documentation evidence from multiple sources.

---

## Summary

The Go data plane correctly builds the **structural graph** (files, functions,
directories, basic relationships) but is missing the **semantic enrichment layers**
that make the QA Python instance useful for service understanding. The Python
instance does significant query-time work (content search, cross-repo scanning,
spec parsing, fact synthesis) that has no equivalent in the Go query path yet.

The relationship accuracy issue (#6 from the original findings) is confirmed:
- Go E2E shows 7 terraform-stack-* repos as consumers (via PROVISIONS_DEPENDENCY_FOR)
- QA Python shows 12 actual service consumers (via hostname_reference + repository_reference)
- These are completely different sets! The Go relationships are "who provisions IaC"
  while the QA relationships are "who actually calls this service"
