import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { createBrowserSessionFetcher } from "./browserSessionFetcher";

describe("createBrowserSessionFetcher", () => {
  it("routes authoritative proof reads through the console origin and session cookie", async () => {
    const evaluate = vi.fn().mockResolvedValue({
      body: '{"data":{"total":887}}',
      headers: [["content-type", "application/json"]],
      status: 200,
      statusText: "OK",
    });
    const page = { evaluate } as unknown as Page;
    const fetcher = createBrowserSessionFetcher(page);

    const response = await fetcher("http://127.0.0.1:18083/api/v0/repositories?limit=1", {
      headers: { Accept: "application/json" },
    });

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({ data: { total: 887 } });
    expect(evaluate).toHaveBeenCalledWith(expect.any(Function), {
      body: null,
      headers: [["accept", "application/json"]],
      method: "GET",
      target: "/eshu-api/api/v0/repositories?limit=1",
      timeoutMs: 15_000,
    });
  });

  it("preserves a browser-session authorization failure for the gate to reject", async () => {
    const page = {
      evaluate: vi.fn().mockResolvedValue({
        body: "forbidden",
        headers: [],
        status: 403,
        statusText: "Forbidden",
      }),
    } as unknown as Page;

    const response = await createBrowserSessionFetcher(page)(
      "http://127.0.0.1:18083/api/v0/repositories",
    );

    expect(response.ok).toBe(false);
    expect(response.status).toBe(403);
  });
});
