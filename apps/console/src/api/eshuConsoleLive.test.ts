import { describe, expect, it } from "vitest";
import { loadConsoleSnapshot } from "./eshuConsoleLive";
import type { EshuApiClient } from "./client";

// The console adapter must match the real API response shapes:
// - ecosystem/overview is enveloped and uses repo_count (not repository_count)
// - index-status and status/ingesters return RAW JSON (no envelope), so they
//   must be read with getJson, not get
// - the language overview comes from repositories/language-inventory, not
//   repositories/by-language (which requires a ?language= and 400s without it)
describe("eshuConsoleLive", () => {
  function fakeClient(): EshuApiClient {
    return {
      get: async (path: string) => {
        if (path.includes("/ecosystem/overview")) {
          return {
            data: { repo_count: 33, workload_count: 21, platform_count: 7, instance_count: 92 },
            error: null,
            truth: { profile: "production", level: "exact", capability: "x", freshness: { state: "fresh" } }
          };
        }
        if (path.includes("/repositories/language-inventory")) {
          return {
            data: { languages: [{ language: "yaml", repository_count: 32 }, { language: "go", repository_count: 5 }] },
            error: null,
            truth: null
          };
        }
        if (path.includes("/repositories/by-language")) {
          throw new Error("by-language requires ?language= and must not be used for the overview");
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async (path: string) => {
        if (path.includes("/index-status")) {
          return { status: "healthy", repository_count: 33, queue: { outstanding: 2, in_flight: 1, dead_letter: 0, succeeded: 333 } };
        }
        if (path.includes("/status/ingesters")) {
          return { ingesters: [{ name: "repository", health: "healthy", runtime_family: "ingester" }] };
        }
        return {};
      },
      post: async () => ({ data: {}, error: null, truth: null })
    } as unknown as EshuApiClient;
  }

  it("maps runtime counts and status from enveloped + raw endpoints", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.runtime.repositories).toBe(33);
    expect(snap.runtime.workloads).toBe(21);
    expect(snap.runtime.platforms).toBe(7);
    expect(snap.runtime.instances).toBe(92);
    expect(snap.runtime.indexStatus).toBe("healthy");
    expect(snap.runtime.queueOutstanding).toBe(2);
    expect(snap.runtime.succeeded).toBe(333);
    expect(snap.runtime.profile).toBe("production");
  });

  it("reads the language overview from language-inventory (repository_count)", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.languages).toEqual([
      { language: "yaml", count: 32 },
      { language: "go", count: 5 }
    ]);
  });

  it("maps the ingester list from the raw status/ingesters payload", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.ingesters).toHaveLength(1);
    expect(snap.ingesters[0]).toMatchObject({ id: "repository", state: "healthy", kind: "ingester" });
  });
});
