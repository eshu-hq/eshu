# Query Contract Normalization For Service Surfaces Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `trace_deployment_chain`, `resolve_entity`, and `get_service_story` surface truthful, normalized, story-first contracts without losing the underlying graph-backed deployment evidence.

**Architecture:** Keep the reducer and graph model unchanged unless a direct graph truth bug is discovered during implementation. Fix the remaining issues in the query/read-model layer by introducing explicit normalization helpers for delivery-path rows, controller semantics, repository identity backfill, and story-vs-context response boundaries.

**Tech Stack:** Go, Neo4j query handlers, Postgres-backed content reader, MCP JSON-RPC over HTTP, focused Go unit/integration tests, compose-backed verification.

---

## Current Flow Summary

1. `GET /api/v0/services/{service_name}/story` and MCP `get_service_story` resolve a workload through [`fetchWorkloadContext`](../../../../go/internal/query/entity.go).
2. The workload context is enriched through [`enrichServiceQueryContextWithOptions`](../../../../go/internal/query/service_query_enrichment.go), which adds:
   - query-time hostnames, entrypoints, API surface, network paths
   - consumer repositories and provisioning source chains
   - deployment evidence synthesized from repository artifacts
   - support/documentation/deployment overview fields
3. `trace_deployment_chain` reuses the same enriched workload context, then layers on deployment-source, controller, K8s, image, and provenance fields through [`buildDeploymentTraceResponse`](../../../../go/internal/query/impact_trace_deployment.go).
4. `resolve_entity` builds a flat mixed-entity result set in [`resolveEntity`](../../../../go/internal/query/entity.go), then ranks/dedupes with [`normalizeResolvedEntities`](../../../../go/internal/query/entity_resolve_results.go).

## Root-Cause Summary

- `trace_deployment_chain` is merging two different delivery-path shapes without a strict typed normalizer, so valid rows can still look null-ish.
- `resolve_entity` ranks canonical rows correctly now, but does not backfill repository metadata for top-level `Repository`, `Workload`, and `WorkloadInstance` rows.
- `get_service_story` is returning many context-oriented arrays because [`buildServiceStoryResponse`](../../../../go/internal/query/service_query_enrichment.go) copies large chunks of `workloadContext` instead of publishing a narrow story DTO.
- content-only consumer repository enrichment seeds `repository` with `repo_id` instead of resolving a human-readable name when graph candidates are absent.
- `controller_overview` is using platform names as controller names in [`buildControllerOverview`](../../../../go/internal/query/impact_trace_deployment_controllers.go), which is semantically wrong.

## Edge Cases That Must Be Covered

- service with no explicit controller entity but valid runtime/platform evidence
- service with deployment-source repos plus repository delivery artifacts
- content-only consumer repo evidence with no graph candidate
- `resolve_entity` mixed result sets containing `Repository`, `Workload`, `WorkloadInstance`, and content entities
- service story requests by bare name and qualified workload ID
- controller-backed service with ArgoCD entities so controller normalization does not regress rich controller output
- repository delivery artifacts that intentionally do not have `path` or `kind` fields shared with canonical trace rows

## Files To Touch

- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/impact_trace_deployment.go`
- Modify: `go/internal/query/service_deployment_evidence.go`
- Modify: `go/internal/query/deployment_trace_support_helpers.go`
- Modify: `go/internal/query/impact_trace_deployment_controllers.go`
- Modify: `go/internal/query/entity.go`
- Modify: `go/internal/query/entity_resolve_results.go`
- Create: `go/internal/query/service_story_response.go`
- Create: `go/internal/query/deployment_trace_delivery_paths.go`
- Create: `go/internal/query/entity_resolve_identity_backfill.go`
- Test: `go/internal/query/impact_trace_deployment_test.go`
- Test: `go/internal/query/impact_trace_deployment_argocd_test.go`
- Test: `go/internal/query/entity_resolve_results_test.go`
- Test: `go/internal/query/deployment_trace_support_helpers_test.go`
- Create Test: `go/internal/query/service_story_response_test.go`
- Create Test: `go/internal/query/deployment_trace_delivery_paths_test.go`
- Create Test: `go/internal/query/entity_resolve_identity_backfill_test.go`
- Modify Docs: `docs/docs/reference/http-api.md`
- Modify Docs: `docs/docs/reference/mcp-reference.md`

---

## Chunk 1: Normalize Trace Delivery Paths

### Task 1: Add failing tests for typed delivery-path normalization

**Files:**
- Create: `go/internal/query/deployment_trace_delivery_paths_test.go`
- Test: `go/internal/query/impact_trace_deployment_test.go`

- [ ] **Step 1: Write a failing unit test for mixed delivery-path inputs**

Cover all of these row variants in one normalization helper test:
- `deployment_source`
- `cloud_resource`
- `k8s_resource`
- `image_ref`
- `k8s_relationship`
- `repository_delivery_artifact`

Assert:
- every row has non-empty `type`
- canonical rows keep their expected fields
- artifact rows keep only their relevant fields
- no row is emitted where all of `type`, `target`, `path`, `kind`, `artifact_type`, and `evidence_kind` are empty

- [ ] **Step 2: Write a failing trace response test for null-ish rows**

Extend `go/internal/query/impact_trace_deployment_test.go` with a fixture that currently produces the 3 null-ish delivery paths. Assert the final response contains zero rows that look empty under the MCP/API contract.

- [ ] **Step 3: Run the focused tests to confirm failure**

Run:
```bash
cd go && go test ./internal/query -run 'Test(BuildNormalizedDeliveryPaths|TraceDeploymentChain.*DeliveryPaths)' -count=1
```

- [ ] **Step 4: Implement the normalizer**

Create `go/internal/query/deployment_trace_delivery_paths.go` and move the row-shaping logic there. The helper should:
- normalize all trace delivery rows into a typed contract
- drop rows that carry no meaningful information
- preserve deterministic ordering
- dedupe semantically identical rows across canonical trace rows and repository artifact rows

- [ ] **Step 5: Wire the normalizer into trace response construction**

Replace the current direct merge path in [`buildDeploymentTraceResponse`](../../../../go/internal/query/impact_trace_deployment.go) and [`mergeTraceRepositoryDeliveryPaths`](../../../../go/internal/query/service_deployment_evidence.go) with the new normalization helper.

- [ ] **Step 6: Re-run the focused tests**

Run:
```bash
cd go && go test ./internal/query -run 'Test(BuildNormalizedDeliveryPaths|TraceDeploymentChain.*DeliveryPaths)' -count=1
```

- [ ] **Step 7: Commit**

```bash
git add go/internal/query/deployment_trace_delivery_paths.go \
  go/internal/query/deployment_trace_delivery_paths_test.go \
  go/internal/query/impact_trace_deployment.go \
  go/internal/query/service_deployment_evidence.go \
  go/internal/query/impact_trace_deployment_test.go
git commit -m "Normalize deployment trace delivery paths"
```

---

## Chunk 2: Backfill Canonical Identity In Resolve Entity

### Task 2: Add failing tests for repository metadata backfill

**Files:**
- Create: `go/internal/query/entity_resolve_identity_backfill_test.go`
- Modify: `go/internal/query/entity_resolve_results_test.go`

- [ ] **Step 1: Write a failing test for canonical repository rows**

Assert that a `Repository` match with `id=repository:...` gets:
- `repo_id` set to the same canonical repository ID
- `repo_name` set to the repository name

- [ ] **Step 2: Write a failing test for workload/workload-instance rows**

Assert that:
- a `Workload` result is backfilled from `(:Repository)-[:DEFINES]->(:Workload)`
- a `WorkloadInstance` result is backfilled through `(:WorkloadInstance)-[:INSTANCE_OF]->(:Workload)<-[:DEFINES]-(:Repository)`

- [ ] **Step 3: Run the focused tests to confirm failure**

Run:
```bash
cd go && go test ./internal/query -run 'TestResolveEntity.*(Canonical|Backfill)' -count=1
```

- [ ] **Step 4: Implement graph-backed identity backfill**

Create `go/internal/query/entity_resolve_identity_backfill.go` with a helper that:
- inspects ranked entities after query execution
- backfills `repo_id` / `repo_name` for canonical repository, workload, and workload-instance rows
- leaves content entities untouched when they already have file/repo metadata

- [ ] **Step 5: Wire the helper into resolveEntity**

Call the backfill helper from [`resolveEntity`](../../../../go/internal/query/entity.go) after content metadata enrichment and before final normalization/ranking, or immediately after ranking if that yields cleaner semantics.

- [ ] **Step 6: Re-run the focused tests**

Run:
```bash
cd go && go test ./internal/query -run 'TestResolveEntity.*(Canonical|Backfill)' -count=1
```

- [ ] **Step 7: Commit**

```bash
git add go/internal/query/entity.go \
  go/internal/query/entity_resolve_identity_backfill.go \
  go/internal/query/entity_resolve_identity_backfill_test.go \
  go/internal/query/entity_resolve_results_test.go
