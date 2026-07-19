import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

export async function loadDeploymentReview(
  trace: Record<string, unknown>,
  freshness = "fresh",
  traceLevel: string | null = "exact",
  changeSurface: Record<string, unknown> = zeroChangeSurface(),
  input: { readonly target: string; readonly targetKind: "service" | "workload" } = {
    target: "catalog-api",
    targetKind: "service",
  },
) {
  const client = new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/api/v0/impact/change-surface/investigate") {
        return Response.json({
          data: changeSurface,
          error: null,
          truth: truthEnvelope("derived", freshness),
        });
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return Response.json({
          data: trace,
          error: null,
          truth: traceLevel === null ? null : truthEnvelope(traceLevel, freshness),
        });
      }
      throw new Error(`unexpected request ${path}`);
    },
  });
  return loadImpactReview(client, input);
}

export function ambiguousChangeSurface(): Record<string, unknown> {
  return {
    ...zeroChangeSurface(),
    target_resolution: {
      candidates: [
        { id: "workload:catalog-api-a", labels: ["Workload"], name: "catalog-api-a" },
        { id: "workload:catalog-api-b", labels: ["Workload"], name: "catalog-api-b" },
      ],
      input: "catalog-api",
      status: "ambiguous",
      target_type: "service",
      truncated: false,
    },
  };
}

export function nonEmptyChangeSurface(): Record<string, unknown> {
  return {
    ...zeroChangeSurface(),
    coverage: {
      direct_count: 1,
      limit: 25,
      max_depth: 4,
      query_shape: "resolved_change_surface_traversal",
      transitive_count: 0,
      truncated: false,
    },
    direct_impact: [
      {
        depth: 1,
        id: "workload:consumer",
        labels: ["Workload"],
        name: "consumer",
        repo_id: "repository:r_consumer",
      },
    ],
    impact_summary: { direct_count: 1, total_count: 1, transitive_count: 0 },
  };
}

export function deploymentTracePayload(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    cloud_resources: [],
    deployment_sources: [deploymentSource()],
    instances: [],
    k8s_resources: [],
    repo_id: "repository:r_catalog",
    repo_name: "catalog-api",
    service_name: "catalog-api",
    story: "catalog-api has exact deployment topology.",
    workload_id: "workload:catalog-api",
    ...overrides,
  };
}

export function deploymentSource(): Record<string, unknown> {
  return {
    confidence: 0.98,
    reason: "canonical deployment source",
    relationship_type: "DEPLOYS_FROM",
    repo_id: "repository:r_config",
    repo_name: "deployment-config",
    source_id: "repository:r_config",
    target_id: "repository:r_catalog",
  };
}

export function directRuntimeTopology(sourceID: string, targetID: string): Record<string, unknown> {
  return {
    topology_basis: "direct_runtime",
    topology_edges: [
      {
        relationship_type: "RUNS_ON",
        source_id: sourceID,
        target_id: targetID,
      },
    ],
  };
}

function truthEnvelope(level: string, freshness: string): Record<string, unknown> {
  return {
    basis: "authoritative_graph",
    capability: "platform_impact.deployment_chain",
    freshness: { state: freshness },
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
      touched_symbols: [],
    },
    coverage: {
      direct_count: 0,
      limit: 25,
      max_depth: 4,
      query_shape: "resolved_change_surface_traversal",
      transitive_count: 0,
      truncated: false,
    },
    direct_impact: [],
    impact_summary: { direct_count: 0, total_count: 0, transitive_count: 0 },
    scope: { limit: 25, max_depth: 4, target: "catalog-api", target_type: "service" },
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
