import { EshuApiClient } from "./client";
import { boundedGraph } from "./impactGraphSelection";
import { loadImpactReview } from "./impactReview";
import {
  ambiguousChangeSurface,
  deploymentSource,
  deploymentTracePayload,
  directRuntimeTopology,
  loadDeploymentReview as loadReview,
  nonEmptyChangeSurface,
} from "./impactReviewDeploymentGraph.testSupport";
import type { GraphEdge, GraphNode } from "../console/types";

describe("impact deployment graph composition", () => {
  it("selects the same bounded edge page regardless of backend row order", () => {
    const nodes: readonly GraphNode[] = [
      { col: 0, hero: true, id: "source", kind: "workload", label: "source", truth: "exact" },
      { col: 1, id: "target", kind: "repo", label: "target", truth: "exact" },
    ];
    const edges: readonly GraphEdge[] = Array.from({ length: 130 }, (_, index) => ({
      layer: "runtime",
      s: "source",
      t: "target",
      verb: `EDGE_${String(index).padStart(3, "0")}`,
    }));

    const forward = boundedGraph(nodes, edges, 0, 0, new Set());
    const reversed = boundedGraph(nodes, [...edges].reverse(), 0, 0, new Set());

    expect(reversed.graph.edges).toEqual(forward.graph.edges);
    expect(forward.graph.edges).toHaveLength(120);
    expect(forward.presentation.omittedEdges).toBe(10);
  });

  it("merges duplicate-edge provenance deterministically without losing observations", () => {
    const nodes: readonly GraphNode[] = [
      { col: 0, id: "source", kind: "repo", label: "source", truth: "exact" },
      { col: 1, id: "target", kind: "workload", label: "target", truth: "exact" },
    ];
    const edges: readonly GraphEdge[] = [
      {
        evidence: ["second observation"],
        layer: "code",
        method: "reducer",
        s: "source",
        sourceFamily: "projection",
        t: "target",
        verb: "DEFINES",
      },
      {
        evidence: ["first observation"],
        layer: "code",
        method: "collector",
        s: "source",
        sourceFamily: "ingestion",
        t: "target",
        verb: "DEFINES",
      },
    ];

    const forward = boundedGraph(nodes, edges, 0, 0, new Set());
    const reversed = boundedGraph(nodes, [...edges].reverse(), 0, 0, new Set());

    expect(reversed.graph.edges).toEqual(forward.graph.edges);
    expect(forward.graph.edges[0]).toMatchObject({
      evidence: ["first observation", "second observation"],
      method: "collector + reducer",
      sourceFamily: "ingestion + projection",
    });
    expect(forward.presentation.duplicateEdges).toBe(1);
  });

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

  it("does not select deployment topology for an ambiguous service target", async () => {
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
      "exact",
      ambiguousChangeSurface(),
    );

    expect(review.graphPresentation.mode).toBe("change_surface");
    expect(review.graphPresentation.limitations).toContain(
      "deployment topology not selected because the service target is ambiguous",
    );
    expect(review.graph.nodes.some((node) => node.id === "platform:ecs:prod")).toBe(false);
  });

  it("keeps non-empty change surface primary for service and workload anchors", async () => {
    const trace = deploymentTracePayload({
      instances: [
        {
          environment: "prod",
          instance_id: "instance:catalog:prod",
          platforms: [
            { platform_id: "platform:ecs:prod", platform_kind: "ecs", platform_name: "prod" },
          ],
        },
      ],
    });

    for (const targetKind of ["service", "workload"] as const) {
      const review = await loadReview(trace, "fresh", "exact", nonEmptyChangeSurface(), {
        target: targetKind === "workload" ? "workload:catalog-api" : "catalog-api",
        targetKind,
      });

      expect(review.graphPresentation.mode).toBe("change_surface");
      expect(review.graphPresentation.sourceApis).toEqual([
        "/api/v0/impact/change-surface/investigate",
      ]);
      expect(review.graph.nodes.some((node) => node.id === "platform:ecs:prod")).toBe(false);
    }
  });

  it("keeps authorization failures visible without leaking or fabricating topology", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (): Promise<Response> =>
        Response.json({
          data: null,
          error: { code: "permission_denied", message: "repository scope is not authorized" },
          truth: null,
        }),
    });

    const review = await loadImpactReview(client, {
      target: "catalog-api",
      targetKind: "service",
    });

    expect(review.changeSurface.status).toBe("unavailable");
    expect(review.deploymentTrace.status).toBe("unavailable");
    expect(review.graph).toEqual({ edges: [], nodes: [] });
    expect(review.graphPresentation).toMatchObject({
      inputEdges: 0,
      inputNodes: 0,
      mode: "empty",
      sourceApis: [],
    });
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
