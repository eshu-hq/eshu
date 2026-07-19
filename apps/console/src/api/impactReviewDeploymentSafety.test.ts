import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

describe("Impact deployment topology safety", () => {
  it("renders the exact repository, workload, instance, and platform relationship backbone", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [
              {
                platform_id: "platform:ecs:prod",
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [
                  topologyEdge(
                    "RUNS_ON",
                    "workload-instance:catalog-api:prod",
                    "platform:ecs:prod",
                  ),
                ],
              },
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 1, 0),
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api"),
          topologyEdge("INSTANCE_OF", "workload-instance:catalog-api:prod", "workload:catalog-api"),
        ],
      }),
    );

    const definesEdge = review.graph.edges.find((edge) => edge.verb === "DEFINES");
    expect(definesEdge).toMatchObject({
      s: "repository:r_catalog",
      t: "workload:catalog-api",
      verb: "DEFINES",
    });
    expect(definesEdge?.evidence).toEqual(
      expect.arrayContaining(["canonical_graph", "exact retained edge"]),
    );
    expect(review.graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          s: "workload-instance:catalog-api:prod",
          t: "workload:catalog-api",
          verb: "INSTANCE_OF",
        }),
        expect.objectContaining({
          s: "workload-instance:catalog-api:prod",
          t: "platform:ecs:prod",
          verb: "RUNS_ON",
        }),
      ]),
    );
    expect(review.graphPresentation.completeness).toBe("complete");
  });

  it("keeps repository provisioning separate from direct instance placement", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
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
              topologyEdge(
                "PROVISIONS_DEPENDENCY_FOR",
                "repository:r_runtime",
                "repository:r_catalog",
              ),
              topologyEdge("PROVISIONS_PLATFORM", "repository:r_runtime", "platform:ecs:shared"),
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 0, 1),
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api"),
          topologyEdge("INSTANCE_OF", "workload-instance:catalog-api:prod", "workload:catalog-api"),
        ],
      }),
    );

    expect(review.deploymentTrace.status).toBe("ready");
    if (review.deploymentTrace.status !== "ready") return;
    expect(review.deploymentTrace.data.instances[0]?.platforms).toEqual([]);
    expect(review.deploymentTrace.data.provisionedPlatforms).toHaveLength(1);
    expect(review.graph.edges.some((edge) => edge.verb === "RUNS_ON")).toBe(false);
    expect(review.graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ verb: "PROVISIONS_DEPENDENCY_FOR" }),
        expect.objectContaining({ verb: "PROVISIONS_PLATFORM" }),
      ]),
    );
    expect(review.graphPresentation.completeness).toBe("complete");
  });

  it.each([
    [
      "the exact INSTANCE_OF relationship",
      {
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 0, 0),
        topology_edges: [topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api")],
      },
      "subject relationship backbone incomplete; exact INSTANCE_OF edges were not returned",
    ],
    [
      "the exact DEFINES relationship",
      {
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 0, 0),
        topology_edges: [
          topologyEdge("INSTANCE_OF", "workload-instance:catalog-api:prod", "workload:catalog-api"),
        ],
      },
      "subject relationship backbone incomplete; exact DEFINES edge was not returned",
    ],
  ])("marks completeness unverified when %s is missing", async (_reason, overrides, limitation) => {
    const review = await loadReview(deploymentTrace(overrides));

    expect(review.graphPresentation.limitations).toContain(limitation);
    expect(review.graphPresentation.completeness).toBe("unverified");
  });

  it.each([
    [
      "a direct runtime platform lacks its exact RUNS_ON relationship",
      {
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [
              {
                platform_id: "platform:ecs:prod",
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [],
              },
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 1, 0),
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api"),
          topologyEdge("INSTANCE_OF", "workload-instance:catalog-api:prod", "workload:catalog-api"),
        ],
      },
      "runtime relationship backbone incomplete; exact RUNS_ON edge was not returned",
    ],
    [
      "a provisioned platform lacks its exact repository dependency relationship",
      {
        provisioned_platforms: [
          {
            platform_id: "platform:ecs:shared",
            platform_kind: "ecs",
            platform_name: "shared",
            topology_basis: "provisioning_fallback",
            topology_edges: [
              topologyEdge("PROVISIONS_PLATFORM", "repository:r_runtime", "platform:ecs:shared"),
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 1),
        topology_edges: [topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api")],
      },
      "provisioning relationship backbone incomplete; exact PROVISIONS_DEPENDENCY_FOR edge was not returned",
    ],
    [
      "a provisioned platform lacks its exact platform relationship",
      {
        provisioned_platforms: [
          {
            platform_id: "platform:ecs:shared",
            platform_kind: "ecs",
            platform_name: "shared",
            topology_basis: "provisioning_fallback",
            topology_edges: [
              topologyEdge(
                "PROVISIONS_DEPENDENCY_FOR",
                "repository:r_runtime",
                "repository:r_catalog",
              ),
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 1),
        topology_edges: [topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api")],
      },
      "provisioning relationship backbone incomplete; exact PROVISIONS_PLATFORM edge was not returned",
    ],
  ])("marks completeness unverified when %s", async (_reason, overrides, limitation) => {
    const review = await loadReview(deploymentTrace(overrides));

    expect(review.graphPresentation.limitations).toContain(limitation);
    expect(review.graphPresentation.completeness).toBe("unverified");
  });

  it("omits topology relationships whose endpoints do not match the selected subject", async () => {
    const review = await loadReview(
      deploymentTrace({
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [
              {
                platform_id: "platform:ecs:prod",
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [
                  topologyEdge("RUNS_ON", "workload-instance:other:prod", "platform:ecs:prod"),
                ],
              },
            ],
          },
        ],
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_other", "workload:catalog-api"),
          topologyEdge("INSTANCE_OF", "workload-instance:other:prod", "workload:catalog-api"),
        ],
      }),
    );

    expect(review.graph.edges.some((edge) => edge.s.includes("other"))).toBe(false);
    expect(review.graphPresentation.limitations).toEqual(
      expect.arrayContaining([
        "DEFINES edge omitted because it does not match the selected repository and workload",
        "INSTANCE_OF edge omitted because it does not match a returned runtime instance",
        "RUNS_ON edge omitted because it does not match the containing instance and platform",
      ]),
    );
  });

  it.each([
    [
      "a runtime platform has no canonical identity",
      {
        instances: [
          {
            environment: "prod",
            instance_id: "workload-instance:catalog-api:prod",
            platforms: [
              {
                platform_kind: "ecs",
                platform_name: "prod",
                topology_basis: "direct_runtime",
                topology_edges: [],
              },
            ],
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(1, 1, 0),
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api"),
          topologyEdge("INSTANCE_OF", "workload-instance:catalog-api:prod", "workload:catalog-api"),
        ],
      },
    ],
    [
      "a deployment-source relationship has no exact endpoints",
      {
        deployment_source_limits: {
          canonical_observed_count: 1,
          limit: 50,
          observed_count: 1,
          observed_count_is_lower_bound: false,
          ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
          query_sentinel_limit: 51,
          repository_observed_count: 0,
          returned_count: 1,
          truncated: false,
        },
        deployment_sources: [
          {
            relationship_type: "DEPLOYS_FROM",
            repo_id: "repository:r_config",
            repo_name: "deployment-config",
          },
        ],
        runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 0),
        topology_edges: [topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api")],
      },
    ],
    [
      "an unsupported topology row is omitted",
      {
        runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 0),
        topology_edges: [
          topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api"),
          topologyEdge("UNSUPPORTED", "repository:r_catalog", "workload:catalog-api"),
        ],
      },
    ],
    [
      "a subject edge does not match the selected repository and workload",
      {
        runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 0),
        topology_edges: [topologyEdge("DEFINES", "repository:r_other", "workload:catalog-api")],
      },
    ],
  ])("marks completeness unverified when %s", async (_reason, overrides) => {
    const review = await loadReview(deploymentTrace(overrides));

    expect(review.graphPresentation.completeness).toBe("unverified");
  });

  it("withholds a name-selected trace when change-surface identity verification is unavailable", async () => {
    const review = await loadReview(deploymentTrace({}), true);

    expect(review.changeSurface.status).toBe("unavailable");
    expect(review.deploymentTrace.status).toBe("ready");
    expect(review.graphPresentation.mode).toBe("empty");
    expect(review.graphPresentation.limitations).toContain(
      "deployment topology not selected because exact service identity verification is unavailable",
    );
  });
});

async function loadReview(trace: Record<string, unknown>, unavailableIdentity = false) {
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

function deploymentTrace(overrides: Record<string, unknown>): Record<string, unknown> {
  return {
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

function topologyEdge(
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

function completeRuntimeTopologyLimits(
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
