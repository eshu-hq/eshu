import { EshuApiClient } from "./client";
import {
  loadCatalogServiceRows,
  loadCatalogRows,
  loadDashboardMetrics,
  loadFindingRows,
  loadSearchCandidates
} from "./liveData";
import { loadDashboardSnapshot } from "./dashboardSnapshot";

function clientFor(
  routes: Record<string, unknown>,
  requests: string[] = []
): EshuApiClient {
  return new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
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
  repositories: [
    {
      id: "repository:r_1",
      local_path: "/Users/allen/repos/mobius/mobius-tools",
      name: "mobius-tools"
    },
    {
      id: "repository:r_2",
      local_path: "/Users/allen/repos/mobius/iac-eks-pcg",
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
      "mobius-tools",
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

  it("uses catalog repository rows for paginated dashboard catalog totals", async () => {
    const requests: string[] = [];
    const metrics = await loadDashboardMetrics({
      client: clientFor({
        "/api/v0/index-status": {
          queue: { outstanding: 0, succeeded: 8347 },
          repository_count: 896,
          status: "healthy"
        },
        "/api/v0/repositories": {
          count: 896,
          limit: 100,
          repositories: repositoriesResponse.repositories
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
          repository: { name: "mobius-tools" }
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
      coverage: "/Users/allen/repos/mobius/mobius-tools",
      freshness: "indexed",
      id: "repository:r_1",
      kind: "repositories",
      name: "mobius-tools"
    });
  });

  it("prefers first-class catalog rows when the API exposes them", async () => {
    const rows = await loadCatalogRows({
      client: clientFor({
        "/api/v0/catalog": {
          repositories: [
            {
              id: "repository:r_1",
              local_path: "/Users/allen/repos/mobius/api-node-boats",
              name: "api-node-boats"
            }
          ],
          services: [
            {
              environments: ["ecs-prod", "eks-prod"],
              id: "workload:api-node-boats",
              kind: "service",
              name: "api-node-boats",
              repo_name: "api-node-boats"
            }
          ],
          workloads: [
            {
              environments: ["ecs-prod", "eks-prod"],
              id: "workload:api-node-boats",
              kind: "service",
              name: "api-node-boats",
              repo_name: "api-node-boats"
            },
            {
              environments: ["prod"],
              id: "workload:billing-sync",
              kind: "cronjob",
              name: "billing-sync",
              repo_name: "api-node-boats"
            }
          ]
        }
      }),
      mode: "private"
    });

    expect(rows).toContainEqual({
      coverage: "/Users/allen/repos/mobius/api-node-boats",
      freshness: "indexed",
      id: "repository:r_1",
      kind: "repositories",
      name: "api-node-boats"
    });
    expect(rows).toContainEqual({
      coverage: "api-node-boats across ecs-prod, eks-prod",
      environments: ["ecs-prod", "eks-prod"],
      freshness: "graph",
      id: "workload:api-node-boats",
      instanceCount: undefined,
      kind: "services",
      materializationStatus: "graph",
      name: "api-node-boats",
      ownerRepo: "api-node-boats",
      workloadKind: "service"
    });
    expect(rows).toContainEqual({
      coverage: "api-node-boats across prod",
      environments: ["prod"],
      freshness: "graph",
      id: "workload:billing-sync",
      instanceCount: undefined,
      kind: "workloads",
      materializationStatus: "graph",
      name: "billing-sync",
      ownerRepo: "api-node-boats",
      workloadKind: "cronjob"
    });
    expect(rows.filter((row) => row.id === "workload:api-node-boats")).toHaveLength(1);
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
                  file_path: "server/src/api/boatsClient.ts",
                  name: "parseRange",
                  repo_name: "boats-chatgpt-app"
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
        entity: "boats-chatgpt-app",
        findingType: "Dead code",
        location: "server/src/api/boatsClient.ts",
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
                file_path: "server/src/api/boatsClient.ts",
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

    expect(rows[0]?.entity).toBe("mobius-tools");
  });
});
