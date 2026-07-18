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
          platform_id: `platform:${index === 0 ? "ecs" : "kubernetes"}:${String(index).padStart(2, "0")}`,
          platform_kind: index === 0 ? "ecs" : "kubernetes",
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
    expect(
      review.graph.nodes.filter((node) => node.kind === "platform").map((node) => node.sub),
    ).toEqual(expect.arrayContaining(["ecs", "kubernetes"]));
    expect(review.graph.nodes.some((node) => node.kind === "env")).toBe(true);
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

    expect(review.graph.edges).toContainEqual({
      layer: "deploy",
      s: "instance:catalog:prod",
      t: "repository:r_config",
      verb: "DEPLOYMENT_SOURCE",
    });
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

async function loadReview(
  trace: Record<string, unknown>,
  freshness = "fresh",
  traceLevel: string | null = "exact",
  changeSurface: Record<string, unknown> = zeroChangeSurface(),
) {
  const client = new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL): Promise<Response> => {
      const path = new URL(new Request(input).url).pathname;
      if (path === "/api/v0/impact/change-surface/investigate") {
        return Response.json({
          data: changeSurface,
          error: null,
          truth: truthEnvelope("derived", freshness),
        });
      }
      if (path === "/api/v0/impact/trace-deployment-chain") {
        return Response.json({
          data: trace,
          error: null,
          truth: traceLevel === null ? null : truthEnvelope(traceLevel, freshness),
        });
      }
      throw new Error(`unexpected request ${path}`);
    },
  });
  return loadImpactReview(client, { target: "catalog-api", targetKind: "service" });
}

function ambiguousChangeSurface(): Record<string, unknown> {
  return {
    ...zeroChangeSurface(),
    target_resolution: {
      candidates: [
        { id: "workload:catalog-api-a", labels: ["Workload"], name: "catalog-api-a" },
        { id: "workload:catalog-api-b", labels: ["Workload"], name: "catalog-api-b" },
      ],
      input: "catalog-api",
      status: "ambiguous",
      target_type: "service",
      truncated: false,
    },
  };
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
    relationship_type: "DEPLOYS_FROM",
    repo_id: "repository:r_config",
    repo_name: "deployment-config",
    source_id: "repository:r_config",
    target_id: "repository:r_catalog",
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
