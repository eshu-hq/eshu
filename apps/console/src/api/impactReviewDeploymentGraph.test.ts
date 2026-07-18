import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";

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
            },
            {
              platform_id: "platform:ecs:a",
              platform_kind: "ecs",
              platform_name: "shared-runtime",
            },
            {
              platform_id: "platform:kubernetes:b",
              platform_kind: "kubernetes",
              platform_name: "shared-runtime",
            },
            { platform_kind: "kubernetes", platform_name: "shared-runtime" },
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
          platform_id: `platform:kubernetes:${String(index).padStart(2, "0")}`,
          platform_kind: "kubernetes",
          platform_name: `cluster-${String(index).padStart(2, "0")}`,
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
  });
});

async function loadReview(trace: Record<string, unknown>, freshness = "fresh") {
  const client = new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/api/v0/impact/change-surface/investigate") {
        return Response.json({
          data: zeroChangeSurface(),
          error: null,
          truth: truthEnvelope("derived", freshness),
        });
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return Response.json({
          data: trace,
          error: null,
          truth: truthEnvelope("exact", freshness),
        });
      }
      throw new Error(`unexpected request ${path}`);
    },
  });
  return loadImpactReview(client, { target: "catalog-api", targetKind: "service" });
}

function deploymentTracePayload(overrides: Record<string, unknown> = {}): Record<string, unknown> {
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

function deploymentSource(): Record<string, unknown> {
  return {
    confidence: 0.98,
    reason: "canonical deployment source",
    repo_id: "repository:r_config",
    repo_name: "deployment-config",
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
