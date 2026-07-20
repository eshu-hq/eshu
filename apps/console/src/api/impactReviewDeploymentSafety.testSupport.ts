import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

export async function loadReview(trace: Record<string, unknown>, unavailableIdentity = false) {
  const client = new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/api/v0/impact/change-surface/investigate") {
        return unavailableIdentity
          ? Response.json({
              data: null,
              error: { code: "graph_unavailable", message: "identity query unavailable" },
              truth: null,
            })
          : Response.json({ data: zeroChangeSurface(), error: null, truth: truth("exact") });
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return Response.json({ data: trace, error: null, truth: truth("exact") });
      }
      throw new Error(`unexpected request ${path}`);
    },
  });
  return loadImpactReview(client, { target: "catalog-api", targetKind: "service" });
}

export function deploymentTrace(overrides: Record<string, unknown>): Record<string, unknown> {
  return {
    cloud_resource_limits: completeCloudResourceLimits(0),
    cloud_resources: [],
    deployment_source_limits: {
      canonical_observed_count: 0,
      limit: 50,
      observed_count: 0,
      observed_count_is_lower_bound: false,
      ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
      query_sentinel_limit: 51,
      repository_observed_count: 0,
      returned_count: 0,
      truncated: false,
    },
    deployment_sources: [],
    instances: [],
    k8s_resource_limits: completeKubernetesLimits(0),
    k8s_resources: [],
    provisioned_platforms: [],
    repo_id: "repository:r_catalog",
    repo_name: "catalog-api",
    service_name: "catalog-api",
    story: "catalog-api deployment trace",
    topology_edges: [],
    workload_id: "workload:catalog-api",
    ...overrides,
  };
}

export function topologyEdge(
  relationshipType: string,
  sourceId: string,
  targetId: string,
): Record<string, unknown> {
  return {
    confidence: 0.99,
    evidence_source: "canonical_graph",
    reason: "exact retained edge",
    relationship_type: relationshipType,
    source_id: sourceId,
    target_id: targetId,
  };
}

export function completeRuntimeTopologyLimits(
  instances: number,
  platformEdges: number,
  provisionedPlatforms: number,
): Record<string, unknown> {
  return {
    instances: completeCollectionLimits(instances),
    platform_edges: completeCollectionLimits(platformEdges),
    provisioned_platforms: completeCollectionLimits(provisionedPlatforms),
  };
}

function completeCollectionLimits(returnedCount: number): Record<string, unknown> {
  return {
    limit: 50,
    observed_count: returnedCount,
    observed_count_is_lower_bound: false,
    ordering: ["canonical_identity"],
    query_sentinel_limit: 51,
    returned_count: returnedCount,
    truncated: false,
  };
}

function completeCloudResourceLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    observation_count: returnedCount,
    observation_count_is_lower_bound: false,
    observation_limit: 2500,
    observation_query_sentinel_limit: 2501,
  };
}

function completeKubernetesLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    content_observed_count: returnedCount,
    content_observed_count_is_lower_bound: false,
    deployment_source_observed_count: 0,
    deployment_source_observed_count_is_lower_bound: false,
    deployment_source_query_sentinel_limit: 201,
  };
}

function truth(level: string): Record<string, unknown> {
  return {
    basis: "authoritative_graph",
    capability: "platform_impact.deployment_chain",
    freshness: { state: "fresh" },
    level,
    profile: "local_authoritative",
  };
}

function zeroChangeSurface(): Record<string, unknown> {
  return {
    code_surface: {
      changed_files: [],
      matched_file_count: 0,
      source_backends: [],
      symbol_count: 0,
    },
    coverage: { direct_count: 0, limit: 25, max_depth: 4, transitive_count: 0, truncated: false },
    direct_impact: [],
    impact_summary: { direct_count: 0, total_count: 0, transitive_count: 0 },
    source_backend: "authoritative_graph",
    target_resolution: {
      input: "catalog-api",
      selected: { id: "workload:catalog-api", labels: ["Workload"], name: "catalog-api" },
      status: "resolved",
      target_type: "service",
      truncated: false,
    },
    transitive_impact: [],
    truncated: false,
  };
}
