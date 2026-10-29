import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { searchFileContent } from "./contentSearch";

describe("searchFileContent", () => {
  it("treats scoped truncated pages without an unread count as exact pagination", async () => {
    const post = vi.fn().mockResolvedValue({
      data: {
        results: [{ repo_id: "repo-a", relative_path: "a.go", content: "match", line_count: 1 }],
        count: 1,
        limit: 1,
        offset: 0,
        truncated: true,
      },
      truth: { level: "derived" },
      error: null,
    });
    const response = await searchFileContent({ post } as unknown as EshuApiClient, {
      query: "match",
      repoId: "repo-a",
      limit: 1,
    });
    expect(response.incomplete).toBe(false);
    expect(response.truncated).toBe(true);
    expect(response.documentsNotSearched).toBe(0);
    expect(response.resume).toBeUndefined();
  });

  it("preserves partial scan evidence and sends the exact resume cursor and offset", async () => {
    const post = vi.fn().mockResolvedValue({
      data: {
        results: [],
        count: 0,
        limit: 50,
        offset: 5,
        truncated: true,
        documents_not_searched: 90,
        partial: {
          reason: "candidate_budget_exceeded",
          rows_scanned_in_order: 10,
          rows_matched: 2,
          cursor: { repo_id: "repo-a", relative_path: "a.go" },
          budget_ms: 800,
          elapsed_ms: 780,
          overrun_ms: 0,
          progressed: true,
          hint: "add repo_id to scope the search",
        },
      },
      truth: { level: "partial" },
      error: null,
    });
    const client = { post } as unknown as EshuApiClient;
    const first = await searchFileContent(client, { query: "token", offset: 5 });
    expect(first.partial?.rowsScannedInOrder).toBe(10);
    expect(first.documentsNotSearched).toBe(90);
    expect(first.resume).toEqual({
      cursor: { repoId: "repo-a", relativePath: "a.go" },
      offset: 3,
    });
    await searchFileContent(client, { query: "token", ...first.resume });
    expect(post).toHaveBeenLastCalledWith("/api/v0/content/files/search", {
      query: "token",
      limit: 50,
      offset: 3,
      cursor: { repo_id: "repo-a", relative_path: "a.go" },
    });
  });
});
