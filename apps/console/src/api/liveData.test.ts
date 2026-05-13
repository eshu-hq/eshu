import { EshuApiClient } from "./client";
import {
  loadCatalogRows,
  loadDashboardMetrics,
  loadFindingRows,
  loadSearchCandidates
} from "./liveData";

function clientFor(routes: Record<string, unknown>): EshuApiClient {
  return new EshuApiClient({
    baseUrl: "http://localhost:8080",
    fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
      const body = routes[new URL(request.url).pathname];
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
        }
      }),
      mode: "private"
    });

    expect(metrics).toContainEqual({ label: "Index status", value: "healthy" });
    expect(metrics).toContainEqual({ label: "Repositories", value: "23" });
    expect(metrics).toContainEqual({ label: "Queue outstanding", value: "0" });
  });

  it("loads catalog rows from live repositories", async () => {
    const rows = await loadCatalogRows({
      client: clientFor({ "/api/v0/repositories": repositoriesResponse }),
      mode: "private"
    });

    expect(rows[0]).toEqual({
      coverage: "/Users/allen/repos/mobius/mobius-tools",
      freshness: "indexed",
      id: "repository:r_1",
      kind: "repositories",
      name: "mobius-tools"
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
