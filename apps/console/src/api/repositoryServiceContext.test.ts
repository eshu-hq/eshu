import { describe, expect, it } from "vitest";

import { EshuApiClient } from "./client";
import { loadWorkspaceStory } from "./repository";

describe("repository workspace service context", () => {
  it("does not treat an opaque workload identity as a service selector", async () => {
    const paths: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        paths.push(path);
        if (path.endsWith("/context")) return Response.json({});
        return Response.json({
          deployment_overview: {
            workload_count: 1,
            workloads: [
              "reducer_git-repository-scope_repository_r_a98a262f_e8c4b70fc29ed15aaa70bb4ae35ae9488af089a56e7f0ddb72db196e8bc2d93b_workload_identity_workload_App_Build_Release_Pipeline_Stage_0",
            ],
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_pipeline/context",
          },
          repository: { id: "repository:r_pipeline", name: "pipeline" },
          subject: { id: "repository:r_pipeline", name: "pipeline", type: "repository" },
        });
      },
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_pipeline",
      entityKind: "repositories",
      mode: "private",
    });

    expect(story?.title).toBe("pipeline");
    expect(paths.some((path) => path.startsWith("/api/v0/services/"))).toBe(false);
  });

  it("uses the first readable workload after an opaque identity", async () => {
    const opaqueWorkload =
      "reducer_git-repository-scope_repository_r_a98a262f_workload_identity_workload_App_Build_Release_Pipeline_Stage_0";
    const paths: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        paths.push(path);
        if (path === "/api/v0/services/payments-api/story") {
          return Response.json({
            service_identity: { service_name: "payments-api" },
          });
        }
        if (path.endsWith("/context")) return Response.json({});
        return Response.json({
          deployment_overview: {
            workload_count: 2,
            workloads: [opaqueWorkload, "payments-api"],
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_pipeline/context",
          },
          repository: { id: "repository:r_pipeline", name: "pipeline" },
          subject: { id: "repository:r_pipeline", name: "pipeline", type: "repository" },
        });
      },
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_pipeline",
      entityKind: "repositories",
      mode: "private",
    });

    expect(paths).toContain("/api/v0/services/payments-api/story");
    expect(paths.some((path) => path.includes(encodeURIComponent(opaqueWorkload)))).toBe(false);
    expect(story?.serviceSpotlight?.name).toBe("payments-api");
  });
});
