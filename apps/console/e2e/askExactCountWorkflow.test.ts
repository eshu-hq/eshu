import { describe, expect, it, vi } from "vitest";

import { consoleRoutes } from "../src/e2e/routeAssertions";
import {
  loadIndexedRepositoryInventoryAnchor,
  parseAskAnswerPayload,
  validateAskExactCountResponse,
} from "./askExactCountWorkflowProbe";

const issuePrompt =
  "How many repositories are currently indexed? Return the count and cite the evidence used.";

describe("Ask exact indexed-repository workflow", () => {
  it("loads a bounded same-run inventory page and keeps its authoritative total", async () => {
    const fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: { repositories: [{}], count: 1, total: 896 } }), {
          status: 200,
        }),
    );

    await expect(
      loadIndexedRepositoryInventoryAnchor("https://eshu.example", "test-token", fetcher),
    ).resolves.toEqual({ count: 1, total: 896 });
    expect(fetcher).toHaveBeenCalledWith(
      "https://eshu.example/api/v0/repositories?limit=1&offset=0",
      {
        headers: { Authorization: "Bearer test-token" },
        signal: expect.any(AbortSignal),
      },
    );
  });

  it("omits bearer authorization when the browser session owns the inventory read", async () => {
    const fetcher = vi.fn(
      async () => new Response(JSON.stringify({ data: { count: 1, total: 896 } }), { status: 200 }),
    );

    await loadIndexedRepositoryInventoryAnchor("https://eshu.example", "", fetcher);

    expect(fetcher).toHaveBeenCalledWith(
      "https://eshu.example/api/v0/repositories?limit=1&offset=0",
      { headers: {}, signal: expect.any(AbortSignal) },
    );
  });

  it("aborts and reports a bounded failure when the inventory anchor times out", async () => {
    vi.useFakeTimers();
    try {
      let observedSignal: AbortSignal | undefined;
      const fetcher = vi.fn(
        async (_input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
          observedSignal = init?.signal ?? undefined;
          return await new Promise<Response>((_resolve, reject) => {
            observedSignal?.addEventListener("abort", () => reject(observedSignal?.reason), {
              once: true,
            });
          });
        },
      );

      const anchor = loadIndexedRepositoryInventoryAnchor(
        "https://eshu.example",
        "test-token",
        fetcher,
        25,
      );
      const rejection = expect(anchor).rejects.toThrow("timed out after 25 ms");
      await vi.advanceTimersByTimeAsync(25);

      await rejection;
      expect(observedSignal?.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("clears the timeout after a successful inventory response", async () => {
    vi.useFakeTimers();
    try {
      let observedSignal: AbortSignal | undefined;
      const fetcher = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        observedSignal = init?.signal ?? undefined;
        return new Response(JSON.stringify({ data: { count: 1, total: 896 } }), { status: 200 });
      });

      await expect(
        loadIndexedRepositoryInventoryAnchor("https://eshu.example", "test-token", fetcher, 25),
      ).resolves.toEqual({ count: 1, total: 896 });

      expect(vi.getTimerCount()).toBe(0);
      await vi.advanceTimersByTimeAsync(25);
      expect(observedSignal?.aborted).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("extracts the terminal answer from the browser's SSE response", () => {
    expect(
      parseAskAnswerPayload(
        'event: trace\ndata: {"tool":"list_indexed_repositories"}\n\n' +
          'event: answer\ndata: {"result_ref":"eshu://api-result/repositories","result":{"total":896}}\n\n' +
          "event: done\ndata: {}\n\n",
        "text/event-stream; charset=utf-8",
      ),
    ).toEqual({ result_ref: "eshu://api-result/repositories", result: { total: 896 } });
  });

  it("uses the exact issue prompt and dedicated truth workflow", () => {
    const workflow = consoleRoutes.find((route) => route.path === "/ask")?.workflow;

    expect(workflow).toMatchObject({
      id: "ask-live-exact-indexed-repository-count",
      kind: "askExactCount",
      prompt: issuePrompt,
      resultRef: "eshu://api-result/repositories",
    });
  });

  it("accepts an authoritative same-run total with tool and aggregate-result evidence", () => {
    expect(
      validateAskExactCountResponse(
        {
          answer_prose:
            "896 indexed repositories visible in your authorized scope. Evidence: list_indexed_repositories.total.",
          truth_class: "deterministic",
          result_ref: "eshu://api-result/repositories",
          result: { total: 896 },
          query_trace: [
            {
              tool: "list_indexed_repositories",
              supported: true,
              truth_class: "deterministic",
            },
          ],
        },
        { count: 1, total: 896 },
        "eshu://api-result/repositories",
      ),
    ).toBeNull();
  });

  it("rejects substituting the current page count for the authoritative total", () => {
    expect(
      validateAskExactCountResponse(
        {
          answer_prose:
            "1 indexed repository visible in your authorized scope. Evidence: list_indexed_repositories.total.",
          truth_class: "deterministic",
          result_ref: "eshu://api-result/repositories",
          result: { total: 1 },
          query_trace: [
            {
              tool: "list_indexed_repositories",
              supported: true,
              truth_class: "deterministic",
            },
          ],
        },
        { count: 1, total: 896 },
        "eshu://api-result/repositories",
      ),
    ).toContain("same-run authoritative total 896");
  });
});
