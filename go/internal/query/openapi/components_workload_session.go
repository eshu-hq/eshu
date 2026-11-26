// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"github.com/eshu-hq/eshu/go/internal/query/openapi/schema"
)

// ComponentsWorkloadSession documents the WorkloadContext and browser
// -session schemas, included into components' "schemas" object as a
// separate constant fragment to keep components.go comfortably under
// the repository's file-size limit — same pattern as
// componentsReplatforming (components_replatforming.go) and
// componentsProviderConfigs (components_provider_configs.go).
const ComponentsWorkloadSession = `      "WorkloadContext": {
        "type": "object",
        "properties": {
          "id": {"type": "string", "description": "Canonical graph workload id the context was read for (workload.id). Owner: entity/workload_context.go fetchWorkloadContextDecision."},
          "name": {"type": "string", "description": "Service/workload display name the context was read for."},
          "kind": {"type": "string", "description": "Workload kind from the graph row (for example service)."},
          "repo_id": {"type": "string", "description": "Canonical repository id the context was resolved to; empty before a repository resolves."},
          "repo_name": {"type": "string", "description": "Repository display name the context was resolved to."},
          "hostnames": {"type": "array", "description": "Exact hostnames found in service evidence, capped at 50 rows. result_limits.hostname_count reports the total before the cut; a cut sets result_limits.truncated and adds hostnames_truncated to partial_reasons.", "items": {"type": "object", "description": "One exact hostname observed in service evidence. Owner: service/query_enrichment_rows.go buildServiceHostnameRows over service HostnameEvidence.", "properties": {
            "hostname": {"type": "string", "description": "Exact hostname found in service evidence."},
            "environment": {"type": "string", "description": "Environment inferred for the hostname; empty when none was inferred."},
            "relative_path": {"type": "string", "description": "Repository-relative file the hostname was observed in."},
            "reason": {"type": "string", "description": "Why the hostname counts as service evidence."}
          }}},
          "entrypoint_candidates": {"type": "array", "description": "Hostname-shaped candidates kept as non-entrypoint supporting evidence with classification and reason. Cut to the 50-row cap on the context routes; result_limits.entrypoint_candidate_count carries the total and partial_reasons names the cut entrypoint_candidates_truncated.", "items": {"type": "object", "description": "One rejected or ambiguous hostname-shaped candidate. Owner: service/query_enrichment_rows.go buildServiceEntrypointCandidateRows over service EntrypointCandidateEvidence.", "properties": {
            "candidate": {"type": "string", "description": "Hostname-shaped candidate value that must not become a public entrypoint."},
            "classification": {"type": "string", "description": "Why the candidate was rejected or left ambiguous."},
            "relative_path": {"type": "string", "description": "Repository-relative file the candidate was observed in."},
            "reason": {"type": "string", "description": "Why the candidate counts as supporting evidence."}
          }}},
          "entrypoints": {"type": "array", "description": "Public and internal service entrypoints, capped at 50 rows. result_limits.entrypoint_count reports the total before the cut; a cut sets result_limits.truncated and adds entrypoints_truncated to partial_reasons.", "items": {"type": "object", "description": "One service entrypoint from docs routes or exact hostname evidence. Owner: service/contract_helpers.go buildServiceEntrypoints.", "properties": {
            "type": {"type": "string", "enum": ["docs_route", "hostname"], "description": "Entrypoint kind: a documented internal route or an observed public hostname."},
            "target": {"type": "string", "description": "Entrypoint target: the docs route or the hostname."},
            "environment": {"type": "string", "description": "Environment the entrypoint was observed in; empty when unknown."},
            "visibility": {"type": "string", "enum": ["internal", "public"], "description": "Docs routes are internal; observed hostnames are public."},
            "relative_path": {"type": "string", "description": "Repository-relative file the entrypoint was observed in."},
            "reason": {"type": "string", "description": "Why the entrypoint counts as service evidence."}
          }}},
          "network_paths": {"type": "array", "description": "Entrypoint-to-runtime paths, one per entrypoint that matches a runtime instance, capped at 50 rows. result_limits.network_path_count reports the total before the cut; a cut sets result_limits.truncated and adds network_paths_truncated to partial_reasons.", "items": {"type": "object", "description": "One entrypoint-to-runtime path. Owner: service/contract_helpers.go buildServiceNetworkPaths.", "properties": {
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
          "ingress_posture": {"type": "object", "description": "WAF coverage and TLS termination posture for the service's internet-facing edge resources, derived strictly from the materialized AWS_wafv2_web_acl_protects_resource and AWS_acm_certificate_used_by_resource edges. waf_coverage and tls_termination are three-valued (protected/unprotected/unproven and terminated/not_terminated/unproven). unproven covers both no edge resource materialized and collector-absent, so absence of collector is never misreported as absence of protection. Owner: service/ingress_posture.go buildIngressPosture.", "properties": {
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
          "observed_config_environments": {"type": "array", "description": "Distinct sorted environment names merged from runtime instance environments and service content evidence. Owner: service/query_enrichment.go over MergeStringSets.", "items": {"type": "string"}},
          "api_surface": {"type": "object", "description": "API surface for the service repository. The graph read (repository/api_surface.go buildGraphAPISurface) wins when it finds materialized EXPOSES_ENDPOINT edges; otherwise the content-evidence fallback (service/query_enrichment_rows.go buildServiceAPISurface) answers. Shared keys ship on both paths; truth_basis, source_paths, detail_limit and detail_truncated are graph-only, while spec_count, parsed_spec_count, docs_routes and hostnames are content-only. endpoints is capped at 50 rows; endpoint_count reports the total, and a cut (or a graph read that already stopped at its detail limit) sets detail_truncated and adds api_surface_endpoints_truncated to partial_reasons.", "additionalProperties": true, "properties": {
            "truth_basis": {"type": "string", "description": "Graph-only. Always graph: the surface was read from materialized EXPOSES_ENDPOINT edges."},
            "endpoint_count": {"type": "integer", "description": "Endpoint rows counted (graph count, or content count when the fallback answered)."},
            "method_count": {"type": "integer", "description": "HTTP method rows counted (0 on the content fallback when absent)."},
            "operation_id_count": {"type": "integer", "description": "Operation-id rows counted."},
            "endpoints": {"type": "array", "description": "Endpoint rows capped at the 50-row cap. Graph rows carry identity keys (id, path, methods, source graph, source_kinds, source_paths, evidence_source, plus workload_id/workload_name when the edge names a workload); content rows carry spec identity (path, methods, operation_ids, spec_path) or framework identity (path, methods, source framework, framework, spec_path).", "items": {"type": "object", "additionalProperties": true, "description": "One API endpoint row. Keys are the union of the graph shape (id, path, methods, operation_ids, source, source_kinds, source_paths, evidence_source, workload_id, workload_name) and the content shape (path, methods, operation_ids, spec_path, or source framework with framework); only the keys the serving path observed are present."}},
            "source_paths": {"type": "array", "description": "Graph-only. Distinct endpoint source paths.", "items": {"type": "string"}},
            "spec_paths": {"type": "array", "description": "Distinct API spec relative paths.", "items": {"type": "string"}},
            "spec_versions": {"type": "array", "description": "Distinct spec versions observed.", "items": {"type": "string"}},
            "api_versions": {"type": "array", "description": "Distinct API versions observed.", "items": {"type": "string"}},
            "detail_limit": {"type": "integer", "description": "Graph-only. Row bound the endpoint detail read ran under."},
            "detail_truncated": {"type": "boolean", "description": "Graph-only. True when the endpoint rows were cut at the detail bound."},
            "frameworks": {"type": "array", "description": "Distinct framework names behind framework-detected routes; present only when at least one was observed.", "items": {"type": "string"}},
            "framework_route_count": {"type": "integer", "description": "Framework-detected endpoint rows; present only when at least one was observed."},
            "spec_count": {"type": "integer", "description": "Content-only. API spec files observed in content evidence."},
            "parsed_spec_count": {"type": "integer", "description": "Content-only. Spec files that parsed successfully."},
            "docs_routes": {"type": "array", "description": "Content-only. Distinct documented internal docs/spec route strings.", "items": {"type": "string"}},
            "hostnames": {"type": "array", "description": "Content-only. Distinct hostnames observed in API spec evidence.", "items": {"type": "string"}}
          }},
          "deployment_overview": {"type": "object", "description": "Deployment summary with counts (including hostname_count and entrypoint_count). It does not repeat the hostnames and entrypoints arrays, which ship once at the top level. Unlike the service-story route, the context keeps the api_surface endpoint rows (capped at 50); nothing is dropped. Owner: service/story_overview.go buildServiceDeploymentOverviewWithContext, the same builder as the story route.", "properties": {
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
            "api_surface": {"type": "object", "description": "API-surface counts plus the capped endpoint rows (no drop on this route, unlike the story). The normalized count keys are always present; any other keys pass through from the workload context map.", "additionalProperties": true, "properties": {
              "endpoint_count": {"type": "integer", "description": "Endpoint rows counted (context count, or rows shipped when absent)."},
              "method_count": {"type": "integer", "description": "Method count (context count, or 0 when absent)."},
              "spec_count": {"type": "integer", "description": "API spec count (context count, spec_files length, or spec_paths length)."},
              "endpoints": {"type": "array", "description": "Endpoint rows capped at the story item limit. Row shape follows evidence_detail: identity keys (id, path, methods) under handles, full rows under full.", "items": {"type": "object", "additionalProperties": true, "description": "One API endpoint row: graph identity keys (id, path, methods, source, source_kinds, source_paths, evidence_source, workload_id, workload_name) or content spec/framework keys (path, methods, operation_ids, spec_path, or source framework with framework)."}},
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
          "deployment_evidence": {"type": "object", "description": "Deployment evidence for the service repository. artifacts, delivery_paths, delivery_workflows and shared_config_paths are each capped at 50 rows. artifact_count reports the artifact rows read, which is a floor when artifacts_truncated is set by the read bound itself, and raw_limits reports the pre-cut count of every list that was cut. A cut adds deployment_evidence_<list>_truncated to partial_reasons, and artifacts_truncated is true when artifact rows were cut. evidence_index.*.resolved_ids still names every artifact row read, so a cut row can be fetched with get_relationship_evidence.", "additionalProperties": true},
          "story_sections": {"type": "array", "description": "Narrative sections backing the service story: deployment first, then one per populated evidence family (entrypoints, network, api, consumers, dependents, provisioning, deployment evidence). Owner: service/story_overview.go buildServiceStorySectionsWithContext.", "items": {"type": "object", "description": "One narrative section: a title plus a one-line summary.", "properties": {
            "title": {"type": "string", "description": "Section title (deployment, entrypoints, network, api, consumers, dependents, provisioning, or a deployment-evidence family)."},
            "summary": {"type": "string", "description": "One-line human-readable summary of the section."}
          }}},
          "documentation_overview": {"type": "object", "description": "Documentation evidence for the service repository: route/spec/hostname counts plus the optional repository identity the graph read resolved. Owner: service/story_overview.go buildServiceDocumentationOverview; absent when neither repo_id nor repo_name resolves.", "properties": {
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
          "support_overview": {"type": "object", "description": "Dependency/dependent/consumer counts plus the paged downstream-consumer drilldown: when a downstream read bound fired, consumer_list_tool (list_service_downstream_consumers) and consumer_list_path return the keyset-ordered consumer set past the story and context read bounds (themselves bounded scans; the list reports lower_bound true only when a bound fired). Owner: service/story_overview.go buildServiceSupportOverviewWithContext; every key is sparse except the base counts.", "properties": {
            "dependency_count": {"type": "integer", "description": "Repository-level dependency rows for the resolved repository."},
            "dependent_count": {"type": "integer", "description": "Graph-derived dependent repositories for the resolved repository."},
            "consumer_repository_count": {"type": "integer", "description": "Consumer repositories observed from graph and content evidence."},
            "provisioning_source_count": {"type": "integer", "description": "Provisioning source chains observed."},
            "observed_environment_count": {"type": "integer", "description": "Distinct observed config environments."},
            "entrypoint_host_count": {"type": "integer", "description": "Exact hostnames observed in service evidence."},
            "entrypoint_count": {"type": "integer", "description": "Service entrypoints observed."},
            "network_path_count": {"type": "integer", "description": "Entrypoint-to-runtime paths observed."},
            "cloud_resource_count": {"type": "integer", "description": "Materialized cloud resources observed."},
            "has_api_surface": {"type": "boolean", "description": "Whether an API surface was observed for the repository."},
            "has_documentation_overview": {"type": "boolean", "description": "Whether a documentation overview was built."},
            "endpoint_count": {"type": "integer", "description": "Present only with an API surface. API endpoint rows counted."},
            "method_count": {"type": "integer", "description": "Present only with an API surface. HTTP method rows counted."},
            "spec_count": {"type": "integer", "description": "Present only with an API surface. API spec files counted."},
            "deployment_tool_family_count": {"type": "integer", "description": "Present only with deployment evidence. Deployment tool families observed."},
            "delivery_path_count": {"type": "integer", "description": "Present only with deployment evidence. Delivery paths observed."},
            "delivery_workflow_count": {"type": "integer", "description": "Present only with deployment evidence. Delivery workflows observed."},
            "target_support": {"type": "object", "description": "Present only when the deployment-trace target support populated the context. Pass-through block (evidence and incident-routing counts plus the missing-evidence reason); keys vary by trace depth.", "additionalProperties": true},
            "consumer_list_tool": {"type": "string", "description": "Present only when a downstream read bound fired. MCP tool returning the full keyset-ordered consumer set: list_service_downstream_consumers."},
            "consumer_list_path": {"type": "string", "description": "Present only when a downstream read bound fired. HTTP path returning the full keyset-ordered consumer set."}
          }},
          "dependents": {"type": "array", "description": "Graph-derived dependent repositories for the service's resolved repository. Owner: service/contract_helpers.go BuildGraphDependents.", "items": {"type": "object", "description": "One dependent repository and the typed relationships behind it.", "properties": {
            "repository": {"type": "string", "description": "Dependent repository display name."},
            "repo_id": {"type": "string", "description": "Dependent repository canonical id; the final sort tiebreaker."},
            "relationship_types": {"type": "array", "description": "Typed graph relationships the dependent was derived from.", "items": {"type": "string"}},
            "relationship_reasons": {"type": "array", "description": "Per-relationship rationale; present only when the candidate carries reasons.", "items": {"type": "string"}}
          }}},
          "dependents_truncated": {"type": "boolean", "description": "Present and true when the graph-derived dependent-repository read hit its bound. That read bounds rows, and one repository can supply several rows, so this reports either that the dependents list is not exhaustive or that the relationship_types and relationship_reasons on a returned entry were clipped. See the Context and stories HTTP API reference for the conditions under which the flag is true although no repository was dropped."},
          "consumer_repositories": {"type": "array", "description": "Consumer repositories observed from graph provisioning candidates merged with content hostname/reference evidence, sorted by consumer score with repo_id as the final tiebreaker. Owner: deployment/consumer_enrichment_sources.go LoadConsumerRepositoryEnrichmentWithSources.", "items": {"type": "object", "description": "One consumer repository and the evidence behind it.", "properties": {
            "repository": {"type": "string", "description": "Consumer repository display name; the raw repo id for content-only rows before display-name backfill."},
            "repo_id": {"type": "string", "description": "Consumer repository canonical id; the final sort tiebreaker."},
            "consumer_kinds": {"type": "array", "description": "How the consumer was observed: graph_provisioning_consumer, service_reference_consumer, hostname_reference_consumer.", "items": {"type": "string"}},
            "graph_relationship_types": {"type": "array", "description": "Typed graph relationships behind a graph-observed consumer; absent on content-only rows.", "items": {"type": "string"}},
            "graph_relationship_reasons": {"type": "array", "description": "Per-relationship rationale; present only when the candidate carries reasons.", "items": {"type": "string"}},
            "evidence_kinds": {"type": "array", "description": "Content evidence kinds behind a content-observed consumer (for example repository_reference, hostname_reference); present only with content evidence.", "items": {"type": "string"}},
            "matched_values": {"type": "array", "description": "Content values that matched the service (hostnames or repository references); present only with content evidence.", "items": {"type": "string"}},
            "sample_paths": {"type": "array", "description": "Sample repository-relative paths the content evidence came from; present only with content evidence.", "items": {"type": "string"}}
          }}},
          "consumer_repositories_truncated": {"type": "boolean", "description": "Present and true when any read underneath the consumer-repository list hit its bound: the graph-derived candidate read, the service evidence file read the observed hostnames are extracted from, any of the three narrowings applied to those hostnames before the cross-repository searches (the affinity filter, the four-hostname cap, and the cut against the caller's own limit), a per-search content row cap, or the final merge cap. The returned consumer_repositories list may not be exhaustive even though it is well under any displayed row limit. See the Context and stories HTTP API reference for the full enumeration and the conditions under which the flag is true although nothing was dropped."},
          "consumer_repositories_downstream_truncated": {"type": "boolean", "description": "Present and true when the consumer-repository content search behind the enrichment hit its bound. Owner: deployment/consumer_enrichment_sources.go ConsumerEnrichmentTruncation.Downstream."},
          "consumer_repositories_evidence_file_truncated": {"type": "boolean", "description": "Present and true when the service file list the consumer enrichment derived its hostnames from filled its window, so a hostname past the cut was never searched. Owner: deployment/consumer_enrichment_sources.go ConsumerEnrichmentTruncation.EvidenceFiles."},
          "provisioning_source_chains": {"type": "array", "description": "Provisioning source chains: per-candidate repository provisioning evidence collected from parsed infrastructure entities. Owner: deployment/enrichment.go LoadProvisioningSourceChainsFromCandidates.", "items": {"type": "object", "description": "One provisioning candidate repository and its collected chain evidence.", "properties": {
            "repository": {"type": "string", "description": "Provisioning candidate repository display name."},
            "repo_id": {"type": "string", "description": "Provisioning candidate repository canonical id; the final sort tiebreaker."},
            "relationship_types": {"type": "array", "description": "Typed graph relationships the candidate was derived from.", "items": {"type": "string"}},
            "relationship_reasons": {"type": "array", "description": "Per-relationship rationale, also folded into evidence_kinds; present only when the candidate carries reasons.", "items": {"type": "string"}},
            "evidence_kinds": {"type": "array", "description": "Provisioning evidence kinds collected from parsed entities; present only when entities were observed.", "items": {"type": "string"}},
            "sample_paths": {"type": "array", "description": "Sample repository-relative paths the chain evidence came from; present only when entities were observed.", "items": {"type": "string"}},
            "modules": {"type": "array", "description": "Provisioning modules observed; present only when entities were observed.", "items": {"type": "string"}},
            "config_paths": {"type": "array", "description": "Provisioning config paths observed; present only when entities were observed.", "items": {"type": "string"}}
          }}},
          "provisioning_source_chains_truncated": {"type": "boolean", "description": "Present and true when the provisioning-source-chain read hit its bound. That read bounds rows, and one repository can supply several rows, so this reports either that the provisioning_source_chains list is not exhaustive or that the relationship_types and relationship_reasons on a returned entry were clipped. See the Context and stories HTTP API reference for the conditions under which the flag is true although no repository was dropped."},
          "cloud_resources": {"type": "array", "description": "Materialized cloud resources the workload's instances USE via a reducer-owned workload-cloud relationship, bounded at the service story item limit. Owner: deployment/cloud_resource_dependencies.go LoadMaterializedServiceCloudResourceDependencies (CompactStringMap: every key sparse).", "items": {"type": "object", "description": "One materialized cloud resource and its workload-cloud relationship evidence.", "properties": {
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
            "evidence_source": {"type": "string", "description": "Evidence source behind the relationship; present only when the edge carries one."},
            "service_anchor_source": {"type": "string", "description": "Service anchor source; present only when the edge carries one."},
            "service_anchor_reason": {"type": "string", "description": "Service anchor reason; present only when the edge carries one."},
            "source_fact_id": {"type": "string", "description": "Source fact id behind the relationship; present only when the edge carries one."},
            "stable_fact_key": {"type": "string", "description": "Stable fact key behind the relationship; present only when the edge carries one."},
            "source_system": {"type": "string", "description": "Source system behind the relationship; present only when the edge carries one."},
            "source_record_id": {"type": "string", "description": "Source record id behind the relationship; present only when the edge carries one."},
            "collector_kind": {"type": "string", "description": "Collector kind behind the relationship; present only when the edge carries one."}
          }}},
          "uncorrelated_cloud_resources": {"type": "array", "description": "Cloud-resource candidates that free-text match the service without a reducer-owned workload relationship. Never silently merged into cloud_resources: the deployment overview counts them separately via uncorrelated_cloud_resource_count. Owner: deployment/cloud_resource_candidates.go LoadUncorrelatedCloudResourceCandidatesBounded (CompactStringMap: every key sparse).", "items": {"type": "object", "description": "One uncorrelated cloud-resource candidate.", "properties": {
            "id": {"type": "string", "description": "Candidate identity."},
            "name": {"type": "string", "description": "Candidate name."},
            "kind": {"type": "string", "description": "Candidate kind."},
            "resource_type": {"type": "string", "description": "Candidate resource type."},
            "provider": {"type": "string", "description": "Candidate provider (provider or source_system coalesced)."},
            "environment": {"type": "string", "description": "Candidate environment; present only when the node carries one."},
            "source": {"type": "string", "description": "Candidate source; present only when the node carries one."},
            "config_path": {"type": "string", "description": "Candidate config path; present only when the node carries one."},
            "resource_service": {"type": "string", "description": "Candidate resource service; present only when the node carries one."},
            "resource_category": {"type": "string", "description": "Candidate resource category; present only when the node carries one."},
            "resource_id": {"type": "string", "description": "Provider resource id; present only when the node carries one."},
            "arn": {"type": "string", "description": "Resource ARN; present only when the node carries one."},
            "account_id": {"type": "string", "description": "Cloud account id; present only when the node carries one."},
            "region": {"type": "string", "description": "Cloud region; present only when the node carries one."},
            "service_kind": {"type": "string", "description": "Service kind anchor; present only when the node carries one."},
            "service_anchor_status": {"type": "string", "description": "Service anchor status behind the candidate_status verdict."},
            "candidate_status": {"type": "string", "description": "Candidate verdict derived from the anchor status. Owner: deployment/cloud_resource_candidates.go cloudResourceCandidateStatus."},
            "service_anchor_reason": {"type": "string", "description": "Service anchor reason; present only when the node carries one."},
            "missing_relationship": {"type": "string", "description": "Always workload_cloud_relationship: names the relationship no candidate carries."}
          }}},
          "uncorrelated_cloud_resources_truncated": {"type": "boolean", "description": "Present and true when the uncorrelated-candidate read filled its limit window, so more candidates may exist past it. Owner: deployment/cloud_resource_candidates.go LoadUncorrelatedCloudResourceCandidatesBounded."},
          "language_breakdown": {"type": "object", "description": "Per-language file counts for the service's primary repository, derived from indexed File nodes. Present only when language data is available. Keys are language names; values are integer file counts.", "additionalProperties": {"type": "integer"}},
          "source_tool_breakdown": {"type": "object", "description": "Per-source_tool outgoing relationship-edge counts for the service's primary repository. Present only when edges carry a source_tool property. Keys are canonical source_tool tokens; values are integer edge counts.", "additionalProperties": {"type": "integer"}},
          "dependencies": {"type": "array", "description": "Repository-level dependency rows for the workload's resolved repository. Present only once a repository has been resolved.", "items": {"type": "object", "description": "One repository-to-repository dependency edge with its evidence pointers. Owner: repository/context_helpers.go QueryRepoDependencies plus copyRelationshipEvidenceMetadata and querycontract AddRelationshipConfidenceBasis.", "properties": {
            "type": {"type": "string", "description": "Edge type (DEPENDS_ON, USES_MODULE, DEPLOYS_FROM, DISCOVERS_CONFIG_IN, PROVISIONS_DEPENDENCY_FOR, READS_CONFIG_FROM, RUNS_ON, or CORRELATES_DEPLOYABLE_UNIT)."},
            "target_name": {"type": "string", "description": "Target repository display name."},
            "target_id": {"type": "string", "description": "Target repository canonical id."},
            "evidence_type": {"type": "string", "description": "Evidence type the edge was derived from; present only when the edge carries one."},
            "resolved_id": {"type": "string", "description": "Resolved evidence id behind the edge; present only when the edge carries one."},
            "generation_id": {"type": "string", "description": "Generation the edge evidence was observed at; present only when the edge carries one."},
            "confidence": {"type": "number", "description": "Correlation confidence; present only when positive."},
            "evidence_count": {"type": "integer", "description": "Supporting evidence rows; present only when positive."},
            "evidence_kinds": {"type": "array", "description": "Supporting evidence kinds; present only when the edge carries any.", "items": {"type": "string"}},
            "resolution_source": {"type": "string", "description": "How the edge was resolved (for example assertion); present only when the edge carries one."},
            "confidence_basis": {"type": "string", "description": "What the confidence rests on (assertion_override, evidence_aggregate, or evidence_constant); present only for a positive confidence. Owner: querycontract AddRelationshipConfidenceBasis."},
            "rationale": {"type": "string", "description": "Human-readable resolution rationale; present only when the edge carries one."}
          }}},
          "infrastructure": {"type": "array", "description": "Repository-level infrastructure entities (Kubernetes, Terraform, Terragrunt, ArgoCD, Helm, Kustomize, Crossplane, CloudFormation) for the workload's resolved repository. Present only once a repository has been resolved; see limitations for degraded or truncated reads.", "items": {"type": "object", "description": "One infrastructure entity from parsed content (preferred) or the graph fallback. Owner: repository/infrastructure.go queryRepoInfrastructureRows.", "properties": {
            "type": {"type": "string", "description": "Entity type (K8sResource, TerraformResource, TerraformModule, ArgoCDApplication, HelmChart, and the other canonical infrastructure types)."},
            "name": {"type": "string", "description": "Entity name."},
            "file_path": {"type": "string", "description": "Repository-relative file the entity was declared in."},
            "kind": {"type": "string", "description": "Entity kind (for example Deployment); present only when the source names one."},
            "source": {"type": "string", "description": "Module source or entity source; present only when the source names one."},
            "terraform_source": {"type": "string", "description": "Terraform module source; present only on TerraformModule and TerragruntConfig rows that carry one."},
            "config_path": {"type": "string", "description": "Terragrunt dependency config path; present only when the source names one."},
            "provider": {"type": "string", "description": "Cloud provider behind the entity; present only when the source names one."},
            "resource_type": {"type": "string", "description": "Cloud resource type; present only when the source names one."},
            "resource_service": {"type": "string", "description": "Cloud resource service; present only when the source names one."},
            "resource_category": {"type": "string", "description": "Cloud resource category; present only when the source names one."},
            "deployment_name": {"type": "string", "description": "Content-only. Deployment name metadata; present only on TerraformModule rows that carry it."},
            "includes": {"type": "array", "description": "Content-only. Terragrunt includes; present only on TerragruntConfig rows that carry any.", "items": {"type": "string"}},
            "inputs": {"type": "array", "description": "Content-only. Terragrunt inputs; present only on TerragruntConfig rows that carry any.", "items": {"type": "string"}},
            "locals": {"type": "array", "description": "Content-only. Terragrunt locals; present only on TerragruntConfig rows that carry any.", "items": {"type": "string"}}
          }}},
          "materialization_status": {"type": "string", "description": "Present only on the repository-read-model fallback path (no materialized graph Workload node yet). \"identity_only\" reports that this response was assembled from repository identity facts rather than a materialized Workload."},
          "query_basis": {"type": "string", "description": "Present only on the repository-read-model fallback path. \"repository_read_model\" reports the response was derived from repository content evidence, not a graph Workload read."},
          "handle": {"type": "string", "description": "Present only on the repository-read-model fallback path. The stable public workload:<name> handle: the display string existing clients used before the re-key scoped node ids by repository (#5385)."},
          "result_limits": {"type": "object", "description": "Additive drilldown block: bounded limit, deterministic ordering, fan-out counts (instance_count, dependent_count, consumer_count, hostname_count, entrypoint_count, network_path_count, artifact_count, infrastructure_count, entrypoint_candidate_count), truncation flag, and the next prompt tool plus context path. downstream_read_limit reports the tighter 25-row bound underneath the fan-out lists (versus limit, the 50-row rendering cap); downstream_render_limit reports the cap the downstream lists actually rendered under (the effective read bound when explicitly requested above the preview, else the 50-row cap); evidence_file_read_limit reports the separate 5,000-file bound on the service repository's own indexed-file read that consumer_repositories_truncated also folds in. When truncated is true because of dependents_truncated or provisioning_source_chains_truncated, downstream_read_limit was exceeded; when it is true only because of consumer_repositories_truncated, evidence_file_read_limit or one of the other bounds documented on that flag may be the one that fired instead; truncated is also true when the infrastructure panel's own read landed past its bound (see limitations' infrastructure_truncated) or when a deployment-artifact candidate bound bit (the three deployment_artifacts_* limitation reasons).", "additionalProperties": true},
          "limitations": {"type": "array", "description": "Raw limitation reasons attached directly to the context payload before promotion into partial_reasons. Populated on the primary graph-materialized path (not only the repository read-model fallback) when an auxiliary read degrades or lands past its bound: infrastructure_read_degraded (the graph read failed and infrastructure answered empty rather than propagating), infrastructure_truncated (a healthy read landed past its LIMIT bound; more rows may exist), hostnames_truncated, entrypoints_truncated, and network_paths_truncated (the list was cut to the 50-row cap; result_limits carries the total), api_surface_endpoints_truncated, infrastructure_rows_truncated (the infrastructure list was cut to the 50-row cap; result_limits.infrastructure_count carries the total, and infrastructure_truncated still means the read hit its own 5,000-row bound), entrypoint_candidates_truncated (the entrypoint_candidates list was cut to the 50-row cap; result_limits.entrypoint_candidate_count carries the total) and deployment_evidence_artifacts_truncated, deployment_evidence_delivery_paths_truncated, deployment_evidence_delivery_workflows_truncated and deployment_evidence_shared_config_paths_truncated (the API surface endpoints or that deployment-evidence list was cut to the 50-row cap, or its read stopped at its own bound; the count fields carry the total), relationships_read_degraded (the repository dependencies read failed and dependencies answered empty), api_surface_read_degraded (the API surface read failed), languages_read_degraded and source_tool_breakdown_read_degraded (the tech-fingerprint reads failed and the breakdown is absent rather than empty), deployment_artifacts_candidates_budget_reached (the request budget of 12,000 config-artifact candidates beyond the per-repository floor of 50 ran out, so the deployment_evidence config rows of at least one related repository are a lower bound), deployment_artifacts_related_file_list_truncated_at_5000 (a related repository has more than 5,000 files, so its candidates past the window are unknown) and deployment_artifacts_anchor_file_list_truncated_at_5000 (the service repository itself has more than 5,000 files, so the deployment evidence read from its file list is a lower bound; each of these three also sets result_limits.truncated), or workload_identity_not_materialized (repository read-model fallback with no materialized Workload node), or dependents_truncated, consumer_repositories_truncated and provisioning_source_chains_truncated (the upstream indirect-evidence read feeding that list hit the downstream_read_limit bound; result_limits.truncated is true and the story derives the same signal from its flags, so both surfaces agree).", "items": {"type": "string", "enum": ["infrastructure_read_degraded", "infrastructure_truncated", "hostnames_truncated", "entrypoints_truncated", "network_paths_truncated", "api_surface_endpoints_truncated", "infrastructure_rows_truncated", "entrypoint_candidates_truncated", "deployment_evidence_artifacts_truncated", "deployment_evidence_delivery_paths_truncated", "deployment_evidence_delivery_workflows_truncated", "deployment_evidence_shared_config_paths_truncated", "relationships_read_degraded", "api_surface_read_degraded", "languages_read_degraded", "source_tool_breakdown_read_degraded", "deployment_artifacts_candidates_budget_reached", "deployment_artifacts_related_file_list_truncated_at_5000", "deployment_artifacts_anchor_file_list_truncated_at_5000", "workload_identity_not_materialized", "dependents_truncated", "consumer_repositories_truncated", "provisioning_source_chains_truncated"]}},
          "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The evidence_detail the response was shaped with. Under handles, deployment_evidence.artifacts and api_surface.endpoints carry only identity keys, the content-derived deployment_evidence values are dropped, and truth.omissions lists each reduced family with its total."},
          "evidence_detail_drilldown": {"type": "object", "description": "Present only under handles when a family was reduced: how to fetch the full rows: repeat the call with evidence_detail full.", "additionalProperties": true},
          "partial_reasons": {"type": "array", "description": "Explicit limitations or unsupported-evidence reasons for the context read; always present so the envelope shape is stable across complete and partial reads.", "items": {"type": "string"}},
          "instances": {
            "type": "array",
            "description": "Runtime workload instances backing the deployment topology, sorted by environment then instance_id. Owner: entity/workload_runtime_topology.go FetchWorkloadRuntimeTopology over newWorkloadInstance rows.",
            "items": {
              "type": "object",
              "description": "One runtime workload instance. Owner: entity/workload_runtime_topology.go newWorkloadInstance, with platform_* promoted from the first attached direct_runtime platform by entity/workload_platform.go attachDirectPlatforms.",
              "properties": {
                "instance_id": {"type": "string", "description": "Canonical graph WorkloadInstance identity."},
                "platform_name": {"type": "string", "description": "First attached platform display name; empty when no RUNS_ON platform attached."},
                "platform_kind": {"type": "string", "description": "First attached platform kind; empty when no RUNS_ON platform attached."},
                "platforms": {
                  "type": "array",
                  "description": "Direct-runtime platforms attached to this instance; empty when none attached.",
                  "items": {
                    "type": "object",
                    "description": "One direct-runtime platform with its supporting RUNS_ON relationships. Owner: entity/workload_platform.go attachDirectPlatforms.",
                    "required": ["topology_basis"],
                    "properties": {
                      "platform_id": {"type": "string", "description": "Canonical graph Platform identity; never derived from the display name."},
                      "platform_name": {"type": "string", "description": "Platform display name from the RUNS_ON edge target."},
                      "platform_kind": {"type": "string", "description": "Platform kind from the RUNS_ON edge target; empty when the edge carries none."},
                      "platform_confidence": {"type": "number", "description": "Confidence of this attached platform (0.0 when the edge carries none)."},
                      "platform_reason": {"type": "string", "description": "Rationale of this attached platform (empty when the edge carries none)."},
                      "topology_basis": {"type": "string", "enum": ["direct_runtime"], "description": "The platform is supported by an exact WorkloadInstance RUNS_ON Platform relationship."},
                      "topology_edges": {
                        "type": "array",
                        "description": "Exact RUNS_ON relationships supporting this platform.",
                        "items": {
                          "type": "object",
                          "description": "One observed graph relationship with its evidence. Owner: entity/workload_platform_topology.go platformTopologyEdge; source_name, target_name, evidence_source and source_tool are sparse (present only when the edge properties carry them).",
                          "required": ["relationship_type", "source_id", "target_id"],
                          "properties": {
                            "relationship_type": {"type": "string", "enum": ["RUNS_ON"], "description": "Edge type: always RUNS_ON on this path."},
                            "source_id": {"type": "string", "description": "Edge source identity (the workload instance)."},
                            "source_name": {"type": "string", "description": "Edge source display name; present only when the edge properties carry one."},
                            "target_id": {"type": "string", "description": "Edge target identity (the platform)."},
                            "target_name": {"type": "string", "description": "Edge target display name; present only when the edge properties carry one."},
                            "confidence": {"type": "number", "description": "Edge confidence (edge value, else the confidence carried in the edge properties)."},
                            "reason": {"type": "string", "description": "Edge rationale (edge value, else the reason carried in the edge properties)."},
                            "evidence_source": {"type": "string", "description": "Evidence source behind the edge; present only when the edge properties carry one."},
                            "source_tool": {"type": "string", "description": "Collector tool behind the edge; present only when the edge properties carry one."},
                            "properties": {"type": "object", "additionalProperties": true, "description": "Full graph relationship properties the edge observation was selected from."}
                          }
                        }
                      }
                    }
                  }
                },
                "environment": {"type": "string", "description": "Runtime environment the instance was materialized in."},
                "materialization_confidence": {"type": "number", "description": "Finite graph float behind the instance materialization. Owner: entity/workload_runtime_topology.go newWorkloadInstance."},
                "materialization_provenance": {"type": "array", "description": "Provenance values behind the instance materialization.", "items": {"type": "string"}},
                "platform_confidence": {"type": "number", "description": "Confidence of the first attached platform (0.0 when no RUNS_ON platform attached)."},
                "platform_reason": {"type": "string", "description": "Rationale of the first attached platform (empty when no RUNS_ON platform attached)."}
              }
            }
          },
          "topology_edges": {
            "type": "array",
            "description": "Exact graph-observed Repository DEFINES Workload and WorkloadInstance INSTANCE_OF Workload edges, retained only for non-truncated instances. Owner: entity/workload_runtime_topology.go observedTopologyEdge; source_name, target_name, confidence, reason, evidence_source and source_tool ride the same platformTopologyEdge shape as the platform edges.",
            "items": {
              "type": "object",
              "description": "One observed DEFINES or INSTANCE_OF edge with its evidence.",
              "required": ["relationship_type", "source_id", "target_id", "properties"],
              "properties": {
                "relationship_type": {"type": "string", "enum": ["DEFINES", "INSTANCE_OF"], "description": "Edge type: repository DEFINES workload, or instance INSTANCE_OF workload."},
                "source_id": {"type": "string", "description": "Edge source identity (repository for DEFINES, instance for INSTANCE_OF)."},
                "source_name": {"type": "string", "description": "Edge source display name; present only when the edge properties carry one."},
                "target_id": {"type": "string", "description": "Edge target identity (workload)."},
                "target_name": {"type": "string", "description": "Edge target display name; present only when the edge properties carry one."},
                "confidence": {"type": "number", "description": "Edge confidence (edge value, else the confidence carried in the edge properties)."},
                "reason": {"type": "string", "description": "Edge rationale (edge value, else the reason carried in the edge properties)."},
                "evidence_source": {"type": "string", "description": "Evidence source behind the edge; present only when the edge properties carry one."},
                "source_tool": {"type": "string", "description": "Collector tool behind the edge; present only when the edge properties carry one."},
                "properties": {"type": "object", "additionalProperties": true, "description": "Full graph relationship properties the edge observation was selected from."}
              }
            }
          },
          "provisioned_platforms": {
            "type": "array",
            "description": "Repository-level provisioning evidence kept separate from runtime instance placement. Owner: entity/workload_provisioned_platforms.go FetchProvisionedPlatformResult over normalizeProvisionedPlatform rows.",
            "items": {
              "type": "object",
              "description": "One fallback platform with its supporting provisioning relationships.",
              "required": ["topology_basis"],
              "properties": {
                "platform_id": {"type": "string", "description": "Canonical graph Platform identity; never derived from the display name."},
                "platform_name": {"type": "string", "description": "Platform display name from the provisioning edge target."},
                "platform_kind": {"type": "string", "description": "Platform kind from the provisioning edge target."},
                "platform_provider": {"type": "string", "description": "Platform provider from the provisioning edge target; empty when the node carries none."},
                "platform_region": {"type": "string", "description": "Platform region from the provisioning edge target; empty when the node carries none."},
                "platform_locator": {"type": "string", "description": "Platform locator from the provisioning edge target."},
                "platform_confidence": {"type": "number", "description": "Confidence of this fallback platform (0.0 when the edge carries none)."},
                "platform_reason": {"type": "string", "description": "Rationale of this fallback platform (empty when the edge carries none)."},
                "topology_basis": {"type": "string", "enum": ["provisioning_fallback"], "description": "The platform is supported by exact repository provisioning relationships, not a WorkloadInstance RUNS_ON relationship."},
                "topology_edges": {
                  "type": "array",
                  "description": "Exact canonical provisioning relationships supporting this fallback platform.",
                  "items": {
                    "type": "object",
                    "description": "One observed provisioning relationship with its evidence. Owner: entity/workload_platform_topology.go platformTopologyEdge; source_name, target_name, evidence_source and source_tool are sparse (present only when the edge properties carry them).",
                    "required": ["relationship_type", "source_id", "target_id", "properties"],
                    "properties": {
                      "relationship_type": {"type": "string", "enum": ["PROVISIONS_DEPENDENCY_FOR", "PROVISIONS_PLATFORM"], "description": "Provisioning edge type."},
                      "source_id": {"type": "string", "description": "Edge source identity (the provisioning repository)."},
                      "source_name": {"type": "string", "description": "Edge source display name; present only when the edge properties carry one."},
                      "target_id": {"type": "string", "description": "Edge target identity (the platform, or the dependent repository for PROVISIONS_DEPENDENCY_FOR)."},
                      "target_name": {"type": "string", "description": "Edge target display name; present only when the edge properties carry one."},
                      "confidence": {"type": "number", "description": "Edge confidence (edge value, else the confidence carried in the edge properties)."},
                      "reason": {"type": "string", "description": "Edge rationale (edge value, else the reason carried in the edge properties)."},
                      "evidence_source": {"type": "string", "description": "Evidence source behind the edge; present only when the edge properties carry one."},
                      "source_tool": {"type": "string", "description": "Collector tool behind the edge; present only when the edge properties carry one."},
                      "properties": {"type": "object", "additionalProperties": true, "description": "Full graph relationship properties the edge observation was selected from."}
                    }
                  }
                }
              }
            }
          },
          "runtime_topology_limits": ` + schema.ImpactRuntimeTopologyLimits + `
        }
      },
      "BrowserSessionAuth": {
        "type": "object",
        "description": "Authorization context attached to a server-managed dashboard browser session. Subject and policy identifiers are hashes or stable opaque ids; raw credentials are never returned. Owner: auth/session/browser_types.go BrowserSessionAuthResponse, built by BrowserSessionAuthResponseFor.",
        "properties": {
          "mode": {"type": "string", "enum": ["browser_session"], "description": "Always browser_session: a server-managed dashboard session, never a bearer or scoped token."},
          "tenant_id": {"type": "string", "description": "Tenant the session is scoped to; empty when the session carries no tenant."},
          "workspace_id": {"type": "string", "description": "Workspace the session is scoped to; empty when the session carries no workspace."},
          "subject_class": {"type": "string", "description": "Subject class the session was issued for (for example user or service identity); empty when unknown."},
          "subject_id_hash": {"type": "string", "description": "Non-reversible hash of the authenticated subject id; never the raw subject."},
          "policy_revision_hash": {"type": "string", "description": "Stable opaque id of the sign-in policy revision the session was issued under."},
          "role_ids": {"type": "array", "description": "Role ids granted to the session subject; empty when the session carries no roles.", "items": {"type": "string"}},
          "all_scopes": {"type": "boolean", "description": "Whether the session bypasses scope grants (all-scopes session)."},
          "allowed_scope_ids": {"type": "array", "description": "Scope ids the session may read; empty when the session carries no scope grant.", "items": {"type": "string"}},
          "allowed_repository_ids": {"type": "array", "description": "Repository ids the session may read; empty when the session carries no repository grant.", "items": {"type": "string"}},
          "permission_catalog_enforced": {"type": "boolean", "description": "Whether the permission catalog gates this session's feature access."},
          "allowed_permission_features": {"type": "array", "description": "Permission-catalog feature names the session may use; present only when the catalog enforces the session.", "items": {"type": "string"}},
          "external_provider_config_id": {"type": "string", "description": "Stored OIDC/SAML provider config id for sessions established via an external IdP; omitted for local sessions. Owner: auth/session/browser_types.go BrowserSessionAuthResponseFor."}
        }
      },
      "BrowserSessionResponse": {
        "type": "object",
        "description": "Dashboard browser session response. csrf_token appears only when creating a session; the raw session secret is never returned in JSON and is sent only via the HttpOnly session cookie. Owner: auth/session/browser_types.go BrowserSessionResponse.",
        "properties": {
          "auth": {"$ref": "#/components/schemas/BrowserSessionAuth"},
          "csrf_token": {"type": "string", "description": "CSRF secret for X-Eshu-CSRF on unsafe cookie-authenticated requests. It is bound to the server-side session hash."},
          "idle_expires_at": {"type": "string", "format": "date-time", "description": "Idle expiry of the session (last activity plus the tenant idle timeout); omitted when the session carries no expiry."},
          "absolute_expires_at": {"type": "string", "format": "date-time", "description": "Absolute expiry of the session (creation plus the tenant absolute timeout); omitted when the session carries no expiry."}
        }
      },
`
