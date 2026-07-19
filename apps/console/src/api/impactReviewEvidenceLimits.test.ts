import {
  deploymentTracePayload,
  loadDeploymentReview,
} from "./impactReviewDeploymentGraph.testSupport";

describe("impact deployment evidence coverage", () => {
  it.each([
    [
      "cloud resources",
      "cloud_resource_limits",
      "cloud_resources",
      { id: "cloud:queue", name: "events", resource_type: "aws_sqs_queue" },
      "cloudResourceLimits",
      "cloud-resource input truncated upstream; showing 1 of at least 2 observed cloud resources; at least 1 resource was not returned",
    ],
    [
      "Kubernetes resources",
      "k8s_resource_limits",
      "k8s_resources",
      { entity_id: "k8s:catalog", entity_name: "catalog", kind: "Deployment" },
      "k8sResourceLimits",
      "Kubernetes-resource input truncated upstream; showing 1 of at least 2 observed Kubernetes resources; at least 1 resource was not returned",
    ],
  ] as const)(
    "surfaces upstream %s truncation instead of claiming the evidence graph is complete",
    async (_family, limitsField, resourcesField, resource, resultField, limitation) => {
      const review = await loadDeploymentReview(
        deploymentTracePayload({
          cloud_resource_limits: completeCollectionLimits(0),
          deployment_source_limits: completeDeploymentSourceLimits(1),
          k8s_resource_limits: completeCollectionLimits(0),
          runtime_topology_limits: completeRuntimeTopologyLimits(),
          [limitsField]: truncatedCollectionLimits(1),
          [resourcesField]: [resource],
          topology_edges: [
            {
              relationship_type: "DEFINES",
              source_id: "repository:r_catalog",
              target_id: "workload:catalog-api",
            },
          ],
        }),
      );

      expect(review.deploymentTrace.status).toBe("ready");
      if (review.deploymentTrace.status !== "ready") return;
      expect(review.deploymentTrace.data[resultField]).toMatchObject({
        observedCount: 2,
        returnedCount: 1,
        truncated: true,
      });
      expect(review.graphPresentation).toMatchObject({
        completeness: "truncated",
        omittedNodes: 1,
        truncated: true,
      });
      expect(review.graphPresentation.limitations).toContain(limitation);
    },
  );

  it.each([
    ["cloud_resource_limits", "cloudResourceLimits", "cloud-resource"],
    ["k8s_resource_limits", "k8sResourceLimits", "Kubernetes-resource"],
  ] as const)(
    "fails %s coverage closed when returned_count contradicts the normalized rows",
    async (limitsField, resultField, family) => {
      const review = await loadDeploymentReview(
        deploymentTracePayload({
          cloud_resource_limits: completeCollectionLimits(0),
          deployment_source_limits: completeDeploymentSourceLimits(1),
          k8s_resource_limits: completeCollectionLimits(0),
          runtime_topology_limits: completeRuntimeTopologyLimits(),
          [limitsField]: completeCollectionLimits(1),
          topology_edges: [
            {
              relationship_type: "DEFINES",
              source_id: "repository:r_catalog",
              target_id: "workload:catalog-api",
            },
          ],
        }),
      );

      expect(review.deploymentTrace.status).toBe("ready");
      if (review.deploymentTrace.status !== "ready") return;
      expect(review.deploymentTrace.data[resultField]).toBeNull();
      expect(review.graphPresentation.completeness).toBe("unverified");
      expect(review.graphPresentation.limitations).toContain(
        `${family} completeness unverified because collection metadata is unavailable`,
      );
    },
  );
});

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

function completeDeploymentSourceLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    canonical_observed_count: returnedCount,
    repository_observed_count: 0,
  };
}

function completeRuntimeTopologyLimits(): Record<string, unknown> {
  return {
    instances: completeCollectionLimits(0),
    platform_edges: completeCollectionLimits(0),
    provisioned_platforms: completeCollectionLimits(0),
  };
}

function truncatedCollectionLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    observed_count: returnedCount + 1,
    observed_count_is_lower_bound: true,
    truncated: true,
  };
}
