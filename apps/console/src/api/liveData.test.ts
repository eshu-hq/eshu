import { EshuApiClient } from "./client";
import { loadDashboardSnapshot } from "./dashboardSnapshot";
import {
  loadCatalogServiceRows,
  loadCatalogRows,
  loadDashboardMetrics,
  loadFindingRows,
  loadSearchCandidates
} from "./liveData";
import { inspectionRequest } from "../test/inspectionRequest";

function clientFor(
  routes: Record<string, unknown>,
  requests: string[] = []
): EshuApiClient {
  return new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = inspectionRequest(input, init);
      const url = new URL(request.url);
      requests.push(`${url.pathname}${url.search}`);
      const body = routes[url.pathname];
      if (body === undefined) {
        return Response.json({ detail: "missing route" }, { status: 404 });
      }
      return Response.json(body);
    }
  });
}

const repositoriesResponse = {
  count: 2,
  // total is the true count independent of page size, added in issue #3392.
  total: 2,
  repositories: [
    {
      id: "repository:r_1",
      local_path: "/workspace/sample/platform-tools",
      name: "platform-tools"
    },
    {
      id: "repository:r_2",
      local_path: "/workspace/sample/iac-eks-pcg",
      name: "iac-eks-pcg"
    }
  ]
};

describe("live Eshu data adapters", () => {
  it("loads repository search candidates from the API instead of fixtures", async () => {
    const candidates = await loadSearchCandidates({
      client: clientFor({ "/api/v0/repositories": repositoriesResponse }),
      mode: "private"
    });

    expect(candidates.map((candidate) => candidate.label)).toEqual([
      "platform-tools",
      "iac-eks-pcg"
    ]);
  });

  it("builds dashboard metrics from live index status", async () => {
    const metrics = await loadDashboardMetrics({
      client: clientFor({
        "/api/v0/index-status": {
          queue: { outstanding: 0, succeeded: 201 },
          repository_count: 23,
          status: "healthy"
        },
        "/api/v0/repositories": repositoriesResponse
      }),
      mode: "private"
    });

    expect(metrics).toContainEqual({
      detail: "No runtime status reasons reported.",
      label: "Index status",
      value: "healthy"
    });
    expect(metrics).toContainEqual({
      detail: "Repository count reported by the graph status endpoint.",
      label: "Graph repositories",
      value: "23"
    });
    expect(metrics).toContainEqual({
      detail: "Repositories available through catalog drilldown.",
      label: "Catalog repositories",
      value: "2"
    });
    expect(metrics).toContainEqual({
      detail: "No queued work is waiting on reducers or projectors.",
      label: "Queue outstanding",
      value: "0"
    });
  });

  it("uses server-supplied total for dashboard catalog count (not page-slice count)", async () => {
    // Regression for issue #3392: loadRepositorySummary must read payload.total
    // (the true count independent of page size) rather than payload.count (the
    // per-page slice length). The limit=1 probe would previously return count=1,
    // so the dashboard showed 1 instead of the actual total.
    const requests: string[] = [];
    const metrics = await loadDashboardMetrics({
      client: clientFor({
        "/api/v0/index-status": {
          queue: { outstanding: 0, succeeded: 8347 },
          repository_count: 896,
          status: "healthy"
        },
        "/api/v0/repositories": {
          count: 1,
          total: 896,
          limit: 1,
          repositories: repositoriesResponse.repositories.slice(0, 1)
        }
      }, requests),
      mode: "private"
    });

    expect(metrics).toContainEqual({
      detail: "Repositories available through catalog drilldown.",
      label: "Catalog repositories",
      value: "896"
    });
    expect(requests).not.toContain("/api/v0/catalog?limit=2000&offset=0");
    expect(requests).toContain("/api/v0/repositories?limit=1&offset=0");
  });

  it("keeps degraded graph status separate from queryable catalog data", async () => {
    const metrics = await loadDashboardMetrics({
      client: clientFor({
        "/api/v0/index-status": {
          queue: { dead_letter: 4, in_flight: 1, outstanding: 1, succeeded: 209 },
          reasons: ["4 work items are dead-lettered"],
          repository_count: 0,
          status: "degraded"
        },
        "/api/v0/repositories": repositoriesResponse
      }),
      mode: "private"
    });

    expect(metrics).toContainEqual({
      detail: "4 work items are dead-lettered",
      label: "Index status",
      value: "degraded"
    });
    expect(metrics).toContainEqual({
      detail: "Repository count reported by the graph status endpoint.",
      label: "Graph repositories",
      value: "0"
    });
    expect(metrics).toContainEqual({
      detail: "Repositories available through catalog drilldown.",
      label: "Catalog repositories",
      value: "2"
    });
    expect(metrics).toContainEqual({
      detail: "4 dead-lettered work item(s).",
      label: "Dead letters",
      value: "4"
    });
  });

  it("builds the dashboard relationship graph from typed deployment evidence", async () => {
    const snapshot = await loadDashboardSnapshot({
      client: clientFor({
        "/api/v0/index-status": {
          queue: { outstanding: 0, succeeded: 245 },
          repository_count: 2,
          status: "healthy"
        },
        "/api/v0/repositories": repositoriesResponse,
        "/api/v0/repositories/repository%3Ar_1/story": {
          drilldowns: { context_path: "/api/v0/repositories/repository:r_1/context" },
          repository: { name: "platform-tools" }
        },
        "/api/v0/repositories/repository%3Ar_1/context": {},
        "/api/v0/repositories/repository%3Ar_2/story": {
          deployment_overview: { workloads: ["iac-eks-pcg"] },
          drilldowns: { context_path: "/api/v0/repositories/repository:r_2/context" },
          repository: { name: "iac-eks-pcg" }
        },
        "/api/v0/repositories/repository%3Ar_2/context": {
          deployment_evidence: {
            artifacts: [
              {
                artifact_family: "argocd",
                name: "iac-eks-pcg",
                relationship_type: "DISCOVERS_CONFIG_IN",
                source_location: {
                  path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                  repo_name: "iac-eks-argocd"
                },
                source_repo_name: "iac-eks-argocd",
                target_repo_name: "iac-eks-pcg"
              }
            ]
          }
        },
        "/api/v0/repositories/repository:r_2/context": {
          deployment_evidence: {
            artifacts: [
              {
                artifact_family: "argocd",
                name: "iac-eks-pcg",
                relationship_type: "DISCOVERS_CONFIG_IN",
                source_location: {
                  path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                  repo_name: "iac-eks-argocd"
                },
                source_repo_name: "iac-eks-argocd",
                target_repo_name: "iac-eks-pcg"
              }
            ]
          }
        },
        "/api/v0/services/iac-eks-pcg/context": {
          deployment_evidence: {
            artifacts: [
              {
                artifact_family: "helm",
                name: "iac-eks-pcg",
                relationship_type: "DEPLOYS_FROM",
                source_location: {
                  path: "charts/platformcontextgraph/values.yaml",
                  repo_name: "helm-charts"
                },
                source_repo_name: "helm-charts",
                target_repo_name: "iac-eks-pcg"
              }
            ]
          }
        }
      }),
      mode: "private"
    });

    expect(snapshot.relationships).toContainEqual({
      count: 1,
      detail: "Controller discovers configuration",
      layer: "canonical",
      verb: "DISCOVERS_CONFIG_IN"
    });
    expect(snapshot.relationships).toContainEqual({
      count: 1,
      detail: "Service deploys from source",
      layer: "canonical",
      verb: "DEPLOYS_FROM"
    });
    expect(snapshot.relationships).toContainEqual({
      count: 0,
      detail: "Runtime placement",
      layer: "canonical",
      verb: "RUNS_ON"
    });
    expect(snapshot.relationships).toContainEqual({
      count: 0,
      detail: "Config read permission or config source",
      layer: "canonical",
      verb: "READS_CONFIG_FROM"
    });
    expect(snapshot.relationships).toContainEqual({
      count: 0,
      detail: "Repository defines workload",
      layer: "topology",
      verb: "DEFINES"
    });
    expect(snapshot.relationships).toContainEqual({
      count: 0,
      detail: "Deployment source context",
      layer: "topology",
      verb: "DEPLOYMENT_SOURCE"
    });
    expect(snapshot.graph.nodes.map((node) => node.label)).toEqual(
      expect.arrayContaining(["iac-eks-argocd", "DISCOVERS_CONFIG_IN", "iac-eks-pcg"])
    );
    expect(snapshot.evidence[0]?.summary).toContain(
      "iac-eks-argocd DISCOVERS_CONFIG_IN iac-eks-pcg"
    );
  });

  it("loads catalog rows from live repositories", async () => {
    const rows = await loadCatalogRows({
      client: clientFor({ "/api/v0/repositories": repositoriesResponse }),
      mode: "private"
    });

    expect(rows).toContainEqual({
      coverage: "/workspace/sample/platform-tools",
      freshness: "indexed",
      id: "repository:r_1",
      kind: "repositories",
      name: "platform-tools"
    });
  });

  it("prefers first-class catalog rows when the API exposes them", async () => {
    const rows = await loadCatalogRows({
      client: clientFor({
        "/api/v0/catalog": {
          repositories: [
            {
              id: "repository:r_1",
              local_path: "/workspace/sample/catalog-api",
              name: "catalog-api"
            }
          ],
          services: [
            {
              environments: ["ecs-prod", "eks-prod"],
              id: "workload:catalog-api",
              kind: "service",
              name: "catalog-api",
              repo_name: "catalog-api"
            }
          ],
          workloads: [
            {
              environments: ["ecs-prod", "eks-prod"],
              id: "workload:catalog-api",
              kind: "service",
              name: "catalog-api",
              repo_name: "catalog-api"
            },
            {
              environments: ["prod"],
              id: "workload:billing-sync",
              kind: "cronjob",
              name: "billing-sync",
              repo_name: "catalog-api"
            }
          ]
        }
      }),
      mode: "private"
    });

    expect(rows).toContainEqual({
      coverage: "/workspace/sample/catalog-api",
      freshness: "indexed",
      id: "repository:r_1",
      kind: "repositories",
      name: "catalog-api"
    });
    expect(rows).toContainEqual({
      coverage: "catalog-api across ecs-prod, eks-prod",
      environments: ["ecs-prod", "eks-prod"],
      freshness: "graph",
      id: "workload:catalog-api",
      instanceCount: undefined,
      kind: "services",
      materializationStatus: "graph",
      name: "catalog-api",
      ownerRepo: "catalog-api",
      workloadKind: "service"
    });
    expect(rows).toContainEqual({
      coverage: "catalog-api across prod",
      environments: ["prod"],
      freshness: "graph",
      id: "workload:billing-sync",
      instanceCount: undefined,
      kind: "workloads",
      materializationStatus: "graph",
      name: "billing-sync",
      ownerRepo: "catalog-api",
      workloadKind: "cronjob"
    });
    expect(rows.filter((row) => row.id === "workload:catalog-api")).toHaveLength(1);
  });

  it("derives service catalog rows from repository stories", async () => {
    const rows = await loadCatalogServiceRows({
      client: clientFor({
        "/api/v0/repositories": repositoriesResponse,
        "/api/v0/repositories/repository%3Ar_1/story": {
          deployment_overview: { workloads: [] }
        },
        "/api/v0/repositories/repository%3Ar_2/story": {
          deployment_overview: { workloads: ["iac-eks-pcg"] }
        }
      }),
      mode: "private"
    });

    expect(rows).toContainEqual({
      coverage: "defined by iac-eks-pcg",
      freshness: "story",
      id: "iac-eks-pcg",
      kind: "services",
      name: "iac-eks-pcg"
    });
  });

  it("loads dead-code findings from the live API", async () => {
    const rows = await loadFindingRows({
      client: new EshuApiClient({
        baseUrl: "http://localhost:8080",
        fetcher: async (): Promise<Response> =>
          Response.json({
            data: {
              results: [
                {
                  classification: "unused",
                  file_path: "server/src/api/itemsClient.ts",
                  name: "parseRange",
                  repo_name: "items-chatgpt-app"
                }
              ]
            },
            error: null,
            truth: {
              capability: "code_quality.dead_code",
              freshness: { state: "fresh" },
              level: "derived",
              profile: "local_authoritative"
            }
          })
      }),
      mode: "private"
    });

    expect(rows).toEqual([
      {
        entity: "items-chatgpt-app",
        findingType: "Dead code",
        location: "server/src/api/itemsClient.ts",
        name: "parseRange",
        truthLevel: "derived"
      }
    ]);
  });

  it("resolves dead-code repository IDs through the live repository catalog", async () => {
    const rows = await loadFindingRows({
      client: clientFor({
        "/api/v0/code/dead-code": {
          data: {
            results: [
              {
                classification: "unused",
                file_path: "server/src/api/itemsClient.ts",
                name: "parseRange",
                repo_id: "repository:r_1"
              }
            ]
          },
          error: null,
          truth: {
            capability: "code_quality.dead_code",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "local_authoritative"
          }
        },
        "/api/v0/repositories": repositoriesResponse
      }),
      mode: "private"
    });

    expect(rows[0]?.entity).toBe("platform-tools");
  });
});
