import { describe, expect, it, vi } from "vitest";

import {
  loadGenerationLifecycle,
  loadRepositoryChangedSince,
  loadServiceChangedSince,
} from "./changedSince";
import type { EshuApiClient } from "./client";

function envelope(data: unknown, capability = "freshness.changed_since") {
  return {
    data,
    error: null,
    truth: {
      basis: "semantic_facts",
      capability,
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production",
    },
  };
}

describe("changed-since adapters", () => {
  it("loads repository changed-since with bounded query parameters and truth", async () => {
    const captured: { path?: string } = {};
    const client = {
      get: vi.fn(async (path: string) => {
        captured.path = path;
        return envelope({
          categories: [
            {
              category: "files",
              counts: { added: 2, updated: 1, unchanged: 5, retired: 1, superseded: 0 },
              samples: {
                added: [{ fact_kind: "file", stable_fact_key: "src/main.go" }],
              },
              truncated: { added: true },
              unavailable: false,
            },
          ],
          current_active_generation_id: "gen-current",
          current_observed_at: "2026-06-13T18:00:00Z",
          repository: "acme/app",
          sample_limit: 10,
          scope_id: "git-repository-scope:acme/app",
          scope_kind: "repository",
          since_generation_id: "gen-prior",
          unavailable: false,
        });
      }),
    } as unknown as EshuApiClient;

    const page = await loadRepositoryChangedSince(client, {
      repository: "acme/app",
      sampleLimit: 10,
      sinceGenerationId: "gen-prior",
    });

    expect(captured.path).toBe(
      "/api/v0/freshness/changed-since?repository=acme%2Fapp&since_generation_id=gen-prior&sample_limit=10",
    );
    expect(page.changedCount).toBe(4);
    expect(page.categories[0].samples.added[0]).toMatchObject({
      factKind: "file",
      stableFactKey: "src/main.go",
    });
    expect(page.categories[0].truncated.added).toBe(true);
    expect(page.truth?.freshness.state).toBe("fresh");
  });

  it("loads service changed-since with service lineage fields", async () => {
    const captured: { path?: string } = {};
    const client = {
      get: vi.fn(async (path: string) => {
        captured.path = path;
        return envelope(
          {
            categories: [
              {
                category: "ownership",
                counts: { added: 1, updated: 0, unchanged: 1, retired: 0, superseded: 0 },
                samples: {
                  added: [{ fact_kind: "service_owner", stable_fact_key: "team/platform" }],
                },
                unavailable: false,
              },
            ],
            current_active_generation_id: "svc-gen-current",
            sample_limit: 25,
            service_id: "svc-checkout",
            since_generation_id: "svc-gen-prior",
            unavailable: false,
          },
          "freshness.service_changed_since",
        );
      }),
    } as unknown as EshuApiClient;

    const page = await loadServiceChangedSince(client, {
      sampleLimit: 25,
      serviceId: "svc-checkout",
      sinceGenerationId: "svc-gen-prior",
    });

    expect(captured.path).toBe(
      "/api/v0/freshness/services/changed-since?service_id=svc-checkout&since_generation_id=svc-gen-prior&sample_limit=25",
    );
    expect(page.scopeLabel).toBe("svc-checkout");
    expect(page.changedCount).toBe(1);
    expect(page.truth?.capability).toBe("freshness.service_changed_since");
  });

  it("loads generation lifecycle rows for baseline selection", async () => {
    const captured: { path?: string } = {};
    const client = {
      get: vi.fn(async (path: string) => {
        captured.path = path;
        return envelope(
          {
            count: 1,
            generations: [
              {
                collector_kind: "git",
                current_active_generation_id: "gen-current",
                generation_id: "gen-prior",
                is_active: false,
                latest_failure: {
                  failure_class: "graph_write_timeout",
                  failure_message: "write canonical projection timed out",
                  observed_at: "2026-06-12T18:01:00Z",
                  work_item_status: "dead_letter",
                },
                observed_at: "2026-06-12T18:00:00Z",
                queue_status: {
                  dead_letter: 0,
                  failed: 0,
                  in_flight: 0,
                  outstanding: 0,
                  retrying: 0,
                  succeeded: 9,
                  total: 9,
                },
                scope_id: "git-repository-scope:acme/app",
                scope_kind: "repository",
                source_system: "github",
                status: "superseded",
                trigger_kind: "scheduled",
              },
            ],
            limit: 50,
            truncated: false,
          },
          "freshness.generation_lifecycle",
        );
      }),
    } as unknown as EshuApiClient;

    const page = await loadGenerationLifecycle(client, {
      limit: 50,
      repository: "acme/app",
    });

    expect(captured.path).toBe("/api/v0/freshness/generations?repository=acme%2Fapp&limit=50");
    expect(page.generations[0]).toMatchObject({
      generationId: "gen-prior",
      latestFailure: "write canonical projection timed out",
      queueOutstanding: 0,
      status: "superseded",
    });
    expect(page.truncated).toBe(false);
  });

  it("forwards cancellation to generation lifecycle requests", async () => {
    const controller = new AbortController();
    const get = vi.fn(async () =>
      envelope(
        { count: 0, generations: [], limit: 2, truncated: false },
        "freshness.generation_lifecycle",
      ),
    );
    const client = { get } as unknown as EshuApiClient;

    await loadGenerationLifecycle(
      client,
      { limit: 2, repository: "acme/app" },
      { signal: controller.signal },
    );

    expect(get).toHaveBeenCalledWith(
      "/api/v0/freshness/generations?repository=acme%2Fapp&limit=2",
      { signal: controller.signal },
    );
  });

  it("rejects malformed success envelopes instead of fabricating empty deltas", async () => {
    const client = {
      get: vi.fn(async () => ({ data: null, error: null, truth: null })),
    } as unknown as EshuApiClient;

    await expect(
      loadRepositoryChangedSince(client, {
        repository: "acme/app",
        sinceGenerationId: "gen-prior",
      }),
    ).rejects.toThrow("missing data or truth");
  });
});
