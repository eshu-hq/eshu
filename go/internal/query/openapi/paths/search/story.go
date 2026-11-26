// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"github.com/eshu-hq/eshu/go/internal/query/openapi/schema"
)

// ServiceStory is the OpenAPI path fragment documenting the service story
// route. openapi.Spec concatenates it into the published document; keep it
// in lockstep with the handler and docs/public/reference/http-api.md.
const ServiceStory = `
    "/api/v0/services/{service_name}/story": {
      "get": {
        "tags": ["entities"],
        "summary": "Get service story",
        "description": "Returns the one-call service dossier for the service. An ambiguous selector answers 409 with error.code ambiguous; each candidate in error.details.candidates carries next_selector, the argument (service_id) and value to pass next. A candidate with legacy_id true is a leftover name-only workload from the identity change, not the live workload; choose a scoped candidate.",
        "operationId": "getServiceStory",
        "x-scoped-token-support": true,
        "parameters": [
          {"$ref": "#/components/parameters/ServiceName"},
          {"name": "downstream_read_limit", "in": "query", "required": false, "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Indirect-evidence row bound for the dependents and consumer_repositories reads. Absent means the default 25; a value above 100 clamps to 100, and the effective bound is echoed on result_limits.downstream_read_limit and the investigation coverage. Non-numeric or negative values return 400 invalid_argument."},
          {"name": "service_id", "in": "query", "description": "Exact workload or service id selector. When supplied, the route resolves this id before service-name candidates.", "schema": {"type": "string"}},
          {"name": "repo", "in": "query", "description": "Repository selector that separates only services in different repositories; two same-named services in one repository stay ambiguous and need service_id. Accepts canonical repository id, repository name, slug, path, or remote URL.", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "description": "Runtime environment selector used to disambiguate duplicate service names by workload instance environment.", "schema": {"type": "string"}},
          {"name": "evidence_detail", "in": "query", "required": false, "schema": {"type": "string", "enum": ["full", "handles"], "default": "full"}, "description": "How the evidence lists are shaped. full (the HTTP default when absent) returns every list in full. handles keeps every section, count, and top-level status, truth, and freshness field, and projects the bulky lists to identity rows that keep their row-level truth labels (including confidence, relationship_types, consumer_kinds, evidence_kinds, relationship_basis, resolution_mode, candidate_status, missing_relationship, service_anchor_status, and service_anchor_reason; an artifact row keeps resolved_id, the key get_relationship_evidence takes for the rest): deployment_evidence.artifacts and api_surface.endpoints (id, relationship_type, resolved_id and id, path, methods), code_to_runtime_trace segment evidence (each segment keeps its own identity keys, so the image_package digest and SBOM identity ship; a preview of 10 handles where the rows also ship in another section, with truncated, evidence_shipped, and evidence_omitted), upstream_dependencies, hostnames, entrypoints, entrypoint_candidates, network_paths, dependents, consumer_repositories, provisioning_source_chains, cloud_resources, uncorrelated_cloud_resources, investigation repositories, and evidence_graph edges. It drops the content-derived deployment_evidence values, the downstream_consumers row lists (named by graph_dependents_shipped_at and content_consumers_shipped_at), and the evidence_graph nodes no shipped edge touches (node_count, nodes_truncated, nodes_omitted). truth.omissions lists each reduced family with its total. Each total is the story's own pre-cap count and may be a lower bound, because the story carries a flag for only some of the bounds its reads apply; treat a total as exact only for the families the Context evidence budget reference lists as exact. investigation.repositories_with_evidence reports the rows it shipped (at most 50), so it is also a lower bound when repositories_considered ends with the __truncated__ marker. The MCP get_service_story tool defaults to handles. Unknown values return 400 invalid_argument."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Service narrative",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "service_name": {"type": "string", "description": "Canonical service name the story answers. Owner: service.BuildServiceStoryResponse via querycontract.CanonicalServiceName."},
                    "story": {"type": "string", "description": "One-paragraph service narrative rendered from the workload context. Owner: service.BuildServiceStoryResponse via querycontract.BuildWorkloadStoryWithAPISurface."},
                    "story_sections": {"type": "array", "description": "Narrative sections backing the service story: deployment first, then one per populated evidence family (entrypoints, network, api, consumers, dependents, provisioning, deployment evidence). Owner: service/story_overview.go buildServiceStorySectionsWithContext.", "items": {"type": "object", "description": "One narrative section: a title plus a one-line summary. Owner: service/story_overview.go buildServiceStorySectionsWithContext.", "properties": {
                      "title": {"type": "string", "description": "Section title (deployment, entrypoints, network, api, consumers, dependents, provisioning, or a deployment-evidence family). Owner: service/story_overview.go buildServiceStorySectionsWithContext."},
                      "summary": {"type": "string", "description": "One-line human-readable summary of the section. Owner: service/story_overview.go buildServiceStorySectionsWithContext."}
                    }}},
                    "code_to_runtime_trace": {"type": "object", "description": "Code-to-runtime trace: seven ordered segments (service_identity, code_entrypoints, ci_cd, image_package, deployment_config, runtime, cloud_dependency) each with a status, basis, evidence count, and capped evidence rows. Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace.", "properties": {
                      "status": {"type": "string", "enum": ["complete", "partial"], "description": "complete when every segment carries evidence; partial when at least one segment reports missing_evidence. Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace."},
                      "segments": {"type": "array", "description": "Seven ordered trace segments from code identity to runtime. Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace.", "items": {"type": "object", "description": "One trace segment with its status and capped evidence rows. Owner: service/story_trace_path.go serviceTraceSegment.", "properties": {
                        "name": {"type": "string", "description": "Segment name (service_identity, code_entrypoints, ci_cd, image_package, deployment_config, runtime, or cloud_dependency). Owner: service/story_trace_path.go serviceTraceSegment."},
                        "status": {"type": "string", "description": "Segment status; missing_evidence when the segment carries no evidence rows. Owner: service/story_trace_path.go serviceTraceSegment."},
                        "basis": {"type": "string", "description": "Evidence basis behind the segment (for example workload_identity or api_surface_or_entrypoints). Owner: service/story_trace_path.go serviceTraceSegment."},
                        "evidence_count": {"type": "integer", "description": "Evidence rows the segment observed before the cap. Owner: service/story_trace_path.go serviceTraceSegment."},
                        "evidence": {"type": "array", "description": "Capped segment evidence rows; keys vary by segment. Owner: service/story_trace_path.go serviceTraceSegment.", "items": {"type": "object", "additionalProperties": true, "description": "One segment evidence row; keys vary by segment (identity keys, endpoint keys, or config keys). Owner: service/story_trace_path.go serviceTrace*Segment."}},
                        "truncated": {"type": "boolean", "description": "True when the segment evidence rows were capped at the service story item limit. Owner: service/story_trace_path.go serviceTraceSegment."}
                      }}},
                      "missing_segments": {"type": "array", "description": "Names of the segments that reported missing_evidence; empty when status is complete. Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace.", "items": {"type": "string"}},
                      "limit": {"type": "integer", "description": "Per-segment evidence row cap (the service story item limit). Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace."},
                      "ordering": {"type": "string", "description": "Segment ordering contract: code_to_runtime. Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace."}
                    }, "required": ["status", "segments", "missing_segments", "limit", "ordering"]},
                    "ci_cd_evidence": {"type": "object", "description": "Repository-scoped static workflow, live run, and artifact evidence. A file read that returns a sentinel row past the 5,000-file limit adds static_workflow_artifacts.candidate_pool_status=unknown_at_limit and missing_evidence class static_workflow_coverage_unknown. A repository with exactly 5,000 files is a complete scan and adds neither. Zero observed workflows on a truncated scan means state=unknown; positive counts describe the observed page. Owner: repositoryartifacts.LoadRepositoryScopedCICDEvidence.", "properties": {
                      "static_workflow_artifacts": {"type": "object", "description": "Static GitHub Actions workflow artifact evidence. Owner: repositoryartifacts.cicdStaticWorkflowArtifactEvidenceMap.", "properties": {
                        "state": {"type": "string", "description": "Static artifact presence state. Owner: repositoryartifacts.cicdStaticWorkflowArtifactEvidenceMap."},
                        "count": {"type": "integer", "description": "Workflow files observed in the candidate page. Owner: repositoryartifacts.cicdStaticWorkflowArtifactEvidenceMap."}
                      }, "required": ["state", "count"]},
                      "live_run_correlations": {"type": "object", "description": "Live run correlation evidence. Owner: repositoryartifacts.cicdLiveRunCorrelationEvidenceMap.", "properties": {
                        "state": {"type": "string", "description": "Live correlation presence state. Owner: repositoryartifacts.cicdLiveRunCorrelationEvidenceMap."},
                        "count": {"type": "integer", "description": "Live correlations counted. Owner: repositoryartifacts.cicdLiveRunCorrelationEvidenceMap."}
                      }, "required": ["state", "count"]},
                      "run_artifact_evidence": {"type": "object", "description": "Run-to-artifact bridge evidence. Owner: repositoryartifacts.cicdRunArtifactEvidenceMap.", "properties": {
                        "state": {"type": "string", "description": "Bridge presence state. Owner: repositoryartifacts.cicdRunArtifactEvidenceMap."},
                        "count": {"type": "integer", "description": "Bridge rows counted. Owner: repositoryartifacts.cicdRunArtifactEvidenceMap."}
                      }, "required": ["state", "count"]},
                      "missing_evidence": {"type": "array", "description": "Missing-evidence classes (for example static_workflow_coverage_unknown when the file scan hit its limit). Owner: repositoryartifacts.cicdRunCorrelationEvidenceSummaryMap.", "items": {"type": "string"}},
                      "reason": {"type": "string", "description": "Evidence summary reason. Owner: repositoryartifacts.cicdRunCorrelationEvidenceSummaryMap."}
                    }, "required": ["static_workflow_artifacts", "live_run_correlations", "run_artifact_evidence"]},
                    "service_identity": {"type": "object", "description": "Workload identity the story was built from: canonical ids plus the sparse fallback and limitation signals. Owner: service/story_dossier.go buildServiceIdentity.", "properties": {
                      "service_id": {"type": "string", "description": "Canonical graph workload id. Owner: service/story_dossier.go buildServiceIdentity."},
                      "service_name": {"type": "string", "description": "Canonical service name. Owner: service/story_dossier.go buildServiceIdentity."},
                      "kind": {"type": "string", "description": "Workload kind from the graph row. Owner: service/story_dossier.go buildServiceIdentity."},
                      "repo_id": {"type": "string", "description": "Canonical repository id the story resolved to. Owner: service/story_dossier.go buildServiceIdentity."},
                      "repo_name": {"type": "string", "description": "Repository display name. Owner: service/story_dossier.go buildServiceIdentity."},
                      "materialization_status": {"type": "string", "description": "Present only on the repository-read-model fallback path. Owner: service/story_dossier.go buildServiceIdentity."},
                      "query_basis": {"type": "string", "description": "Present only on the repository-read-model fallback path. Owner: service/story_dossier.go buildServiceIdentity."},
                      "limitations": {"type": "array", "description": "Workload-context limitation reasons carried onto the identity; present only when the context carries any. Owner: service/story_dossier.go buildServiceIdentity.", "items": {"type": "string"}}
                    }, "required": ["service_id", "service_name", "kind", "repo_id", "repo_name"]},
                    "api_surface": {"type": "object", "description": "The API surface, shipped once with its endpoint rows (unlike deployment_overview.api_surface, which drops them). deployment_overview.api_surface keeps the counts and names this key in endpoints_shipped_at. Owner: service/story_dossier.go buildServiceDossierAPISurface.", "additionalProperties": true, "properties": {
                      "endpoint_count": {"type": "integer", "description": "Endpoint rows counted (context count, or rows shipped when absent). Owner: service/story_dossier.go buildServiceDossierAPISurface."},
                      "method_count": {"type": "integer", "description": "Method count (context count, or 0 when absent). Owner: service/story_dossier.go buildServiceDossierAPISurface."},
                      "spec_count": {"type": "integer", "description": "API spec count (context count, spec_files length, or spec_paths length). Owner: service/story_dossier.go serviceAPISpecCount."},
                      "endpoints": {"type": "array", "description": "Endpoint rows capped at the service story item limit. Graph rows carry identity keys (id, path, methods); content rows carry spec identity (path, methods, operation_ids, spec_path) or framework identity. Owner: service/story_dossier.go buildServiceDossierAPISurface.", "items": {"type": "object", "additionalProperties": true, "description": "One API endpoint row: graph identity keys or content spec/framework keys; only the keys the serving path observed are present. Owner: service/story_dossier.go buildServiceDossierAPISurface."}},
                      "truncated": {"type": "boolean", "description": "True when the endpoint rows were capped at the service story item limit. Owner: service/story_dossier.go buildServiceDossierAPISurface."},
                      "truth_basis": {"type": "string", "description": "Graph-only. Always graph: the surface was read from materialized EXPOSES_ENDPOINT edges. Owner: repository/api_surface.go buildGraphAPISurface."},
                      "operation_id_count": {"type": "integer", "description": "Operation-id rows counted. Owner: repository/api_surface.go buildGraphAPISurface."},
                      "source_paths": {"type": "array", "description": "Graph-only. Distinct endpoint source paths. Owner: repository/api_surface.go buildGraphAPISurface.", "items": {"type": "string"}},
                      "spec_paths": {"type": "array", "description": "Distinct API spec relative paths. Owner: service/query_enrichment_rows.go buildServiceAPISurface.", "items": {"type": "string"}},
                      "detail_limit": {"type": "integer", "description": "Graph-only. Row bound the endpoint detail read ran under. Owner: repository/api_surface.go buildGraphAPISurface."},
                      "detail_truncated": {"type": "boolean", "description": "Graph-only. True when the endpoint rows were cut at the detail bound. Owner: repository/api_surface.go buildGraphAPISurface."}
                    }, "required": ["endpoint_count", "method_count", "spec_count", "endpoints", "truncated"]},
                    "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The evidence_detail the response was shaped with."},
                    "evidence_detail_drilldown": {"type": "object", "additionalProperties": true, "description": "Present only under handles when a family was reduced: how to fetch the full rows."},
                    "deployment_lanes": {"type": "array", "description": "Deployment lanes grouping runtime instances and deployment artifacts into ecs_terraform or k8s_gitops lanes, sorted by lane_type. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "object", "description": "One deployment lane: its type plus the sorted environment, platform, repository, relationship, and confidence signals behind it. Owner: service/story_dossier.go buildServiceDeploymentLanes serviceLane.", "properties": {
                      "lane_type": {"type": "string", "enum": ["ecs_terraform", "k8s_gitops"], "description": "Lane family derived from platform kind and artifact family. Owner: service/story_dossier.go serviceLaneType."},
                      "environments": {"type": "array", "description": "Sorted environments observed in the lane. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "runtime_platforms": {"type": "array", "description": "Sorted runtime platform names observed in the lane. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "platform_kinds": {"type": "array", "description": "Sorted platform kinds observed in the lane. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "source_repositories": {"type": "array", "description": "Sorted source repository names behind the lane artifacts. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "relationship_types": {"type": "array", "description": "Sorted relationship types behind the lane artifacts. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "resolved_ids": {"type": "array", "description": "Sorted resolved evidence ids behind the lane artifacts. Owner: service/story_dossier.go buildServiceDeploymentLanes.", "items": {"type": "string"}},
                      "max_confidence": {"type": "number", "description": "Highest artifact confidence observed in the lane; present only when a positive confidence was observed. Owner: service/story_dossier.go buildServiceDeploymentLanes."}
                    }, "required": ["lane_type"]}},
                    "upstream_dependencies": {"type": "array", "items": {"type": "object", "description": "One upstream dependency row: a deployment-artifact edge, a repository dependency row, or a provisioning source chain. Owner: service/story_dossier.go serviceUpstreamDependencyRows.", "properties": {
                      "source": {"type": "string", "description": "Upstream source name (artifact source repository, workload name, or chain repository). Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "source_repo_id": {"type": "string", "description": "Upstream source repository id; present on artifact and chain rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "source_repo_canonical_id": {"type": "string", "description": "Canonical id of the upstream source repository; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "source_repo_scope_key": {"type": "string", "description": "Scope key of the upstream source repository; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "target": {"type": "string", "description": "Downstream target name. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "target_repo_id": {"type": "string", "description": "Downstream target repository id; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "target_id": {"type": "string", "description": "Downstream target id; present on repository dependency rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "relationship_type": {"type": "string", "description": "Edge or row relationship type (for example DEPLOYS_FROM or PROVISIONING_SOURCE_CHAIN). Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "resolved_id": {"type": "string", "description": "Resolved evidence id; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "confidence": {"type": "number", "description": "Row confidence. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "evidence_count": {"type": "integer", "description": "Supporting evidence rows; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "rationale": {"type": "string", "description": "Human-readable row rationale; present on artifact rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows."},
                      "modules": {"type": "array", "description": "Provisioning modules; present on provisioning source chain rows. Owner: service/story_dossier.go serviceUpstreamDependencyRows.", "items": {"type": "string"}}
                    }}, "description": "One row per upstream edge behind the service: incoming deployment-artifact edges (DEPLOYS_FROM, PROVISIONS_DEPENDENCY_FOR and siblings, keyed by resolved_id), repository dependency rows (READS_CONFIG_FROM and siblings), and provisioning source chains (relationship_type PROVISIONING_SOURCE_CHAIN). Sorted by relationship_type, then source. The summary dependency count (result_limits.upstream_count) counts all three row kinds together. Owner: service/story_dossier.go buildServiceUpstreamDependencies."},
                    "downstream_consumers": {"type": "object", "description": "Downstream consumer rollup: graph dependents and content consumers rendered under the downstream render bound, with counts and the truncation signal. Owner: service/story_dossier.go BuildServiceDownstreamConsumers.", "properties": {
                      "graph_dependent_count": {"type": "integer", "description": "Graph dependent rows read. Owner: service/story_dossier.go BuildServiceDownstreamConsumers."},
                      "content_consumer_count": {"type": "integer", "description": "Content consumer rows read. Owner: service/story_dossier.go BuildServiceDownstreamConsumers."},
                      "graph_dependents": {"type": "array", "description": "Capped graph dependent rows. Owner: service/story_dossier.go BuildServiceDownstreamConsumers.", "items": {"type": "object", "additionalProperties": true, "description": "One graph dependent row; keys follow the dependents row shape. Owner: service/story_dossier.go BuildServiceDownstreamConsumers."}},
                      "content_consumers": {"type": "array", "description": "Capped content consumer rows. Owner: service/story_dossier.go BuildServiceDownstreamConsumers.", "items": {"type": "object", "additionalProperties": true, "description": "One content consumer row; keys follow the consumer_repositories row shape. Owner: service/story_dossier.go BuildServiceDownstreamConsumers."}},
                      "truncated": {"type": "boolean", "description": "True when a rendered list was capped or an upstream read was truncated. Owner: service/story_dossier.go BuildServiceDownstreamConsumers."}
                    }, "required": ["graph_dependent_count", "content_consumer_count", "graph_dependents", "content_consumers", "truncated"]},
                    "evidence_graph": {"type": "object", "description": "Bounded evidence graph backing the story: service, repository, runtime, and downstream nodes plus deployment-artifact and RUNS_AS edges. Owner: service/story_evidence_graph.go buildServiceEvidenceGraph.", "properties": {
                      "nodes": {"type": "array", "description": "Deduplicated evidence nodes (service, source repository, deployment configuration, runtime instances, downstream consumers). Owner: service/story_evidence_graph.go buildServiceEvidenceGraph.", "items": {"type": "object", "description": "One evidence node. Owner: service/story_evidence_graph.go addEvidenceNode.", "properties": {
                        "id": {"type": "string", "description": "Node identity. Owner: service/story_evidence_graph.go addEvidenceNode."},
                        "label": {"type": "string", "description": "Node display label. Owner: service/story_evidence_graph.go addEvidenceNode."},
                        "kind": {"type": "string", "description": "Node kind (service, repository, or runtime). Owner: service/story_evidence_graph.go addEvidenceNode."},
                        "category": {"type": "string", "description": "Node category (service, source, deployment, runtime, or downstream). Owner: service/story_evidence_graph.go addEvidenceNode."},
                        "role": {"type": "string", "description": "Node role (workload, source_repository, deployment_configuration, runtime_instance, or downstream_consumer). Owner: service/story_evidence_graph.go addEvidenceNode."}
                      }, "required": ["id", "label", "kind", "category", "role"]}},
                      "edges": {"type": "array", "description": "Deduplicated evidence edges (deployment-artifact edges plus service RUNS_AS instance edges), sorted by relationship_type. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges.", "items": {"type": "object", "description": "One evidence edge. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges.", "properties": {
                        "id": {"type": "string", "description": "Edge identity (resolved id, or derived visualization edge id for RUNS_AS). Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "source": {"type": "string", "description": "Edge source identity. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "target": {"type": "string", "description": "Edge target identity. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "relationship_type": {"type": "string", "description": "Edge relationship type (deployment-artifact type or RUNS_AS). Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "confidence": {"type": "number", "description": "Edge confidence; present on deployment-artifact edges. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "evidence_count": {"type": "integer", "description": "Supporting evidence rows; present on deployment-artifact edges. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "rationale": {"type": "string", "description": "Edge rationale; present on deployment-artifact edges. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."},
                        "resolved_id": {"type": "string", "description": "Resolved evidence id; present on deployment-artifact edges. Owner: service/story_evidence_graph.go serviceEvidenceGraphEdges."}
                      }, "required": ["id", "source", "target", "relationship_type"]}},
                      "edge_count": {"type": "integer", "description": "Edges observed before the cap. Owner: service/story_evidence_graph.go buildServiceEvidenceGraph."},
                      "truncated": {"type": "boolean", "description": "True when the edge rows were capped at the service story item limit. Owner: service/story_evidence_graph.go buildServiceEvidenceGraph."}
                    }, "required": ["nodes", "edges", "edge_count", "truncated"]},
                    "result_limits": {"type": "object", "description": "Additive drilldown block for the story dossier: bounded limit, deterministic ordering, fan-out counts, truncation flag, and the next prompt tool plus context path. downstream_read_limit reports the tighter bound underneath the downstream lists; downstream_render_limit reports the cap the downstream lists actually rendered under; evidence_file_read_limit reports the separate 5,000-file bound on the service repository's own indexed-file read. truncated is true when a rendered list was capped, an upstream read was truncated, the infrastructure read landed past its bound, or a deployment-artifact bound named in limitations bit. Owner: service/story_dossier.go buildServiceResultLimitsWithContext.", "properties": {
                      "limit": {"type": "integer", "description": "Per-list rendering cap (the service story item limit). Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "downstream_read_limit": {"type": "integer", "description": "Tighter bound underneath the downstream lists. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "downstream_render_limit": {"type": "integer", "description": "Cap the downstream lists actually rendered under. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "evidence_file_read_limit": {"type": "integer", "description": "File bound on the service repository's own indexed-file read. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "ordering": {"type": "string", "description": "Deterministic ordering contract for the dossier lists. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "endpoint_count": {"type": "integer", "description": "Endpoint rows counted. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "upstream_count": {"type": "integer", "description": "Upstream dependency rows counted. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "downstream_count": {"type": "integer", "description": "Deprecated sum alias of downstream_entries, kept for one minor version. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "downstream_entries": {"type": "integer", "description": "Sum of the dependents and consumer_repositories list lengths. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "downstream_repository_count": {"type": "integer", "description": "Distinct repositories across the two downstream lists. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "lower_bound": {"type": "boolean", "description": "True when a downstream read bound fired, so the downstream counts are a lower bound. Owner: service/story_dossier.go downstreamBoundFired."},
                      "truncated": {"type": "boolean", "description": "True when a rendered list was capped, an upstream read was truncated, or an infrastructure or deployment-artifact bound fired. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "drilldown_basis": {"type": "string", "description": "Drilldown identity basis (resolved_id). Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "relationship_tool": {"type": "string", "description": "Prompt tool for relationship drilldown (get_relationship_evidence). Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "service_context_path": {"type": "string", "description": "HTTP path of the sibling service context route. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "endpoint_list_tool": {"type": "string", "description": "Prompt tool paging the full ordered endpoint set. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "endpoint_list_path": {"type": "string", "description": "HTTP path paging the full ordered endpoint set. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "consumer_list_tool": {"type": "string", "description": "Prompt tool returning the full keyset-ordered consumer set. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."},
                      "consumer_list_path": {"type": "string", "description": "HTTP path returning the full keyset-ordered consumer set. Owner: service/story_dossier.go buildServiceResultLimitsWithContext."}
                    }, "required": ["limit", "downstream_read_limit", "downstream_render_limit", "evidence_file_read_limit", "ordering", "endpoint_count", "upstream_count", "downstream_count", "downstream_entries", "downstream_repository_count", "lower_bound", "truncated", "drilldown_basis", "relationship_tool", "service_context_path", "endpoint_list_tool", "endpoint_list_path", "consumer_list_tool", "consumer_list_path"]},
                    "investigation": {"type": "object", "description": "Service investigation packet: repositories considered, evidence families found, coverage, per-family findings, and recommended next calls. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext.", "properties": {
                      "service_name": {"type": "string", "description": "Canonical service name the investigation answers. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."},
                      "repositories_considered": {"type": "array", "description": "Every repository the investigation merged, capped at the service story item limit. Past the cap a {repo_id __truncated__, note} marker row closes the list instead of a repository row. Owner: service/investigation_packet.go serviceInvestigationRepositories.", "items": {"type": "object", "description": "One considered repository (or the __truncated__ marker row past the cap, which carries only repo_id and note). Owner: service/investigation_packet.go serviceInvestigationRepositories addInvestigationRepo.", "properties": {
                        "repo_id": {"type": "string", "description": "Repository canonical id, or __truncated__ on the marker row. Owner: service/investigation_packet.go addInvestigationRepo."},
                        "repo_name": {"type": "string", "description": "Repository display name; absent on the marker row. Owner: service/investigation_packet.go addInvestigationRepo."},
                        "roles": {"type": "array", "description": "Repository roles in this investigation (service_owner, deployment_source, and related kinds); absent on the marker row. Owner: service/investigation_packet.go addInvestigationRepo.", "items": {"type": "string"}},
                        "evidence_families": {"type": "array", "description": "Evidence families observed for the repository; absent on the marker row. Owner: service/investigation_packet.go addInvestigationRepo.", "items": {"type": "string"}},
                        "evidence_family_count": {"type": "integer", "description": "Number of evidence families observed for the repository; absent on the marker row. Owner: service/investigation_packet.go addInvestigationRepo."},
                        "note": {"type": "string", "description": "Present only on the marker row: the truncation note. Owner: service/investigation_packet.go serviceInvestigationRepositories."}
                      }}},
                      "repositories_with_evidence": {"type": "array", "description": "Repositories that carry at least one evidence family. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext.", "items": {"type": "object", "additionalProperties": true, "description": "One repository with evidence; keys follow the repositories_considered row shape. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."}},
                      "evidence_families_found": {"type": "array", "description": "Sorted evidence families observed (api_surface, deployment_lanes, documentation, upstream_dependencies, downstream_consumers, support). Owner: service/investigation_packet.go serviceInvestigationEvidenceFamiliesWithContext.", "items": {"type": "string"}},
                      "coverage_summary": {"type": "object", "additionalProperties": true, "description": "Coverage verdict over the considered repositories (state, reason, and counts); keys vary with the verdict. Owner: service/investigation_packet.go serviceInvestigationCoverage."},
                      "investigation_findings": {"type": "array", "description": "One finding per evidence family found. Owner: service/investigation_packet.go serviceInvestigationFindingsWithContext.", "items": {"type": "object", "description": "One evidence-family finding. Owner: service/investigation_packet.go serviceInvestigationFindingsWithContext.", "properties": {
                        "family": {"type": "string", "description": "Evidence family this finding summarizes. Owner: service/investigation_packet.go serviceInvestigationFindingsWithContext."},
                        "summary": {"type": "string", "description": "One-line family summary. Owner: service/investigation_packet.go serviceInvestigationFamilySummaryWithContext."},
                        "evidence_path": {"type": "string", "description": "Dossier key carrying the family evidence. Owner: service/investigation_packet.go serviceInvestigationEvidencePath."}
                      }, "required": ["family", "summary", "evidence_path"]}},
                      "recommended_next_calls": {"type": "array", "description": "Bounded follow-up calls (tool, reason, arguments). Owner: service/investigation_packet.go serviceInvestigationNextCalls.", "items": {"type": "object", "additionalProperties": true, "description": "One recommended follow-up call. Owner: service/investigation_packet.go serviceInvestigationNextCalls."}},
                      "service_story_path": {"type": "string", "description": "HTTP path of the service story route. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."},
                      "service_context_path": {"type": "string", "description": "HTTP path of the sibling service context route. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."},
                      "environment": {"type": "string", "description": "Present only when the investigation was scoped to an environment. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."},
                      "intent": {"type": "string", "description": "Present only when the investigation carries an intent. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."},
                      "question": {"type": "string", "description": "Present only when the investigation carries a question. Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext."}
                    }, "required": ["service_name", "repositories_considered", "repositories_with_evidence", "evidence_families_found", "coverage_summary", "investigation_findings", "recommended_next_calls", "service_story_path", "service_context_path"]},
                    "deployment_overview": {"type": "object", "description": "Deployment summary counts. It does not repeat the hostnames and entrypoints arrays, which ship once at the top level; its api_surface keeps the counts but not the endpoint rows (endpoints_shipped_at names where they are). Owner: service/story_overview.go buildServiceDeploymentOverviewWithContext via dropStoryOverviewEndpoints.", "properties": {
                      "instance_count": {"type": "integer", "description": "Number of materialized workload instances."},
                      "environment_count": {"type": "integer", "description": "Number of distinct materialized environments."},
                      "materialized_environment_count": {"type": "integer", "description": "Number of distinct materialized environments (same signal as environment_count)."},
                      "config_environment_count": {"type": "integer", "description": "Number of distinct observed config environments."},
                      "platform_count": {"type": "integer", "description": "Number of distinct platforms."},
                      "platforms": {"type": "array", "items": {"type": "string"}, "description": "Distinct sorted platform names across instances."},
                      "environments": {"type": "array", "items": {"type": "string"}, "description": "Distinct materialized environments."},
                      "materialized_environments": {"type": "array", "items": {"type": "string"}, "description": "Distinct materialized environments (same signal as environments)."},
                      "deployment_truth_tier": {"type": "string", "description": "Classified deployment-truth tier; present only when a tier classifies."},
                      "config_environments": {"type": "array", "items": {"type": "string"}, "description": "Observed config environments; present only when at least one was observed."},
                      "hostname_count": {"type": "integer", "description": "Hostname rows read; present only when hostnames were observed."},
                      "entrypoint_count": {"type": "integer", "description": "Entrypoint rows read; present only when entrypoints were observed."},
                      "network_path_count": {"type": "integer", "description": "Network-path rows read; present only when network paths were observed."},
                      "api_surface": {"type": "object", "description": "API-surface counts without the endpoint rows (the story route drops them via dropStoryOverviewEndpoints); endpoints_shipped_at names where the rows ship. The normalized count keys are always present; any other keys pass through from the workload context map.", "additionalProperties": true, "properties": {
                        "endpoints_shipped_at": {"type": "string", "description": "Top-level key carrying the endpoint rows (api_surface.endpoints). Always api_surface.endpoints on this route."},
                        "endpoint_count": {"type": "integer", "description": "Endpoint rows counted (context count, or rows shipped when absent)."},
                        "method_count": {"type": "integer", "description": "Method count (context count, or 0 when absent)."},
                        "spec_count": {"type": "integer", "description": "API spec count (context count, spec_files length, or spec_paths length)."},
                        "truncated": {"type": "boolean", "description": "True when the endpoint rows were capped."}
                      }},
                      "dependent_count": {"type": "integer", "description": "Graph-derived dependent repositories; present only when dependents were observed."},
                      "consumer_repository_count": {"type": "integer", "description": "Consumer repositories; present only when consumers were observed."},
                      "provisioning_source_chain_count": {"type": "integer", "description": "Provisioning source chains; present only when chains were observed."},
                      "cloud_resource_count": {"type": "integer", "description": "Materialized cloud resources; present only when resources were observed."},
                      "cloud_relationship_basis": {"type": "string", "description": "Relationship basis for the cloud resources (materialized workload-cloud relationship, or mixed)."},
                      "uncorrelated_cloud_resource_count": {"type": "integer", "description": "Cloud-resource candidates without a reducer-owned workload relationship; present only when candidates were observed."},
                      "missing_cloud_relationship": {"type": "string", "description": "Names the missing workload_cloud_relationship when uncorrelated candidates exist."},
                      "deployment_tool_families": {"type": "array", "items": {"type": "string"}, "description": "Deployment tool families observed in deployment evidence."},
                      "deployment_evidence_artifact_count": {"type": "integer", "description": "Artifact rows counted in deployment evidence."},
                      "delivery_path_count": {"type": "integer", "description": "Delivery paths counted in deployment evidence."},
                      "delivery_workflow_count": {"type": "integer", "description": "Delivery workflows counted in deployment evidence."},
                      "shared_config_path_count": {"type": "integer", "description": "Shared config paths counted in deployment evidence."}
                    }},
                    "limitations": {"type": "array", "description": "Explicit reasons the service evidence is partial, present only when one applies. deployment_artifacts_candidates_budget_reached means the request budget of 12,000 config-artifact candidates beyond the per-repository floor of 50 ran out, so the deployment_evidence config rows of at least one related repository are a lower bound; deployment_artifacts_related_file_list_truncated_at_5000 means a related repository has more than 5,000 files, so its candidates past the window are unknown; deployment_artifacts_anchor_file_list_truncated_at_5000 means the service repository itself has more than 5,000 files, so the deployment evidence read from its file list is a lower bound; dependents_truncated, consumer_repositories_truncated and provisioning_source_chains_truncated mean the upstream indirect-evidence read feeding that list hit the downstream_read_limit bound. Each folds into result_limits.truncated and answer_metadata.truncated.", "items": {"type": "string"}},
                    "hostnames": {"type": "array", "description": "Exact hostnames found in service evidence, carried from the workload context and capped at the downstream render bound. Owner: service/query_enrichment_rows.go buildServiceHostnameRows over service HostnameEvidence.", "items": {"type": "object", "description": "One exact hostname observed in service evidence. Owner: service/query_enrichment_rows.go buildServiceHostnameRows over service HostnameEvidence.", "properties": {
                      "hostname": {"type": "string", "description": "Exact hostname found in service evidence."},
                      "environment": {"type": "string", "description": "Environment inferred for the hostname; empty when none was inferred."},
                      "relative_path": {"type": "string", "description": "Repository-relative file the hostname was observed in."},
                      "reason": {"type": "string", "description": "Why the hostname counts as service evidence."}
                    }}},
                    "entrypoint_candidates": {"type": "array", "description": "Dotted hostname-shaped candidates kept as non-entrypoint supporting evidence when rejected as config keys or field paths, or when ambiguous without stronger hostname evidence. Carried from the workload context. Owner: service/query_enrichment_rows.go buildServiceEntrypointCandidateRows.", "items": {"type": "object", "description": "One rejected or ambiguous hostname-shaped candidate. Owner: service/query_enrichment_rows.go buildServiceEntrypointCandidateRows over service EntrypointCandidateEvidence.", "properties": {
                      "candidate": {"type": "string", "description": "Hostname-shaped candidate value that must not become a public entrypoint."},
                      "classification": {"type": "string", "description": "Why the candidate was rejected or left ambiguous."},
                      "relative_path": {"type": "string", "description": "Repository-relative file the candidate was observed in."},
                      "reason": {"type": "string", "description": "Why the candidate counts as supporting evidence."}
                    }}},
                    "entrypoints": {"type": "array", "description": "Public and internal service entrypoints carried from the workload context. Owner: service/contract_helpers.go buildServiceEntrypoints.", "items": {"type": "object", "description": "One service entrypoint from docs routes or exact hostname evidence. Owner: service/contract_helpers.go buildServiceEntrypoints.", "properties": {
                      "type": {"type": "string", "enum": ["docs_route", "hostname"], "description": "Entrypoint kind: a documented internal route or an observed public hostname."},
                      "target": {"type": "string", "description": "Entrypoint target: the docs route or the hostname."},
                      "environment": {"type": "string", "description": "Environment the entrypoint was observed in; empty when unknown."},
                      "visibility": {"type": "string", "enum": ["internal", "public"], "description": "Docs routes are internal; observed hostnames are public."},
                      "relative_path": {"type": "string", "description": "Repository-relative file the entrypoint was observed in."},
                      "reason": {"type": "string", "description": "Why the entrypoint counts as service evidence."}
                    }}},
                    "network_paths": {"type": "array", "description": "Entrypoint-to-runtime paths carried from the workload context, one per entrypoint that matches a runtime instance. Owner: service/contract_helpers.go buildServiceNetworkPaths.", "items": {"type": "object", "description": "One entrypoint-to-runtime path. Owner: service/contract_helpers.go buildServiceNetworkPaths.", "properties": {
                      "path_type": {"type": "string", "enum": ["entrypoint_to_runtime", "hostname_to_runtime", "docs_route_to_runtime"], "description": "Path kind, derived from the entrypoint type."},
                      "from_type": {"type": "string", "description": "Entrypoint type the path starts from (docs_route or hostname)."},
                      "from": {"type": "string", "description": "Entrypoint target the path starts from."},
                      "to_type": {"type": "string", "description": "Always runtime_platform: every path lands on a runtime platform."},
                      "to": {"type": "string", "description": "Platform name the path lands on."},
                      "platform_kind": {"type": "string", "description": "Platform kind the path lands on."},
                      "environment": {"type": "string", "description": "Environment of the matched runtime instance."},
                      "reason": {"type": "string", "description": "Entrypoint evidence reason carried onto the path."},
                      "visibility": {"type": "string", "description": "Entrypoint visibility carried onto the path (internal or public)."}
                    }}},
                    "ingress_posture": {"type": "object", "description": "WAF coverage and TLS termination posture for the service's internet-facing edge resources, derived strictly from the materialized AWS_wafv2_web_acl_protects_resource and AWS_acm_certificate_used_by_resource edges. waf_coverage and tls_termination are three-valued (protected/unprotected/unproven and terminated/not_terminated/unproven) so an observed-negative is never confused with missing evidence. Owner: service/ingress_posture.go buildIngressPosture.", "properties": {
                      "waf_coverage": {"type": "string", "enum": ["protected", "unprotected", "unproven"], "description": "Rolled-up WAF tile: positive only when every edge is observed-positive."},
                      "tls_termination": {"type": "string", "enum": ["terminated", "not_terminated", "unproven"], "description": "Rolled-up TLS tile: positive only when every edge is observed-positive."},
                      "edge_count": {"type": "integer", "description": "Number of internet-facing edge resources the posture was rolled up from."},
                      "waf_protected": {"type": "integer", "description": "Edges observed WAF-positive."},
                      "tls_terminated": {"type": "integer", "description": "Edges observed TLS-positive."},
                      "truth_basis": {"type": "string", "description": "Always observed: the posture rests on materialized edges, never on inference."},
                      "reason": {"type": "string", "description": "Human-readable posture rollup, including the no-edge-resource case."},
                      "edges": {"type": "array", "description": "Per-edge posture rows; absent when no edge resource is materialized.", "items": {"type": "object", "description": "One internet-facing edge resource and its observed posture.", "properties": {
                        "id": {"type": "string", "description": "Edge resource identity."},
                        "name": {"type": "string", "description": "Edge resource display name."},
                        "resource_type": {"type": "string", "description": "Edge resource type."},
                        "waf_coverage": {"type": "string", "description": "Observed WAF state for this edge (protected, unprotected, or unproven)."},
                        "tls_termination": {"type": "string", "description": "Observed TLS state for this edge (terminated, not_terminated, or unproven)."}
                      }}}
                    }},
                    "observed_config_environments": {"type": "array", "description": "Distinct sorted environment names merged from runtime instance environments and service content evidence, carried from the workload context. Owner: service/query_enrichment.go over MergeStringSets.", "items": {"type": "string"}},
                    "dependents": {"type": "array", "description": "Graph-derived dependent repositories for the service's resolved repository, carried from the workload context. Owner: service/contract_helpers.go BuildGraphDependents.", "items": {"type": "object", "description": "One dependent repository and the typed relationships behind it. Owner: service/contract_helpers.go BuildGraphDependents.", "properties": {
                      "repository": {"type": "string", "description": "Dependent repository display name."},
                      "repo_id": {"type": "string", "description": "Dependent repository canonical id; the final sort tiebreaker."},
                      "relationship_types": {"type": "array", "description": "Typed graph relationships the dependent was derived from.", "items": {"type": "string"}},
                      "relationship_reasons": {"type": "array", "description": "Per-relationship rationale; present only when the candidate carries reasons.", "items": {"type": "string"}}
                    }}},
                    "consumer_repositories": {"type": "array", "description": "Consumer repositories observed from graph provisioning candidates merged with content hostname/reference evidence, sorted by consumer score with repo_id as the final tiebreaker. Carried from the workload context. Owner: deployment/consumer_enrichment_sources.go LoadConsumerRepositoryEnrichmentWithSources.", "items": {"type": "object", "description": "One consumer repository and the evidence behind it. Owner: deployment/consumer_enrichment_sources.go LoadConsumerRepositoryEnrichmentWithSources.", "properties": {
                      "repository": {"type": "string", "description": "Consumer repository display name; the raw repo id for content-only rows before display-name backfill."},
                      "repo_id": {"type": "string", "description": "Consumer repository canonical id; the final sort tiebreaker."},
                      "consumer_kinds": {"type": "array", "description": "How the consumer was observed: graph_provisioning_consumer, service_reference_consumer, hostname_reference_consumer.", "items": {"type": "string"}},
                      "graph_relationship_types": {"type": "array", "description": "Typed graph relationships behind a graph-observed consumer; absent on content-only rows.", "items": {"type": "string"}},
                      "graph_relationship_reasons": {"type": "array", "description": "Per-relationship rationale; present only when the candidate carries reasons.", "items": {"type": "string"}},
                      "evidence_kinds": {"type": "array", "description": "Content evidence kinds behind a content-observed consumer (for example repository_reference, hostname_reference); present only with content evidence.", "items": {"type": "string"}},
                      "matched_values": {"type": "array", "description": "Content values that matched the service (hostnames or repository references); present only with content evidence.", "items": {"type": "string"}},
                      "sample_paths": {"type": "array", "description": "Sample repository-relative paths the content evidence came from; present only with content evidence.", "items": {"type": "string"}}
                    }}},
                    "provisioning_source_chains": {"type": "array", "description": "Provisioning source chains: per-candidate repository provisioning evidence collected from parsed infrastructure entities, carried from the workload context. Owner: deployment/enrichment.go LoadProvisioningSourceChainsFromCandidates.", "items": {"type": "object", "description": "One provisioning candidate repository and its collected chain evidence. Owner: deployment/enrichment.go LoadProvisioningSourceChainsFromCandidates.", "properties": {
                      "repository": {"type": "string", "description": "Provisioning candidate repository display name."},
                      "repo_id": {"type": "string", "description": "Provisioning candidate repository canonical id; the final sort tiebreaker."},
                      "relationship_types": {"type": "array", "description": "Typed graph relationships the candidate was derived from.", "items": {"type": "string"}},
                      "relationship_reasons": {"type": "array", "description": "Per-relationship rationale, also folded into evidence_kinds; present only when the candidate carries reasons.", "items": {"type": "string"}},
                      "evidence_kinds": {"type": "array", "description": "Provisioning evidence kinds collected from parsed entities; present only when entities were observed.", "items": {"type": "string"}},
                      "sample_paths": {"type": "array", "description": "Sample repository-relative paths the chain evidence came from; present only when entities were observed.", "items": {"type": "string"}},
                      "modules": {"type": "array", "description": "Provisioning modules observed; present only when entities were observed.", "items": {"type": "string"}},
                      "config_paths": {"type": "array", "description": "Provisioning config paths observed; present only when entities were observed.", "items": {"type": "string"}}
                    }}},
                    "cloud_resources": {"type": "array", "description": "CloudResource dependencies admitted only from materialized WorkloadInstance USES CloudResource relationships, carried from the workload context. Owner: deployment/cloud_resource_dependencies.go LoadMaterializedServiceCloudResourceDependencies (CompactStringMap: every key sparse).", "items": {"type": "object", "description": "One materialized cloud resource and its workload-cloud relationship evidence. Owner: deployment/cloud_resource_dependencies.go LoadMaterializedServiceCloudResourceDependencies.", "properties": {
                      "id": {"type": "string", "description": "Cloud resource identity (id, uid, resource_id, arn, or name coalesced)."},
                      "name": {"type": "string", "description": "Cloud resource name."},
                      "kind": {"type": "string", "description": "Cloud resource kind (kind, resource_type, or data_type coalesced)."},
                      "resource_type": {"type": "string", "description": "Cloud resource type."},
                      "provider": {"type": "string", "description": "Cloud provider (provider or source_system coalesced)."},
                      "environment": {"type": "string", "description": "Environment the relationship was observed in."},
                      "resource_id": {"type": "string", "description": "Provider resource id; present only when the node carries one."},
                      "arn": {"type": "string", "description": "Resource ARN; present only when the node carries one."},
                      "account_id": {"type": "string", "description": "Cloud account id; present only when the node carries one."},
                      "region": {"type": "string", "description": "Cloud region; present only when the node carries one."},
                      "relationship_basis": {"type": "string", "description": "Relationship basis for this resource (materialized workload-cloud relationship, or mixed)."},
                      "resolution_mode": {"type": "string", "description": "How the relationship resolved; present only when the edge carries one."},
                      "evidence_source": {"type": "string", "description": "Evidence source behind the relationship; present only when the edge carries one."}
                    }}},
                    "uncorrelated_cloud_resources": {"type": "array", "description": "CloudResource candidates that match the service name or cloud identifier but remain missing the reducer-owned workload-to-cloud relationship required for dependency truth. Never silently merged into cloud_resources. Carried from the workload context. Owner: deployment/cloud_resource_candidates.go LoadUncorrelatedCloudResourceCandidatesBounded (CompactStringMap: every key sparse).", "items": {"type": "object", "description": "One uncorrelated cloud-resource candidate. Owner: deployment/cloud_resource_candidates.go LoadUncorrelatedCloudResourceCandidatesBounded.", "properties": {
                      "id": {"type": "string", "description": "Candidate identity."},
                      "name": {"type": "string", "description": "Candidate name."},
                      "kind": {"type": "string", "description": "Candidate kind."},
                      "resource_type": {"type": "string", "description": "Candidate resource type."},
                      "provider": {"type": "string", "description": "Candidate provider (provider or source_system coalesced)."},
                      "environment": {"type": "string", "description": "Candidate environment; present only when the node carries one."},
                      "resource_id": {"type": "string", "description": "Provider resource id; present only when the node carries one."},
                      "arn": {"type": "string", "description": "Resource ARN; present only when the node carries one."},
                      "account_id": {"type": "string", "description": "Cloud account id; present only when the node carries one."},
                      "region": {"type": "string", "description": "Cloud region; present only when the node carries one."},
                      "service_anchor_status": {"type": "string", "description": "Service anchor status behind the candidate_status verdict."},
                      "candidate_status": {"type": "string", "description": "Candidate verdict derived from the anchor status. Owner: deployment/cloud_resource_candidates.go cloudResourceCandidateStatus."},
                      "service_anchor_reason": {"type": "string", "description": "Service anchor reason; present only when the node carries one."},
                      "missing_relationship": {"type": "string", "description": "Always workload_cloud_relationship: names the relationship no candidate carries."}
                    }}},
                    "uncorrelated_cloud_resources_truncated": {"type": "boolean", "description": "Present and true when the uncorrelated_cloud_resources list was capped at the dossier item limit and additional candidates exist in the graph."},
                    "deployment_evidence": {
                      "type": "object",
                      "description": "Deployment, CI, and environment evidence pointers for the service repository. artifacts, delivery_paths, delivery_workflows and shared_config_paths are each capped at 50 rows. artifact_count reports the artifact rows read, which is a floor when artifacts_truncated is set by the read bound itself, and raw_limits reports the pre-cut count of every list that was cut. A cut adds deployment_evidence_<list>_truncated to partial_reasons. Artifacts carry source_location plus resolved_id/generation_id for Postgres evidence drilldown; evidence_index groups those pointers by relationship type, artifact family, and evidence kind. Owner: deployment/enrichment.go deployment evidence readers over the service workload context.",
                      "additionalProperties": true
                    },
                    "documentation_overview": {"type": "object", "description": "Documentation evidence for the service repository: route/spec/hostname counts plus the optional repository identity the graph read resolved. Owner: service/story_overview.go buildServiceDocumentationOverview.", "properties": {
                      "repo_id": {"type": "string", "description": "Canonical repository id the documentation evidence was read for."},
                      "repo_name": {"type": "string", "description": "Repository display name."},
                      "portable_identifier": {"type": "string", "description": "Portable repository identifier; the repo_id."},
                      "docs_route_count": {"type": "integer", "description": "Number of documentation routes observed in service evidence."},
                      "api_spec_count": {"type": "integer", "description": "Number of API spec files observed in service evidence."},
                      "entrypoint_host_count": {"type": "integer", "description": "Number of entrypoint hostnames observed in service evidence."},
                      "remote_url": {"type": "string", "description": "Repository remote URL from the graph read; present only when the graph resolved the repository."},
                      "repo_slug": {"type": "string", "description": "Repository slug from the graph read; present only when the graph resolved the repository."},
                      "has_remote": {"type": "boolean", "description": "Whether the graph-resolved repository carries a remote URL."},
                      "local_path_present": {"type": "boolean", "description": "Whether the graph-resolved repository carries a local path."},
                      "api_spec_paths": {"type": "array", "items": {"type": "string"}, "description": "Sorted API spec relative paths; present only when at least one spec was observed."}
                    }},
                    "support_overview": {"type": "object", "description": "Dependency/dependent/consumer counts plus the paged downstream-consumer drilldown: when a downstream read bound fired, consumer_list_tool (list_service_downstream_consumers) and consumer_list_path return the keyset-ordered consumer set past the dossier read bound (itself a bounded scan; the list reports lower_bound true only when a bound fired). Owner: service/story_overview.go buildServiceSupportOverviewWithContext; every key is sparse except the base counts.", "properties": {
                      "dependency_count": {"type": "integer", "description": "Repository-level dependency rows for the resolved repository. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "dependent_count": {"type": "integer", "description": "Graph-derived dependent repositories for the resolved repository. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "consumer_repository_count": {"type": "integer", "description": "Consumer repositories observed from graph and content evidence. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "provisioning_source_count": {"type": "integer", "description": "Provisioning source chains observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "observed_environment_count": {"type": "integer", "description": "Distinct observed config environments. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "entrypoint_host_count": {"type": "integer", "description": "Exact hostnames observed in service evidence. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "entrypoint_count": {"type": "integer", "description": "Service entrypoints observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "network_path_count": {"type": "integer", "description": "Entrypoint-to-runtime paths observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "cloud_resource_count": {"type": "integer", "description": "Materialized cloud resources observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "has_api_surface": {"type": "boolean", "description": "Whether an API surface was observed for the repository. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "has_documentation_overview": {"type": "boolean", "description": "Whether a documentation overview was built. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "endpoint_count": {"type": "integer", "description": "Present only with an API surface. API endpoint rows counted. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "method_count": {"type": "integer", "description": "Present only with an API surface. HTTP method rows counted. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "spec_count": {"type": "integer", "description": "Present only with an API surface. API spec files counted. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "deployment_tool_family_count": {"type": "integer", "description": "Present only with deployment evidence. Deployment tool families observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "delivery_path_count": {"type": "integer", "description": "Present only with deployment evidence. Delivery paths observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "delivery_workflow_count": {"type": "integer", "description": "Present only with deployment evidence. Delivery workflows observed. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "target_support": {"type": "object", "description": "Present only when the deployment-trace target support populated the context. Pass-through block (evidence and incident-routing counts plus the missing-evidence reason); keys vary by trace depth. Owner: service/story_overview.go buildServiceSupportOverviewWithContext.", "additionalProperties": true},
                      "consumer_list_tool": {"type": "string", "description": "Present only when a downstream read bound fired. MCP tool returning the full keyset-ordered consumer set: list_service_downstream_consumers. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."},
                      "consumer_list_path": {"type": "string", "description": "Present only when a downstream read bound fired. HTTP path returning the full keyset-ordered consumer set. Owner: service/story_overview.go buildServiceSupportOverviewWithContext."}
                    }, "required": ["dependency_count", "dependent_count", "consumer_repository_count", "provisioning_source_count", "observed_environment_count", "entrypoint_host_count", "entrypoint_count", "network_path_count", "cloud_resource_count", "has_api_surface", "has_documentation_overview"]},
                    "answer_metadata": {"type": "object", "description": "Normalized additive answer companion (answer.AnswerMetadata) derived from the story payload by answer.AttachAnswerMetadata via entity.BuildServiceStoryEnvelope.", "properties": {
                      "schema_version": {"type": "string", "description": "Answer metadata schema version emitted by answer.BuildAnswerMetadata."},
                      "evidence_handles": {"type": "array", "description": "Evidence handles carried by the story payload, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true, "description": "One evidence handle; keys vary by evidence kind. Owner: answer.BuildAnswerMetadata."}},
                      "missing_evidence": {"type": "array", "description": "Evidence the story payload lacks, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true, "description": "One missing-evidence entry; keys vary by gap kind. Owner: answer.BuildAnswerMetadata."}},
                      "limitations": {"type": "array", "description": "Bounded caveats for the story answer, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true, "description": "One limitation entry. Owner: answer.BuildAnswerMetadata."}},
                      "truncated": {"type": "boolean", "description": "True when the story answer or its coverage is truncated, per answer.BuildAnswerMetadata."},
                      "coverage": {"type": "object", "description": "Story answer coverage keyed by coverage field names, per answer.BuildAnswerMetadata.", "additionalProperties": true},
                      "partial_reasons": {"type": "array", "description": "Why the story answer is partial, per answer.BuildAnswerMetadata.", "items": {"type": "string"}},
                      "recommended_next_calls": {"type": "array", "description": "Follow-up calls for the story answer, per answer.BuildAnswerMetadata.", "items": {"type": "object", "additionalProperties": true, "description": "One recommended follow-up call (tool, reason, arguments). Owner: answer.BuildAnswerMetadata."}}
                    }, "required": ["schema_version", "evidence_handles", "missing_evidence", "limitations", "truncated", "coverage", "partial_reasons", "recommended_next_calls"]},
                    "evidence_boundaries": ` + schema.EvidenceBoundaries + `
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"}
        }
      }
    },
`
