import { loadDeploymentReview } from "./impactReviewDeploymentGraph.testSupport";

describe("Impact deployment topology basis completeness", () => {
  it.each([
    ["missing", undefined],
    ["unsupported", "name_inference"],
  ])(
    "marks completeness unverified when a direct runtime platform has %s topology_basis",
    async (_reason, topologyBasis) => {
      const review = await loadDeploymentReview(
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
                  ...(topologyBasis === undefined ? {} : { topology_basis: topologyBasis }),
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
            topologyEdge(
              "INSTANCE_OF",
              "workload-instance:catalog-api:prod",
              "workload:catalog-api",
            ),
          ],
        }),
      );

      expect(review.graphPresentation.limitations).toContain(
        "runtime topology basis unverified; expected direct_runtime",
      );
      expect(review.graphPresentation.completeness).toBe("unverified");
    },
  );

  it.each([
    ["missing", undefined],
    ["unsupported", "name_inference"],
  ])(
    "marks completeness unverified when a provisioned platform has %s topology_basis",
    async (_reason, topologyBasis) => {
      const review = await loadDeploymentReview(
        deploymentTrace({
          provisioned_platforms: [
            {
              platform_id: "platform:ecs:shared",
              platform_kind: "ecs",
              platform_name: "shared",
              ...(topologyBasis === undefined ? {} : { topology_basis: topologyBasis }),
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
          runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 1),
          topology_edges: [topologyEdge("DEFINES", "repository:r_catalog", "workload:catalog-api")],
        }),
      );

      expect(review.graphPresentation.limitations).toContain(
        "provisioning topology basis unverified; expected provisioning_fallback",
      );
      expect(review.graphPresentation.completeness).toBe("unverified");
    },
  );
});

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
    runtime_topology_limits: completeRuntimeTopologyLimits(0, 0, 0),
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
