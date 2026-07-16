import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadConsoleSnapshot } from "./eshuConsoleLive";

// Parallel-load + abort-resilience coverage for loadConsoleSnapshot (issue
// #1727). The mapping/contract tests live in eshuConsoleLive.test.ts; this
// sibling keeps both files under the 500-line cap.
describe("loadConsoleSnapshot concurrency + resilience", () => {
  it("issues the independent snapshot sections concurrently, not serially", async () => {
    // We measure live request overlap rather than wall-clock time so the test
    // is not timing-flaky. The series bundle already runs in parallel, so a
    // generic "peak > 1" would pass even with serial top-level sections. To
    // prove the *top-level* sections are parallelized, we assert that distinct
    // top-level section requests (catalog/services, languages, images,
    // dead-code) are in flight at the same time.
    const live = new Set<string>();
    let topLevelOverlap = false;
    const sectionFor = (path: string): string | null => {
      if (path.includes("/catalog")) return "services";
      if (path.includes("/language-inventory")) return "languages";
      if (path.includes("/api/v0/images")) return "images";
      if (path.includes("/dead-code")) return "findings";
      return null;
    };
    const defer = async <T>(path: string, value: T): Promise<T> => {
      const sec = sectionFor(path);
      if (sec) {
        live.add(sec);
        // Two or more distinct top-level sections in flight at once proves the
        // top-level fan-out, independent of the series bundle's own Promise.all.
        if (live.size > 1) topLevelOverlap = true;
      }
      // Yield across two macrotasks so genuinely concurrent fetches all
      // register before any of them settle.
      await new Promise((resolve) => setTimeout(resolve, 0));
      await new Promise((resolve) => setTimeout(resolve, 0));
      if (sec) live.delete(sec);
      return value;
    };
    const enveloped = (path: string, data: unknown): Promise<unknown> =>
      defer(path, { data, error: null, truth: null });
    const client = {
      get: async (path: string) => {
        if (path.includes("/catalog")) {
          return enveloped(path, {
            services: [
              {
                id: "workload:api",
                name: "api",
                kind: "service",
                repo_id: "repository:r_1",
                repo_name: "api",
              },
            ],
          });
        }
        if (path.includes("/language-inventory")) {
          return enveloped(path, { languages: [{ language: "go", repository_count: 5 }] });
        }
        if (path.includes("/api/v0/images")) {
          return enveloped(path, { images: [] });
        }
        if (path.includes("/supply-chain/impact/findings") && path.includes("affected_exact")) {
          return enveloped(path, {
            findings: [
              {
                advisory_id: "GHSA-x",
                package_name: "p",
                cvss_score: 7,
                repository_id: "repository:r_1",
              },
            ],
          });
        }
        return enveloped(path, {});
      },
      getJson: async (path: string) =>
        defer(path, path.includes("/index-status") ? { status: "healthy", queue: {} } : {}),
      post: async (path: string) => enveloped(path, { results: [{ name: "x", repo_name: "api" }] }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(topLevelOverlap).toBe(true);
    // Parallelization must not turn a repository association into service
    // evidence. The finding has repository_id only, so no reachable service is
    // proven even though the catalog contains a matching repository.
    expect(snap.services).toHaveLength(1);
    expect(snap.vulnerabilities[0]?.services).toEqual([]);
  });

  it("retries a transient AbortError on the services fetch instead of degrading to empty", async () => {
    // Under React StrictMode dev double-invoke / re-render, the catalog
    // (services) fetch can ERR_ABORT transiently. A single retry must recover
    // the section so the Catalog is not left blank when the API has data.
    let catalogCalls = 0;
    const client = {
      get: async (path: string) => {
        if (path.includes("/catalog")) {
          catalogCalls += 1;
          if (catalogCalls === 1) {
            throw new DOMException("The user aborted a request.", "AbortError");
          }
          return {
            data: {
              services: [{ id: "workload:api", name: "api", kind: "service", repo_name: "api" }],
            },
            error: null,
            truth: {
              profile: "production",
              level: "exact",
              capability: "x",
              freshness: { state: "fresh" },
            },
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ status: "healthy", queue: {} }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);
    expect(catalogCalls).toBe(2);
    expect(snap.services).toHaveLength(1);
    expect(snap.provenance.services).toBe("live");
  });

  it("does not retry a genuine endpoint failure (HTTP 500 still degrades visibly)", async () => {
    // Real server errors must still degrade the section, not be masked by the
    // abort retry path. A 500 surfaces as a plain Error, not an AbortError.
    let catalogCalls = 0;
    const client = {
      get: async (path: string) => {
        if (path.includes("/catalog")) {
          catalogCalls += 1;
          throw new Error("Eshu API request failed with HTTP 500");
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ status: "healthy", queue: {} }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);
    // No retry for a non-abort failure: one call, section degrades to empty.
    expect(catalogCalls).toBe(1);
    expect(snap.services).toEqual([]);
    expect(snap.provenance.services).toBe("unavailable");
  });
});
