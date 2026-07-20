import { EshuApiClient } from "./client";
import { selectImpactGraph } from "./impactGraph";
import { boundedGraph } from "./impactGraphSelection";
import { loadImpactReview } from "./impactReview";
import {
  ambiguousChangeSurface,
  deploymentTracePayload,
  loadDeploymentReview,
  nonEmptyChangeSurface,
} from "./impactReviewDeploymentGraph.testSupport";
import type { GraphEdge, GraphNode } from "../console/types";

describe("impact review selection safety", () => {
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

  it("does not select deployment topology from a different repository scope", async () => {
    const changeSurface = nonEmptyChangeSurface();
    const resolution = changeSurface.target_resolution as Record<string, unknown>;
    const selected = resolution.selected as Record<string, unknown>;
    const scope = changeSurface.scope as Record<string, unknown>;
    const review = await loadDeploymentReview(deploymentTracePayload(), "fresh", "exact", {
      ...changeSurface,
      scope: { ...scope, repo_id: "repository:r_requested" },
      target_resolution: {
        ...resolution,
        selected: { ...selected, repo_id: "repository:r_requested" },
      },
    });

    expect(review.graphPresentation.mode).toBe("change_surface");
    expect(review.graphPresentation.limitations).toContain(
      "deployment topology not selected because trace and change-surface repository identities disagree",
    );
    expect(review.graph.nodes.some((node) => node.id === "repository:r_catalog")).toBe(false);
  });

  it("surfaces truncation from a selected non-empty change surface", async () => {
    const changeSurface = nonEmptyChangeSurface();
    const coverage = changeSurface.coverage as Record<string, unknown>;
    const review = await loadDeploymentReview(deploymentTracePayload(), "fresh", "exact", {
      ...changeSurface,
      coverage: { ...coverage, truncated: true },
      truncated: true,
    });

    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      mode: "change_surface",
      truncated: true,
    });
  });

  it("bounds a saturated blast-radius graph and reports every omitted identity", () => {
    const nodes: readonly GraphNode[] = [
      { col: 0, hero: true, id: "source", kind: "repo", label: "source", truth: "exact" },
      ...Array.from(
        { length: 130 },
        (_, index): GraphNode => ({
          col: 1,
          id: `repository:r_${String(index).padStart(3, "0")}`,
          kind: "repo",
          label: `repo-${String(index).padStart(3, "0")}`,
          truth: "exact",
        }),
      ),
    ];
    const edges: readonly GraphEdge[] = nodes.slice(1).map((node) => ({
      layer: "runtime",
      s: node.id,
      t: "source",
      verb: "DEPENDS_ON",
    }));

    const selected = selectImpactGraph(
      "source",
      "repository",
      {
        data: {
          affected: [],
          affectedCount: 130,
          graph: { edges, nodes },
          limit: 100,
          target: "source",
          targetType: "repository",
          truncated: false,
        },
        source: "/api/v0/impact/blast-radius",
        status: "ready",
        truth: null,
      },
      { reason: "not needed", source: "change", status: "skipped" },
      { reason: "not supported", source: "deployment", status: "skipped" },
    );

    expect(selected.graph.nodes).toHaveLength(60);
    expect(selected.graph.edges).toHaveLength(59);
    expect(selected.presentation).toMatchObject({
      completeness: "truncated",
      inputEdges: 130,
      inputNodes: 131,
      omittedEdges: 71,
      omittedNodes: 71,
      renderedEdges: 59,
      renderedNodes: 60,
      truncated: true,
    });
  });

  it("bounds a saturated change surface without retaining edges to omitted nodes", async () => {
    const changeSurface = nonEmptyChangeSurface();
    const directImpact = Array.from({ length: 130 }, (_, index) => ({
      depth: 1,
      environment: "",
      id: `workload:consumer-${String(index).padStart(3, "0")}`,
      labels: ["Workload"],
      name: `consumer-${String(index).padStart(3, "0")}`,
      repo_id: `repository:r_${String(index).padStart(3, "0")}`,
    }));
    const review = await loadDeploymentReview(deploymentTracePayload(), "fresh", "exact", {
      ...changeSurface,
      direct_impact: directImpact,
      impact_summary: { direct_count: 130, total_count: 130, transitive_count: 0 },
      transitive_impact: [],
    });

    expect(review.graph.nodes).toHaveLength(60);
    expect(review.graph.edges).toHaveLength(59);
    expect(
      review.graph.edges.every((edge) => review.graph.nodes.some((node) => node.id === edge.s)),
    ).toBe(true);
    expect(review.graphPresentation).toMatchObject({
      completeness: "truncated",
      inputEdges: 130,
      inputNodes: 131,
      mode: "change_surface",
      omittedEdges: 71,
      omittedNodes: 71,
      renderedEdges: 59,
      renderedNodes: 60,
      truncated: true,
    });
  });

  it("does not select deployment topology for an ambiguous service target", async () => {
    const review = await loadDeploymentReview(
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
      const review = await loadDeploymentReview(trace, "fresh", "exact", nonEmptyChangeSurface(), {
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
