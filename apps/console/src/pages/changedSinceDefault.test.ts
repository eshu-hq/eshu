import { describe, expect, it, vi } from "vitest";

import { discoverDefaultChangedSinceParams } from "./changedSinceDefault";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";

function repository(id: string): RepoListItem {
  return {
    groupKey: "",
    groupKind: "",
    groupReason: "",
    groupSource: "",
    groupTruth: "",
    id,
    isDependency: false,
    name: id,
    remoteUrl: "",
    repoSlug: id,
  };
}

function lifecycleEnvelope(scopeId: string, priorStatus?: "completed" | "failed" | "superseded") {
  const generations = [
    {
      current_active_generation_id: "generation:active",
      generation_id: "generation:active",
      is_active: true,
      observed_at: "2026-07-14T01:00:00Z",
      queue_status: {},
      scope_id: scopeId,
      scope_kind: "repository",
      status: "active",
    },
  ];
  if (priorStatus) {
    generations.push({
      current_active_generation_id: "generation:active",
      generation_id: `generation:${priorStatus}`,
      is_active: false,
      observed_at: "2026-07-13T01:00:00Z",
      queue_status: {},
      scope_id: scopeId,
      scope_kind: "repository",
      status: priorStatus,
    });
  }
  return {
    data: { count: generations.length, generations, limit: 3, truncated: false },
    error: null,
    truth: {
      basis: "durable_lifecycle",
      capability: "freshness.generation_lifecycle",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production",
    },
  };
}

