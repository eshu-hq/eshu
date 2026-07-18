import { EshuApiClient } from "./client";
import { loadImpactReview } from "./impactReview";
import { inspectionRequest } from "../test/inspectionRequest";

describe("impact review adapter", () => {
  it("posts bounded service impact and deployment trace requests", async () => {
    const calls: Request[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        calls.push(request);
        const path = new URL(request.url).pathname;
        if (path === "/api/v0/impact/change-surface/investigate") {
          return Response.json({
            data: changeSurfacePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.change_surface"),
          });
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return Response.json({
            data: deploymentTracePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.deployment_chain"),
          });
        }
        throw new Error(`unexpected request ${path}`);
      },
    });

    const result = await loadImpactReview(client, {
      environment: "prod",
      repoId: "repository:r_catalog",
      target: "catalog-api",
      targetKind: "service",
    });

    expect(calls.map((call) => new URL(call.url).pathname)).toEqual([
      "/api/v0/impact/change-surface/investigate",
      "/api/v0/impact/trace-deployment-chain",
    ]);
    await expect(calls[0]?.json()).resolves.toMatchObject({
      environment: "prod",
      limit: 25,
      max_depth: 4,
      repo_id: "repository:r_catalog",
      service_name: "catalog-api",
    });
    await expect(calls[1]?.json()).resolves.toMatchObject({
      direct_only: false,
      include_related_module_usage: false,
      max_depth: 4,
      service_name: "catalog-api",
    });
    expect(result.blast.status).toBe("skipped");
    expect(result.changeSurface.status).toBe("ready");
    expect(result.deploymentTrace.status).toBe("ready");
    expect(result.graph.nodes.map((node) => node.label).sort()).toEqual([
      "catalog-api",
      "sample-communicator",
      "terraform-stack-node10",
    ]);
    expect(result.graph.edges).toHaveLength(2);
  });

  it("uses exact deployment topology when service impact has no dependents", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        if (path === "/api/v0/impact/change-surface/investigate") {
          return Response.json({
            data: {
              ...changeSurfacePayload(),
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
              transitive_impact: [],
            },
            error: null,
            truth: truthEnvelope("platform_impact.change_surface"),
          });
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return Response.json({
            data: dualPlatformDeploymentTracePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.deployment_chain"),
          });
        }
        throw new Error(`unexpected request ${path}`);
      },
    });

    const result = await loadImpactReview(client, {
      target: "catalog-api",
      targetKind: "service",
    });

    expect(result.graphPresentation.mode).toBe("deployment_trace");
    expect(result.graphPresentation.sourceApis).toEqual(["/api/v0/impact/trace-deployment-chain"]);
    expect(result.graphPresentation).toMatchObject({
      duplicateEdges: 0,
      duplicateNodes: 0,
      omittedEdges: 0,
      omittedNodes: 0,
      renderedEdges: 9,
      renderedNodes: 12,
      truncated: false,
    });
    expect(result.graph.nodes.map((node) => node.id)).toEqual([
      "repository:r_config",
      "repository:r_catalog",
      "workload:catalog-api",
      "instance:catalog:prod",
      "instance:catalog:stage",
      "environment:prod",
      "environment:stage",
      "platform:ecs:catalog-ecs",
      "platform:kubernetes:catalog-eks",
      "platform:kubernetes:catalog-stage-eks",
      "cloud:queue",
      "k8s:catalog",
    ]);
    expect(result.graph.edges).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          s: "instance:catalog:prod",
          t: "workload:catalog-api",
          verb: "INSTANCE_OF",
        }),
        expect.objectContaining({
          s: "instance:catalog:prod",
          t: "platform:ecs:catalog-ecs",
          verb: "RUNS_ON",
        }),
        expect.objectContaining({
          s: "workload:catalog-api",
          t: "environment:prod",
          verb: "MATERIALIZED_IN_ENVIRONMENT",
        }),
        expect.objectContaining({
          s: "repository:r_config",
          t: "repository:r_catalog",
          verb: "DEPLOYS_FROM",
        }),
      ]),
    );
  });

  it("uses the blast-radius endpoint for repository anchors and keeps trace skipped", async () => {
    const calls: Request[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = inspectionRequest(input, init);
        calls.push(request);
        const path = new URL(request.url).pathname;
        if (path === "/api/v0/impact/blast-radius") {
          return Response.json({
            data: {
              affected: [{ hops: 1, repo: "consumer-api", repo_id: "repository:r_consumer" }],
              affected_count: 1,
              limit: 25,
              target: "catalog-api",
              target_type: "repository",
              truncated: false,
            },
            error: null,
            truth: truthEnvelope("platform_impact.blast_radius"),
          });
        }
        if (path === "/api/v0/impact/change-surface/investigate") {
          return Response.json({
            data: changeSurfacePayload(),
            error: null,
            truth: truthEnvelope("platform_impact.change_surface"),
          });
        }
        throw new Error(`unexpected request ${path}`);
      },
    });

    const result = await loadImpactReview(client, {
      limit: 25,
      target: "catalog-api",
      targetKind: "repository",
    });

    expect(calls.map((call) => new URL(call.url).pathname)).toEqual([
      "/api/v0/impact/blast-radius",
      "/api/v0/impact/change-surface/investigate",
    ]);
    await expect(calls[0]?.json()).resolves.toMatchObject({
      limit: 25,
      target: "catalog-api",
      target_type: "repository",
    });
    await expect(calls[1]?.json()).resolves.toMatchObject({
      limit: 25,
      max_depth: 4,
      target: "catalog-api",
      target_type: "repository",
    });
    expect(result.blast.status).toBe("ready");
    expect(result.deploymentTrace.status).toBe("skipped");
    expect(result.graph.nodes.map((node) => node.label).sort()).toEqual([
      "catalog-api",
      "consumer-api",
    ]);
  });

  it("keeps unsupported blast and error envelopes visible without fabricating graph state", async () => {
    const calls: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        calls.push(path);
        if (path === "/api/v0/impact/change-surface/investigate") {
          return Response.json({
            data: {
              code_surface: {
                changed_files: [],
                evidence_groups: [],
                matched_file_count: 0,
                symbol_count: 0,
                touched_symbols: [],
              },
              direct_impact: [],
              impact_summary: {
                direct_count: 0,
                total_count: 0,
                transitive_count: 0,
              },
              target_resolution: {
                candidates: [
                  {
                    id: "workload:catalog-api-a",
                    labels: ["Workload"],
                    name: "catalog-api-a",
                  },
                  {
                    id: "workload:catalog-api-b",
                    labels: ["Workload"],
                    name: "catalog-api-b",
                  },
                ],
                input: "catalog-api",
                status: "ambiguous",
                target_type: "service",
                truncated: true,
              },
              transitive_impact: [],
            },
            error: null,
            truth: {
              ...truthEnvelope("platform_impact.change_surface"),
              freshness: { state: "stale" },
            },
          });
        }
        if (path === "/api/v0/impact/trace-deployment-chain") {
          return Response.json({
            data: null,
            error: {
              code: "unsupported_capability",
              message: "deployment-chain tracing requires authoritative platform truth",
            },
            truth: null,
          });
        }
        throw new Error(`unexpected request ${path}`);
      },
    });

    const result = await loadImpactReview(client, {
      target: "catalog-api",
      targetKind: "service",
    });

    expect(calls).toEqual([
      "/api/v0/impact/change-surface/investigate",
      "/api/v0/impact/trace-deployment-chain",
    ]);
    expect(result.blast.status).toBe("skipped");
    expect(result.changeSurface.status).toBe("ready");
    expect(
      result.changeSurface.status === "ready" ? result.changeSurface.data.resolution.status : "",
    ).toBe("ambiguous");
    expect(
      result.changeSurface.status === "ready" ? result.changeSurface.truth?.freshness.state : "",
    ).toBe("stale");
    expect(result.deploymentTrace.status).toBe("unavailable");
    expect(
      result.deploymentTrace.status === "unavailable" ? result.deploymentTrace.error : "",
    ).toContain("unsupported_capability");
    expect(result.graph.nodes).toHaveLength(1);
    expect(result.graph.edges).toHaveLength(0);
  });
});

