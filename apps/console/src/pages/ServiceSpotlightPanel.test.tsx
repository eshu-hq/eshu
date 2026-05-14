import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { vi } from "vitest";
import { ServiceSpotlightPanel } from "./ServiceSpotlightPanel";
import type { ServiceSpotlight } from "../api/serviceSpotlight";

describe("ServiceSpotlightPanel", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", emptyCodeTopicFetch());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("presents deployment lanes before raw evidence details", async () => {
    render(<ServiceSpotlightPanel spotlight={spotlight} />);

    expect(screen.getByRole("heading", { name: "api-node-boats" })).toBeInTheDocument();
    expect(screen.getAllByText("38 endpoints").length).toBeGreaterThan(0);
    expect(screen.getByText("44 methods")).toBeInTheDocument();
    expect(screen.getAllByText("2 lanes").length).toBeGreaterThan(0);
    expect(screen.getByText("25 references")).toBeInTheDocument();
    expect(screen.getByText("17 typed dependents")).toBeInTheDocument();
    expect(screen.getAllByText(/Dual deployment/i).length).toBeGreaterThan(0);
    expect(screen.getByRole("heading", { name: "Investigation coverage" })).toBeInTheDocument();
    expect(screen.getByText("Partial")).toBeInTheDocument();
    expect(screen.getByText("26 with evidence of 26 checked")).toBeInTheDocument();
    expect(screen.getAllByText("API Surface").length).toBeGreaterThan(0);
    expect(screen.getByText("38 endpoint(s) across 0 spec file(s)")).toBeInTheDocument();
    expect(screen.getByText("Service story")).toBeInTheDocument();
    expect(screen.getByText("retrieve the full one-call dossier")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Traffic path" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "api-node-boats traffic path" })).toBeInTheDocument();
    expect(screen.getAllByText("CloudFront distribution").length).toBeGreaterThan(1);
    expect(screen.getAllByText("origin-alb-primary").length).toBeGreaterThan(1);
    expect(screen.getAllByText("prod").length).toBeGreaterThan(1);
    expect(screen.getByText("CloudFront distribution E123")).toBeInTheDocument();

    const laneMap = screen.getByLabelText("api-node-boats deployment lane map");
    expect(within(laneMap).getByText("api-node-boats")).toBeInTheDocument();
    expect(within(laneMap).getByRole("button", { name: /Kubernetes lane/i })).toBeInTheDocument();
    expect(within(laneMap).getByRole("button", { name: /ECS lane/i })).toBeInTheDocument();

    expect(screen.getAllByText("bg-dev, bg-prod, bg-qa, ops-prod, ops-qa").length).toBeGreaterThan(0);
    expect(screen.getAllByText("7 items").length).toBeGreaterThan(0);
    expect(screen.getAllByText("DEPLOYS_FROM").length).toBeGreaterThan(0);
    expect(screen.queryByText("Lane evidence")).not.toBeInTheDocument();

    fireEvent.click(within(laneMap).getByRole("button", { name: /ECS lane/i }));

    expect(screen.getAllByText("bg-dev, bg-prod, bg-qa").length).toBeGreaterThan(0);
    expect(screen.getAllByText("57 items").length).toBeGreaterThan(0);
    expect(screen.getAllByText("PROVISIONS_DEPENDENCY_FOR").length).toBeGreaterThan(0);
    expect(screen.getAllByText("READS_CONFIG_FROM").length).toBeGreaterThan(0);
    expect(screen.getAllByText("terraform-stack-node10").length).toBeGreaterThan(0);

    expect(screen.getByRole("heading", { name: "What deploys or provisions it" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Repos that mention it" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Typed dependents" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Upstream relationships" })).toBeInTheDocument();
    expect(screen.getByText("25 observed")).toBeInTheDocument();
    expect(screen.getByText("17 observed")).toBeInTheDocument();
    expect(screen.getAllByText("api-node-boats.prod.bgrp.io").length).toBeGreaterThan(1);

    const search = screen.getByRole("searchbox", { name: "Search API endpoints" });
    fireEvent.change(search, { target: { value: "listing" } });
    expect(screen.getByText("/getListing")).toBeInTheDocument();
    expect(screen.queryByText("/_version")).not.toBeInTheDocument();
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
  });

  it("adds bounded code-topic evidence when the MCP contract returns topic results", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json({
          data: {
            coverage: {
              empty: false,
              limit: 6,
              returned_count: 3,
              searched_terms: ["api", "handlers", "dependencies"],
              truncated: false
            },
            evidence_groups: [
              {
                language: "json",
                matched_terms: ["api", "dependencies"],
                rank: 1,
                recommended_next_calls: [
                  {
                    args: {
                      relative_path: "package-lock.json",
                      repo_id: "api-node-boats"
                    },
                    tool: "get_file_lines"
                  }
                ],
                relative_path: "package-lock.json",
                score: 99,
                source_kind: "file"
              },
              {
                entity_name: "getListing",
                entity_type: "Function",
                language: "typescript",
                matched_terms: ["api", "handlers"],
                rank: 2,
                recommended_next_calls: [
                  {
                    args: {
                      end_line: 44,
                      relative_path: "server/handlers/listing.ts",
                      repo_id: "api-node-boats",
                      start_line: 12
                    },
                    tool: "get_file_lines"
                  },
                  {
                    args: {
                      direction: "both",
                      entity_id: "content-entity:e_listing",
                      limit: 25,
                      repo_id: "api-node-boats"
                    },
                    tool: "get_code_relationship_story"
                  }
                ],
                relative_path: "server/handlers/listing.ts",
                score: 32,
                source_kind: "symbol"
              },
              {
                language: "typescript",
                matched_terms: ["api", "handlers"],
                rank: 3,
                recommended_next_calls: [],
                relative_path: "server/routes/listing.ts",
                score: 18,
                source_kind: "file"
              }
            ],
            matched_files: [
              {
                language: "json",
                relative_path: "package-lock.json"
              },
              {
                language: "typescript",
                relative_path: "server/handlers/listing.ts"
              }
            ],
            matched_symbols: [
              {
                entity_name: "getListing",
                entity_type: "Function",
                language: "typescript",
                rank: 1,
                relative_path: "server/handlers/listing.ts"
              }
            ],
            recommended_next_calls: [
              {
                args: {
                  relative_path: "package-lock.json",
                  repo_id: "api-node-boats"
                },
                tool: "get_file_lines"
              }
            ],
            topic: "api-node-boats API handlers"
          },
          error: null,
          truth: {
            basis: "content_index",
            capability: "code_search.topic_investigation",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "production"
          }
        })
      )
    );

    render(<ServiceSpotlightPanel spotlight={spotlight} />);

    expect(await screen.findByRole("heading", { name: "Code paths Eshu found" })).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getAllByText("getListing").length).toBeGreaterThan(0);
    expect(screen.getAllByText("server/handlers/listing.ts").length).toBeGreaterThan(0);
    expect(screen.queryByText("package-lock.json")).not.toBeInTheDocument();
    expect(screen.getByText("Source lines")).toBeInTheDocument();
    expect(screen.getByText("Code relationship story")).toBeInTheDocument();
  });
});

