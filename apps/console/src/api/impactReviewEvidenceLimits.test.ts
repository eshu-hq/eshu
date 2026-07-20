import {
  deploymentTracePayload,
  loadDeploymentReview,
} from "./impactReviewDeploymentGraph.testSupport";

describe("impact deployment evidence coverage", () => {
  it("surfaces truncated config-derived candidates even when no candidate rows were returned", async () => {
    const review = await loadDeploymentReview(
      deploymentTracePayload({
        cloud_resource_limits: completeCloudResourceLimits(0),
        deployment_source_limits: completeDeploymentSourceLimits(1),
        k8s_resource_limits: completeKubernetesLimits(0),
        runtime_topology_limits: completeRuntimeTopologyLimits(),
        topology_edges: [
          {
            relationship_type: "DEFINES",
            source_id: "repository:r_catalog",
            target_id: "workload:catalog-api",
          },
        ],
        uncorrelated_cloud_resources_truncated: true,
      }),
    );

    expect(review.deploymentTrace.status).toBe("ready");
    if (review.deploymentTrace.status !== "ready") return;
    expect(review.deploymentTrace.data.uncorrelatedCloudResourcesTruncated).toBe(true);
    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      omittedNodes: 0,
      truncated: true,
    });
    expect(review.graphPresentation.limitations).toContain(
      "uncorrelated cloud-resource candidate input truncated upstream; additional candidates may be omitted",
    );
  });

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
      "Kubernetes resource input truncated upstream; showing 1 of at least 2 observed Kubernetes resources; at least 1 resource was not returned",
    ],
  ] as const)(
    "surfaces upstream %s truncation instead of claiming the evidence graph is complete",
    async (_family, limitsField, resourcesField, resource, resultField, limitation) => {
      const review = await loadDeploymentReview(
        deploymentTracePayload({
          cloud_resource_limits: completeCloudResourceLimits(0),
          deployment_source_limits: completeDeploymentSourceLimits(1),
          k8s_resource_limits: completeKubernetesLimits(0),
          runtime_topology_limits: completeRuntimeTopologyLimits(),
          [limitsField]:
            limitsField === "k8s_resource_limits"
              ? truncatedKubernetesLimits(1)
              : truncatedCloudResourceLimits(1),
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

  it("does not invent omitted cloud-resource nodes when only observation coverage is bounded", async () => {
    const review = await loadDeploymentReview(
      deploymentTracePayload({
        cloud_resource_limits: observationTruncatedCloudLimits(1),
        cloud_resources: [{ id: "cloud:queue", name: "events", resource_type: "aws_sqs_queue" }],
        deployment_source_limits: completeDeploymentSourceLimits(1),
        k8s_resource_limits: completeKubernetesLimits(0),
        runtime_topology_limits: completeRuntimeTopologyLimits(),
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
    expect(review.deploymentTrace.data.cloudResourceLimits).toMatchObject({
      observationCount: 2501,
      observationCountIsLowerBound: true,
      observationLimit: 2500,
      observationQuerySentinelLimit: 2501,
      observedCount: 1,
      returnedCount: 1,
      truncated: true,
    });
    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      omittedNodes: 0,
      truncated: true,
    });
    expect(review.graphPresentation.limitations).toContain(
      "cloud-resource relationship observations truncated upstream; additional observations or resource identities may be omitted, but their count is unknown",
    );
    expect(review.graphPresentation.limitations).not.toContain(
      "cloud-resource input truncated upstream; showing 1 of at least 1 observed cloud resource; at least 1 resource was not returned",
    );
  });

  it("does not invent omitted Kubernetes identities when a saturated probe deduplicates to the returned set", async () => {
    const review = await loadDeploymentReview(
      deploymentTracePayload({
        cloud_resource_limits: completeCloudResourceLimits(0),
        deployment_source_limits: completeDeploymentSourceLimits(1),
        k8s_resource_limits: {
          ...completeKubernetesLimits(1),
          content_observed_count_is_lower_bound: true,
          observed_count_is_lower_bound: true,
          truncated: true,
        },
        k8s_resources: [{ entity_id: "k8s:catalog", entity_name: "catalog", kind: "Deployment" }],
        runtime_topology_limits: completeRuntimeTopologyLimits(),
        topology_edges: [
          {
            relationship_type: "DEFINES",
            source_id: "repository:r_catalog",
            target_id: "workload:catalog-api",
          },
        ],
      }),
    );

    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      omittedNodes: 0,
      truncated: true,
    });
    expect(review.graphPresentation.limitations).toContain(
      "Kubernetes resource input truncated upstream; showing 1 of at least 1 observed Kubernetes resource; additional resources may exist, but their count is unknown",
    );
  });

  it("fails cloud-resource completeness closed when observation metadata is missing", async () => {
    const review = await loadDeploymentReview(
      deploymentTracePayload({
        cloud_resource_limits: completeCollectionLimits(0),
        deployment_source_limits: completeDeploymentSourceLimits(1),
        k8s_resource_limits: completeKubernetesLimits(0),
        runtime_topology_limits: completeRuntimeTopologyLimits(),
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
    expect(review.deploymentTrace.data.cloudResourceLimits).toBeNull();
    expect(review.graphPresentation.completeness).toBe("unverified");
    expect(review.graphPresentation.limitations).toContain(
      "cloud-resource completeness unverified because collection metadata is unavailable",
    );
  });

  it.each([
    ["cloud_resource_limits", "cloudResourceLimits", "cloud-resource"],
    ["k8s_resource_limits", "k8sResourceLimits", "Kubernetes-resource"],
  ] as const)(
    "fails %s coverage closed when returned_count contradicts the normalized rows",
    async (limitsField, resultField, family) => {
      const review = await loadDeploymentReview(
        deploymentTracePayload({
          cloud_resource_limits: completeCloudResourceLimits(0),
          deployment_source_limits: completeDeploymentSourceLimits(1),
          k8s_resource_limits: completeKubernetesLimits(0),
          runtime_topology_limits: completeRuntimeTopologyLimits(),
          [limitsField]:
            limitsField === "k8s_resource_limits"
              ? completeKubernetesLimits(1)
              : completeCloudResourceLimits(1),
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

function completeCloudResourceLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    observation_count: returnedCount,
    observation_count_is_lower_bound: false,
    observation_limit: 2500,
    observation_query_sentinel_limit: 2501,
  };
}

function observationTruncatedCloudLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCloudResourceLimits(returnedCount),
    observation_count: 2501,
    observation_count_is_lower_bound: true,
    observation_limit: 2500,
    observation_query_sentinel_limit: 2501,
    observed_count_is_lower_bound: true,
    truncated: true,
  };
}

function completeDeploymentSourceLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    canonical_observed_count: returnedCount,
    repository_observed_count: 0,
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

function truncatedCloudResourceLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...truncatedCollectionLimits(returnedCount),
    observation_count: returnedCount + 1,
    observation_count_is_lower_bound: true,
    observation_limit: 2500,
    observation_query_sentinel_limit: 2501,
  };
}

function truncatedKubernetesLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeKubernetesLimits(returnedCount),
    content_observed_count: returnedCount + 1,
    content_observed_count_is_lower_bound: true,
    observed_count: returnedCount + 1,
    observed_count_is_lower_bound: true,
    truncated: true,
  };
}
