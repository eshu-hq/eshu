import { vi } from "vitest";

import { EshuApiClient } from "./client";
import { resolveEntity } from "./entityResolution";

describe("resolveEntity", () => {
  it("normalizes bounded resolver envelope candidates", async () => {
    const fetcher = vi.fn(async () =>
      Response.json({
        data: {
          count: 2,
          entities: [
            {
              entity_id: "workload:catalog-api",
              labels: ["Workload"],
              name: "catalog-api"
            },
            {
              file_path: "services/catalog-api/main.tf",
              id: "repo:terraform-stack-node10",
              labels: ["Repository"],
              name: "terraform-stack-node10",
              repo_id: "repository:terraform-stack-node10"
            }
          ],
          limit: 1,
          truncated: true
        },
        error: null,
        truth: {
          basis: "hybrid_graph_and_content",
          capability: "code_search.fuzzy_symbol",
          freshness: { state: "fresh" },
          level: "derived",
          profile: "local_authoritative"
        }
      })
    );

    const result = await resolveEntity({
      client: new EshuApiClient({ baseUrl: "/eshu-api/", fetcher }),
      limit: 1,
      name: "catalog-api",
      type: "repository"
    });

    expect(fetcher).toHaveBeenCalledWith(
      "http://localhost:5174/eshu-api/api/v0/entities/resolve",
      expect.objectContaining({
        body: JSON.stringify({
          limit: 1,
          name: "catalog-api",
          type: "repository"
        }),
        method: "POST"
      })
    );
    expect(result).toEqual({
      candidates: [
        {
          filePath: "",
          id: "workload:catalog-api",
          labels: ["Workload"],
          name: "catalog-api",
          repoId: "",
          repoName: "",
          type: "Workload"
        },
        {
          filePath: "services/catalog-api/main.tf",
          id: "repo:terraform-stack-node10",
          labels: ["Repository"],
          name: "terraform-stack-node10",
          repoId: "repository:terraform-stack-node10",
          repoName: "",
          type: "Repository"
        }
      ],
      count: 2,
      limit: 1,
      truncated: true
    });
  });
});