function emptyCodeTopicFetch(): ReturnType<typeof vi.fn> {
  return vi.fn(async () =>
    Response.json({
      data: {
        coverage: {
          empty: true,
          limit: 6,
          returned_count: 0,
          searched_terms: [],
          truncated: false
        },
        evidence_groups: [],
        matched_files: [],
        matched_symbols: [],
        recommended_next_calls: [],
        topic: "empty"
      },
      error: null,
      truth: {
        basis: "content_index",
        capability: "code_search.topic_investigation",
        freshness: { state: "fresh" },
        level: "derived",
        profile: "production"
      }
    })
  );
}

const spotlight: ServiceSpotlight = {
  api: {
    endpointCount: 38,
    endpoints: [
      {
        methods: ["get"],
        operationIds: ["getListing"],
        path: "/getListing",
        sourcePaths: ["specs/index.yaml"]
      },
      {
        methods: ["get"],
        operationIds: ["getVersion"],
        path: "/_version",
        sourcePaths: ["catalog-specs.yaml"]
      }
    ],
    methodCount: 44,
    sourcePaths: ["catalog-specs.yaml", "specs/index.yaml"]
  },
  consumers: [
    {
      consumerKinds: ["service_reference_consumer"],
      matchedValues: ["api-node-boats"],
      relationshipTypes: [],
      repository: "terraform-stack-node10",
      samplePaths: ["environments/bg-prod/ecs.tf"]
    }
  ],
  graphDependents: [
    {
      consumerKinds: ["graph_provisioning_consumer"],
      matchedValues: ["api-node-boats"],
      relationshipTypes: ["DEPLOYS_FROM"],
      repository: "iac-eks-argocd",
      samplePaths: []
    }
  ],
  dependencies: [
    {
      evidenceCount: 4,
      rationale: "Reusable workflow owns deployment logic.",
      targetName: "core-engineering-automation",
      type: "DEPLOYS_FROM"
    }
  ],
  deploymentGraph: { links: [], nodes: [] },
  lanes: [
    {
      environments: ["bg-dev", "bg-prod", "bg-qa", "ops-prod", "ops-qa"],
      evidenceCount: 7,
      label: "Kubernetes",
      relationshipTypes: ["DEPLOYS_FROM"],
      sourceRepos: ["api-node-boats", "iac-eks-argocd", "helm-charts"]
    },
    {
      environments: ["bg-dev", "bg-prod", "bg-qa"],
      evidenceCount: 57,
      label: "ECS",
      relationshipTypes: ["PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM"],
      sourceRepos: ["terraform-stack-node10", "terraform-stack-helm"]
    }
  ],
  name: "api-node-boats",
  hostnames: [
    {
      environment: "prod",
      hostname: "api-node-boats.prod.bgrp.io",
      path: "config/production.json"
    }
  ],
  trafficPaths: [
    {
      edge: "CloudFront distribution",
      environment: "prod",
      evidenceKind: "aws_cloudfront_distribution",
      hostname: "api-node-boats.prod.bgrp.io",
      origin: "origin-alb-primary",
      reason: "CloudFront distribution E123",
      runtime: "ECS bg-prod",
      sourceRepo: "terraform-stack-node10",
      visibility: "public",
      workload: "api-node-boats"
    }
  ],
  relationshipCounts: {
    downstream: 42,
    graphDependents: 17,
    references: 25,
    upstream: 35
  },
  repoName: "api-node-boats",
  investigation: {
    coverage: {
      reason: "evidence was found, but Eshu cannot prove exhaustive coverage",
      repositoryCount: 26,
      repositoriesWithEvidence: 26,
      state: "partial",
      truncated: false
    },
    evidenceFamilies: ["api_surface", "deployment_lanes", "downstream_consumers"],
    findings: [
      {
        family: "api_surface",
        path: "api_surface",
        summary: "38 endpoint(s) across 0 spec file(s)"
      }
    ],
    nextCalls: [
      {
        arguments: { workload_id: "api-node-boats" },
        reason: "retrieve the full one-call dossier",
        tool: "get_service_story"
      }
    ],
    repositories: [
      {
        evidenceFamilies: ["api_surface", "deployment_lanes"],
        name: "api-node-boats",
        roles: ["service_owner"]
      }
    ]
  },
  summary: "api-node-boats exposes 38 endpoint(s), runs through 2 deployment lane(s), has 35 upstream relationship(s), and 42 downstream relationship(s)."
};
