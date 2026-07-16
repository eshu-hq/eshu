import { EshuApiClient } from "./client";
import { loadWorkspaceStory } from "./repository";

describe("repository consumer evidence adapter", () => {
  it("uses service consumer repositories as deployment evidence when artifact rows are absent", async () => {
    const paths: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        paths.push(path);
        if (path === "/api/v0/services/items-chatgpt-app/story") {
          return Response.json({
            downstream_consumers: {
              content_consumers: [
                {
                  consumer_kinds: ["service_reference_consumer"],
                  evidence_kinds: ["repository_reference"],
                  repository: "iac-eks-argocd",
                  sample_paths: ["applicationsets/devops/core-mcps/items-search-mcp.yaml"],
                },
                {
                  consumer_kinds: ["service_reference_consumer"],
                  evidence_kinds: ["repository_reference"],
                  repository: "helm-charts",
                  sample_paths: ["charts/items-chatgpt-app/Chart.yaml"],
                },
              ],
            },
            deployment_evidence: {
              artifacts: [],
            },
          });
        }
        if (path.endsWith("/context")) {
          return Response.json({});
        }
        return Response.json({
          deployment_overview: {
            workload_count: 1,
            workloads: ["items-chatgpt-app"],
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_items/context",
          },
          repository: { id: "repository:r_items", name: "items-chatgpt-app" },
          subject: {
            id: "repository:r_items",
            name: "items-chatgpt-app",
            type: "repository",
          },
        });
      },
    });
    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_items",
      entityKind: "repositories",
      mode: "private",
    });
    expect(paths).toContain("/api/v0/services/items-chatgpt-app/story");
    expect(story?.story).toContain("iac-eks-argocd and helm-charts reference it");
    expect(story?.evidence).toContainEqual(
      expect.objectContaining({
        source: "iac-eks-argocd",
        title: "Deployed by ArgoCD",
      }),
    );
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("iac-eks-argocd");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain(
      "ArgoCD ApplicationSet",
    );
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("helm-charts");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("Helm chart/values");
  });
});
