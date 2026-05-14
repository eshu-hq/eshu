import { EshuApiClient } from "./client";
import {
  loadServiceChangeSurface,
  normalizeChangeSurfaceInvestigation
} from "./changeSurface";

describe("change-surface investigation adapter", () => {
  it("posts a bounded service-scoped investigation request", async () => {
    const calls: Request[] = [];
    const client = new EshuApiClient({
      baseUrl: "http://localhost:8080",
      fetcher: async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        const request = new Request(input, init);
        calls.push(request);
        return Response.json({
          data: changeSurfacePayload(),
          error: null,
          truth: {
            basis: "hybrid_graph_and_content",
            capability: "platform_impact.change_surface",
            freshness: { state: "fresh" },
            level: "derived",
            profile: "local_authoritative"
          }
        });
      }
    });

    const result = await loadServiceChangeSurface({
      client,
      repoName: "api-node-boats",
      serviceName: "api-node-boats"
    });

    expect(new URL(calls[0]?.url ?? "").pathname).toBe(
      "/api/v0/impact/change-surface/investigate"
    );
    await expect(calls[0]?.json()).resolves.toMatchObject({
      limit: 16,
      max_depth: 4,
      repo_id: "api-node-boats",
      service_name: "api-node-boats"
    });
    expect(result.impact.totalCount).toBe(5);
    expect(result.directImpact.map((node) => node.name)).toEqual([
      "api-node-communicator",
      "api-node-spam-fraud"
    ]);
    expect(result.transitiveImpact.map((node) => node.depth)).toEqual([2, 3]);
    expect(result.codeSurface.symbols[0]?.name).toBe("postLead");
    expect(result.nextCalls.map((call) => call.tool)).toContain("find_change_surface");
  });

  it("marks the response empty only when no code or graph surface is present", () => {
    const empty = normalizeChangeSurfaceInvestigation({
      code_surface: {
        changed_files: [],
        evidence_groups: [],
        matched_file_count: 0,
        symbol_count: 0,
        touched_symbols: []
      },
      direct_impact: [],
      impact_summary: {
        direct_count: 0,
        total_count: 0,
        transitive_count: 0
      },
      target_resolution: {
        candidates: [],
        input: "",
        status: "not_requested",
        target_type: "",
        truncated: false
      },
      transitive_impact: []
    });

    expect(empty.empty).toBe(true);

    const contentOnly = normalizeChangeSurfaceInvestigation({
      code_surface: {
        changed_files: [{ relative_path: "server/routes/leads.ts", repo_id: "api-node-boats" }],
        evidence_groups: [],
        matched_file_count: 1,
        symbol_count: 0,
        touched_symbols: []
      },
      direct_impact: [],
      impact_summary: {
        direct_count: 0,
        total_count: 0,
        transitive_count: 0
      },
      target_resolution: {
        candidates: [],
        input: "",
        status: "not_requested",
        target_type: "",
        truncated: false
      },
      transitive_impact: []
    });

    expect(contentOnly.empty).toBe(false);
  });

  it("keeps unresolved graph targets visible so the UI can explain the gap", () => {
    const unresolved = normalizeChangeSurfaceInvestigation({
      code_surface: {
        changed_files: [],
        evidence_groups: [],
        matched_file_count: 0,
        symbol_count: 0,
        touched_symbols: []
      },
      direct_impact: [],
      impact_summary: {
        direct_count: 0,
        total_count: 0,
        transitive_count: 0
      },
      recommended_next_calls: [
        {
          args: { repo_id: "api-node-boats", topic: "api-node-boats routes" },
          tool: "investigate_code_topic"
        }
      ],
      target_resolution: {
        candidates: [],
        input: "api-node-boats",
        status: "no_match",
        target_type: "service",
        truncated: false
      },
      transitive_impact: []
    });

    expect(unresolved.empty).toBe(false);
    expect(unresolved.resolution.status).toBe("no_match");
    expect(unresolved.nextCalls[0]?.tool).toBe("investigate_code_topic");
  });
});

function changeSurfacePayload(): Record<string, unknown> {
  return {
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
      transitive_count: 2,
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
      total_count: 5,
      transitive_count: 3
    },
    recommended_next_calls: [
      {
        args: { target: "workload:api-node-boats", limit: 16 },
        tool: "find_change_surface"
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
    source_backend: "hybrid_graph_and_content",
    target_resolution: {
      candidates: [],
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
      },
      {
        depth: 3,
        id: "resource:queue",
        labels: ["CloudResource"],
        name: "bm.leads.boats queue"
      }
    ],
    truncated: false
  };
}
