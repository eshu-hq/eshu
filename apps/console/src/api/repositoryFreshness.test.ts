import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadRepositoryFreshness, shortSha } from "./repositoryFreshness";

// mockClient answers GET /api/v0/repositories/{id}/freshness with a fixed
// wire payload, or throws/errors to exercise the degrade-to-unavailable path.
function mockClient(options: {
  readonly data?: unknown;
  readonly error?: { readonly code: string; readonly message: string } | null;
  readonly throws?: boolean;
}): EshuApiClient & { readonly calls: string[] } {
  const calls: string[] = [];
  const client = {
    calls,
    get: async (path: string) => {
      calls.push(path);
      if (options.throws) throw new Error("freshness endpoint offline");
      return { data: options.data ?? null, error: options.error ?? null, truth: null };
    },
  };
  return client as unknown as EshuApiClient & { readonly calls: string[] };
}

const now = Date.parse("2026-06-21T12:05:00Z");
const clock = (): number => now;

function baseWire(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    scope_id: "scope:checkout-service",
    verdict: "current",
    observed_commit: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
    observed_at: "2026-06-21T12:03:00Z",
    generation: {
      id: "generation:demo-1",
      status: "active",
      trigger_kind: "webhook",
      is_delta: true,
      activated_at: "2026-06-21T12:03:30Z",
    },
    stages: { collected: true, reduced: true, projected: true, materialized: true },
    outstanding_by_stage: [],
    shared_enrichment: { pending: false, pending_domains: [] },
    unobserved_push: null,
    as_of: "2026-06-21T12:05:00Z",
    scoped: false,
    ...overrides,
  };
}

