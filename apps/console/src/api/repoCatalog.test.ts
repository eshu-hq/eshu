import { describe, expect, it } from "vitest";
import { loadRepositories, loadRepositoryDetail, loadRepositoryNameMap } from "./repoCatalog";
import type { EshuApiClient } from "./client";

describe("repoCatalog", () => {
  it("maps the repository list and drops entries without an id", async () => {
    const client = {
      get: async () => ({
        data: { repositories: [
          { id: "repo-1", name: "checkout", repo_slug: "org/checkout", is_dependency: false },
          { name: "", id: "" }
        ] }, error: null, truth: null
      })
    } as unknown as EshuApiClient;
    const repos = await loadRepositories(client);
    expect(repos).toHaveLength(1);
    expect(repos[0]).toMatchObject({ id: "repo-1", name: "checkout", repoSlug: "org/checkout", isDependency: false });
  });

  it("builds a repository id to name map from the live repository list", async () => {
    const client = {
      get: async () => ({
        data: { repositories: [
          { id: "repository:r1", name: "api-node-platform" },
          { id: "repository:r2", name: "helm-charts" }
        ] }, error: null, truth: null
      })
    } as unknown as EshuApiClient;

    const names = await loadRepositoryNameMap(client);

    expect(names.get("repository:r1")).toBe("api-node-platform");
    expect(names.get("repository:r2")).toBe("helm-charts");
  });

  it("maps repo detail from stats + story, preserving null counts (no fabrication)", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/stats")) {
          return { data: { repository: { name: "checkout" }, file_count: 42, entity_count: null, languages: ["go"], entity_types: ["function"], coverage: { source_backend: "content_store" } }, error: null, truth: null };
        }
        return { data: { highlights: ["Primary service", { title: "Deploys to prod" }] }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
    const detail = await loadRepositoryDetail(client, "repo-1");
    expect(detail.name).toBe("checkout");
    expect(detail.stats.fileCount).toBe(42);
    expect(detail.stats.entityCount).toBeNull();
    expect(detail.stats.languages).toEqual(["go"]);
    expect(detail.highlights).toEqual(["Primary service", "Deploys to prod"]);
    expect(detail.provenance).toBe("live");
  });

  it("returns an unavailable detail when stats errors", async () => {
    const client = { get: async () => { throw new Error("401"); } } as unknown as EshuApiClient;
    const detail = await loadRepositoryDetail(client, "repo-1");
    expect(detail.provenance).toBe("unavailable");
    expect(detail.stats.fileCount).toBeNull();
  });
});