function truthEnvelope(capability: string) {
  return {
    basis: "hybrid_graph_and_content",
    capability,
    freshness: { state: "fresh" },
    level: "derived",
    profile: "local_authoritative",
  };
}

function changeSurfacePayload(): Record<string, unknown> {
  return {
    code_surface: {
      changed_files: [{ relative_path: "server/routes/leads.ts", repo_id: "repository:r_catalog" }],
      matched_file_count: 1,
      source_backends: ["postgres_content_store"],
      symbol_count: 1,
      touched_symbols: [
        {
          entity_id: "entity:post-lead",
          entity_name: "postLead",
          entity_type: "Function",
          language: "typescript",
          relative_path: "server/routes/leads.ts",
        },
      ],
    },
    coverage: {
      direct_count: 1,
      limit: 25,
      max_depth: 4,
      query_shape: "resolved_change_surface_traversal",
      transitive_count: 1,
      truncated: false,
    },
    direct_impact: [
      {
        depth: 1,
        id: "workload:sample-communicator",
        labels: ["Workload"],
        name: "sample-communicator",
        repo_id: "repository:r_sample",
      },
    ],
    impact_summary: {
      direct_count: 1,
      total_count: 2,
      transitive_count: 1,
    },
    scope: {
      limit: 25,
      max_depth: 4,
      repo_id: "repository:r_catalog",
      target: "catalog-api",
      target_type: "service",
    },
    source_backend: "hybrid_graph_and_content",
    target_resolution: {
      input: "catalog-api",
      selected: {
        id: "workload:catalog-api",
        labels: ["Workload"],
        name: "catalog-api",
      },
      status: "resolved",
      target_type: "service",
      truncated: false,
    },
    transitive_impact: [
      {
        depth: 2,
        id: "repo:terraform-stack-node10",
        labels: ["Repository"],
        name: "terraform-stack-node10",
        repo_id: "repository:r_stack",
      },
    ],
    truncated: false,
  };
}

