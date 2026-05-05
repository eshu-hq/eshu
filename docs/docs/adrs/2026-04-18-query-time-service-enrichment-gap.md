# ADR: Relationship-Mapping Accuracy And MCP Contract Parity — Closing the Gap Between Go and Python Data Planes

**Date:** 2026-04-18
**Status:** Accepted with follow-up
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

---

## Status Review (2026-05-03)

**Current disposition:** Accepted with follow-up.

The P0 contract and enrichment gaps are largely implemented. Service and
workload context now load graph API surface, deployment evidence, hostnames,
entrypoints, consumers, provisioning chains, docs, support signals, and story
sections. Tests cover plain service names, qualified workload IDs, graph-backed
deployment evidence, API surface, and observed signals without materialized
instances.

**Remaining work:** keep closing full service-query parity. Materialized runtime
instances and environments are still separate from query-time observed config
signals, and workload/service story responses remain narrative-first rather
than complete deployment-mapping contracts.

## Context

The Go data plane has reached structural correctness — 896 repos indexed with zero
terminal failures after the cross-phase EntityNotFound race fix. However, validation
of the MCP tool surface against the QA Python instance reveals that the Go query
layer does not yet provide **relationship-mapping accuracy** or **contract
correctness** at the level required for the same service investigation workflow.

The newest validation also shows a second problem: the test-machine E2E MCP is now
reachable and populated, but it does not yet provide **tool-contract parity**,
**service-query parity**, or **relationship-parity** with the QA MCP surface. In
practice, QA can answer service-oriented questions for `sample-service-api`, while E2E
currently answers some repository-oriented and deployment-artifact questions
reliably but still misses or misclassifies important service relationships.

This ADR documents the gap with concrete evidence, proposes architectural options for
closing it, and recommends a phased approach. It also serves as the
**contract-parity ADR** for the overlapping QA and E2E MCP capabilities used in the
service investigation workflow.

The governing principle for this ADR is:

- **Accuracy first**: if identity resolution, relationship mapping, or deployment
  provenance are wrong, the rest of the MCP surface is not trustworthy.
- **Compatibility second**: overlapping QA and E2E tools should accept comparable
  inputs and return semantically comparable outputs unless they are intentionally
  different.
- **Enrichment third**: richer stories, framework summaries, and operator
  narratives matter, but only after the underlying mappings are correct.

---

## Evidence: sample-service-api Comparison (2026-04-18)

### MCP Availability And Contract Parity

Both MCP servers are now reachable from Codex.

| Probe | Go E2E MCP (`mcp__eshu_e2e__`) | QA MCP (`mcp__eshu__`) |
|---|---|---|
| `list_indexed_repositories()` | Works | Works |
| `get_ecosystem_overview()` | Works | Works |
| `resolve_entity("sample-service-api")` | Fails with `HTTP 500` Neo4j syntax error (`invalid comma`) | Works |
| `get_service_story("workload:sample-service-api")` | Works for qualified ID, but returns no deployed instances | Works |
| `get_service_context("workload:sample-service-api")` | Works for qualified ID, but remains repo-centric | Works |
| `get_repo_summary("sample-service-api")` | Works | Works |
| `search_file_content(pattern="sample-service-api...")` | Fails with `HTTP 400: repo_id is required` | Cross-repo capable through current wrapper |

This matters because part of the observed gap is no longer just "missing facts."
The E2E MCP surface itself is currently using a different request contract than the
QA MCP wrapper expects.

### MCP Capability Matrix (QA vs E2E)

The table below focuses on overlapping capabilities that should behave similarly
across the QA and E2E MCP surfaces.

| Capability Family | Representative Tools | Go E2E MCP | QA MCP | Better Today | Finding |
|---|---|---|---|---|---|
| Inventory / ecosystem | `list_indexed_repositories`, `get_ecosystem_overview` | Works; repo inventory shape is richer | Works; ecosystem narrative is richer | Mixed | E2E is stronger for raw repo metadata; QA is stronger for operator-facing summarization |
| Name -> canonical ID resolution | `resolve_entity("sample-service-api")` | Fails with `HTTP 500` Neo4j syntax error (`invalid comma`) | Works; returns ranked matches with confidence scoring, finds both repos and workloads | QA | Qualified lookup path now reaches Neo4j, but the Cypher builder emits a trailing comma and breaks the query |
| Service story / service context | `get_service_story`, `get_service_context` | Qualified-ID path now works and returns structured service objects, but deployment data is still empty and service context remains repo-centric | Works with qualified ID and returns rich structured service view with story_sections, deployment_overview, documentation_overview, support_overview | QA | Contract compatibility improved, but relationship/service enrichment is still materially behind QA |
| Workload context | `get_workload_context("workload:sample-service-api")` | Works, but returns flat object with dependencies, 3 script `main` entry_points, 1 K8sResource, `instances: []` | Works, returns full API surface (21 endpoints with methods/operation_ids), hostnames (2), observed_config_environments, coverage data, and limitations | QA | E2E has workload data but returns repo-centric entry points instead of service entrypoints; QA includes full API surface |
| Repository story / context | `get_repo_story`, `get_repo_context` | Works; deployment artifacts and workflow artifacts are richer | Works; service semantics, framework detection, topology story, and consumer evidence are richer | Mixed | E2E is better for raw deployment/config artifact surfacing; QA is better for curated repo/service narrative |
| File retrieval | `get_file_content("catalog-info.yaml")` | Works | Works | Tie | Both return correct content |
| Content search | `search_file_content`, `search_entity_content` | Fails with `HTTP 400: repo_id is required` when called with `repo_ids` (array) | Works; cross-repo search returns 50+ matches across 15+ repos with snippets | QA | Field-name mismatch: tool schema exposes `repo_ids` (array) but Go handler requires `repo_id` (singular string); dispatch passes raw args so key never matches |
| Infra discovery | `find_infra_resources("sample-service-api")` | Returns 4 infrastructure entities (3 `K8sResource`, 1 `ArgoCDApplicationSet`) | Returns K8s resources (ConfigMap, GrafanaDashboard, GrafanaFolder, API), 2 ArgoCD ApplicationSets, 2 Crossplane XIRSARole claims | QA | Semantics are fixed on E2E, but coverage is still thinner and `kind` is still incomplete |
| Deployment trace | `trace_deployment_chain("sample-service-api")` | Times out on the full graph | Returns 2 ApplicationSets, K8s resources, 11 provisioning_source_chains with terraform module detail, 12 consumer_repositories | QA | E2E trace reliability is now the blocker; it no longer returns a usable minimal answer |
| Repo summary | `get_repo_summary("sample-service-api")` | Works; returns file counts, languages, deployment artifacts, and 7 provisioning consumers | Works; returns file counts, code entities, 8 dependents, 12 consumer_repositories with evidence_kinds, API surface, hostnames, framework_summary (Hapi/React), deployment_overview with topology_story, deployment_facts | QA | Name-based summary path is fixed in E2E, but QA still exposes richer service semantics and consumer evidence |
| Coverage summary | `get_repository_coverage(repo_id)` | Works and now returns completeness/gap/timestamp fields plus full `summary` sub-object | Works; returns `completeness_state`, `graph_gap_count`, `content_gap_count`, `graph_available`, `server_content_available`, timestamps, `last_error`, and full `summary` sub-object | Tie | This contract gap has been closed on E2E |
| Health / operator status | `get_index_status`, `list_ingesters` | Works for global health (896 repos, all succeeded, queue drained); `list_ingesters()` also works | Both `get_index_status` and `list_ingesters` fail (tool execution error / 500) | E2E | E2E is strictly better — QA's operator status tools are completely broken in this session |
| Code search / code analysis | `find_code`, `calculate_cyclomatic_complexity`, `analyze_code_relationships` | Still broken, but now split across three failure classes: `find_code` fails on repo-id wiring, `analyze_code_relationships` fails on malformed Cypher, and `calculate_cyclomatic_complexity` fails on entity lookup (`404 entity not found`) | All work: `find_code` returns ranked function/content matches, `calculate_cyclomatic_complexity` returns complexity scores, `analyze_code_relationships` returns caller chains with source context | QA | E2E still has overlapping code-analysis gaps, but the failure modes are distinct and should be tracked separately |
| Environment comparison | `compare_environments` | Returns "One or both environments not found" (no materialized environment data) | Times out (upstream request timeout) | Neither | Both sides broken — E2E lacks environment materialization, QA query is too expensive or unimplemented |

Three important conclusions fall out of this matrix:

- **This is not just a missing-data problem.** Several E2E tools have enough
  underlying data but expose the wrong request contract, the wrong semantic type,
  or a broken query builder.
- **E2E is already ahead in a few repo/deployment artifact areas.** The goal is
  not to copy QA blindly; it is to restore contract correctness while keeping the
  stronger raw deployment evidence that E2E already surfaces.