git commit -m "Backfill canonical resolve entity repository identity"
```

---

## Chunk 3: Split Story-First And Context-First Service Contracts

### Task 3: Add failing tests for a narrow story response

**Files:**
- Create: `go/internal/query/service_story_response_test.go`
- Create: `go/internal/query/service_story_response.go`
- Modify: `go/internal/query/service_query_enrichment.go`
- Modify: `go/internal/query/entity.go`

- [ ] **Step 1: Write a failing test for story-first shape**

Assert that `buildServiceStoryResponse` returns:
- `service_name`
- `subject`
- `story`
- `story_sections`
- `deployment_overview`
- optional `documentation_overview`
- optional `support_overview`
- `drilldowns`

Assert that it does **not** inline heavy context arrays such as:
- `consumer_repositories`
- `provisioning_source_chains`
- `deployment_evidence`
- `hostnames`
- `entrypoints`
- `network_paths`

- [ ] **Step 2: Write a failing regression test for MCP/API story parity**

Add a handler-level test around `getServiceStory` confirming that bare-name and qualified workload-ID lookups both return the same story-first payload class.

- [ ] **Step 3: Run the focused tests to confirm failure**

Run:
```bash
cd go && go test ./internal/query -run 'Test(ServiceStoryResponse|GetServiceStory)' -count=1
```

- [ ] **Step 4: Implement a dedicated service story DTO builder**

Create `go/internal/query/service_story_response.go` with a single responsibility:
- construct the story-first response contract
- keep story fields small and narrative-oriented
- leave heavy evidence arrays to context and trace routes

- [ ] **Step 5: Wire story handler to the new DTO**

Update [`getServiceStory`](../../../../go/internal/query/entity.go) and [`buildServiceStoryResponse`](../../../../go/internal/query/service_query_enrichment.go) so story routes no longer dump full query context.

- [ ] **Step 6: Re-run the focused tests**

Run:
```bash
cd go && go test ./internal/query -run 'Test(ServiceStoryResponse|GetServiceStory)' -count=1
```

- [ ] **Step 7: Commit**

```bash
git add go/internal/query/service_story_response.go \
  go/internal/query/service_story_response_test.go \
  go/internal/query/service_query_enrichment.go \
  go/internal/query/entity.go
git commit -m "Separate service story and service context contracts"
```

---

## Chunk 4: Normalize Consumer Repository Names And Controller Semantics

### Task 4: Add failing tests for repository-name backfill and controller meaning

**Files:**
- Modify: `go/internal/query/deployment_trace_support_helpers_test.go`
- Modify: `go/internal/query/impact_trace_deployment_argocd_test.go`
- Modify: `go/internal/query/impact_trace_deployment_test.go`
- Modify: `go/internal/query/impact_trace_deployment_controllers.go`

- [ ] **Step 1: Write a failing consumer repository naming test**

Add a test for content-only consumer evidence where the repo is not present in graph provisioning candidates. Assert:
- `repo_id` remains canonical
- `repository` is resolved to a human-readable repo name rather than echoing `repository:r_*`

- [ ] **Step 2: Write a failing controller overview semantics test**

Assert:
- runtime/platform-only evidence does not populate `controllers` with environment/platform names
- `controller_count` reflects actual controller entities or controller-family signals, not platform count
- ArgoCD controller entities still populate `controller_overview.entities`

- [ ] **Step 3: Run the focused tests to confirm failure**

Run:
```bash
cd go && go test ./internal/query -run 'Test(LoadConsumerRepositoryEnrichment|BuildControllerOverview|TraceDeploymentChain.*Controller)' -count=1
```

- [ ] **Step 4: Implement repo-name backfill for content-only consumer rows**

Update [`loadConsumerRepositoryEnrichmentWithLimit`](../../../../go/internal/query/deployment_trace_support_helpers.go) so content-only consumer entries do not default `repository` to the repo ID string. Use graph/content repo metadata lookup to populate a real repo name whenever possible.

- [ ] **Step 5: Implement controller-overview semantics fix**

Update [`buildControllerOverview`](../../../../go/internal/query/impact_trace_deployment_controllers.go) so:
- `controllers` contains actual controller names or entity identifiers
- `controller_kinds` reflects actual controller kinds
- runtime/platform evidence remains represented in `runtime_overview`, not in `controllers`

- [ ] **Step 6: Re-run the focused tests**

Run:
```bash
cd go && go test ./internal/query -run 'Test(LoadConsumerRepositoryEnrichment|BuildControllerOverview|TraceDeploymentChain.*Controller)' -count=1
```

- [ ] **Step 7: Commit**

```bash
git add go/internal/query/deployment_trace_support_helpers.go \
  go/internal/query/deployment_trace_support_helpers_test.go \
  go/internal/query/impact_trace_deployment_controllers.go \
  go/internal/query/impact_trace_deployment_test.go \
  go/internal/query/impact_trace_deployment_argocd_test.go
