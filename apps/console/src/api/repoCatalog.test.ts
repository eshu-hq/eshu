import { describe, expect, it } from "vitest";
import { loadRepositories, loadRepositoryDetail, loadRepositoryNameMap } from "./repoCatalog";
import type { EshuApiClient } from "./client";

describe("repoCatalog", () => {
  it("maps the repository list and drops entries without an id", async () => {
    const client = {
      get: async () => ({
        data: { repositories: [
          {
            id: "repo-1",
            name: "checkout",
            repo_slug: "org/checkout",
            is_dependency: false,
            group_key: "Checkout",
            group_source: "repo_slug_namespace",
            group_truth: "derived",
            group_kind: "source",
            group_reason: "derived from repository slug namespace"
          },
          { name: "", id: "" }
        ] }, error: null, truth: null
      })
    } as unknown as EshuApiClient;
    const repos = await loadRepositories(client);
    expect(repos).toHaveLength(1);
    expect(repos[0]).toMatchObject({
      id: "repo-1",
      name: "checkout",
      repoSlug: "org/checkout",
      isDependency: false,
      groupKey: "Checkout",
      groupSource: "repo_slug_namespace",
      groupTruth: "derived",
      groupKind: "source",
      groupReason: "derived from repository slug namespace"
    });
  });

  it("pages through every offset until the API stops reporting more repositories", async () => {
    // 906-repo stack: API max page is 500, so a single fetch leaves 406 repos
    // invisible. The loader must page (offset 0 -> 500) until truncated is false.
    const total = 906;
    const pageLimit = 500;
    const wireRepos = Array.from({ length: total }, (_, index) => ({
      id: `repository:r_${index}`,
      name: `repo-${index}`,
      repo_slug: `org/repo-${index}`
    }));
    const requested: { limit: string | null; offset: string | null }[] = [];
    const client = {
      get: async (path: string) => {
        const url = new URL(path, "http://console.test");
        const limit = Number(url.searchParams.get("limit") ?? "0");
        const offset = Number(url.searchParams.get("offset") ?? "0");
        requested.push({ limit: url.searchParams.get("limit"), offset: url.searchParams.get("offset") });
        const page = wireRepos.slice(offset, offset + limit);
        return {
          data: { repositories: page, count: page.length, limit, offset, truncated: offset + limit < total },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    const repos = await loadRepositories(client);

    expect(repos).toHaveLength(total);
    expect(repos[0]?.id).toBe("repository:r_0");
    expect(repos[total - 1]?.id).toBe(`repository:r_${total - 1}`);
    expect(requested).toEqual([
      { limit: String(pageLimit), offset: "0" },
      { limit: String(pageLimit), offset: String(pageLimit) }
    ]);
  });

  it("terminates after REPOSITORY_MAX_PAGES calls when the API keeps reporting truncated:true", async () => {
    // Guard against an API that never stops claiming more pages: the loop must
    // exit after the page ceiling and must not silently return a clean result —
    // a console.warn must fire to surface the incomplete list.
    let calls = 0;
    const warnMessages: string[] = [];
    const originalWarn = console.warn;
    console.warn = (...args: unknown[]) => { warnMessages.push(String(args[0])); };
    const client = {
      get: async (path: string) => {
        calls += 1;
        const url = new URL(path, "http://console.test");
        const offset = Number(url.searchParams.get("offset") ?? "0");
        const limit = Number(url.searchParams.get("limit") ?? "0");
        const page = Array.from({ length: limit }, (_, i) => ({ id: `repository:r_${offset + i}`, name: `repo-${offset + i}` }));
        return { data: { repositories: page, truncated: true, offset }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;
    try {
      const repos = await loadRepositories(client);
      // Must terminate (not hang) and must warn about incompleteness
      expect(calls).toBeLessThanOrEqual(24);
      expect(warnMessages.some((m) => m.includes("incomplete"))).toBe(true);
      // Repos returned should equal calls × page limit (no duplicates from stalled offset)
      expect(repos.length).toBe(calls * 500);
    } finally {
      console.warn = originalWarn;
    }
  });

  it("stops immediately and returns zero repos when the API returns an empty page with truncated:true", async () => {
    // An empty page with truncated:true is contradictory; the loader must not
    // loop forever — empty wire data is always terminal.
    let calls = 0;
    const client = {
      get: async () => {
        calls += 1;
        return { data: { repositories: [], truncated: true }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    const repos = await loadRepositories(client);

    expect(repos).toHaveLength(0);
    expect(calls).toBe(1);
  });

  it("stops paging when the response echoes a non-advancing offset (server-side clamp)", async () => {
    // The server clamps offset at 10000 (repositoryListMaxOffset). If the
    // echoed offset stops advancing, the loader would re-fetch the same page
    // indefinitely and accumulate duplicates. Break on offset stall.
    let calls = 0;
    const stalledOffset = 10000;
    const client = {
      get: async (path: string) => {
        calls += 1;
        const url = new URL(path, "http://console.test");
        const requestedOffset = Number(url.searchParams.get("offset") ?? "0");
        // Server clamps: once requested offset exceeds stalledOffset the
        // echoed offset stays at stalledOffset no matter what we request.
        const echoedOffset = Math.min(requestedOffset, stalledOffset);
        const page = Array.from({ length: 500 }, (_, i) => ({ id: `repository:r_${echoedOffset + i}`, name: `repo-${echoedOffset + i}` }));
        return { data: { repositories: page, truncated: true, offset: echoedOffset }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    const repos = await loadRepositories(client);

    // Loader must stop when it detects offset did not advance, not spin
    // until MAX_PAGES appending duplicates.
    const uniqueIds = new Set(repos.map((r) => r.id));
    expect(uniqueIds.size).toBe(repos.length); // no duplicates
    // stall is detected on the second call at the clamped offset
    expect(calls).toBeLessThanOrEqual(22); // 10000/500 = 20 advancing pages + stall detection
  });

  it("stops paging when a short final page returns fewer rows than the page limit", async () => {
    // truncated is the authoritative paging signal, but a short page (fewer than
    // limit rows) is also a terminal page; the loader must not request again.
    const wireRepos = Array.from({ length: 120 }, (_, index) => ({ id: `repository:r_${index}`, name: `repo-${index}` }));
    let calls = 0;
    const client = {
      get: async (path: string) => {
        calls += 1;
        const url = new URL(path, "http://console.test");
        const limit = Number(url.searchParams.get("limit") ?? "0");
        const offset = Number(url.searchParams.get("offset") ?? "0");
        const page = wireRepos.slice(offset, offset + limit);
        // The fixture API omits truncated here; the short page is the stop signal.
        return { data: { repositories: page }, error: null, truth: null };
      }
    } as unknown as EshuApiClient;

    const repos = await loadRepositories(client);

    expect(repos).toHaveLength(120);
    expect(calls).toBe(1);
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

  it("uses the repository slug leaf instead of an opaque id when name is missing", async () => {
    const client = {
      get: async () => ({
        data: { repositories: [
          { id: "repository:r_078043f1", repo_slug: "platform/api-node-platform" },
          { id: "repository:r_dd626fe7", name: "repository:r_dd626fe7", repo_slug: "platform/iac-eks-argocd" }
        ] }, error: null, truth: null
      })
    } as unknown as EshuApiClient;

    const repos = await loadRepositories(client);

    expect(repos.map((repo) => repo.name)).toEqual(["api-node-platform", "iac-eks-argocd"]);
  });

  it("propagates repository list error envelopes instead of returning no repositories", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "repository list unavailable",
          capability: "repository.list"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadRepositories(client)).rejects.toThrow("unsupported_runtime_profile");
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

  it("returns an unavailable detail when stats returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "repository stats unavailable",
          capability: "repository.stats"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    const detail = await loadRepositoryDetail(client, "repo-1");

    expect(detail.provenance).toBe("unavailable");
    expect(detail.name).toBe("repo-1");
    expect(detail.stats.fileCount).toBeNull();
  });
});