describe("loadRepositoryFreshness", () => {
  it("requests the unparameterized freshness path when no expected commit is given", async () => {
    const client = mockClient({ data: baseWire() });
    await loadRepositoryFreshness(client, "repository:checkout-service");
    expect(client.calls).toEqual(["/api/v0/repositories/repository%3Acheckout-service/freshness"]);
  });

  it("appends expected_commit when supplied", async () => {
    const client = mockClient({ data: baseWire() });
    await loadRepositoryFreshness(client, "repository:checkout-service", {
      expectedCommit: "deadbeef",
    });
    expect(client.calls).toEqual([
      "/api/v0/repositories/repository%3Acheckout-service/freshness?expected_commit=deadbeef",
    ]);
  });

  it("maps the current verdict to teal copy with a relative observed_at", async () => {
    const client = mockClient({ data: baseWire() });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service", {
      clock,
    });

    expect(freshness.provenance).toBe("live");
    expect(freshness.verdict).toBe("current");
    expect(freshness.observedCommit).toBe("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2");
    expect(freshness.stages).toEqual({
      collected: true,
      reduced: true,
      projected: true,
      materialized: true,
    });
    expect(freshness.copy).toEqual({
      tone: "teal",
      headline: "Current through a1b2c3d4e5",
      detail: "Answers include your latest indexed push (2m ago).",
    });
  });

  it("maps the building verdict using the first outstanding stage with a nonzero count", async () => {
    const client = mockClient({
      data: baseWire({
        verdict: "building",
        stages: { collected: true, reduced: true, projected: false, materialized: false },
        outstanding_by_stage: [
          { stage: "reduce", status: "succeeded", count: 0 },
          { stage: "project", status: "running", count: 12 },
        ],
      }),
    });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service", {
      clock,
    });

    expect(freshness.verdict).toBe("building");
    expect(freshness.outstandingByStage).toEqual([
      { stage: "reduce", status: "succeeded", count: 0 },
      { stage: "project", status: "running", count: 12 },
    ]);
    expect(freshness.copy).toEqual({
      tone: "violet",
      headline: "Indexing a1b2c3d4e5",
      detail: "projecting — 12 items left",
    });
  });

  it("falls back to the shared-enrichment note when own stages are drained but shared work is pending", async () => {
    const client = mockClient({
      data: baseWire({
        verdict: "building",
        stages: { collected: true, reduced: true, projected: true, materialized: false },
        outstanding_by_stage: [],
        shared_enrichment: {
          pending: true,
          pending_domains: [{ domain: "package_registry", count: 3 }],
        },
      }),
    });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service", {
      clock,
    });

    expect(freshness.sharedEnrichment).toEqual({
      pending: true,
      pendingDomains: [{ domain: "package_registry", count: 3 }],
    });
    expect(freshness.copy.detail).toBe("your repo is done; cross-repo enrichment still running");
  });

  it("maps the behind verdict with both the observed and expected short SHAs", async () => {
    const client = mockClient({
      data: baseWire({
        verdict: "behind",
        observed_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      }),
    });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service", {
      expectedCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      clock,
    });

    expect(freshness.verdict).toBe("behind");
    expect(freshness.copy).toEqual({
      tone: "warn",
      headline: "Behind your commit",
      detail: "eshu has aaaaaaaaaa; expected bbbbbbbbbb not indexed yet.",
    });
  });

  it("maps the unobserved verdict using the unobserved_push ref and target sha", async () => {
    const client = mockClient({
      data: baseWire({
        verdict: "unobserved",
        observed_commit: "",
        unobserved_push: {
          target_sha: "cccccccccccccccccccccccccccccccccccccccc",
          ref: "refs/heads/main",
          received_at: "2026-06-21T12:04:00Z",
        },
      }),
    });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service", {
      clock,
    });

    expect(freshness.verdict).toBe("unobserved");
    expect(freshness.unobservedPush).toEqual({
      targetSha: "cccccccccccccccccccccccccccccccccccccccc",
      ref: "refs/heads/main",
      receivedAt: "2026-06-21T12:04:00Z",
    });
    expect(freshness.copy).toEqual({
      tone: "warn",
      headline: "Push not picked up",
      detail: "A push to refs/heads/main (cccccccccc) was received but no indexing has started.",
    });
  });

  it("renders the unknown verdict honestly instead of fabricating a SHA for an empty observed_commit", async () => {
    const client = mockClient({
      data: baseWire({
        verdict: "unknown",
        observed_commit: "",
        stages: { collected: false, reduced: false, projected: false, materialized: false },
      }),
    });
    const freshness = await loadRepositoryFreshness(client, "aws:demo-account", { clock });

    expect(freshness.verdict).toBe("unknown");
    expect(freshness.observedCommit).toBe("");
    expect(freshness.copy).toEqual({
      tone: "neutral",
      headline: "No commit receipt",
      detail:
        "This scope has no recorded commit — not a git repository, or indexing has not produced one yet.",
    });
  });

  it("degrades to an unavailable freshness when the endpoint throws", async () => {
    const client = mockClient({ throws: true });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service");

    expect(freshness.provenance).toBe("unavailable");
    expect(freshness.copy.tone).toBe("neutral");
    expect(freshness.copy.headline).toBe("Freshness unavailable");
  });

  it("degrades to an unavailable freshness on an envelope error", async () => {
    const client = mockClient({ error: { code: "not_found", message: "no reader configured" } });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service");
    expect(freshness.provenance).toBe("unavailable");
  });

  it("degrades to an unavailable freshness when data is missing", async () => {
    const client = mockClient({ data: null });
    const freshness = await loadRepositoryFreshness(client, "repository:checkout-service");
    expect(freshness.provenance).toBe("unavailable");
  });
});

describe("shortSha", () => {
  it("renders an em dash instead of fabricating a SHA for an empty commit", () => {
    expect(shortSha("")).toBe("—");
    expect(shortSha("   ")).toBe("—");
  });

  it("truncates to 10 characters, matching RepoSourcePage's indexed-ref badge", () => {
    expect(shortSha("a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")).toBe("a1b2c3d4e5");
  });
});