describe("discoverDefaultChangedSinceParams", () => {
  it("looks past a pending generation to select the retained exact baseline", async () => {
    const generations = [
      {
        current_active_generation_id: "generation:active",
        generation_id: "generation:pending",
        is_active: false,
        observed_at: "2026-07-15T01:00:00Z",
        queue_status: {},
        scope_id: "scope:one",
        scope_kind: "repository",
        status: "pending",
      },
      {
        current_active_generation_id: "generation:active",
        generation_id: "generation:active",
        is_active: true,
        observed_at: "2026-07-14T01:00:00Z",
        queue_status: {},
        scope_id: "scope:one",
        scope_kind: "repository",
        status: "active",
      },
      {
        current_active_generation_id: "generation:active",
        generation_id: "generation:superseded",
        is_active: false,
        observed_at: "2026-07-13T01:00:00Z",
        queue_status: {},
        scope_id: "scope:one",
        scope_kind: "repository",
        status: "superseded",
      },
    ];
    const get = vi.fn(async (path: string) => {
      const limit = Number(new URL(path, "http://localhost").searchParams.get("limit"));
      const page = generations.slice(0, limit);
      return {
        data: {
          count: page.length,
          generations: page,
          limit,
          truncated: page.length < generations.length,
        },
        error: null,
        truth: {
          basis: "durable_lifecycle",
          capability: "freshness.generation_lifecycle",
          freshness: { state: "fresh" },
          level: "exact",
          profile: "production",
        },
      };
    });

    await expect(
      discoverDefaultChangedSinceParams({ get } as unknown as EshuApiClient, [
        repository("repo-one"),
      ]),
    ).resolves.toEqual({
      scopeId: "scope:one",
      sinceGenerationId: "generation:superseded",
    });
    expect(get).toHaveBeenCalledWith(
      "/api/v0/freshness/generations?repository=repo-one&limit=3",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("probes exact repositories until it finds an active and retained prior pair", async () => {
    const get = vi.fn(async (path: string) =>
      path.includes("repository=repo-one")
        ? lifecycleEnvelope("scope:one")
        : lifecycleEnvelope("scope:two", "failed"),
    );

    await expect(
      discoverDefaultChangedSinceParams({ get } as unknown as EshuApiClient, [
        repository("repo-one"),
        repository("repo-two"),
      ]),
    ).resolves.toEqual({
      scopeId: "scope:two",
      sinceGenerationId: "generation:failed",
    });
    const calls = get.mock.calls as unknown as readonly [
      string,
      { readonly signal?: AbortSignal },
    ][];
    expect(calls[0]?.[0]).toBe("/api/v0/freshness/generations?repository=repo-one&limit=3");
    expect(calls[0]?.[1].signal).toBeInstanceOf(AbortSignal);
    expect(calls[1]?.[0]).toBe("/api/v0/freshness/generations?repository=repo-two&limit=3");
    expect(calls[1]?.[1].signal).toBeInstanceOf(AbortSignal);
  });

  it("continues after a repository lifecycle request fails", async () => {
    const get = vi
      .fn()
      .mockRejectedValueOnce(new Error("not found"))
      .mockResolvedValueOnce(lifecycleEnvelope("scope:two", "superseded"));

    await expect(
      discoverDefaultChangedSinceParams({ get } as unknown as EshuApiClient, [
        repository("missing"),
        repository("repo-two"),
      ]),
    ).resolves.toEqual({
      scopeId: "scope:two",
      sinceGenerationId: "generation:superseded",
    });
  });

  it("caps discovery at 25 repositories and fails closed without an exact pair", async () => {
    const get = vi.fn(async () => lifecycleEnvelope("scope:single"));
    const repositories = Array.from({ length: 30 }, (_, index) => repository(`repo-${index}`));

    await expect(
      discoverDefaultChangedSinceParams({ get } as unknown as EshuApiClient, repositories),
    ).resolves.toBeNull();
    expect(get).toHaveBeenCalledTimes(25);
  });

  it("bounds concurrent probes and aborts the batch at the total discovery budget", async () => {
    vi.useFakeTimers();
    try {
      const signals: AbortSignal[] = [];
      let active = 0;
      let peakActive = 0;
      const get = vi.fn(
        (_path: string, options?: { readonly signal?: AbortSignal }) =>
          new Promise<never>((_resolve, reject) => {
            const signal = options?.signal;
            if (!signal) throw new Error("discovery request must carry an abort signal");
            signals.push(signal);
            active += 1;
            peakActive = Math.max(peakActive, active);
            signal.addEventListener(
              "abort",
              () => {
                active -= 1;
                reject(
                  signal.reason instanceof Error
                    ? signal.reason
                    : new DOMException("discovery aborted", "AbortError"),
                );
              },
              { once: true },
            );
          }),
      );

      const result = discoverDefaultChangedSinceParams(
        { get } as unknown as EshuApiClient,
        Array.from({ length: 25 }, (_, index) => repository(`repo-${index}`)),
        { budgetMs: 100, maxConcurrency: 4 },
      );
      await Promise.resolve();

      expect(get).toHaveBeenCalledTimes(4);
      expect(peakActive).toBe(4);
      await vi.advanceTimersByTimeAsync(100);
      await expect(result).resolves.toBeNull();
      expect(get).toHaveBeenCalledTimes(4);
      expect(active).toBe(0);
      expect(signals.every((signal) => signal.aborted)).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("cancels in-flight discovery when the caller aborts", async () => {
    const controller = new AbortController();
    const get = vi.fn(
      (_path: string, options?: { readonly signal?: AbortSignal }) =>
        new Promise<never>((_resolve, reject) => {
          const signal = options?.signal;
          if (!signal) throw new Error("discovery request must carry an abort signal");
          signal.addEventListener(
            "abort",
            () =>
              reject(
                signal.reason instanceof Error
                  ? signal.reason
                  : new DOMException("discovery aborted", "AbortError"),
              ),
            { once: true },
          );
        }),
    );

    const result = discoverDefaultChangedSinceParams(
      { get } as unknown as EshuApiClient,
      [repository("repo-one"), repository("repo-two")],
      { signal: controller.signal },
    );
    await Promise.resolve();
    controller.abort(new DOMException("page unmounted", "AbortError"));

    await expect(result).resolves.toBeNull();
    expect(get).toHaveBeenCalledTimes(2);
  });

  it("normalizes fractional concurrency below one to a single probe", async () => {
    const get = vi.fn(async () => lifecycleEnvelope("scope:one", "completed"));

    await expect(
      discoverDefaultChangedSinceParams(
        { get } as unknown as EshuApiClient,
        [repository("repo-one")],
        { maxConcurrency: 0.5 },
      ),
    ).resolves.toEqual({
      scopeId: "scope:one",
      sinceGenerationId: "generation:completed",
    });
    expect(get).toHaveBeenCalledTimes(1);
  });
});
