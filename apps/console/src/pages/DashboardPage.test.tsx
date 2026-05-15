import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { DashboardPage } from "./DashboardPage";

describe("DashboardPage", () => {
  it("shows live runtime and indexing status", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/api/v0/repositories")) {
          return Response.json({
            repositories: [
              { id: "repository:r_1", name: "mobius-tools" },
              { id: "repository:r_3", name: "hapi-phraseapp" },
              { id: "repository:r_2", name: "checkout-api" }
            ]
          });
        }
        if (path.endsWith("/api/v0/repositories/repository%3Ar_1/story")) {
          return Response.json({
            drilldowns: { context_path: "/api/v0/repositories/repository:r_1/context" }
          });
        }
        if (path.endsWith("/api/v0/repositories/repository:r_1/context")) {
          return Response.json({});
        }
        if (path.endsWith("/api/v0/repositories/repository%3Ar_2/story")) {
          return Response.json({
            deployment_overview: { workloads: ["checkout-api"] },
            drilldowns: { context_path: "/api/v0/repositories/repository:r_2/context" }
          });
        }
        if (
          path.endsWith("/api/v0/repositories/repository:r_2/context") ||
          path.endsWith("/api/v0/repositories/repository%3Ar_2/context")
        ) {
          return Response.json({
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "argocd",
                  relationship_type: "DISCOVERS_CONFIG_IN",
                  source_location: {
                    path: "applicationsets/devops/core-mcps/platformcontextgraph.yaml",
                    repo_name: "iac-eks-argocd"
                  },
                  source_repo_name: "iac-eks-argocd",
                  target_repo_name: "checkout-api"
                }
              ]
            }
          });
        }
        if (path.endsWith("/api/v0/services/checkout-api/context")) {
          return Response.json({
            api_surface: {
              endpoint_count: 3,
              endpoints: [
                {
                  methods: ["get"],
                  operation_ids: ["listOrders"],
                  path: "/v1/orders",
                  source_paths: ["specs/openapi.yaml"]
                },
                {
                  methods: ["post"],
                  operation_ids: ["createOrder"],
                  path: "/v1/orders",
                  source_paths: ["specs/openapi.yaml"]
                },
                {
                  methods: ["get"],
                  operation_ids: ["getOrder"],
                  path: "/v1/orders/{id}",
                  source_paths: ["specs/openapi.yaml"]
                }
              ],
              method_count: 3,
              source_paths: ["specs/openapi.yaml"]
            },
            consumer_repositories: [
              {
                consumer_kinds: ["hostname_reference_consumer"],
                matched_values: ["checkout.example.test"],
                repo_name: "support-dashboard",
                sample_paths: ["src/config/checkout.ts"]
              },
              {
                consumer_kinds: ["service_reference_consumer"],
                matched_values: ["checkout-api"],
                repo_name: "billing-worker",
                sample_paths: ["src/checkout.ts"]
              }
            ],
            dependencies: [
              {
                confidence: 0.93,
                evidence_count: 4,
                evidence_kinds: ["GITHUB_ACTIONS_REUSABLE_WORKFLOW"],
                rationale: "Reusable workflow owns deployment logic.",
                resolved_id: "resolved_workflow",
                target_name: "core-automation",
                type: "DEPLOYS_FROM"
              },
              {
                confidence: 0.99,
                evidence_count: 1,
                evidence_kinds: ["ARGOCD_APPLICATIONSET_DEPLOY_SOURCE"],
                rationale: "ApplicationSet points at deployment manifests.",
                resolved_id: "resolved_helm",
                target_name: "helm-charts",
                type: "DEPLOYS_FROM"
              }
            ],
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "helm",
                  relationship_type: "DEPLOYS_FROM",
                  source_location: {
                    path: "charts/platformcontextgraph/values.yaml",
                    repo_name: "helm-charts"
                  },
                  source_repo_name: "helm-charts",
                  target_repo_name: "checkout-api"
                },
                {
                  artifact_family: "terraform",
                  evidence_kind: "TERRAFORM_ECS_SERVICE",
                  path: "environments/prod/ecs.tf",
                  relationship_type: "PROVISIONS_DEPENDENCY_FOR",
                  resolved_id: "resolved_ecs",
                  source_repo_name: "terraform-runtime",
                  target_repo_name: "checkout-api"
                }
              ]
            },
            hostnames: [
              {
                environment: "prod",
                hostname: "checkout.example.test",
                relative_path: "config/production.json"
              }
            ],
            instances: [
              {
                environment: "prod",
                platforms: [
                  { platform_kind: "kubernetes", platform_name: "prod-eks" },
                  { platform_kind: "ecs", platform_name: "legacy-ecs" }
                ]
              },
              {
                environment: "qa",
                platforms: [{ platform_kind: "kubernetes", platform_name: "qa-eks" }]
              }
            ],
            kind: "service",
            name: "checkout-api",
            provisioning_source_chains: [
              {
                relationship_types: ["PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM"],
                repository: "terraform-runtime",
                sample_paths: ["environments/prod/ecs.tf", "shared/iam.tf"]
              }
            ],
            repo_name: "checkout-api"
          });
        }
        if (path.endsWith("/api/v0/services/mobius-tools/context")) {
          return Response.json({
            api_surface: { endpoint_count: 0, endpoints: [], method_count: 0 },
            deployment_evidence: {
              artifacts: [
                {
                  artifact_family: "github_actions",
                  relationship_type: "DEPLOYS_FROM",
                  source_repo_name: "mobius-tools",
                  target_repo_name: "automation"
                }
              ]
            }
          });
        }
        if (path.endsWith("/api/v0/services/hapi-phraseapp/context")) {
          return Response.json({
            api_surface: { endpoint_count: 0, endpoints: [], method_count: 0 },
            consumer_repositories: Array.from({ length: 20 }, (_, index) => ({
              consumer_kinds: ["service_reference_consumer"],
              repo_name: `consumer-${index}`,
              sample_paths: [`services/${index}.yaml`]
            })),
            dependencies: Array.from({ length: 8 }, (_, index) => ({
              rationale: "Config reference only.",
              target_name: `dependency-${index}`,
              type: "READS_CONFIG_FROM"
            })),
            kind: "service",
            name: "hapi-phraseapp",
            repo_name: "hapi-phraseapp"
          });
        }
        return Response.json({
          queue: { outstanding: 0, succeeded: 201 },
          repository_count: 23,
          status: "healthy"
        });
      })
    );

    render(<DashboardPage />);

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: /index status healthy/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect graph repositories 23/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect catalog repositories 3/i })
    ).toBeInTheDocument();
    expect(screen.getAllByText("23").length).toBeGreaterThan(0);
    expect(screen.getByText("Runtime dossier")).toBeInTheDocument();
    expect(screen.getByText(/23 repositories indexed by graph status/i)).toBeInTheDocument();
    expect(screen.getByText("Queue ledger")).toBeInTheDocument();
    expect(await screen.findByText("Deployment relationship graph")).toBeInTheDocument();
    expect(screen.getByLabelText("Run readiness")).toBeInTheDocument();
    expect(screen.getByText("Relationship coverage")).toBeInTheDocument();
    expect(screen.getByText("Evidence dossier")).toBeInTheDocument();
    expect(screen.getAllByText("Observed").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Missing").length).toBeGreaterThan(0);
    expect(screen.getByText("Canonical verbs")).toBeInTheDocument();
    expect(screen.getByText("Runtime topology")).toBeInTheDocument();
    expect(screen.getAllByText("DISCOVERS_CONFIG_IN").length).toBeGreaterThan(0);
    expect(screen.getAllByText("DEPLOYS_FROM").length).toBeGreaterThan(0);
    expect(screen.getAllByText("RUNS_ON").length).toBeGreaterThan(0);
    expect(screen.getAllByText("READS_CONFIG_FROM").length).toBeGreaterThan(0);
    expect(screen.getByText("PROVISIONS_PLATFORM")).toBeInTheDocument();
    expect(screen.getByText("DEPLOYMENT_SOURCE")).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { level: 1, name: "checkout-api" })
    ).toBeInTheDocument();
    expect(screen.getAllByText("3 endpoints").length).toBeGreaterThan(0);
    expect(screen.getAllByText("/v1/orders").length).toBeGreaterThan(0);
    expect(screen.getByText("/v1/orders/{id}")).toBeInTheDocument();
    expect(screen.getAllByText("Kubernetes").length).toBeGreaterThan(0);
    expect(screen.getAllByText("ECS").length).toBeGreaterThan(0);
    expect(screen.getAllByText("terraform-runtime").length).toBeGreaterThan(0);
    expect(screen.getAllByText("support-dashboard").length).toBeGreaterThan(0);
    expect(screen.getAllByText("billing-worker").length).toBeGreaterThan(0);

    const queueLedger = screen.getByLabelText("Queue ledger");
    fireEvent.click(within(queueLedger).getByRole("button", { name: /queue outstanding 0/i }));

    const detail = screen.getByLabelText("Runtime dossier");
    expect(
      within(detail).getByText(/No queued work is waiting on reducers or projectors/)
    ).toBeInTheDocument();
  });

  it("shows degraded projection separately from catalog availability", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = new URL(new Request(input).url).pathname;
        if (path.endsWith("/api/v0/repositories")) {
          return Response.json({
            repositories: [
              { id: "repository:r_1", name: "boats-chatgpt-app" },
              { id: "repository:r_2", name: "iac-eks-pcg" }
            ]
          });
        }
        if (path.includes("/story") || path.includes("/context")) {
          return Response.json({});
        }
        return Response.json({
          queue: { dead_letter: 4, in_flight: 1, outstanding: 1, succeeded: 209 },
          reasons: ["4 work items are dead-lettered"],
          repository_count: 0,
          status: "degraded"
        });
      })
    );

    render(<DashboardPage />);

    expect(
      await screen.findByRole("button", { name: /index status degraded/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect graph repositories 0/i })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /inspect catalog repositories 2/i })
    ).toBeInTheDocument();

    const queueLedger = screen.getByLabelText("Queue ledger");
    fireEvent.click(within(queueLedger).getByRole("button", { name: /dead letters 4/i }));

    const detail = screen.getByLabelText("Runtime dossier");
    expect(within(detail).getByText("4 dead-lettered work item(s).")).toBeInTheDocument();
  });

  it("shows the local API failure reason when loading fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("blocked by test");
      })
    );

    render(<DashboardPage />);

    expect(
      await screen.findByText("Local Eshu API unavailable: blocked by test.")
    ).toBeInTheDocument();
  });
});
