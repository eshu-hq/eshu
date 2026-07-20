import {
  deploymentSource,
  deploymentTracePayload,
  directRuntimeTopology,
  loadDeploymentReview as loadReview,
} from "./impactReviewDeploymentGraph.testSupport";

describe("impact deployment graph composition", () => {
  it("preserves distinct canonical identities and reports duplicates and omissions", async () => {
    const trace = deploymentTracePayload({
      deployment_sources: [deploymentSource(), deploymentSource()],
      instances: [
        {
          environment: "prod",
          instance_id: "instance:catalog:prod",
          platforms: [
            {
              platform_id: "platform:ecs:a",
              platform_kind: "ecs",
              platform_name: "shared-runtime",
              ...directRuntimeTopology("instance:catalog:prod", "platform:ecs:a"),
            },
            {
              platform_id: "platform:ecs:a",
              platform_kind: "ecs",
              platform_name: "shared-runtime",
              ...directRuntimeTopology("instance:catalog:prod", "platform:ecs:a"),
            },
            {
              platform_id: "platform:kubernetes:b",
              platform_kind: "kubernetes",
              platform_name: "shared-runtime",
              ...directRuntimeTopology("instance:catalog:prod", "platform:kubernetes:b"),
            },
            {
              platform_kind: "kubernetes",
              platform_name: "shared-runtime",
              topology_basis: "direct_runtime",
              topology_edges: [
                { relationship_type: "RUNS_ON", source_id: "instance:catalog:prod" },
              ],
            },
          ],
        },
      ],
    });

    const review = await loadReview(trace, "stale");

    expect(review.graphPresentation).toMatchObject({
      duplicateEdges: 2,
      duplicateNodes: 2,
      freshness: "stale",
      mode: "deployment_trace",
      omittedEdges: 1,
      omittedNodes: 1,
      truthLevel: "exact",
    });
    expect(
      review.graph.nodes.filter((node) => node.label === "shared-runtime").map((node) => node.id),
    ).toEqual(["platform:ecs:a", "platform:kubernetes:b"]);
    expect(review.graphPresentation.limitations).toEqual(
      expect.arrayContaining([
        "runtime platform omitted because it has no canonical platform_id",
        "RUNS_ON edge omitted because an endpoint lacks canonical identity",
      ]),
    );
  });

  it("enforces deterministic graph bounds and reports truncation", async () => {
    const instances = Array.from({ length: 70 }, (_, index) => ({
      environment: `environment-${String(index).padStart(2, "0")}`,
      instance_id: `instance:catalog:${String(index).padStart(2, "0")}`,
      platforms: [
        {
          platform_id: `platform:${index === 0 ? "ecs" : "kubernetes"}:${String(index).padStart(2, "0")}`,
          platform_kind: index === 0 ? "ecs" : "kubernetes",
          platform_name: `cluster-${String(index).padStart(2, "0")}`,
          ...directRuntimeTopology(
            `instance:catalog:${String(index).padStart(2, "0")}`,
            `platform:${index === 0 ? "ecs" : "kubernetes"}:${String(index).padStart(2, "0")}`,
          ),
        },
      ],
    }));

    const review = await loadReview(deploymentTracePayload({ instances }));

    expect(review.graph.nodes).toHaveLength(60);
    expect(review.graph.edges.length).toBeLessThanOrEqual(120);
    expect(review.graphPresentation).toMatchObject({
      edgeLimit: 120,
      mode: "deployment_trace",
      nodeLimit: 60,
      renderedNodes: 60,
      truncated: true,
    });
    expect(review.graphPresentation.omittedNodes).toBeGreaterThan(0);
    expect(review.graphPresentation.omittedEdges).toBeGreaterThan(0);
    expect(
      review.graph.nodes.filter((node) => node.kind === "platform").map((node) => node.sub),
    ).toEqual(expect.arrayContaining(["ecs", "kubernetes"]));
    expect(review.graph.nodes.some((node) => node.kind === "env")).toBe(false);
  });

  it.each([
    [
      "instances",
      {
        instances: truncatedCollectionLimits(1),
        platform_edges: completeCollectionLimits(1),
        provisioned_platforms: completeCollectionLimits(1),
      },
      "runtime instance input truncated upstream; showing 1 of at least 2 observed runtime instances; at least 1 instance was not returned",
      1,
      1,
    ],
    [
      "direct placements",
      {
        instances: completeCollectionLimits(1),
        platform_edges: truncatedCollectionLimits(1),
        provisioned_platforms: completeCollectionLimits(1),
      },
      "direct placement input truncated upstream; showing 1 of at least 2 observed direct placements; at least 1 relationship was not returned",
      0,
      1,
    ],
    [
      "provisioned platforms",
      {
        instances: completeCollectionLimits(1),
        platform_edges: completeCollectionLimits(1),
        provisioned_platforms: truncatedCollectionLimits(1),
      },
      "provisioned platform input truncated upstream; showing 1 of at least 2 observed provisioned platforms; at least 1 platform record was not returned",
      0,
      2,
    ],
  ])(
    "surfaces upstream %s sentinel truncation in graph completeness",
    async (_family, runtimeTopologyLimits, limitation, omittedNodes, omittedEdges) => {
      const review = await loadReview(
        deploymentTracePayload({
          instances: [
            {
              environment: "prod",
              instance_id: "instance:catalog:prod",
              platforms: [
                {
                  platform_id: "platform:ecs:prod",
                  platform_kind: "ecs",
                  platform_name: "prod",
                  ...directRuntimeTopology("instance:catalog:prod", "platform:ecs:prod"),
                },
              ],
            },
          ],
          provisioned_platforms: [
            {
              platform_id: "platform:kubernetes:provisioned",
              platform_kind: "kubernetes",
              platform_name: "provisioned",
              topology_edges: [
                {
                  relationship_type: "PROVISIONS_DEPENDENCY_FOR",
                  source_id: "repository:r_infra",
                  target_id: "repository:r_catalog",
                },
                {
                  relationship_type: "PROVISIONS_PLATFORM",
                  source_id: "repository:r_infra",
                  target_id: "platform:kubernetes:provisioned",
                },
              ],
            },
          ],
          runtime_topology_limits: runtimeTopologyLimits,
          topology_edges: [
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

      expect(review.graphPresentation).toMatchObject({
        omittedEdges,
        omittedNodes,
        truncated: true,
      });
      expect(review.graphPresentation.limitations).toContain(limitation);
    },
  );

  it.each([
    [
      "runtime instances",
      {
        instances: lowerBoundCollectionLimits(1),
        platform_edges: completeCollectionLimits(1),
        provisioned_platforms: completeCollectionLimits(1),
      },
      "runtime instance input truncated upstream; showing 1 of at least 1 observed runtime instance; additional instances may exist, but their count is unknown",
    ],
    [
      "direct placements",
      {
        instances: completeCollectionLimits(1),
        platform_edges: lowerBoundCollectionLimits(1),
        provisioned_platforms: completeCollectionLimits(1),
      },
      "direct placement input truncated upstream; showing 1 of at least 1 observed direct placement; additional relationships may exist, but their count is unknown",
    ],
    [
      "provisioned platforms",
      {
        instances: completeCollectionLimits(1),
        platform_edges: completeCollectionLimits(1),
        provisioned_platforms: lowerBoundCollectionLimits(1),
      },
      "provisioned platform input truncated upstream; showing 1 of at least 1 observed provisioned platform; additional platform records may exist, but their count is unknown",
    ],
  ])(
    "does not invent omitted identities when saturated %s deduplicate to the returned set",
    async (_family, runtimeTopologyLimits, limitation) => {
      const review = await loadReview(
        deploymentTracePayload({
          instances: [
            {
              environment: "prod",
              instance_id: "instance:catalog:prod",
              platforms: [
                {
                  platform_id: "platform:ecs:prod",
                  platform_kind: "ecs",
                  platform_name: "prod",
                  ...directRuntimeTopology("instance:catalog:prod", "platform:ecs:prod"),
                },
              ],
            },
          ],
          provisioned_platforms: [
            {
              platform_id: "platform:kubernetes:provisioned",
              platform_kind: "kubernetes",
              platform_name: "provisioned",
              topology_edges: [
                {
                  relationship_type: "PROVISIONS_DEPENDENCY_FOR",
                  source_id: "repository:r_infra",
                  target_id: "repository:r_catalog",
                },
                {
                  relationship_type: "PROVISIONS_PLATFORM",
                  source_id: "repository:r_infra",
                  target_id: "platform:kubernetes:provisioned",
                },
              ],
            },
          ],
          runtime_topology_limits: runtimeTopologyLimits,
          topology_edges: [
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

      expect(review.graphPresentation).toMatchObject({
        omittedEdges: 0,
        omittedNodes: 0,
        truncated: true,
      });
      expect(review.graphPresentation.limitations).toContain(limitation);
    },
  );

  it("separates instances, platforms, and evidence into navigable lanes", async () => {
    const instances = Array.from({ length: 6 }, (_, index) => ({
      environment: `environment-${index}`,
      instance_id: `instance:catalog:${index}`,
      platforms: [
        {
          platform_id: `platform:kubernetes:${index}`,
          platform_kind: "kubernetes",
          platform_name: `cluster-${index}`,
        },
        ...(index === 0
          ? [
              {
                platform_id: "platform:ecs:0",
                platform_kind: "ecs",
                platform_name: "cluster-ecs-0",
              },
            ]
          : []),
      ],
    }));
    const review = await loadReview(
      deploymentTracePayload({
        cloud_resources: [{ id: "cloud:queue", name: "events", resource_type: "aws_sqs_queue" }],
        instances,
        k8s_resources: [{ entity_id: "k8s:service", entity_name: "catalog" }],
      }),
    );

    expect(
      new Set(
        review.graph.nodes.filter((node) => node.kind === "instance").map((node) => node.col),
      ),
    ).toEqual(new Set([3]));
    expect(
      new Set(
        review.graph.nodes.filter((node) => node.kind === "platform").map((node) => node.col),
      ),
    ).toEqual(new Set([5]));
    expect(
      new Set(
        review.graph.nodes
          .filter((node) => node.kind === "aws" || node.kind === "k8s")
          .map((node) => node.col),
      ),
    ).toEqual(new Set([6]));
    const nodesPerColumn = new Map<number, number>();
    for (const node of review.graph.nodes) {
      nodesPerColumn.set(node.col, (nodesPerColumn.get(node.col) ?? 0) + 1);
    }
    expect(Math.max(...nodesPerColumn.values())).toBeLessThanOrEqual(7);
  });

  it("inherits the deployment trace truth level on every topology node", async () => {
    const review = await loadReview(
      deploymentTracePayload({
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [
              { platform_id: "platform:ecs:prod", platform_kind: "ecs", platform_name: "prod" },
            ],
          },
        ],
      }),
      "fresh",
      "derived",
    );

    expect(review.graph.nodes.every((node) => node.truth === "derived")).toBe(true);
  });

  it("preserves exact instance deployment-source endpoints without inventing DEPLOYS_FROM", async () => {
    const review = await loadReview(
      deploymentTracePayload({
        deployment_sources: [
          {
            relationship_type: "DEPLOYMENT_SOURCE",
            repo_id: "repository:r_config",
            repo_name: "deployment-config",
            source_id: "instance:catalog:prod",
            target_id: "repository:r_config",
          },
        ],
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [],
          },
        ],
      }),
    );

    expect(review.graph.edges).toContainEqual(
      expect.objectContaining({
        layer: "deploy",
        s: "instance:catalog:prod",
        t: "repository:r_config",
        verb: "DEPLOYMENT_SOURCE",
      }),
    );
    expect(review.graph.edges.some((edge) => edge.verb === "DEPLOYS_FROM")).toBe(false);
  });

  it("does not promote a missing trace truth envelope to exact", async () => {
    const review = await loadReview(
      deploymentTracePayload({
        instances: [
          {
            environment: "prod",
            instance_id: "instance:catalog:prod",
            platforms: [
              { platform_id: "platform:ecs:prod", platform_kind: "ecs", platform_name: "prod" },
            ],
          },
        ],
      }),
      "fresh",
      null,
    );

    expect(review.graph.nodes.every((node) => node.truth === "inferred")).toBe(true);
    expect(review.graphPresentation.truthLevel).toBeUndefined();
    expect(review.graphPresentation.truthBasis).toBeUndefined();
  });
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

function truncatedCollectionLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    observed_count: returnedCount + 1,
    observed_count_is_lower_bound: true,
    truncated: true,
  };
}

function lowerBoundCollectionLimits(returnedCount: number): Record<string, unknown> {
  return {
    ...completeCollectionLimits(returnedCount),
    observed_count_is_lower_bound: true,
    truncated: true,
  };
}
