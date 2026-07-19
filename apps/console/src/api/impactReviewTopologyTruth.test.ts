import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

describe("Impact deployment topology truth", () => {
  it("keeps runtime environment as instance evidence without inventing a graph edge", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [{ environment: "prod", instance_id: "instance:catalog:prod", platforms: [] }],
      }),
    );

    expect(review.deploymentTrace.status).toBe("ready");
    if (review.deploymentTrace.status !== "ready") return;
    expect(review.deploymentTrace.data.instances[0]?.environment).toBe("prod");
    expect(review.graph.nodes.some((node) => node.kind === "env")).toBe(false);
    expect(review.graph.edges.some((edge) => edge.verb === "MATERIALIZED_IN_ENVIRONMENT")).toBe(
      false,
    );
  });

  it("renders exact provisioning fallback edges without inventing RUNS_ON", async () => {
    const review = await loadReview(
      deploymentTrace({
        deployment_sources: [],
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [],
          },
        ],
        provisioned_platforms: [
          {
            platform_id: "platform:ecs:shared",
            platform_kind: "ecs",
            platform_name: "shared",
            topology_basis: "provisioning_fallback",
            topology_edges: [
              {
                confidence: 0.93,
                evidence_source: "canonical_graph",
                reason: "infrastructure repository provisions the service repository",
                relationship_type: "PROVISIONS_DEPENDENCY_FOR",
                source_id: "repository:r_runtime",
                source_name: "runtime-config",
                source_tool: "terraform",
                target_id: "repository:r_catalog",
              },
              {
                relationship_type: "PROVISIONS_PLATFORM",
                source_id: "repository:r_runtime",
                source_name: "runtime-config",
                target_id: "platform:ecs:shared",
                target_name: "shared",
              },
            ],
          },
        ],
      }),
    );

    expect(review.deploymentTrace.status).toBe("ready");
    if (review.deploymentTrace.status !== "ready") return;
    expect(review.deploymentTrace.data.instances[0]?.platforms).toHaveLength(0);
    expect(review.deploymentTrace.data.provisionedPlatforms[0]).toMatchObject({
      topologyBasis: "provisioning_fallback",
      topologyEdges: [
        expect.objectContaining({
          confidence: 0.93,
          evidenceSource: "canonical_graph",
          relationshipType: "PROVISIONS_DEPENDENCY_FOR",
          sourceTool: "terraform",
        }),
        expect.objectContaining({ relationshipType: "PROVISIONS_PLATFORM" }),
      ],
    });
    expect(review.graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          layer: "infra",
          s: "repository:r_runtime",
          t: "repository:r_catalog",
          verb: "PROVISIONS_DEPENDENCY_FOR",
        }),
        expect.objectContaining({
          layer: "infra",
          s: "repository:r_runtime",
          t: "platform:ecs:shared",
          verb: "PROVISIONS_PLATFORM",
        }),
      ]),
    );
    expect(review.graph.edges.some((edge) => edge.verb === "RUNS_ON")).toBe(false);
    expect(review.graph.nodes).toContainEqual(
      expect.objectContaining({ id: "repository:r_runtime", label: "runtime-config" }),
    );
  });

  it("keeps an empty trace out of deployment-topology mode", async () => {
    const review = await loadReview(
      deploymentTrace({
        cloud_resources: [],
        deployment_sources: [],
        instances: [],
        k8s_resources: [],
      }),
    );

    expect(review.graphPresentation.mode).toBe("change_surface");
    expect(review.graph.edges).toHaveLength(0);
  });

  it("fails closed when a returned topology edge lacks an exact endpoint", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [
              {
                platform_id: "platform:ecs:prod",
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [
                  {
                    relationship_type: "RUNS_ON",
                    source_id: "instance:catalog:prod",
                  },
                ],
              },
            ],
          },
        ],
      }),
    );

    expect(review.graph.edges.some((edge) => edge.verb === "RUNS_ON")).toBe(false);
    expect(review.graphPresentation.limitations).toContain(
      "RUNS_ON edge omitted because an endpoint lacks canonical identity",
    );
  });

  it("counts malformed and unknown topology rows as disclosed omissions", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [
              {
                platform_id: "platform:ecs:prod",
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [
                  null,
                  { relationship_type: "UNKNOWN", source_id: "x", target_id: "y" },
                  {
                    relationship_type: "RUNS_ON",
                    source_id: "instance:catalog:prod",
                    target_id: "platform:ecs:prod",
                  },
                ],
              },
            ],
          },
        ],
        topology_edges: [
          null,
          { relationship_type: "UNKNOWN", source_id: "x", target_id: "y" },
          {
            relationship_type: "DEFINES",
            source_id: "repository:r_catalog",
            target_id: "workload:catalog-api",
          },
          {
            relationship_type: "INSTANCE_OF",
            source_id: "instance:catalog:prod",
            target_id: "workload:catalog-api",
          },
        ],
      }),
    );

    expect(review.deploymentTrace.status).toBe("ready");
    if (review.deploymentTrace.status !== "ready") return;
    expect(review.deploymentTrace.data.invalidTopologyEdgeCount).toBe(4);
    expect(review.graphPresentation.omittedEdges).toBeGreaterThanOrEqual(4);
    expect(review.graphPresentation.limitations).toContain(
      "4 topology rows omitted because their relationship shape is unsupported or malformed",
    );
  });
});

async function loadReview(trace: Record<string, unknown>) {
  const client = new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/api/v0/impact/change-surface/investigate") {
        return Response.json({ data: zeroChangeSurface(), error: null, truth: truth("derived") });
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return Response.json({ data: trace, error: null, truth: truth("exact") });
      }
      throw new Error(`unexpected request ${path}`);
    },
  });
  return loadImpactReview(client, { target: "catalog-api", targetKind: "service" });
}

function deploymentTrace(overrides: Record<string, unknown>): Record<string, unknown> {
  return {
    cloud_resources: [],
    deployment_sources: [
      {
        relationship_type: "DEPLOYS_FROM",
        repo_id: "repository:r_config",
        repo_name: "deployment-config",
        source_id: "repository:r_config",
        target_id: "repository:r_catalog",
      },
    ],
    instances: [],
    k8s_resources: [],
    repo_id: "repository:r_catalog",
    repo_name: "catalog-api",
    service_name: "catalog-api",
    story: "catalog-api deployment trace",
    workload_id: "workload:catalog-api",
    ...overrides,
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
