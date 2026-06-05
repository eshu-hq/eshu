import { describe, expect, it } from "vitest";
import { loadConsoleSnapshot } from "./eshuConsoleLive";
import type { EshuApiClient } from "./client";

describe("eshuConsoleLive", () => {
  // Regression: the runtime summary must read the API's repo_count field. It was
  // reading overview.repository_count, which the API never sends, so the Dashboard
  // "Repositories" tile showed 0 even with repositories indexed.
  it("maps ecosystem overview repo_count to runtime.repositories", async () => {
    const client = {
      get: async (path: string) => {
        if (path.includes("/ecosystem/overview")) {
          return {
            data: { repo_count: 33, workload_count: 21, platform_count: 7, instance_count: 92 },
            error: null,
            truth: null
          };
        }
        if (path.includes("/index-status")) {
          return { data: { status: "ready", queue: {} }, error: null, truth: null };
        }
        return { data: {}, error: null, truth: null };
      },
      post: async () => ({ data: {}, error: null, truth: null })
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);
    expect(snap.runtime.repositories).toBe(33);
    expect(snap.runtime.workloads).toBe(21);
    expect(snap.runtime.platforms).toBe(7);
    expect(snap.runtime.instances).toBe(92);
  });
});
