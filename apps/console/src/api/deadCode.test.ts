import { describe, expect, it, vi } from "vitest";
import type { EshuApiClient } from "./client";
import { loadDeadCodePage } from "./deadCode";

function envelope(results: readonly Record<string, unknown>[], opts: { readonly truncated?: boolean } = {}) {
  return {
    data: {
      analysis: {
        dead_code_language_maturity: { typescript: "experimental" },
        modeled_public_api: false
      },
      limit: 100,
      results,
      truncated: opts.truncated === true
    },
    error: null,
    truth: {
      capability: "code_quality.dead_code",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production"
    }
  };
}

describe("deadCode", () => {
  it("posts a bounded scan request with optional repo and language filters", async () => {
    const post = vi.fn(async () => envelope([]));
    const client = { post } as unknown as EshuApiClient;

    await loadDeadCodePage(client, {
      language: "typescript",
      limit: 100,
      repoId: "repository:r1"
    });

    expect(post).toHaveBeenCalledWith("/api/v0/code/dead-code", {
      language: "typescript",
      limit: 100,
      repo_id: "repository:r1"
    });
  });

  it("maps dead-code rows, analysis, truncation, and truth", async () => {
    const post = vi.fn(async () => envelope([{
      classification: "unused",
      end_line: 22,
      entity_id: "function:f1",
      file_path: "server/routes.ts",
      labels: ["Function"],
      language: "typescript",
      name: "unusedRoute",
      repo_id: "repository:r1",
      repo_name: "svc-platform",
      start_line: 10
    }], { truncated: true }));
    const client = { post } as unknown as EshuApiClient;

    const page = await loadDeadCodePage(client, { limit: 100 });

    expect(page).toMatchObject({
      analysis: {
        dead_code_language_maturity: { typescript: "experimental" },
        modeled_public_api: false
      },
      limit: 100,
      truncated: true,
      truth: {
        freshness: "fresh",
        level: "derived",
        profile: "production"
      }
    });
    expect(page.rows[0]).toMatchObject({
      classification: "unused",
      detail: "server/routes.ts · unused",
      entity: "svc-platform",
      entityId: "function:f1",
      endLine: 22,
      filePath: "server/routes.ts",
      labels: ["Function"],
      language: "typescript",
      repoId: "repository:r1",
      startLine: 10,
      title: "Unreferenced symbol unusedRoute",
      truth: "derived",
      type: "Dead code"
    });
  });

  it("uses repository names for labels without leaking unresolved internal repo ids", async () => {
    const post = vi.fn(async () => envelope([
      {
        entity_id: "function:f1",
        file_path: "server/routes.ts",
        name: "unusedRoute",
        repo_id: "repository:r1"
      },
      {
        entity_id: "function:f2",
        file_path: "server/profile.ts",
        name: "unusedProfile",
        repo_id: "repository:r_078043f1"
      },
      {
        entity_id: "function:f3",
        file_path: "server/search.ts",
        name: "unusedSearch",
        repo_id: "repository:svc-search"
      }
    ]));
    const client = { post } as unknown as EshuApiClient;

    const page = await loadDeadCodePage(client, { limit: 100 }, new Map([["repository:r1", "svc-platform"]]));

    expect(page.rows.map((row) => row.entity)).toEqual([
      "svc-platform",
      "unresolved repository",
      "svc-search"
    ]);
    expect(page.rows[1]?.repoId).toBe("repository:r_078043f1");
  });

  it("rejects Eshu error envelopes instead of returning an empty page", async () => {
    const post = vi.fn(async () => ({
      data: null,
      error: {
        code: "unsupported_capability",
        message: "dead-code analysis unavailable"
      },
      truth: null
    }));
    const client = { post } as unknown as EshuApiClient;

    await expect(loadDeadCodePage(client, { limit: 100 })).rejects.toThrow(
      "unsupported_capability: dead-code analysis unavailable"
    );
  });
});
