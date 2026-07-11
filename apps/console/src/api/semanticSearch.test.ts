import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import {
  defaultSemanticSearchLimit,
  defaultSemanticSearchMode,
  defaultSemanticSearchTimeoutMs,
  searchSemantic,
} from "./semanticSearch";

function envelope(data: unknown) {
  return {
    data,
    error: null,
    truth: {
      basis: "hybrid",
      capability: "search.semantic",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production",
    },
  };
}

const responseFixture = {
  query: "checkout retry logic",
  repo_id: "acme/checkout-service",
  mode: "hybrid",
  search_mode: "hybrid",
  limit: 20,
  timeout_ms: 8000,
  results: [
    {
      rank: 1,
      score: 0.92,
      search_method: "hybrid",
      document: {
        id: "doc-1",
        repo_id: "acme/checkout-service",
        source_kind: "repository_file",
        title: "retry.go",
        path: "internal/checkout/retry.go",
        context_text: "Implements exponential backoff for checkout retries.",
        labels: ["language:go"],
        updated_at: "2026-06-01T00:00:00Z",
      },
      truth_scope: { level: "derived", basis: "content_index" },
      freshness: { state: "fresh" },
    },
    {
      rank: 2,
      score: 0.81,
      search_method: "hybrid",
      document: {
        id: "doc-2",
        repo_id: "acme/checkout-service",
        source_kind: "repository_file",
        title: "retry.rs",
        path: "src/retry.rs",
        context_text: "Rust retry helper.",
        labels: ["language:rust"],
        updated_at: "2026-06-02T00:00:00Z",
      },
      truth_scope: { level: "exact", basis: "read_model" },
      freshness: { state: "stale" },
      failures: ["partial_index"],
    },
  ],
  truncated: false,
  indexed_document_count: 512,
  corpus_limit: 1000,
  corpus_may_be_truncated: false,
  retrieval_state: "ready",
  facets: { languages: { go: 1, rust: 1 } },
};

describe("searchSemantic", () => {
  it("posts a bounded request with defaults and normalizes the response", async () => {
    const captured: { path?: string; body?: unknown } = {};
    const client = {
      post: vi.fn(async (path: string, body: unknown) => {
        captured.path = path;
        captured.body = body;
        return envelope(responseFixture);
      }),
    } as unknown as EshuApiClient;

    const result = await searchSemantic(client, {
      repoId: "acme/checkout-service",
      query: "checkout retry logic",
    });

    expect(captured.path).toBe("/api/v0/search/semantic");
    expect(captured.body).toEqual({
      repo_id: "acme/checkout-service",
      query: "checkout retry logic",
      mode: defaultSemanticSearchMode,
      limit: defaultSemanticSearchLimit,
      timeout_ms: defaultSemanticSearchTimeoutMs,
    });

    expect(result.query).toBe("checkout retry logic");
    expect(result.repoId).toBe("acme/checkout-service");
    expect(result.results).toHaveLength(2);
    expect(result.results[0]).toEqual({
      rank: 1,
      score: 0.92,
      searchMethod: "hybrid",
      document: {
        id: "doc-1",
        repoId: "acme/checkout-service",
        sourceKind: "repository_file",
        title: "retry.go",
        path: "internal/checkout/retry.go",
        contextText: "Implements exponential backoff for checkout retries.",
        labels: ["language:go"],
        updatedAt: "2026-06-01T00:00:00Z",
      },
      truthScope: { level: "derived", basis: "content_index" },
      freshness: { state: "fresh" },
      failures: [],
    });
    expect(result.results[1].failures).toEqual(["partial_index"]);
    expect(result.facets.languages).toEqual({ go: 1, rust: 1 });
  });

  it("includes languages and rerank in the request body when provided", async () => {
    const captured: { body?: unknown } = {};
    const client = {
      post: vi.fn(async (_path: string, body: unknown) => {
        captured.body = body;
        return envelope({ ...responseFixture, results: [] });
      }),
    } as unknown as EshuApiClient;

    await searchSemantic(client, {
      repoId: "acme/checkout-service",
      query: "retry",
      languages: ["go", "rust"],
      mode: "semantic",
      limit: 10,
      timeoutMs: 3000,
      rerank: true,
    });

    expect(captured.body).toEqual({
      repo_id: "acme/checkout-service",
      query: "retry",
      mode: "semantic",
      limit: 10,
      timeout_ms: 3000,
      languages: ["go", "rust"],
      rerank: true,
    });
  });

  it("omits languages from the request body when the array is empty", async () => {
    const captured: { body?: unknown } = {};
    const client = {
      post: vi.fn(async (_path: string, body: unknown) => {
        captured.body = body;
        return envelope({ ...responseFixture, results: [] });
      }),
    } as unknown as EshuApiClient;

    await searchSemantic(client, {
      repoId: "acme/checkout-service",
      query: "retry",
      languages: [],
    });

    expect((captured.body as Record<string, unknown>).languages).toBeUndefined();
  });

  it("returns an empty languages facet map when the server omits facets", async () => {
    const client = {
      post: vi.fn(async () => envelope({ ...responseFixture, facets: undefined, results: [] })),
    } as unknown as EshuApiClient;

    const result = await searchSemantic(client, {
      repoId: "acme/checkout-service",
      query: "retry",
    });

    expect(result.facets.languages).toEqual({});
  });

  it("throws EshuEnvelopeError when the server reports an error", async () => {
    const client = {
      post: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "repository not found" },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    await expect(
      searchSemantic(client, { repoId: "missing/repo", query: "retry" }),
    ).rejects.toBeInstanceOf(EshuEnvelopeError);
  });
});
