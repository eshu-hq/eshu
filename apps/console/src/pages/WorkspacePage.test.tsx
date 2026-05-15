import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { vi } from "vitest";
import { WorkspacePage } from "./WorkspacePage";

describe("WorkspacePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders a live repository story with proof, freshness, and deployment", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
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
            direct_story: ["Runs through ArgoCD into prod."]
          },
          limitations: ["coverage_not_computed"],
          story: "Repository mobius-tools contains indexed files.",
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
      )
    );

    render(
      <MemoryRouter initialEntries={["/workspace/repositories/repository:r_1"]}>
        <Routes>
          <Route element={<WorkspacePage />} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </MemoryRouter>
    );

    expect(await screen.findByRole("heading", { name: "mobius-tools" })).toBeInTheDocument();
    expect(screen.getByText(/contains 41 indexed files/i)).toBeInTheDocument();
    expect(screen.getByText("exact")).toBeInTheDocument();
    expect(screen.getByText("fresh")).toBeInTheDocument();
    expect(screen.getByText("Evidence graph")).toBeInTheDocument();
    expect(screen.getByText(/Deployment relationships found/i)).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /cd-helm/i }).length).toBeGreaterThan(0);
    expect(screen.getByText("Evidence story")).toBeInTheDocument();
    expect(screen.getByText("41 indexed files across 2 language families")).toBeInTheDocument();
    expect(screen.getByText("Evidence index")).toBeInTheDocument();
    expect(screen.getAllByText("Inspect evidence")).toHaveLength(1);
    expect(screen.getByText("Known gaps")).toBeInTheDocument();
  });

  it("renders service context details when the selected repository has one workload", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/api/v0/services/api-node-boats/story")) {
          return Response.json({
            service_identity: {
              repo_name: "api-node-boats",
              service_name: "api-node-boats"
            },
            api_surface: {
              endpoint_count: 38,
              endpoints: [
                {
                  methods: ["get"],
                  operation_ids: ["getListing"],
                  path: "/getListing",
                  source_paths: ["specs/index.yaml"]
                }
              ],
              method_count: 44,
              source_paths: ["catalog-specs.yaml", "specs/index.yaml"]
            },
            deployment_lanes: [
              {
                environments: ["bg-prod"],
                lane_type: "k8s_gitops",
                resolved_ids: ["rel:k8s"],
                source_repositories: ["helm-charts"]
              },
              {
                environments: ["bg-prod"],
                lane_type: "ecs_terraform",
                resolved_ids: ["rel:ecs"],
                source_repositories: ["terraform-stack-node10"]
              }
            ],
            downstream_consumers: {
              content_consumers: [
                {
                  consumer_kinds: ["service_reference_consumer"],
                  repository: "terraform-stack-node10",
                  sample_paths: ["environments/bg-prod/ecs.tf"]
                }
              ]
            },
            upstream_dependencies: [
              {
                evidence_count: 4,
                rationale: "called by reusable workflow",
                relationship_type: "DEPLOYS_FROM",
                target: "core-engineering-automation"
              }
            ],
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "terraform",
                  evidence_kind: "TERRAFORM_ECS_SERVICE",
                  source_repo_name: "terraform-stack-node10"
                },
                {
                  artifact_family: "helm",
                  evidence_kind: "HELM_CHART_REFERENCE",
                  source_repo_name: "helm-charts"
                }
              ]
            },
            provisioning_source_chains: [{ repository: "terraform-stack-node10" }]
          });
        }
        if (path.endsWith("/api/v0/repositories/repository:r_472ddee5/context")) {
          return Response.json({ deployment_evidence: { artifacts: [] } });
        }
        return Response.json({
          deployment_overview: {
            workload_count: 1,
            workloads: ["api-node-boats"]
          },
          drilldowns: {
            context_path: "/api/v0/repositories/repository:r_472ddee5/context"
          },
          story: "Repository api-node-boats contains indexed files.",
          story_sections: [{ summary: "538 indexed files", title: "codebase" }],
          subject: {
            id: "repository:r_472ddee5",
            name: "api-node-boats",
            type: "repository"
          }
        });
      })
    );

    render(
      <MemoryRouter initialEntries={["/workspace/repositories/repository:r_472ddee5"]}>
        <Routes>
          <Route element={<WorkspacePage />} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </MemoryRouter>
    );

    expect(
      await screen.findByRole("heading", { level: 1, name: "api-node-boats" })
    ).toBeInTheDocument();
    expect(screen.getAllByText(/38 endpoint/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Kubernetes/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/ECS/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText("core-engineering-automation").length).toBeGreaterThan(0);
    expect(screen.getAllByText("terraform-stack-node10").length).toBeGreaterThan(0);
    expect(screen.getByText("/getListing")).toBeInTheDocument();
    expect(screen.getByText(/Raw deployment evidence behind the service story/i)).toBeInTheDocument();
  });

  it("renders the same service dossier for a direct service workspace route", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          service_identity: {
            repo_name: "api-node-boats",
            service_name: "api-node-boats"
          },
          story: "Workload api-node-boats is defined in repository api-node-boats.",
          api_surface: {
            endpoint_count: 38,
            endpoints: [
              {
                methods: ["get"],
                operation_ids: ["getListing"],
                path: "/getListing",
                source_paths: ["catalog-specs.yaml"]
              },
              {
                methods: ["get"],
                operation_ids: ["getVersion"],
                path: "/_version",
                source_paths: ["catalog-specs.yaml"]
              }
            ],
            method_count: 44,
            source_paths: ["catalog-specs.yaml"]
          },
          deployment_lanes: [
            {
              environments: ["bg-dev", "bg-prod", "bg-qa"],
              lane_type: "ecs_terraform",
              relationship_types: ["PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM"],
              resolved_ids: ["rel:ecs"],
              source_repositories: ["terraform-stack-node10"]
            },
            {
              environments: ["bg-dev", "bg-prod", "bg-qa", "ops-prod", "ops-qa"],
              lane_type: "k8s_gitops",
              relationship_types: ["DEPLOYS_FROM"],
              resolved_ids: ["rel:k8s"],
              source_repositories: ["helm-charts", "iac-eks-argocd"]
            }
          ],
          downstream_consumers: {
            content_consumer_count: 25,
            graph_dependent_count: 17,
            content_consumers: [
              {
                consumer_kinds: ["service_reference_consumer"],
                repository: "terraform-stack-node10"
              }
            ],
            graph_dependents: [
              {
                graph_relationship_types: ["DEPLOYS_FROM"],
                repository: "iac-eks-argocd"
              }
            ]
          },
          hostnames: [
            {
              environment: "prod",
              hostname: "api-node-boats.prod.bgrp.io",
              relative_path: "config/production.json"
            }
          ],
          result_limits: {
            downstream_count: 42,
            upstream_count: 35
          },
          upstream_dependencies: [
            {
              relationship_type: "DEPLOYS_FROM",
              target: "core-engineering-automation"
            }
          ],
          investigation: {
            coverage_summary: {
              reason: "evidence was found, but Eshu cannot prove exhaustive coverage",
              repositories_with_evidence_count: 26,
              repository_count: 26,
              state: "partial"
            },
            evidence_families_found: ["api_surface", "deployment_lanes"],
            investigation_findings: [
              {
                evidence_path: "api_surface",
                family: "api_surface",
                summary: "38 endpoint(s) across 0 spec file(s)"
              }
            ],
            recommended_next_calls: [
              {
                arguments: { workload_id: "api-node-boats" },
                reason: "retrieve the full one-call dossier",
                tool: "get_service_story"
              },
              {
                arguments: {
                  limit: 25,
                  relationship_type: "CALLS",
                  target: "createLead"
                },
                reason: "inspect call graph behind one API path",
                tool: "get_code_relationship_story"
              }
            ],
            repositories_with_evidence: [
              {
                evidence_families: ["api_surface", "deployment_lanes"],
                repo_name: "api-node-boats",
                roles: ["service_owner"]
              }
            ]
          }
        })
      )
    );

    render(
      <MemoryRouter initialEntries={["/workspace/services/workload:api-node-boats"]}>
        <Routes>
          <Route element={<WorkspacePage />} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </MemoryRouter>
    );

    expect(
      await screen.findByRole("heading", { level: 1, name: "api-node-boats" })
    ).toBeInTheDocument();
    expect(screen.queryByText("Files")).not.toBeInTheDocument();
    expect(screen.getByRole("searchbox", { name: "Search API endpoints" })).toBeInTheDocument();
    expect(screen.getAllByText(/Dual deployment/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/ECS Terraform/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Kubernetes GitOps/i).length).toBeGreaterThan(0);
    expect(screen.getByText("api-node-boats.prod.bgrp.io")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Investigation coverage" })).toBeInTheDocument();
    expect(screen.getByText("Partial")).toBeInTheDocument();
    expect(screen.getByText("26 with evidence of 26 checked")).toBeInTheDocument();
    expect(screen.getAllByText("API Surface").length).toBeGreaterThan(0);
    expect(screen.getByText("Service story")).toBeInTheDocument();
    expect(screen.getByText("Code relationship story")).toBeInTheDocument();
  });
});
