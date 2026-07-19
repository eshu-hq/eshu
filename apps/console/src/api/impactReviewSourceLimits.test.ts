import type { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

describe("impact deployment-source coverage normalization", () => {
  it("preserves the complete bounded-query coverage contract", async () => {
    const result = await loadImpactReview(clientWithDeploymentSourceLimits(), {
      target: "catalog-api",
      targetKind: "service",
    });

    expect(result.deploymentTrace.status).toBe("ready");
    if (result.deploymentTrace.status !== "ready") return;

    expect(result.deploymentTrace.data.deploymentSourceLimits).toEqual({
      canonicalObservedCount: 51,
      limit: 50,
      observedCount: 51,
      observedCountIsLowerBound: true,
      ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
      querySentinelLimit: 51,
      repositoryObservedCount: 3,
      returnedCount: 50,
      truncated: true,
    });
    expect(result.graphPresentation).toMatchObject({
      inputEdges: 52,
      mode: "deployment_trace",
      omittedEdges: 1,
      renderedEdges: 51,
      truncated: true,
    });
    expect(result.graphPresentation.limitations).toContain(
      "deployment-source input truncated upstream; at least 1 relationship was not returned",
    );
  });

  it("keeps the deployment trace usable when coverage metadata is malformed", async () => {
    const result = await loadImpactReview(clientWithDeploymentSourceLimits(null), {
      target: "catalog-api",
      targetKind: "service",
    });

    expect(result.deploymentTrace.status).toBe("ready");
    if (result.deploymentTrace.status !== "ready") return;
    expect(result.deploymentTrace.data.deploymentSourceLimits).toBeNull();
  });

  it.each([
    ["returned count differs from the response rows", { returned_count: 49 }],
    ["returned count exceeds the response limit", { limit: 49 }],
    ["observed count is smaller than the returned count", { observed_count: 49 }],
    ["a lower-bound observation is not marked truncated", { truncated: false }],
  ])("rejects contradictory coverage metadata when %s", async (_reason, override) => {
    const result = await loadImpactReview(
      clientWithDeploymentSourceLimits({ ...validDeploymentSourceLimits(), ...override }),
      {
        target: "catalog-api",
        targetKind: "service",
      },
    );

    expect(result.deploymentTrace.status).toBe("ready");
    if (result.deploymentTrace.status !== "ready") return;
    expect(result.deploymentTrace.data.deploymentSourceLimits).toBeNull();
  });

  it("rejects contradictory runtime-topology coverage metadata", async () => {
    const result = await loadImpactReview(
      clientWithDeploymentSourceLimits(validDeploymentSourceLimits(), {
        instances: { ...completeRuntimeLimits(), returned_count: 1 },
        platform_edges: completeRuntimeLimits(),
        provisioned_platforms: completeRuntimeLimits(),
      }),
      { target: "catalog-api", targetKind: "service" },
    );

    expect(result.deploymentTrace.status).toBe("ready");
    if (result.deploymentTrace.status !== "ready") return;
    expect(result.deploymentTrace.data.runtimeTopologyLimits).toBeNull();
    expect(result.graphPresentation.limitations).toContain(
      "runtime-topology completeness unverified because collection metadata is unavailable",
    );
  });
});

function clientWithDeploymentSourceLimits(
  deploymentSourceLimits: unknown = validDeploymentSourceLimits(),
  runtimeTopologyLimits?: unknown,
): EshuApiClient {
  return {
    post: async (path: string) => {
      if (path === "/api/v0/impact/change-surface/investigate") {
        return { data: zeroChangeSurface(), error: null, truth: null };
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return {
          data: {
            deployment_source_limits: deploymentSourceLimits,
            deployment_sources: Array.from({ length: 50 }, (_, index) => ({
              relationship_type: "DEPLOYS_FROM",
              repo_id: `repository:r_config_${index}`,
              repo_name: `deployment-config-${index}`,
              source_id: `repository:r_config_${index}`,
              target_id: "repository:r_catalog",
            })),
            instances: [],
            repo_id: "repository:r_catalog",
            repo_name: "catalog-api",
            runtime_topology_limits: runtimeTopologyLimits,
            service_name: "catalog-api",
            story: "Deployment source results are bounded.",
            topology_edges: [
              {
                relationship_type: "DEFINES",
                source_id: "repository:r_catalog",
                target_id: "workload:catalog-api",
              },
            ],
            workload_id: "workload:catalog-api",
          },
          error: null,
          truth: null,
        };
      }
      throw new Error(`unexpected path ${path}`);
    },
  } as unknown as EshuApiClient;
}

function completeRuntimeLimits(): Record<string, unknown> {
  return {
    limit: 50,
    observed_count: 0,
    observed_count_is_lower_bound: false,
    ordering: ["canonical_identity"],
    query_sentinel_limit: 51,
    returned_count: 0,
    truncated: false,
  };
}

function validDeploymentSourceLimits(): Record<string, unknown> {
  return {
    canonical_observed_count: 51,
    limit: 50,
    observed_count: 51,
    observed_count_is_lower_bound: true,
    ordering: ["relationship_type_priority", "repo_name", "source_id", "target_id"],
    query_sentinel_limit: 51,
    repository_observed_count: 3,
    returned_count: 50,
    truncated: true,
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
