import type { EshuApiClient } from "./client";
import { loadSourceBackedSuggestedQuestions } from "./suggestedQuestions";

describe("loadSourceBackedSuggestedQuestions", () => {
  it("does not call repo-scoped graph or changed-since endpoints without a repository scope", async () => {
    const calls: string[] = [];
    const client = clientFor((method, path) => {
      calls.push(`${method} ${path}`);
      if (path.startsWith("/api/v0/repositories")) {
        return { data: { repositories: [] }, error: null, truth: null };
      }
      if (path.includes("severity=critical") || path.includes("severity=high")) {
        return { data: { findings: [] }, error: null, truth: null };
      }
      throw new Error(`unexpected ${path}`);
    });

    const questions = await loadSourceBackedSuggestedQuestions(client);

    expect(questions).toEqual([]);
    expect(calls).toHaveLength(3);
    expect(calls).toEqual(
      expect.arrayContaining([
        "GET /api/v0/repositories?limit=500&offset=0",
        "GET /api/v0/supply-chain/impact/findings?limit=5&severity=critical",
        "GET /api/v0/supply-chain/impact/findings?limit=5&severity=high",
      ]),
    );
  });

  it("requires a prior generation before loading changed-since", async () => {
    const calls: string[] = [];
    const client = clientFor((method, path, body) => {
      calls.push(`${method} ${path}`);
      if (path.startsWith("/api/v0/repositories")) {
        return {
          data: { repositories: [{ id: "repository:r1", name: "checkout-api" }] },
          error: null,
          truth: null,
        };
      }
      if (path === "/api/v0/ecosystem/graph-summary") {
        expect(body).toEqual({ limit: 3, repo_id: "repository:r1" });
        return { data: { hot_entities: [] }, error: null, truth: null };
      }
      if (path.startsWith("/api/v0/freshness/generations")) {
        return {
          data: {
            generations: [{ generation_id: "gen-current", is_active: true, status: "active" }],
          },
          error: null,
          truth: null,
        };
      }
      if (path.includes("severity=critical") || path.includes("severity=high")) {
        return { data: { findings: [] }, error: null, truth: null };
      }
      throw new Error(`unexpected ${path}`);
    });

    await loadSourceBackedSuggestedQuestions(client);

    expect(calls.some((path) => path.includes("/api/v0/freshness/changed-since"))).toBe(false);
  });

  it("does not guess a changed-since baseline when the active generation is unknown", async () => {
    const calls: string[] = [];
    const client = clientFor((method, path, body) => {
      calls.push(`${method} ${path}`);
      if (path.startsWith("/api/v0/repositories")) {
        return {
          data: { repositories: [{ id: "repository:r1", name: "checkout-api" }] },
          error: null,
          truth: null,
        };
      }
      if (path === "/api/v0/ecosystem/graph-summary") {
        expect(body).toEqual({ limit: 3, repo_id: "repository:r1" });
        return { data: { hot_entities: [] }, error: null, truth: null };
      }
      if (path.startsWith("/api/v0/freshness/generations")) {
        return {
          data: {
            generations: [
              { generation_id: "gen-a", is_active: false, status: "completed" },
              { generation_id: "gen-b", is_active: false, status: "superseded" },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.includes("severity=critical") || path.includes("severity=high")) {
        return { data: { findings: [] }, error: null, truth: null };
      }
      throw new Error(`unexpected ${path}`);
    });

    await loadSourceBackedSuggestedQuestions(client);

    expect(calls.some((path) => path.includes("/api/v0/freshness/changed-since"))).toBe(false);
  });

  it("turns bounded graph summary, changed-since, and high findings into route targets", async () => {
    const client = clientFor((_method, path) => {
      if (path.startsWith("/api/v0/repositories")) {
        return {
          data: { repositories: [{ id: "repository:r1", name: "checkout-api" }] },
          error: null,
          truth: null,
        };
      }
      if (path === "/api/v0/ecosystem/graph-summary") {
        return {
          data: {
            hot_entities: [
              {
                file_path: "src/router.ts",
                function_id: "content-entity:routeCheckout",
                function_name: "routeCheckout",
                incoming_calls: 2,
                outgoing_calls: 5,
                total_degree: 7,
              },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.startsWith("/api/v0/freshness/generations")) {
        return {
          data: {
            generations: [
              { generation_id: "gen-current", is_active: true, status: "active" },
              { generation_id: "gen-prior", is_active: false, status: "superseded" },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.startsWith("/api/v0/freshness/changed-since")) {
        return {
          data: {
            categories: [
              { name: "facts", counts: { added: 2, retired: 1, unchanged: 10, updated: 3 } },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.includes("severity=critical")) {
        return { data: { findings: [] }, error: null, truth: null };
      }
      if (path.includes("severity=high")) {
        return {
          data: {
            findings: [
              {
                advisory_id: "CVE-2026-4321",
                cvss_score: 8.4,
                package_name: "openssl",
                service_ids: ["workload:checkout-api"],
                severity: "high",
              },
            ],
          },
          error: null,
          truth: null,
        };
      }
      throw new Error(`unexpected ${path}`);
    });

    const questions = await loadSourceBackedSuggestedQuestions(client);

    expect(questions.map((question) => question.href)).toEqual([
      "/explorer?q=routeCheckout",
      "/changed-since?mode=repository&repository=repository%3Ar1&since_generation_id=gen-prior",
      "/vulnerabilities/CVE-2026-4321",
    ]);
    expect(questions.map((question) => question.source)).toEqual([
      "POST /api/v0/ecosystem/graph-summary",
      "GET /api/v0/freshness/generations -> GET /api/v0/freshness/changed-since",
      "GET /api/v0/supply-chain/impact/findings",
    ]);
  });
});

function clientFor(
  respond: (method: "GET" | "POST", path: string, body?: unknown) => unknown,
): EshuApiClient {
  return {
    get: async (path: string) => respond("GET", path),
    post: async (path: string, body: unknown) => respond("POST", path, body),
  } as unknown as EshuApiClient;
}