- **QA has capabilities with no E2E equivalent yet.** Framework detection
  (Hapi/React/Express/FastAPI), topology story synthesis, dual consumer views
  (graph-based `dependents` + content-search-based `consumer_repositories`), and
  structured `deployment_facts` with confidence thresholds are QA-only today.

### Service Query Parity

**Tools:** `resolve_entity("sample-service-api")`, `get_service_story(workload_id="workload:sample-service-api")`

| Data Point | Go E2E MCP | QA MCP |
|---|---|---|
| Workload resolution | Fails with Neo4j syntax error after dispatch reaches the resolver | Resolves `workload:sample-service-api` |
| Service story lookup | 200 for qualified ID `workload:sample-service-api`, but story still says "No deployed instances found" | Rich story returned for both forms |
| Environments surfaced | None via service MCP path | 2 (`qa`, `production`) |
| Workload instances | None via service MCP path | 2 (`qa`, `production`) |
| Public hostnames | None via service MCP path | 2 (`sample-service-api.qa.example.com`, `sample-service-api.production.example.com`) |
| API surface | None via service MCP path | 21 endpoints, `v3`, `/_specs`, `specs/index.yaml` |
| Deployment fact summary | Not available via service MCP path | `evidence_only`, medium-confidence entrypoint/environment facts |
| Service-level limitations | Not yet synthesized on the E2E service path | `runtime_platform_unknown`, `deployment_chain_incomplete` |

### Repository Query Parity

**Tools:** `list_indexed_repositories()`, `get_repo_story(repo_id)`, `get_repo_context(repo_id)`, `get_file_content(repo_id, "catalog-info.yaml")`

| Data Point | Go E2E MCP | QA MCP |
|---|---|---|
| Repository presence | Present as `repository:r_472ddee5` | Present as `repository:r_cd0afdc8` |
| File retrieval | Works | Works |
| Repo story | Works | Works |
| Workload count in repo story | 1 | 1 |
| Workflow relationships | `DEPLOYS_FROM` + `DISCOVERS_CONFIG_IN` to `shared-automation` | Same relationship family visible |
| Consumer repositories | 7 terraform-stack repos | Service-facing consumer set visible at service layer |
| Service-facing hostnames | Only indirectly through file content | Directly surfaced at service layer |
| API surface | Not synthesized at repo layer | Synthesized at service layer |

The key distinction is:

- **E2E is not empty.** It has the repo, the files, workflow artifacts, Docker
  artifacts, one workload, and repository consumers.
- **E2E is not service-query complete.** The workload-facing MCP path cannot yet
  return the service story that QA can.

### Consumer Detection — Currently Skewed Toward Provisioning Evidence

**Go E2E currently produces repository consumers from repo-context evidence:**
```
terraform-stack-conversation, terraform-stack-external-search,
terraform-stack-service-a, terraform-stack-poc-nlp-search,
terraform-stack-provisioning, terraform-stack-sitemaps, terraform-stack-wordsmith
```

These are useful as **deployment/config consumers**, but they are not equivalent to
the **service consumers** that operators expect when asking "who uses
sample-service-api?" They primarily reflect Terraform/module provisioning references.

**QA Python produces service-consumer evidence (via hostname + name references):**
```
sample-saved-search-api   [repo_reference, hostname_reference]
marketplace-api           [repo_reference]
traffic-reporter-api      [repo_reference]
sample-search-api         [hostname_reference: sample-service-api.qa.example.com]
provisioning-indexer-api  [repo_reference]
marketplace-automation    [hostname_reference: sample-service-api.qa.example.com, sample-service-api.production.example.com]
sample-conversation-api   [hostname_reference: sample-service-api.qa.example.com, sample-service-api.production.example.com]
config-service            [hostname_reference]
php-common-lib            [hostname_reference]
product-description-api   [hostname_reference]
sample-service-api-canary [hostname_reference] ← DUAL DEPLOYMENT EVIDENCE
content-indexer           [hostname_reference]
```

The dual deployment (`sample-service-api-canary`) is only visible through hostname
matching.

### Deployment Source And GitOps Evidence

Local repo inspection still proves the QA/EKS deployment path even when the E2E
service MCP path does not.

**Service repo evidence (`sample-service-api`):**
- `config/qa.json` contains `sample-service-api.qa.example.com`
- `config/production.json` contains `sample-service-api.production.example.com`
- both configs also contain ECS-related settings
- `catalog-info.yaml` declares a Backstage `API`
- `server/init/plugins/spec.js` exposes `/_specs` from `specs/index.yaml`

**GitOps repo evidence (`deployment-charts`):**
- `argocd/sample-service-api/overlays/env-qa/...` exists
- overlay values point at ECR image
  `111122223333.dkr.ecr.us-east-1.amazonaws.com/sample-service-api`
- overlay hostname is `sample-service-api.qa.svc.example.test`
- base includes `xirsarole.yaml` and dashboard resources

This means the current gap is not "the deployment evidence does not exist." The gap
is that E2E does not yet assemble or surface that evidence through the same service
investigation contract that QA does.

## Live QA vs E2E Validation: node-service-a (2026-04-18, Post-Token Refresh)

After refreshing the Codex MCP token and re-running the comparison against the live
QA and E2E MCPs, the parity picture is clearer:

- **Both MCPs are reachable from Codex.** The E2E token issue is resolved.
- **Some previously broken E2E contracts now respond**, but several still differ
  materially from QA in shape, accuracy, and operator usefulness.
- **The remaining gap is not just missing enrichment.** E2E still has multiple
  cases where the contract shape is older or the extracted evidence is overly noisy.

### Live Tool Snapshot

| Probe | QA MCP (`mcp__eshu__`) | Go E2E MCP (`mcp__eshu_e2e__`) | Finding |
|---|---|---|---|
| `resolve_entity("node-service-a")` | Ranked `matches` with canonical workload/repository refs and confidence | Flat `entities` list with mixed directories, repo, workload, K8s, and ArgoCD entities | E2E now resolves, but contract shape still differs from QA |
| `get_service_story("workload:node-service-a")` | 2 instances (`qa`, `production`), 2 public hostnames, 21 API endpoints, docs route `/_specs`, structured sections | Service story still says `No materialized workload instances found` and returns only minimal deployment summary | Core service-query gap remains live on E2E |
| `get_repo_summary("node-service-a")` | Rich service-facing summary with consumer evidence, hostnames, API surface, framework summary, deployment facts, and topology story | Richer raw deployment/config artifact dump, but still skewed toward provisioning stacks and lacks service-correct runtime story | Mixed: E2E is artifact-rich, QA is more accurate for service investigation |
| `search_file_content(pattern="node-service-a")` | Cross-repo `matches` with snippets, repo IDs, language, source backend | Cross-repo `results` now return, but with older shape (`count/results`) and empty `content` instead of snippets | E2E request path works now, but contract parity is still incomplete |
| `trace_deployment_chain("node-service-a")` | Returns ArgoCD ApplicationSets, K8s resources, hostname/API surface, consumer repos, and capped story | Returns a very large mixed payload, still no materialized instances, many false-positive hostnames, and an oversized delivery/artifact dump | E2E trace is alive, but precision and service accuracy are still behind QA |

### What Improved

- `resolve_entity("node-service-a")` on E2E no longer fails with the old malformed
  Cypher error.
- `search_file_content(pattern="node-service-a")` on E2E no longer fails with the
  old `repo_id is required` behavior. It now performs cross-repo search.
- `trace_deployment_chain("node-service-a")` on E2E no longer times out in this
  run. It returns a response.

These are real wins, and they mean the transport and several P0 request paths are
healthier than they were earlier in the day.

### What Is Still Wrong On E2E

#### 1. Service-story accuracy is still not at QA level

The live QA `get_service_story("workload:node-service-a")` returns:

- 2 workload instances (`qa`, `production`)
- public entrypoints `node-service-a.qa.bgrp.io` and
  `node-service-a.prod.bgrp.io`
- docs route `/_specs`
- 21 structured API endpoints with methods and operation IDs
- service-facing documentation and support sections

The live E2E `get_service_story("workload:node-service-a")` still returns:

- `No materialized workload instances found`
- empty deployment environments/platforms
- no usable service-facing API surface in the story itself

That means the most important operator-facing service path is still materially behind
QA even though the repository and content stores contain enough evidence.

#### 2. E2E still over-classifies hostname evidence

The live E2E `trace_deployment_chain("node-service-a")` now returns many obvious
false-positive "hostnames", including values like:

- `node-service-a.ts`
- `build-elasticsearch.js`
- `console.log`
- `module.exports`
- `pkg.name`
- `server.inject`

This is a precision problem, not just a completeness problem. It reduces trust in
all downstream service-entrypoint and network-path reasoning.