function deploymentTracePayload(): Record<string, unknown> {
  return {
    cloud_resources: [{ id: "cloud:queue", name: "lead-events", resource_type: "aws_sqs_queue" }],
    deployment_overview: {
      cloud_resource_count: 1,
      deployment_source_count: 1,
      environments: ["prod"],
      k8s_resource_count: 1,
    },
    deployment_sources: [
      {
        path: "applications/catalog-api.yaml",
        repo_name: "deployment-config",
        relationship_type: "DEPLOYS_FROM",
      },
    ],
    k8s_resources: [{ entity_name: "catalog-api", kind: "Deployment" }],
    service_name: "catalog-api",
    story: "catalog-api reaches runtime through a deployment-config repository.",
    workload_id: "workload:catalog-api",
  };
}

function dualPlatformDeploymentTracePayload(): Record<string, unknown> {
  return {
    cloud_resources: [{ id: "cloud:queue", name: "lead-events", resource_type: "aws_sqs_queue" }],
    deployment_facts: [
      { target: "prod", type: "MATERIALIZED_IN_ENVIRONMENT" },
      { target: "stage", type: "MATERIALIZED_IN_ENVIRONMENT" },
      { target: "deployment-config", target_id: "repository:r_config", type: "DEPLOYS_FROM" },
    ],
    deployment_overview: {
      environments: ["prod", "stage"],
      platform_kinds: ["ecs", "kubernetes"],
      platforms: ["catalog-ecs", "catalog-eks", "catalog-stage-eks"],
    },
    deployment_sources: [
      {
        confidence: 0.98,
        reason: "canonical deployment source",
        relationship_type: "DEPLOYS_FROM",
        repo_id: "repository:r_config",
        repo_name: "deployment-config",
        source_id: "repository:r_config",
        target_id: "repository:r_catalog",
      },
    ],
    instances: [
      {
        environment: "prod",
        instance_id: "instance:catalog:prod",
        platforms: [
          {
            platform_id: "platform:ecs:catalog-ecs",
            platform_kind: "ecs",
            platform_name: "catalog-ecs",
          },
          {
            platform_id: "platform:kubernetes:catalog-eks",
            platform_kind: "kubernetes",
            platform_name: "catalog-eks",
          },
        ],
      },
      {
        environment: "stage",
        instance_id: "instance:catalog:stage",
        platforms: [
          {
            platform_id: "platform:kubernetes:catalog-stage-eks",
            platform_kind: "kubernetes",
            platform_name: "catalog-stage-eks",
          },
        ],
      },
    ],
    k8s_resources: [{ entity_id: "k8s:catalog", entity_name: "catalog-api", kind: "Deployment" }],
    repo_id: "repository:r_catalog",
    repo_name: "catalog-api",
    service_name: "catalog-api",
    story: "catalog-api runs on ECS and Kubernetes.",
    workload_id: "workload:catalog-api",
  };
}