git commit -m "Normalize controller overview and consumer repository names"
```

---

## Chunk 5: Docs, Compose Proof, MCP Proof

### Task 5: Prove the final contract and document it

**Files:**
- Modify: `docs/docs/reference/http-api.md`
- Modify: `docs/docs/reference/mcp-reference.md`

- [ ] **Step 1: Update docs to match the new story-vs-context boundary**

Document:
- `get_service_story` as a narrow story-first contract
- `get_service_context` as the heavier evidence drill-down
- `trace_deployment_chain` as the full deployment evidence surface with typed delivery-path rows
- `resolve_entity` canonical rows carrying repository identity

- [ ] **Step 2: Run focused query package tests**

Run:
```bash
cd go && go test ./internal/query -count=1
```

- [ ] **Step 3: Run repo hygiene**

Run:
```bash
git diff --check
```

- [ ] **Step 4: Run fresh compose verification from zero**

Run:
```bash
docker-compose down -v --remove-orphans
ESHU_KEEP_COMPOSE_STACK=true ./scripts/verify_relationship_platform_compose.sh
```

- [ ] **Step 5: Validate MCP truth directly against local compose**

Run:
```bash
curl -fsS -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_service_story","arguments":{"workload_id":"service-edge-api"}}}' \
  http://localhost:8081/mcp/message

curl -fsS -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_service_context","arguments":{"workload_id":"service-edge-api"}}}' \
  http://localhost:8081/mcp/message

curl -fsS -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"trace_deployment_chain","arguments":{"service_name":"service-edge-api"}}}' \
  http://localhost:8081/mcp/message

curl -fsS -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"resolve_entity","arguments":{"query":"service-edge-api"}}}' \
  http://localhost:8081/mcp/message
```

- [ ] **Step 6: Record final acceptance proof**

Confirm all of these are true:
- `trace_deployment_chain` returns zero null-ish `delivery_paths`
- `resolve_entity` top canonical rows carry meaningful `repo_id` / `repo_name`
- `get_service_story` is materially smaller than `get_service_context`
- consumer repositories do not render as `repository:r_*` when the repo name is knowable
- `controller_overview.controllers` never contains plain environment/platform values like `modern` unless that is truly the controller identity

- [ ] **Step 7: Commit**

```bash
git add docs/docs/reference/http-api.md docs/docs/reference/mcp-reference.md
git commit -m "Document normalized service query contracts"
```

---

## Verification Plan

Focused package gates:

```bash
cd go && go test ./internal/query -run 'Test(BuildNormalizedDeliveryPaths|TraceDeploymentChain.*DeliveryPaths|ResolveEntity.*Backfill|ServiceStoryResponse|LoadConsumerRepositoryEnrichment|BuildControllerOverview)' -count=1
cd go && go test ./internal/query -count=1
git diff --check
```

Fresh runtime proof:

```bash
docker-compose down -v --remove-orphans
ESHU_KEEP_COMPOSE_STACK=true ./scripts/verify_relationship_platform_compose.sh
```

Direct MCP proof:

- `get_service_story`
- `get_service_context`
- `trace_deployment_chain`
- `resolve_entity`

Truth gate:

- Graph truth must remain unchanged or improve
- MCP and API surfaces must agree where they intentionally share the same DTO
- Story and context surfaces must stop masquerading as each other

## Observability Notes

This work is query-surface normalization, not reducer behavior, so new telemetry should stay minimal unless diagnosis reveals hidden fan-out cost. If helper-level filtering or enrichment introduces expensive extra graph/content lookups, add:

- structured debug logs around resolve-entity identity backfill hit/miss rates
- trace annotations around service query enrichment stages if latency meaningfully changes

## Done Criteria

- All five reported issues are fixed in direct local MCP proof
- Focused query tests and full `./internal/query` pass
- Fresh compose verification passes from zero
- Docs reflect the narrowed story contract and typed trace contract

Plan complete and saved to `docs/superpowers/plans/2026-04-19-query-contract-normalization-for-service-surfaces.md`. Ready to execute?
