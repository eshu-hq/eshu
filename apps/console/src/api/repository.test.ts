import { EshuApiClient } from "./client";
import { loadWorkspaceStory } from "./repository";

describe("workspace story adapter", () => {
  it("loads demo workspace stories from typed fixtures", async () => {
    const story = await loadWorkspaceStory({
      entityId: "workload:checkout-service",
      entityKind: "workloads",
      mode: "demo"
    });

    expect(story?.title).toBe("checkout-service");
  });

  it("calls service story routes for private workload stories", async () => {
    const paths: string[] = [];
    const fetcher = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const request = new Request(input, init);
      paths.push(new URL(request.url).pathname);
      return Response.json({
        data: {
          story: "checkout-service deploys through ArgoCD",
          subject: "checkout-service"
        },
        error: null,
        truth: {
          capability: "platform_impact.context_overview",
          freshness: { state: "fresh" },
          level: "exact",
          profile: "local_full_stack"
        }
      });
    };
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "workload:checkout-service",
      entityKind: "workloads",
      mode: "private"
    });

    expect(paths).toEqual(["/api/v0/services/workload%3Acheckout-service/story"]);
    expect(story?.story).toContain("ArgoCD");
  });

  it("loads legacy repository story payloads from the live HTTP API", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (): Promise<Response> =>
        Response.json({
          deployment_overview: {
            delivery_paths: [
              {
                artifact_type: "github_actions_workflow",
                delivery_command_families: ["helm"],
                environments: ["prod"],
                kind: "workflow_artifact",
                path: ".github/workflows/cd-helm.yml",
                signals: ["workflow_file", "run_commands"],
                trigger_events: ["push"],
                workflow_name: "cd-helm"
              }
            ],
            direct_story: ["Runs through ArgoCD into prod."],
            topology_story: ["GitHub Actions publishes an image."]
          },
          limitations: ["coverage_not_computed"],
          repository: {
            id: "repository:r_1",
            name: "mobius-tools"
          },
          story: "Repository mobius-tools contains 41 indexed files.",
          story_sections: [
            {
              summary: "41 indexed files across 2 language families",
              title: "codebase"
            }
          ],
          subject: {
            id: "repository:r_1",
            name: "mobius-tools",
            type: "repository"
          }
        })
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_1",
      entityKind: "repositories",
      mode: "private"
    });

    expect(story?.title).toBe("mobius-tools");
    expect(story?.deploymentPath).toEqual(["Runs through ArgoCD into prod."]);
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toEqual([
      "mobius-tools repo",
      "mobius-tools service",
      "GitHub Actions: cd-helm",
      "Helm artifact"
    ]);
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).not.toContain("push");
    expect(story?.overviewStats).toContainEqual(
      expect.objectContaining({
        label: "Files",
        value: "41"
      })
    );
    expect(story?.evidence).toContainEqual(
      expect.objectContaining({
        basis: "repository_story",
        source: "codebase",
        summary: "41 indexed files across 2 language families"
      })
    );
    expect(story?.limitations).toEqual(["coverage_not_computed"]);
  });

  it("promotes ArgoCD context evidence into the main workspace model", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/context")) {
          return Response.json({
            consumers: [{ id: "repository:r_argocd", name: "iac-eks-argocd" }],
            deployment_evidence: {
              artifact_count: 2,
              artifacts: [
                {
                  artifact_family: "argocd",
                  evidence_kind: "ARGOCD_APPLICATIONSET_DISCOVERY",
                  path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                  relationship_type: "DISCOVERS_CONFIG_IN",
                  source_location: {
                    path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                    repo_name: "iac-eks-argocd"
                  },
                  source_repo_name: "iac-eks-argocd",
                  target_repo_name: "iac-eks-pcg"
                }
              ]
            },
            file_count: 58,
            infrastructure: [{ name: "platformcontextgraph", type: "HelmChart" }]
          });
        }
        return Response.json({
          deployment_overview: {
            infrastructure_families: ["helm", "kubernetes", "kustomize"],
            workload_count: 1
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_aba334de/context"
          },
          infrastructure_overview: {
            families: ["helm", "kubernetes", "kustomize"]
          },
          story_sections: [
            {
              summary: "58 indexed file(s) across 3 language family(s)",
              title: "codebase"
            }
          ],
          subject: {
            id: "repository:r_aba334de",
            name: "iac-eks-pcg",
            type: "repository"
          },
          support_overview: {
            languages: ["yaml", "json", "template"]
          }
        });
      }
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_aba334de",
      entityKind: "repositories",
      mode: "private"
    });

    expect(story?.story).toContain("iac-eks-argocd references it");
    expect(story?.evidence[0]).toMatchObject({
      basis: "DISCOVERS_CONFIG_IN",
      source: "iac-eks-argocd",
      title: "Deployed by ArgoCD"
    });
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("ArgoCD ApplicationSet");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("iac-eks-argocd");
  });

  it("uses service context for single-workload repository deployment graphs", async () => {
    const paths: string[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        paths.push(path);
        if (path === "/api/v0/services/boats-chatgpt-app/context") {
          return Response.json({
            deployment_evidence: {
              artifact_count: 3,
              artifacts: [
                {
                  artifact_family: "argocd",
                  evidence_kind: "ARGOCD_APPLICATIONSET_DISCOVERY",
                  path: "applicationsets/devops/core-mcps/boats-search-mcp.yaml",
                  relationship_type: "DISCOVERS_CONFIG_IN",
                  source_repo_name: "iac-eks-argocd"
                },
                {
                  artifact_family: "helm",
                  environment: "bg-qa",
                  evidence_kind: "HELM_CHART_REFERENCE",
                  path: "charts/boats-chatgpt-app/Chart.yaml",
                  relationship_type: "DEPLOYS_FROM",
                  source_repo_name: "helm-charts"
                }
              ]
            }
          });
        }
        if (path.endsWith("/context")) {
          return Response.json({ deployment_evidence: { artifacts: [] } });
        }
        return Response.json({
          deployment_overview: {
            delivery_paths: [
              {
                delivery_command_families: ["docker"],
                kind: "workflow_artifact",
                path: ".github/workflows/cd-docker.yml",
                trigger_events: ["push", "workflow_dispatch"],
                workflow_name: "cd-docker"
              }
            ],
            workload_count: 1,
            workloads: ["boats-chatgpt-app"]
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_5ea26675/context"
          },
          repository: {
            id: "repository:r_5ea26675",
            name: "boats-chatgpt-app"
          },
          story_sections: [{ summary: "69 indexed file(s)", title: "codebase" }],
          subject: {
            id: "repository:r_5ea26675",
            name: "boats-chatgpt-app",
            type: "repository"
          }
        });
      }
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_5ea26675",
      entityKind: "repositories",
      mode: "private"
    });

    expect(paths).toContain("/api/v0/services/boats-chatgpt-app/context");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toEqual([
      "boats-chatgpt-app repo",
      "boats-chatgpt-app service",
      "iac-eks-argocd",
      "ArgoCD ApplicationSet",
      "helm-charts",
      "Helm chart/values",
      "bg-qa",
      "GitHub Actions: cd-docker",
      "Docker image"
    ]);
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).not.toContain("push");
  });

  it("uses service consumer repositories as deployment evidence when artifact rows are absent", async () => {
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL): Promise<Response> => {
        const path = new URL(new Request(input).url).pathname;
        if (path === "/api/v0/services/boats-chatgpt-app/context") {
          return Response.json({
            consumer_repositories: [
              {
                consumer_kinds: ["service_reference_consumer"],
                evidence_kinds: ["repository_reference"],
                repo_name: "iac-eks-argocd",
                sample_paths: [
                  "applicationsets/devops/core-mcps/boats-search-mcp.yaml"
                ]
              },
              {
                consumer_kinds: ["service_reference_consumer"],
                evidence_kinds: ["repository_reference"],
                repo_name: "helm-charts",
                sample_paths: ["charts/boats-chatgpt-app/Chart.yaml"]
              }
            ],
            deployment_evidence: {
              artifacts: []
            }
          });
        }
        if (path.endsWith("/context")) {
          return Response.json({});
        }
        return Response.json({
          deployment_overview: {
            workload_count: 1,
            workloads: ["boats-chatgpt-app"]
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_boats/context"
          },
          repository: {
            id: "repository:r_boats",
            name: "boats-chatgpt-app"
          },
          subject: {
            id: "repository:r_boats",
            name: "boats-chatgpt-app",
            type: "repository"
          }
        });
      }
    });

    const story = await loadWorkspaceStory({
      client,
      entityId: "repository:r_boats",
      entityKind: "repositories",
      mode: "private"
    });

    expect(story?.story).toContain("iac-eks-argocd and helm-charts reference it");
    expect(story?.evidence).toContainEqual(
      expect.objectContaining({
        source: "iac-eks-argocd",
        title: "Deployed by ArgoCD"
      })
    );
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("iac-eks-argocd");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("ArgoCD ApplicationSet");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("helm-charts");
    expect(story?.deploymentGraph.nodes.map((node) => node.label)).toContain("Helm chart/values");
  });
});
