import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { ServiceChangeSurfacePanel } from "./ServiceChangeSurfacePanel";
import type { ServiceSpotlight } from "../api/serviceSpotlight";

describe("ServiceChangeSurfacePanel", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders clickable direct and transitive impact from the change-surface contract", async () => {
    const fetcher = vi.fn(async () =>
      Response.json({
        data: {
          code_surface: {
            changed_files: [
              {
                relative_path: "server/routes/leads.ts",
                repo_id: "api-node-boats"
              }
            ],
            coverage: {
              limit: 16,
              query_shape: "content_topic_and_changed_path_surface",
              returned_symbols: 1,
              truncated: false
            },
            evidence_groups: [
              {
                entity_name: "postLead",
                entity_type: "Function",
                language: "typescript",
                matched_terms: ["lead", "queue"],
                relative_path: "server/routes/leads.ts",
                source_kind: "symbol"
              }
            ],
            matched_file_count: 1,
            source_backends: ["postgres_content_store"],
            symbol_count: 1,
            touched_symbols: [
              {
                entity_id: "entity:post-lead",
                entity_name: "postLead",
                entity_type: "Function",
                language: "typescript",
                relative_path: "server/routes/leads.ts"
              }
            ],
            topic: "api-node-boats deployment and lead flow"
          },
          coverage: {
            direct_count: 2,
            limit: 16,
            max_depth: 4,
            query_shape: "resolved_change_surface_traversal",
            transitive_count: 1,
            truncated: false
          },
          direct_impact: [
            {
              depth: 1,
              id: "workload:api-node-communicator",
              labels: ["Workload"],
              name: "api-node-communicator",
              repo_id: "api-node-communicator"
            },
            {
              depth: 1,
              id: "workload:api-node-spam-fraud",
              labels: ["Workload"],
              name: "api-node-spam-fraud",
              repo_id: "api-node-spam-fraud"
            }
          ],
          impact_summary: {
            direct_count: 2,
            total_count: 3,
            transitive_count: 1
          },
          recommended_next_calls: [
            {
              args: {
                entity_id: "entity:post-lead",
                limit: 16
              },
              tool: "get_code_relationship_story"
            }
          ],
          scope: {
            limit: 16,
            max_depth: 4,
            repo_id: "api-node-boats",
            target: "api-node-boats",
            target_type: "service",
            topic: "api-node-boats deployment and lead flow"
          },
          target_resolution: {
            input: "api-node-boats",
            selected: {
              id: "workload:api-node-boats",
              labels: ["Workload"],
              name: "api-node-boats"
            },
            status: "resolved",
            target_type: "service",
            truncated: false
          },
          transitive_impact: [
            {
              depth: 2,
              id: "repo:terraform-stack-node10",
              labels: ["Repository"],
              name: "terraform-stack-node10",
              repo_id: "terraform-stack-node10"
            }
          ]
        },
        error: null,
        truth: {
          basis: "hybrid_graph_and_content",
          capability: "platform_impact.change_surface",
          freshness: { state: "fresh" },
          level: "derived",
          profile: "local_authoritative"
        }
      })
    );
    vi.stubGlobal(
      "fetch",
      fetcher
    );

    render(<ServiceChangeSurfacePanel spotlight={spotlight} />);

    expect(fetcher).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Run scoped impact review" }));

    expect(await screen.findByRole("heading", { name: "Impact review" })).toBeInTheDocument();
    expect(screen.getByText("3 total")).toBeInTheDocument();
    expect(screen.getByText("2 direct")).toBeInTheDocument();
    expect(screen.getByText("1 deeper")).toBeInTheDocument();
    expect(screen.getByText("postLead")).toBeInTheDocument();
    expect(screen.getByText("server/routes/leads.ts")).toBeInTheDocument();

    const graph = screen.getByRole("img", { name: "api-node-boats change surface" });
    expect(within(graph).getByText("api-node-spam-fraud")).toBeInTheDocument();
    expect(within(graph).getByText("terraform-stack-node10")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /terraform-stack-node10/i }));

    expect(screen.getByText("Depth 2")).toBeInTheDocument();
    expect(screen.getByText("Repository")).toBeInTheDocument();
    expect(screen.getByText("Code relationship story")).toBeInTheDocument();
  });
});

const spotlight: ServiceSpotlight = {
  api: {
    endpointCount: 38,
    endpoints: [],
    methodCount: 44,
    sourcePaths: []
  },
  consumers: [],
  dependencies: [],
  deploymentGraph: { links: [], nodes: [] },
  graphDependents: [],
  hostnames: [],
  investigation: {
    coverage: {
      reason: "",
      repositoryCount: 0,
      repositoriesWithEvidence: 0,
      state: "",
      truncated: false
    },
    evidenceFamilies: [],
    findings: [],
    nextCalls: [],
    repositories: []
  },
  lanes: [],
  name: "api-node-boats",
  relationshipCounts: {
    downstream: 0,
    graphDependents: 0,
    references: 0,
    upstream: 0
  },
  repoName: "api-node-boats",
  summary: "api-node-boats service"
};