#### 3. E2E trace output is still artifact-heavy but service-light

The live E2E deployment trace returns:

- 0 materialized instances
- 0 deployment environments in the runtime overview
- 1 K8s API resource from `catalog-info.yaml`
- a huge `delivery_paths` / `deployment_artifacts` payload
- 7 graph provisioning source chains
- many consumer repositories

By contrast, QA keeps the service trace centered on:

- service entrypoints
- GitOps objects that actually deploy the service
- service-facing consumer evidence
- bounded, operator-readable story sections

This confirms that E2E still needs **service-oriented synthesis and precision
controls**, not just more raw artifact collection.

#### 4. Contract parity is still incomplete even where E2E works

Examples from the live run:

- QA `resolve_entity` returns `matches` with confidence/inference metadata; E2E
  returns a flat `entities` list
- QA `search_file_content` returns `matches` with snippets and source backend; E2E
  returns `count/results` with empty `content`
- QA `get_service_story` is a structured service contract; E2E still behaves more
  like a lightly enriched repository/deployment view

So the ADR should continue to treat **contract parity** as a first-class problem,
not merely an implementation detail.

### Updated Interpretation

The live comparison changes the state of the problem in one important way:

- The E2E MCP is no longer broadly "broken" on transport or basic reachability.
- Several earlier P0 failures have moved into the **partially fixed** bucket.
- The remaining work is now more clearly concentrated in:
  - service-story accuracy
  - hostname and entrypoint precision
  - service-oriented deployment synthesis
  - response-contract parity with QA for overlapping tools

That is a healthier problem than the earlier "transport + contract + enrichment all
broken at once" state, but it still leaves the E2E MCP short of the operator-grade
service investigation workflow that QA already provides.

---

## Root Cause

### Architectural Difference

The Go data plane is primarily a **pre-computed graph model**: relationships are built
at ingestion/projection time and stored as Neo4j edges. However, the current query
layer is not graph-only. It already mixes Neo4j with repository-scoped content-store
lookups for infrastructure and deployment detail.

The QA Python instance leans further into a **query-time enrichment model**: when a
service context is requested, it performs cross-repo content searches, parses specs,
and synthesizes evidence on the fly.

The service-context gap is therefore not one single missing feature. It is the result
of several separate issues:

1. **Missing extracted signals** for service hostnames and API surface.
2. **Missing or weak derived edges** for deployment-source and consumer relationships.
3. **Query-layer enrichment that is repository-scoped today**, even where lower-level
   storage primitives already support cross-repo scans.
4. **MCP/query contract divergence** between the E2E and QA surfaces for some
   workload-facing tools.
5. **Several overlapping E2E query handlers are broken or mis-routed** rather than
   merely less enriched:
   - `resolve_entity` and `analyze_code_relationships` still fail with malformed
     Cypher (`invalid comma`)
   - `search_file_content` and `find_code` still fail on `repo_ids` vs `repo_id`
     request/handler wiring
   - `calculate_cyclomatic_complexity` still cannot reliably find entities without
     scoped lookup
   - `trace_deployment_chain` now times out on the full graph

### What the Go Pipeline Has

1. **39 evidence kinds** for IaC, controller, and workflow relationships
   (Terraform, ArgoCD, GitHub Actions, Helm, Jenkins, Ansible, etc.)
2. **Per-generation evidence discovery** from current-repo facts plus a deferred
   backward-evidence readiness/backfill path for newly introduced repositories
3. **Regex-based matching**: patterns like `app_repo = "..."`, `github.com/org/repo`
4. **Resolution with confidence scoring** and assertion overrides
5. **PostgreSQL content store** with trigram indexes, including a storage-layer API
   that supports cross-repo file/entity search when `repo_id` is omitted
6. **Query-time enrichment already exists in parts of the Go API**, including
   repository-scoped content lookups and controller-entity aggregation from derived
   deployment source repositories
7. **Repository story/context in E2E already contains valuable deployment artifacts**:
   Dockerfiles, GitHub Actions workflows, one workload, a Kubernetes API resource,
   and terraform-stack consumer repositories are all available today
8. **Controller/workflow evidence is already classified in the query layer**:
   repository relationship summaries already distinguish controller-driven
   (`argocd_`, `ansible_`, `jenkins_`), workflow-driven (`github_actions_`), and
   IaC-driven evidence families
9. **Cross-repo content primitives already exist below the broken MCP surface**:
   repo-scoped `search_file_content` works today when a repo identifier is provided,
   and the query package already contains `SearchFileContentAnyRepo` and
   `SearchEntitiesByNameAnyRepo`
10. **Repository controller artifact extraction already exists**: Jenkins pipeline
    metadata plus Ansible inventories, vars, task entrypoints, and role paths are
    already extracted for repository-level views

### What the Go Pipeline Lacks

1. **No hostname extraction/materialization**: config/qa.json, config/production.json,
   and similar files are stored, but service hostnames are not extracted into
   first-class evidence or graph entities
2. **No API surface extraction/materialization**: catalog-specs.yaml and related spec
   files are not parsed into service-facing routes, versions, or spec metadata
3. **No hostname-aware consumer discovery**: the existing relationship pipeline can
   backfill repo-alias evidence, but it does not discover cross-repo references to
   service hostnames or other service entrypoints
4. **No cross-repo query enrichment in the `query` package**: the storage layer
   supports optional `repo_id`, but the `query.ContentReader` used by handlers is
   still repo-scoped
5. **Deployment/controller context depends on upstream derived edges being correct**:
   controller aggregation exists, but it only sees repositories already surfaced as
   deployment sources
6. **Service-context response shape is still repository-centric**: `get_service_context`
   returns repo dependencies, repo infrastructure, and code entry points, not service
   entrypoints, API surface, or consumer evidence
7. **E2E MCP wrappers and handlers are only partially converged with QA** for the same
   logical tools: qualified-ID service story/context and name-based repo summary now
   work, but `resolve_entity` still fails in the query layer, `search_file_content`
   and `find_code` still require incorrect repo wiring, and `trace_deployment_chain`
   is not yet reliable at full-graph scale
8. **No framework detection**: QA's `get_repo_summary` identifies runtime frameworks
   (Hapi, React, Express, FastAPI, Next.js) from code patterns and exposes them in
   `framework_summary`. E2E has no equivalent.
9. **No dual consumer view**: QA provides both graph-derived `dependents` (8 repos via
   typed edges) AND content-search-derived `consumer_repositories` (12 repos via
   hostname + name matching). E2E only has the graph-derived provisioning consumers
   (7 terraform-stack repos) which are not actual service callers.
10. **No topology story synthesis**: QA generates narrative summaries like "Top
    consumer-only repository sample-service-consumer references this service via
    hostname references..." that summarize the deployment topology for operators.
11. **`compare_environments` is unimplemented on both sides**: E2E returns "environments
    not found" (no materialized environment data), QA times out. Neither can answer
    "what differs between qa and production for this workload?"
12. **Deployment trace reliability gap**: `trace_deployment_chain` is still not
    dependable enough for operator workflows. QA returns a materially richer
    deployment chain, while E2E now times out on the full graph instead of returning
    a sparse-but-usable answer.
13. **No runtime adapter or workload-instance materialization**: E2E still cannot
    prove that the same service is deployed through different runtime adapters
    (for example ECS in one environment family and EKS/ArgoCD in another) because
    workload instances and platform adapters are not being materialized as
    first-class service facts.
14. **Controller/workflow provenance is not yet elevated into the service trace**:
    the codebase already recognizes ArgoCD, Helm, GitHub Actions, Jenkins, and
    Ansible evidence, but service traces still privilege ArgoCD/Terraform-style
    evidence and do not treat Jenkins/Ansible/GitHub Actions as first-class
    deployment provenance for services that use those systems.
15. **No network path evidence chain**: neither side cleanly models
    `public hostname -> DNS/API gateway/ingress/load balancer -> runtime target`
    as an evidence-backed chain, which means "how does traffic reach this service?"
    still requires operator inference.
16. **No cloud dependency chain at the service layer**: image repositories,
    IAM/IRSA/XIRSARole, SSM/Secrets, DynamoDB, S3, SNS/SQS, RDS, and related AWS
    dependencies are partially visible in repo or provisioning views, but not yet
    promoted into a service-facing dependency chain with clear provenance.
17. **No artifact lineage chain**: the system does not yet connect
    `workflow/controller -> build artifact/image -> deployment target -> public or
    internal service entrypoint` in one traceable story.
18. **Confidence/provenance is not yet first-class across all fact families**: the
    system should be able to say which repo/path/evidence kind produced a fact,
    whether it is direct or synthesized, and what confidence band it carries for
    service entrypoints, runtime adapters, controller provenance, consumers, and
    cloud dependencies.

---

## Evidence from Code Investigation

### PROVISIONS_DEPENDENCY_FOR is Overly Broad

**File:** `go/internal/relationships/evidence.go:54-90`

The regex patterns that trigger PROVISIONS_DEPENDENCY_FOR:
```go
// Matches ANY github.com/org/repo reference in Terraform files
Pattern: regexp.MustCompile(`(?i)github\.com[:/][^/"'\s]+/([A-Za-z0-9._-]+)(?:\.git)?`)
Confidence: 0.98

// Matches app_name = "..." in ANY Terraform file
Pattern: regexp.MustCompile(`(?i)\bapp_name\b\s*=\s*"([^"]+)"`)
Confidence: 0.94

// Matches /configd/ or /api/ path patterns
Pattern: regexp.MustCompile(`(?i)/(?:configd|api)/([A-Za-z0-9._-]+)/`)
Confidence: 0.90
```

This creates false positives: any repo mentioning a service name in a GitHub URL,
comment, or config path generates a PROVISIONS_DEPENDENCY_FOR edge.

### Cross-Repo Search Exists In Storage But Not In The Query Adapter

**Files:** `go/internal/storage/postgres/content_store.go:151-205`,
`go/internal/query/content_reader.go:163-235`

```go
func (s ContentStore) SearchFileContent(ctx, query, repoID, limit) ([]FileContentRow, error) {
    // repoID is optional — passing "" searches ALL repos
    // PostgreSQL trigram index (gin_trgm_ops) enables fast ILIKE across 896 repos
}
```

But the handler-facing query adapter is narrower:

```go
func (cr *ContentReader) SearchFileContent(ctx, repoID, pattern string, limit int) ([]FileContent, error) {
    // WHERE repo_id = $1 AND content ILIKE ...
}
```

So the underlying storage layer can do cross-repo search, but the query package does
not currently expose that capability to `get_service_context` or
`trace_deployment_chain`.

The Postgres schema still supports the broader search:
```sql
CREATE INDEX content_files_content_trgm_idx
    ON content_files USING gin (content gin_trgm_ops);
```

This means the ADR should treat cross-repo consumer search as a real query-layer
implementation project, not just a small wiring task.

### Backward Evidence Exists, But Not For Hostname-Level Service Discovery

**Files:** `go/internal/reducer/cross_repo_resolution.go:88-123`,
`go/internal/storage/postgres/ingestion.go:588-631`

The reducer still resolves evidence per generation:
```go
evidenceFacts := h.EvidenceLoader.ListEvidenceFacts(ctx, generationID)
```

But the system is not purely forward-only anymore. Cross-repo resolution is gated on
`GraphProjectionPhaseBackwardEvidenceCommitted`, and ingestion performs a corpus-wide
re-discovery/backfill for newly introduced target repositories.

The real limitation is narrower:

- backward-style repo-alias evidence exists
- hostname/service-entrypoint evidence does not
- therefore consumer discovery remains inaccurate for services whose best evidence is
  hostname usage rather than repository alias references

### Controller And Workflow Evidence Already Exists But Is Not Elevated

**Files:** `go/internal/relationships/evidence.go:149-188`,
`go/internal/query/repository_relationship_overview.go:39-77`,
`go/internal/query/repository_controller_artifacts.go:10-52`

The Go pipeline already knows about a broader evidence surface than the ADR
currently treats as first-class:

- `discoverFromEnvelope(...)` dispatches to ArgoCD, Helm, Jenkins, Ansible,
  Docker, Docker Compose, and GitHub Actions evidence discovery, not just
  Terraform and ArgoCD.
- repository relationship summaries already split evidence into
  controller-driven (`argocd_`, `ansible_`, `jenkins_`), workflow-driven
  (`github_actions_`), and IaC-driven families.
- repository controller artifacts already extract Jenkins pipeline metadata and
  attach Ansible inventories, vars, task entrypoints, and role-path hints.

This means the missing capability is not just "add Jenkins and Ansible support."
That support is already present in the data plane at the repository layer. The
real gap is that those controller/workflow facts are not yet being promoted into
the same service-level trace and runtime story that operators use for
service investigation.

### Live MCP Validation Refined Two Assumptions

**Files:** `go/internal/query/content_reader.go:163-235`,
`go/internal/query/content_reader_names.go:11-131`,
`go/internal/query/impact_trace_deployment.go:13-76`

The live QA vs E2E comparison also clarified two important details:

1. **`search_file_content` is not completely broken in E2E.**
   Repo-scoped search works when `repo_id` is provided. The broken path is the
   cross-repo form used by QA-style investigation flows when `repo_id` is
   omitted. The query package already contains `SearchFileContentAnyRepo`, but
   the MCP/query surface still routes callers through the repo-scoped path.
2. **`compare_environments` now fails structurally, not opaquely, in E2E.**
   E2E returns a structured "unsupported" result because no workload instances
   are materialized for `qa` or `production`. That is better than a generic
   failure and tells us the missing evidence class precisely: environment/runtime
   instance materialization.

---

## Options

### Option A: Query-Time Enrichment (Python Parity)

Add cross-repo content search to the Go query layer at request time.

**Implementation:**
1. Extend the `query` package with a cross-repo content-search adapter instead of
   relying on the current repo-scoped `ContentReader`
2. `get_service_context` queries content store for hostname patterns across all repos
3. `trace_deployment_chain` uses the same adapter for consumer discovery and
   optional provenance lookup
4. Parse OpenAPI specs on demand
5. Synthesize service entrypoints and consumer evidence directly in the response

This option should not be described as "search deployment-charts repo" because the current
query layer already knows how to aggregate ArgoCD controller entities from derived
deployment source repositories. The gap is getting the right deployment source repos
and service-entrypoint evidence into that path.

**Pros:**
- Exact parity with QA Python behavior
- No schema changes required
- Leverages existing trigram indexes

**Cons:**
- Query latency increases from ~50ms to 1-3s
- No audit trail for dynamically discovered evidence
- Repeated computation on every request
- Harder to debug "why does this relationship exist?"

### Option B: Ingestion-Time Enrichment (Materialized at Write)

Extend the ingestion pipeline to extract hostnames, parse OpenAPI specs, and compute
backward references at ingestion time. Store as evidence facts and resolve into graph edges.

**Implementation:**
1. New evidence kinds for service entrypoints and cross-repo usage, for example:
   `CONFIG_HOSTNAME_REFERENCE`, `OPENAPI_SERVER_URL`, `HOSTNAME_CONSUMER_REFERENCE`
2. New extractor: parse `config/*.json` for hostname patterns during ingestion
3. New extractor: parse OpenAPI specs for endpoints + server URLs
4. New backward discovery: when repo A is ingested, search all repos' content for
   references to repo A's hostnames or emitted service entrypoints
5. Materialize service-facing query data so `get_service_context` stops falling back
   to repository-only entry points
6. Store as `relationship_evidence_facts` and/or service-specific materialized
   entities, then resolve into graph edges

**Pros:**
- Sub-100ms query latency (everything pre-computed)
- Full audit trail (evidence_id, confidence, rationale)
- Assertion overrides work (humans can reject false positives)
- Consistent with existing architecture

**Cons:**
- Higher ingestion cost per repo (cross-repo search during commit)
- Requires re-ingestion to populate existing repos
- More complex evidence lifecycle (stale hostname evidence when configs change)
- Larger Neo4j graph (more edges)

### Option C: Hybrid — Materialized Core + Query-Time Enrichment

Pre-compute the high-value, stable data at ingestion time. Provide query-time
enrichment for expensive or volatile data behind an `?enrich=true` parameter.

**Materialized at ingestion (fast path):**
- Hostname extraction from config files -> service-entrypoint evidence/entities
- OpenAPI spec parsing -> API surface entities stored in graph
- Tighten deployment-source derivation so existing controller aggregation sees the
  right source repositories
- Service-context response shape updated to prefer service-facing entrypoints over
  repository `main` functions

**Query-time enrichment (slow path, opt-in):**
- Cross-repo consumer search (hostname + name pattern matching)
- Provisioning source chain construction (follow terraform modules)
- Documentation/support overview generation
- Rich story narrative synthesis

**Pros:**
- Fast default queries with pre-computed relationships
- Rich opt-in queries for deep investigation
- Clear separation: graph = truth, enrichment = derived
- Incremental: ship fast path first, enrichment later

**Cons:**
- Two code paths to maintain
- Client must know about `?enrich=true`
- Potential inconsistency between fast/enriched results

---

## Decision

**Recommended: Option C (Hybrid) with Option B as the end state.**

This ADR should also be treated as the contract-fix plan for the overlapping QA and
E2E MCP tools, not just the enrichment plan for the service query layer.

### Rationale

1. **Immediate value** comes from restoring accuracy in identity, relationship
   mapping, and diagnosis paths, not from treating the problem as cosmetic service
   enrichment. The first step is to separate:
   - incorrect or missing service signals
   - incorrect or missing deployment-source derivation
   - repo-scoped versus cross-repo query limitations
   - MCP contract divergence between E2E and QA
   - broken handlers that currently fail even when the underlying data exists

2. **Medium-term** add hostname extraction and service-facing materialization so the
   default service context becomes structurally useful without expensive ad hoc scans.

3. **Selective query-time enrichment** still makes sense for consumer discovery and
   deep provenance, but it should be treated as an explicit secondary path built on a
   query adapter that can safely do cross-repo scans.

4. **Long-term** migrate high-volume query-time enrichments into materialized service
   facts once their semantics stabilize.

All phases below are important. The sequencing reflects **dependency order and
trust-building order**, not importance. `P0` is first because the rest of the
system cannot be trusted until identity resolution, relationship lookup, and core
contract behavior are correct.

### Phase Plan

| Phase | Scope | Timeline |
|---|---|---|
| **P0: Accuracy And Contract Blockers** | Finish the still-open correctness work: (1) fix malformed Cypher in `resolve_entity`, `analyze_code_relationships`, and related builders; (2) fix `search_file_content` / `find_code` request wiring so `repo_ids` and omitted-repo cross-search behave correctly; (3) make `calculate_cyclomatic_complexity` do reliable entity lookup without brittle scoped assumptions; (4) ensure `trace_deployment_chain` returns a bounded, non-timeout result; (5) keep improving `find_infra_resources` so actual resource kinds are surfaced; (6) stop presenting repo `main` functions as service entrypoints | 3-5 days |
| **P1: Runtime And Service Signal Materialization** | Materialize workload instances, runtime adapters, environment facts, hostnames, and service entrypoints from config and deployment evidence so E2E can prove `qa`/`production` and ECS/EKS-style deployment shapes instead of returning empty instance sets | 3-5 days |
| **P2: Cross-Repo Consumer Accuracy** | Add a cross-repo search adapter to the query layer and implement opt-in consumer enrichment so service callers are not confused with provisioning/config consumers; retain the dual view of graph `dependents` plus content-search `consumer_repositories` | 4-7 days |
| **P3: API Surface And Network Entry Accuracy** | OpenAPI spec parsing -> API surface entities and service-context response fields; model public/internal entrypoints and their network-path evidence (docs route, API gateway, ingress, load balancer, hostname) | 1 week |
| **P4: Deployment Provenance Accuracy** | Tighten deployment-source derivation and promote controller/workflow provenance so ArgoCD, Helm, GitHub Actions, Jenkins, and Ansible evidence all contribute correctly to service traces | 1 week |
| **P5: Environment And Cloud Dependency Contract Completion** | `compare_environments` moves from "unsupported/timeout" behavior to implemented environment diffs or an explicit not-yet-supported contract; service traces surface cloud dependency chains (image repos, IAM/IRSA/XIRSARole, secrets/config stores, data-plane dependencies) with provenance | 1 week |
| **P6: Provisioning Chain And Artifact Lineage Accuracy** | Provisioning source chain construction and ranking; connect `workflow/controller -> artifact/image -> deployment target -> service entrypoint` into one evidence-backed lineage | 1 week |
| **P7: Operator Narrative And Framework Enrichment** | Story/documentation/framework enrichment: topology story synthesis, framework detection (Hapi/React/Express/FastAPI), documentation overview generation, and confidence/provenance summaries after service facts and consumer evidence are trustworthy | 1 week |

### Evidence Contract Checklist

The checklist below turns the live QA-vs-E2E validation into a sanitized
acceptance contract for implementation. The goal is not byte-for-byte equality
with QA. The goal is that E2E can answer the same operator questions with
correct, evidence-backed semantics.

#### P0 Acceptance Contract

- `resolve_entity("sample-service-api")` returns at least one repository match
  and one `workload:sample-service-api` service/workload match without Cypher
  failure.
- `search_file_content(pattern="sample-service-api")` supports both repo-scoped
  search and omitted-repo cross-repo search without requiring callers to guess a
  hidden handler-specific field shape.
- `find_code`, `analyze_code_relationships`, and
  `calculate_cyclomatic_complexity` return successful responses for basic
  service-name and function-name queries instead of query-builder failures.
- `find_infra_resources("sample-service-api")` returns infrastructure entities
  with non-empty `kind` and source metadata instead of generic `K8sResource`
  stubs.
- `trace_deployment_chain("sample-service-api")` returns a bounded response
  within the tool timeout budget instead of timing out.
- `get_workload_context("workload:sample-service-api")` no longer presents repo
  script `main` functions as service entrypoints.

#### P1 Acceptance Contract

- `get_service_story("workload:sample-service-api")` surfaces at least 2
  environments and 2 workload instances for the canonical dual-environment
  sample.
- `get_service_context("workload:sample-service-api")` includes materialized
  runtime/environment evidence rather than an empty `instances` array.
- Runtime/platform evidence is explicit enough to distinguish adapter families
  such as ECS-style and EKS/ArgoCD-style deployment shapes when the source
  evidence supports that distinction.

#### P2 Acceptance Contract

- `search_file_content` cross-repo search is usable by service-level enrichment
  without a repo identifier.
- `get_service_context` or `trace_deployment_chain` exposes a dual consumer view:
  graph-derived dependents and content-search-derived consumer repositories.
- Consumer evidence is classified so callers are not conflated with
  provisioning/config consumers.

#### P3 Acceptance Contract

- `get_service_story`, `get_service_context`, and/or `get_workload_context`
  surface API versions, docs routes, spec files, endpoint count, and endpoint
  paths for the sample service.
- Public and internal entrypoints are separated and typed as entrypoints rather
  than mixed with repo code entry points.
- The network path from hostname or docs route toward the runtime target is
  evidence-backed where source artifacts exist.

#### P4 Acceptance Contract

- `trace_deployment_chain("sample-service-api")` surfaces controller/workflow
  provenance from the relevant families present in the source artifacts:
  ArgoCD, Helm, GitHub Actions, Jenkins, and Ansible.
- Service traces promote controller/workflow evidence into the service-facing
  narrative instead of leaving it trapped in repo-only summaries.
- ArgoCD ApplicationSet, dashboard, and Crossplane/IRSA-style controller facts
  are surfaced as typed service-trace evidence when present.

#### P5 Acceptance Contract

- `compare_environments("qa","production")` returns a supported comparison for
  workloads with materialized instances, or an explicit structured unsupported
  contract only when the evidence is truly absent.
- Service-level traces surface cloud dependency evidence such as image
  repositories, IAM/IRSA/XIRSARole, secrets/config backends, and core AWS data
  dependencies when the deployment evidence supports them.
- Cloud/resource evidence includes provenance, not just names.

#### P6 Acceptance Contract

- `trace_deployment_chain` or an equivalent service trace connects
  `workflow/controller -> artifact/image -> deployment target -> service
  entrypoint` in one lineage view where source evidence exists.
- Provisioning source chains are ranked and filtered so unrelated Terraform
  module consumers do not dominate the service trace.

#### P7 Acceptance Contract

- `get_repo_summary` or a service-facing summary exposes framework detection
  comparable in semantics to QA when evidence exists.
- Topology narrative and documentation/support summaries are grounded in the
  materialized service facts and trace evidence from earlier phases.
- Each surfaced fact family can report its evidence provenance and confidence
  band so operators can distinguish direct facts from synthesized narrative.

---

## Consequences

### Positive
- Service context becomes useful for operators (environments, endpoints, consumers visible)
- Dual deployment patterns become discoverable (hostname-based matching)
- MCP tools return actionable intelligence instead of structural data
- Parity with QA Python for the critical service investigation workflow

### Negative
- Query latency increases for enriched queries (mitigated by caching)
- Ingestion time increases slightly for hostname/spec extraction
- More complex evidence lifecycle (hostname evidence can go stale)
- Need to re-ingest 896 repos to populate new evidence types

### Risks
- Cross-repo trigram search at 896 repos may hit performance limits for common patterns
- False positive consumer detection (e.g., "catalog" matching unrelated repos)
- OpenAPI spec parsing complexity (multiple formats, $ref resolution)

### Mitigations
- Index hostname patterns in a materialized view for O(1) lookup
- Use confidence scoring to rank consumer evidence (hostname > name substring)
- Start with simple OpenAPI parsing (paths/methods only, no $ref resolution)
- Cache enrichment results with TTL tied to content store freshness

---

## Appendix: Evidence Comparison Data

### QA Python Service Context Response (sample-service-api)

```json
{
  "instances": [
    {"id": "workload-instance:sample-service-api:qa", "environment": "qa"},
    {"id": "workload-instance:sample-service-api:production", "environment": "production"}
  ],
  "entrypoints": [
    {"hostname": "sample-service-api.qa.example.com", "environment": "qa", "visibility": "public"},
    {"hostname": "sample-service-api.production.example.com", "environment": "production", "visibility": "public"}
  ],
  "api_surface": {
    "endpoint_count": 21,
    "api_versions": ["v3"],
    "docs_routes": ["/_specs"],
    "spec_files": [{"relative_path": "specs/index.yaml", "discovered_from": "server/init/plugins/spec.js"}],
    "endpoints": [
      {"path": "/v3", "methods": ["get", "post", "delete"], "operation_ids": ["search", "postSearch", "deleteSearchScroll"]},
      {"path": "/v3/multiSearch", "methods": ["get", "post"], "operation_ids": ["multiSearch", "postMultiSearch"]},
      {"path": "/v3/cities", "methods": ["get"], "operation_ids": ["getCities"]},
      {"path": "/v3/listing/{id}", "methods": ["get"], "operation_ids": ["getListing"]},
      "... 17 more endpoints"
    ]
  },
  "observed_config_environments": ["qa", "production"],
  "coverage": {"completeness_state": "complete", "functions": 384, "content_entity_count": 3191}
}
```

### QA MCP Service Story Response (sample-service-api)

```json
{
  "subject": {"id": "workload:sample-service-api", "kind": "service", "name": "sample-service-api"},
  "story": [
    "sample-service-api has 2 known workload instances across environments.",
    "Owned by repositories sample-service-api.",
    "Public entrypoints: sample-service-api.production.example.com, sample-service-api.qa.example.com."
  ],
  "deployment_overview": {
    "instances": [
      {"id": "workload-instance:sample-service-api:qa", "environment": "qa"},
      {"id": "workload-instance:sample-service-api:production", "environment": "production"}
    ],
    "hostnames": [
      {"hostname": "sample-service-api.production.example.com", "environment": "production"},
      {"hostname": "sample-service-api.qa.example.com", "environment": "qa"}
    ],
    "api_surface": {
      "endpoint_count": 21,
      "api_versions": ["v3"],
      "docs_routes": ["/_specs"]
    }
  }
}
```

### Go E2E MCP Service Story Result (sample-service-api)

```json
{
  "service_name": "sample-service-api",
  "story": "Workload sample-service-api (kind: service) is defined in repository sample-service-api. No deployed instances found.",
  "deployment_overview": {
    "environment_count": 0,
    "instance_count": 0,
    "environments": [],
    "platforms": []
  }
}
```

### Go E2E Repository Story / Context (sample-service-api)

```json
{
  "repository": {"id": "repository:r_472ddee5", "name": "sample-service-api"},
  "story": "Repository sample-service-api contains 537 indexed files. Defines 1 workload(s): sample-service-api.",
  "workflow_relationships": [
    {"type": "DEPLOYS_FROM", "target_name": "shared-automation", "evidence_type": "github_actions_reusable_workflow_ref"},
    {"type": "DISCOVERS_CONFIG_IN", "target_name": "shared-automation", "evidence_type": "github_actions_workflow_input_repository"}
  ],
  "consumer_repositories": [
    "terraform-stack-conversation",
    "terraform-stack-external-search",
    "terraform-stack-service-a",
    "terraform-stack-poc-nlp-search",
    "terraform-stack-provisioning",
    "terraform-stack-sitemaps",
    "terraform-stack-wordsmith"
  ]
}
```

### QA Python Consumer Evidence (sample)

```json
{
  "repository": "sample-saved-search-api",
  "repo_id": "repository:r_0a6c413b",
  "evidence_kinds": ["repository_reference", "hostname_reference"],
  "matched_values": ["sample-service-api", "sample-service-api.qa.example.com"],
  "sample_paths": ["sample-saved-search.ts", "config/local.json", "server/init/plugins/sample-service-api-batch.js"]
}
```

### Go E2E Consumer Evidence (what exists today)

```json
// Via repository context / provisioning-style evidence — useful, but not equivalent
// to service-consumer evidence
[
  {"consumer": "terraform-stack-conversation", "consumer_id": "repository:r_4a0e4b73"},
  {"consumer": "terraform-stack-external-search", "consumer_id": "repository:r_a6d882eb"},
  "... 5 more terraform-stack-* repos"
]
```

### GitOps Evidence Proven By Repo Inspection

```json
{
  "service_repo": {
    "hostnames": [
      {"path": "config/qa.json", "value": "sample-service-api.qa.example.com"},
      {"path": "config/production.json", "value": "sample-service-api.production.example.com"}
    ],
    "spec_route": {"path": "server/init/plugins/spec.js", "spec_file": "specs/index.yaml", "docs_route": "/_specs"}
  },
  "helm_charts_repo": {
    "argocd_overlay": "argocd/sample-service-api/overlays/env-qa",
    "image_repository": "111122223333.dkr.ecr.us-east-1.amazonaws.com/sample-service-api",
    "service_hostname": "sample-service-api.qa.svc.example.test",
    "base_resources": ["xirsarole.yaml", "dashboard-overview.yaml", "folder.yaml"]
  }
}
```

### ArgoCD Evidence (QA service narrative)

```json
{
  "argocd_applicationsets": [
    {
      "app_name": "sample-service-api",
      "project": "{{.argocd.project}}",
      "dest_namespace": "{{.helm.namespace}}",
      "source_repos": "https://github.com/example-org/deployment-charts",
      "source_roots": "argocd/sample-service-api/"
    },
    {
      "app_name": "dashboard-sample-service-api",
      "project": "monitoring",
      "dest_namespace": "monitoring",
      "source_repos": "https://github.com/example-org/deployment-charts",
      "source_roots": "argocd/sample-service-api/"
    }
  ]
}
```

---

## Appendix: E2E Validation Results — Post-Implementation (2026-04-19)

### Pipeline Health

Full 896-repo fresh ingestion on the E2E test machine after pulling
`codex/go-data-plane-architecture` branch (commit `7e58a11b`).
Clean start: `docker compose down -v && docker compose up --build -d`.

| Metric | Value |
|---|---|
| Repos indexed | 896 |
| Queue succeeded | 6,641 |
| Queue failed | 0 |
| Deadlocks | 0 |
| EntityNotFound errors | 0 |
| Overall status | healthy, queue fully drained |

### P0 Scorecard

| # | P0 Item | Pre-Implementation (commit `bbbdf144`) | Post-Implementation (commit `7e58a11b`) | Status |
|---|---|---|---|---|
| 1 | `resolve_entity` contract/Cypher | HTTP 500 Neo4j syntax error (trailing comma) | **200** — 14 entities returned (Repository, Workload, K8sResource, ArgoCDApplicationSet across 3 repos) | **FIXED** |
| 2 | `get_service_story` / `get_service_context` qualified-ID | HTTP 404 → fixed to 200 in prior commit | 200 — story + dependencies + infra | **Fixed** (prior commit) |
| 3 | `search_file_content` cross-repo | HTTP 400 "repo_id is required" | **200** — 10 cross-repo results across multiple repos | **FIXED** |
| 4a | `find_code` without repo_id | HTTP 400 "repo_id is required" | **200** — 10 results including `@dmm/node-service-a-client` package refs in 6 consumer repos | **FIXED** |
| 4b | `analyze_code_relationships` Cypher | HTTP 500 Neo4j syntax error | HTTP 404 "entity not found" — Cypher fixed, but entity lookup by unqualified name still fails | Partial |
| 5 | `find_infra_resources` semantics | 1 workload stub → fixed to 4 infra entities in prior commit | 4 results (3 K8sResource + 1 ArgoCDApplicationSet) | **Fixed** (prior commit) |
| 6 | `get_repo_summary` name lookup | HTTP 404 → fixed in prior commit | 200 — 537 files, 7 consumers, deployment artifacts | **Fixed** (prior commit) |
| 7 | K8s resource `kind` field | Empty | Still empty on all results | Open |
| 8 | Entry points = script mains | Script main functions shown | Script mains in `get_service_context`; but `trace_deployment_chain` now returns 19 API endpoints + docs routes | **Mostly fixed** |

**Result: 6 of 8 P0 items fully resolved. 1 partially fixed. 1 open.**

### Critical New Capability: `trace_deployment_chain`

Previously timed out. Now returns a 440KB structured response with:

**Hostnames extracted:**

```json
[
  {"hostname": "node-service-a.prod.bgrp.io", "environment": "prod",
   "reason": "content_hostname_reference", "relative_path": "config/production.json"},
  {"hostname": "node-service-a.qa.bgrp.io", "environment": "qa",
   "reason": "content_hostname_reference", "relative_path": "config/qa.json"}
]
```

**Environments detected:** prod, qa, test

**API surface:** 19 endpoints, docs route `/_specs`

**Workflow provenance:** `manual-deploy.yml`, `pr-ci-dispatch.yml`,
`pr-command-dispatch.yml`

**Delivery paths:** 339 paths across controller, workflow, and IaC evidence

This is a major step toward QA parity. QA returned 21 endpoints; E2E now
returns 19. Hostnames and environments are now surfaced through content
analysis — these were completely absent before.

### Cross-Repo Search Now Works

`search_file_content(pattern="node-service-a")` returns matches across repos
without requiring `repo_id`:

| Repo ID | File | Language |
|---|---|---|
| `repository:r_02414fbe` | `src/utils/urlHelpers/catalog.js` | javascript |
| `repository:r_02586545` | `api-node-salesforce-sync.ts` | typescript |
| `repository:r_02586545` | `server/handlers/party/{id}/listings.js` | javascript |
| `repository:r_0e3f1089` | `node-service-c.ts` | typescript |
| `repository:r_0e3f1089` | `server/resources/listing/listing-util.js` | javascript |

### `find_code` Cross-Repo Entity Search Now Works

`find_code(query="node-service-a")` returns code entities across repos:

| Entity | Repo | Labels |
|---|---|---|
| `@dmm/node-service-a-client` | node-service-a | Variable (package.json) |
| `@dmm/node-service-a-client` | api-node-conversation | Variable |
| `@dmm/node-service-a-client` | node-service-b | Variable |
| `@dmm/node-service-a-client` | api-node-external-search | Variable |
| `@dmm/node-service-a-client` | api-node-provisioning-indexer | Variable |
| `@dmm/node-service-a-client` | api-node-platform | Variable |
| `node-service-a` | helm-charts | K8sResource (xirsarole) |
| `node-service-a` | iac-eks-argocd | ArgoCDApplicationSet |

This is service-consumer evidence via package dependency — 6 repos depend on
the `@dmm/node-service-a-client` package. This is a different consumer signal
than QA's hostname-matching consumers, but equally valid for dependency mapping.

### Comparison: Pre vs Post Implementation

| Capability | Pre-Implementation | Post-Implementation | QA Python |
|---|---|---|---|
| Entity resolution | Broken (500) | **14 entities across 3 repos** | Works |
| Cross-repo file search | Broken (400) | **10 matches across repos** | 50+ matches |
| Cross-repo code search | Broken (400) | **10 entities across 8 repos** | Works |
| Deployment trace | Timed out | **440KB: hostnames, API surface, environments, 339 delivery paths** | Works |
| Hostnames | None | **2 real hostnames** (prod + qa) | 2 hostnames |
| API surface | None | **19 endpoints, /_specs** | 21 endpoints |
| Environments | None | **3 (prod, qa, test)** | 2 (qa, production) |
| Service instances | None | Still 0 | 2 |
| Consumer repos (package) | None | **6 package consumers** | N/A (different signal) |
| Consumer repos (hostname) | 7 terraform-stack | 7 terraform-stack | 12 (hostname + name) |
| Framework detection | None | None | Hapi/React |
| Topology narrative | None | None | Rich narrative |

### Remaining Gaps

1. **`analyze_code_relationships`** — Cypher syntax fixed but entity lookup by
   unqualified name returns 404. Needs qualified entity ID or scoped lookup.
2. **K8s resource `kind` field** — Empty on all `find_infra_resources` results.
   The `kind` (Deployment, Service, ConfigMap, etc.) is not being projected.
3. **Service instances** — `instances: []` in all service paths. Workload
   instance materialization not yet implemented (P1 scope).
4. **Hostname consumers** — Cross-repo search works, but hostname-aware consumer
   discovery (finding repos that reference `node-service-a.qa.bgrp.io`) is not
   yet wired into the service consumer path (P2 scope).
5. **`trace_deployment_chain` hostname noise** — Returns some false-positive
   hostnames (e.g., `node-service-a.ts` from `tsup.config.ts`). Hostname
   extraction needs filtering for actual network endpoints vs file references.

---

## Appendix: E2E Validation Results (2026-04-18)

### Pipeline Health

Full 896-repo fresh ingestion on the E2E test machine (`ubuntu@10.208.198.57`)
after pulling `codex/go-data-plane-architecture` branch (commit `bbbdf144`).
Clean start: `docker compose down -v && docker compose up --build -d`.

| Metric | Value |
|---|---|
| Repos indexed | 896 |
| Queue succeeded | 6,641 |
| Queue failed | 0 |
| Deadlocks | 0 |
| EntityNotFound errors | 0 |
| Bootstrap wall time | 30.3 minutes |
| Largest repo | `php-large-repo-b` — 534,573 facts, 483s projection |
| Overall status | healthy, queue fully drained |
| Cross-repo resolutions completed | 383 |
| Code call projection cycles | 322 |

**Bootstrap completion evidence (raw log):**

```json
{"timestamp":"2026-04-18T21:53:01.840346702Z","severity_text":"INFO",
 "message":"bootstrap projection complete","items_projected":896,
 "workers":8,"total_duration_seconds":1818.014153474,
 "pipeline_phase":"projection"}

{"timestamp":"2026-04-18T21:53:01.911020795Z","severity_text":"INFO",
 "message":"bootstrap pipeline complete","total_seconds":1818.084830497,
 "overlap_seconds":1498.196585304,"pipeline_phase":"projection"}
```

**Reducer queue evidence (`get_index_status` response):**

```json
{"repository_count":896,"status":"healthy",
 "queue":{"succeeded":6641,"failed":0,"pending":0,"in_flight":0,
          "dead_letter":0,"retrying":0,"overdue_claims":0,
          "oldest_outstanding_age":0},
 "scope_activity":{"active":896,"changed":0,"unchanged":896},
 "reasons":["no outstanding queue backlog"]}
```

**Error counters from full resolution-engine log scan:**

| Counter | Value |
|---|---|
| `"status":"failed"` | 0 |
| `DeadlockDetected` | 0 |
| `EntityNotFound` | 0 |

### P0 Items — Fixed

| # | Tool / Behaviour | ADR Baseline | E2E Result |
|---|---|---|---|
| 6 | `get_repo_summary("sample-service-api")` (name-based lookup) | HTTP 404 "repository not found" | **200** — 537 files, 7 consumers, deployment artifacts, 5 languages |
| 5 | `find_infra_resources("sample-service-api")` | 1 workload stub | **4 results**: 3 K8sResource + 1 ArgoCDApplicationSet |
| 2 | `get_service_story("workload:sample-service-api")` qualified-ID path | HTTP 404 "service not found" | **200** — story, deployment overview, kind=service |
| 2 | `get_service_context("workload:sample-service-api")` qualified-ID path | Not reachable with qualified ID | **200** — dependencies (DEPLOYS_FROM, DISCOVERS_CONFIG_IN), 1 K8sResource infra |
| — | `get_repository_coverage(repo_id)` contract | Only entity_count, file_count, languages | **Full contract**: completeness_state, content_gap_count, graph_gap_count, content_last_indexed_at, graph_available, server_content_available, full summary sub-object — matches QA |

### P0 Items — Still Broken

| # | Tool / Behaviour | ADR Baseline | E2E Result | Root Cause |
|---|---|---|---|---|
| 1 | `resolve_entity("sample-service-api")` | HTTP 400 contract mismatch (`query` vs `name`) | HTTP 500 Neo4j syntax error (trailing comma in Cypher) | Cypher query builder emits trailing comma after last RETURN column |
| 3 | `search_file_content(pattern="sample-service-api")` cross-repo | HTTP 400 "repo_id is required" | Repo-scoped search works, but cross-repo search still returns HTTP 400 "repo_id is required" | Tool schema exposes `repo_ids` (array) but the omitted-repo any-repo path is not wired through the handler/query surface |
| 4 | `find_code(query="sample-service-api")` without repo_id | HTTP 500 Neo4j syntax errors | HTTP 400 "repo_id is required" | Same field-name mismatch as search_file_content; MCP dispatch passes `repo_ids` but handler reads `repo_id` |
| 4 | `analyze_code_relationships(find_callers, "search")` | HTTP 500 Neo4j syntax errors | Same — HTTP 500 invalid comma | Same Cypher builder trailing-comma bug as resolve_entity |
| 4 | `calculate_cyclomatic_complexity("search")` | HTTP 500 Neo4j syntax errors | HTTP 404 "entity not found" | Entity lookup fails without scoped repo_id; possibly also affected by Cypher bug |
| 8 | Entry points are repo `main` functions | Script main functions shown as entry points | Same — entry_points are `scripts/compare-to.js`, `scripts/create-new-version.js`, `scripts/gen-client.js` | Service entrypoint extraction not implemented |
| — | `trace_deployment_chain("sample-service-api")` | Minimal (1 K8s resource, "none") | **Timed out** | Query too expensive or internal deadlock on large graph |

### P1–P7 Enrichment Gaps — Confirmed As Expected

These items were not in P0 scope and remain open as documented in the phase
plan above.

| Phase | Capability | QA MCP Today | Go E2E Today | Status |
|---|---|---|---|---|
| P1 | Service instances and runtime adapters | 2 instances, environment-aware service story | 0 instances, no materialized runtime adapter/platform | Not started |
| P1 | Hostname extraction | `sample-service-api.qa.example.com`, `sample-service-api.production.example.com` | None | Not started |
| P2 | Cross-repo consumer search | 12 consumers (hostname + name matching) | 7 consumers (terraform-stack provisioning only) | Not started |
| P3 | API surface (OpenAPI) | 21 endpoints, v3, /_specs | None | Not started |
| P3 | Network entrypoint mapping | Public + internal entrypoints surfaced in service story | Only repo-centric entry points or none | Not started |
| P4 | Controller/workflow provenance | ArgoCD-centric chain plus richer service synthesis | Controller/workflow evidence exists in repo views but is not elevated into service trace | Not started |
| P5 | compare_environments | Times out on QA side | Returns structured unsupported result because no instances are materialized | Not started |
| P5 | Cloud dependency chain | Partially inferred in QA narrative | Not surfaced as service-facing chain | Not started |
| P7 | Framework detection | Hapi/React | None | Not started |
| P7 | Topology story narrative | Rich multi-section narrative | None | Not started |

### Detailed Tool-By-Tool Comparison

#### `get_index_status()`

```json
{
  "repository_count": 896,
  "status": "healthy",
  "queue": {
    "succeeded": 6641, "failed": 0, "pending": 0,
    "in_flight": 0, "dead_letter": 0
  },
  "scope_activity": { "active": 896, "changed": 0, "unchanged": 896 }
}
```

Verdict: **works, matches operator expectations**.

#### `resolve_entity("sample-service-api")`

```
HTTP 500: Neo4jError: Neo.ClientError.Statement.SyntaxError
  Invalid input ',': expected an expression (line 9, column 1 (offset: 376))
```

The ADR baseline was HTTP 400 (contract mismatch: `query` vs `name`). The
contract mismatch may have been fixed, but a new Cypher builder bug was
introduced — the query now reaches Neo4j but the generated Cypher has a
trailing comma after the last RETURN column.

#### `get_service_story("workload:sample-service-api")`

```json
{
  "service_name": "sample-service-api",
  "story": "Workload sample-service-api (kind: service) is defined in repository sample-service-api. No deployed instances found.",
  "deployment_overview": {
    "environment_count": 0, "instance_count": 0,
    "environments": [], "platforms": []
  }
}
```

Qualified-ID path **fixed** (was 404). Story content is structurally correct
but empty for deployment data — expected until P1 hostname extraction lands.

#### `get_service_context("workload:sample-service-api")`

```json
{
  "id": "workload:sample-service-api",
  "kind": "service",
  "repo_name": "sample-service-api",
  "dependencies": [
    {"type": "DEPLOYS_FROM", "target_name": "shared-automation"},
    {"type": "DISCOVERS_CONFIG_IN", "target_name": "shared-automation"}
  ],
  "infrastructure": [{"name": "sample-service-api", "type": "K8sResource"}],
  "instances": []
}
```

**Fixed.** Structural correctness achieved — dependencies and infra are present.
Missing: instances, hostnames, API surface (P1/P3 scope).

#### `get_workload_context("workload:sample-service-api")`

```json
{
  "entry_points": [
    {"name": "main", "relative_path": "scripts/compare-to.js"},
    {"name": "main", "relative_path": "scripts/create-new-version.js"},
    {"name": "main", "relative_path": "scripts/gen-client.js"}
  ],
  "instances": []
}
```

Entry points are script `main` functions, not service entrypoints. QA returns
21 API endpoints with methods and operation IDs. This is P0 item 8 — still
open.

#### `get_repo_summary("sample-service-api")`

**Fixed** (was 404). Returns 186 KB response with:
- 537 files, 5 languages (json, javascript, yaml, typescript, dockerfile)
- 7 consumers (all terraform-stack-\* provisioning repos)
- deployment_artifacts with config_paths, workflow_artifacts
- infrastructure and infrastructure_overview
- relationship_overview

Missing vs QA: framework_summary (Hapi/React), API surface, hostname-based
consumers, topology_story (all P2–P7).

#### `find_infra_resources("sample-service-api")`

```json
{
  "count": 4,
  "results": [
    {"name": "sample-service-api", "labels": ["K8sResource"]},
    {"name": "sample-service-api", "labels": ["K8sResource"]},
    {"name": "sample-service-api", "labels": ["K8sResource"]},
    {"name": "sample-service-api", "labels": ["ArgoCDApplicationSet"]}
  ]
}
```

**Improved** from 1 workload stub to 4 infra resources. Now returns actual
infrastructure entities. Missing vs QA: kind field is empty on all results,
ConfigMap/GrafanaDashboard/GrafanaFolder/Crossplane XIRSARole not surfaced.

#### `get_repository_coverage("repository:r_472ddee5")`

```json
{
  "completeness_state": "complete",
  "content_gap_count": 0,
  "graph_gap_count": 0,
  "entity_count": 3137,
  "file_count": 537,
  "graph_available": true,
  "server_content_available": true,
  "content_last_indexed_at": "2026-04-18T21:23:40.181576Z",
  "languages": [
    {"language": "json", "file_count": 336},
    {"language": "javascript", "file_count": 139},
    {"language": "yaml", "file_count": 49},
    {"language": "typescript", "file_count": 12},
    {"language": "dockerfile", "file_count": 1}
  ],
  "summary": { "..." }
}
```

**Fully fixed.** Contract now matches QA: completeness_state, gap counts,
timestamps, availability flags, and full summary sub-object. Operators can
trust this to know whether a repo's data is complete.

#### `search_file_content(pattern="sample-service-api")` (cross-repo)

```
HTTP 400: {"detail":"repo_id is required"}
```

Still broken for the QA-style cross-repo workflow. Repo-scoped search works
when `repo_id` is provided, but the omitted-repo any-repo path is not wired
through the handler/query surface. The tool schema exposes `repo_ids` (array),
while the handler and query path still behave as if a single `repo_id` were
required for the working code path.

#### `find_code(query="sample-service-api")`

```
HTTP 400: {"detail":"repo_id is required"}
```

Same field-name mismatch as `search_file_content`. The tool schema uses
`repo_id` (optional) but the Go handler apparently requires it.

#### `analyze_code_relationships(find_callers, "search")`

```
HTTP 500: Neo4jError: Invalid input ','
```

Same trailing-comma Cypher builder bug as `resolve_entity`.

#### `trace_deployment_chain("sample-service-api")`

```
Timed out
```

Was minimal before (1 K8s resource, mapping_mode "none"). Now times out
entirely — the query may be too expensive against the full 896-repo graph,
or it may be hitting an internal lock. Needs investigation.

---

## Appendix: Code Architecture References

### Evidence Discovery Pipeline
- Entry: `go/internal/storage/postgres/ingestion.go:317` — `DiscoverEvidence()` called during commit
- Discovery: `go/internal/relationships/evidence.go:16-194` — Pattern matching against catalog
- Catalog: `go/internal/storage/postgres/ingestion.go:553-586` — Loads all repo aliases
- Storage: `go/internal/storage/postgres/relationship_store.go:134-181` — Upserts to relationship_evidence_facts

### Resolution Pipeline
- Handler: `go/internal/reducer/cross_repo_resolution.go:59-231` — Loads evidence, resolves, writes edges
- Resolver: `go/internal/relationships/resolver.go:62-95` — Confidence aggregation + assertions
- Neo4j: `go/internal/storage/neo4j/canonical_relationships.go:19-61` — Typed edge MERGE

### Query Layer
- Service context: `go/internal/query/entity.go:376-465` — workload fetch plus
  repository-scoped dependency/infrastructure/entry-point enrichment
- Deployment chain: `go/internal/query/impact_trace_deployment.go:18-70` — Neo4j + single-repo content
- Query adapter: `go/internal/query/content_reader.go:163-235` — repo-scoped file/entity search only
- Storage search: `go/internal/storage/postgres/content_store.go:151-205` — supports cross-repo when `repo_id` is omitted

### Untapped Infrastructure
- Trigram index: `content_files_content_trgm_idx` using `gin (content gin_trgm_ops)` — fast cross-repo ILIKE
- Entity search: `content_entities_source_trgm_idx` using `gin (source_cache gin_trgm_ops)`
- Both support cross-repo search when repo_id parameter is omitted
