import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { humanizeAge, loadOperationsBoard, repoLabel } from "./operationsBoard";

// mockClient answers GET /api/v0/status/operations with a fixed wire payload,
// or throws/errors to exercise the degrade-to-unavailable path.
function mockClient(options: {
  readonly data?: unknown;
  readonly error?: { readonly code: string; readonly message: string } | null;
  readonly throws?: boolean;
}): EshuApiClient {
  const calls: string[] = [];
  const client = {
    calls,
    get: async (path: string) => {
      calls.push(path);
      if (options.throws) throw new Error("operations endpoint offline");
      return { data: options.data ?? null, error: options.error ?? null, truth: null };
    },
  };
  return client as unknown as EshuApiClient;
}

const wirePayload = {
  version: "1.2.3",
  as_of: "2026-06-21T12:00:00Z",
  scoped: false,
  health: { state: "degraded", reasons: ["queue_backlog"] },
  collectors: [
    {
      instance_id: "git-1",
      collector_kind: "git",
      mode: "poll",
      display_name: "Git",
      health: "healthy",
      last_observed_at: "2026-06-21T11:59:30Z",
    },
    {
      instance_id: "sbom-1",
      collector_kind: "sbom_attestation",
      mode: "claim",
      display_name: "SBOM",
      health: "degraded",
      last_observed_at: "2026-06-21T11:45:00Z",
    },
  ],
  stage_summaries: [
    {
      stage: "reduce",
      pending: 3,
      claimed: 2,
      running: 1,
      retrying: 0,
      succeeded: 100,
      failed: 0,
      dead_letter: 0,
    },
  ],
  queue: {
    outstanding: 12,
    in_flight: 3,
    retrying: 1,
    succeeded: 900,
    dead_letter: 2,
    failed: 0,
    overdue_claims: 0,
  },
  live_activity: [
    {
      work_item_id: "wi-1",
      stage: "reduce",
      status: "running",
      domain: "repository:checkout-service",
      lease_owner: "reducer-1",
      claim_until: "2026-06-21T12:05:00Z",
      attempt_count: 1,
      updated_at: "2026-06-21T11:59:00Z",
      created_at: "2026-06-21T11:58:00Z",
      age_seconds: 90,
      scope_kind: "repository",
      collector_kind: "git",
      source_system: "github",
      source_key: "repository:r_ea78e8bb",
      source_display: "acme/checkout-service",
    },
  ],
  truncated: false,
  limit: 50,
};

describe("loadOperationsBoard", () => {
  it("uses only the bounded status/operations read path", async () => {
    const client = mockClient({ data: wirePayload });
    await loadOperationsBoard(client);
    const calls = (client as unknown as { calls: string[] }).calls;
    expect(calls).toEqual(["/api/v0/status/operations"]);
  });

  it("appends the limit query parameter when a bounded limit is requested", async () => {
    const client = mockClient({ data: wirePayload });
    await loadOperationsBoard(client, 25);
    const calls = (client as unknown as { calls: string[] }).calls;
    expect(calls).toEqual(["/api/v0/status/operations?limit=25"]);
  });

  it("maps health, stage summaries, queue, collectors, and live_activity from the wire shape", async () => {
    const now = Date.parse("2026-06-21T12:00:00Z");
    const client = mockClient({ data: wirePayload });
    const board = await loadOperationsBoard(client, undefined, () => now);

    expect(board.provenance).toBe("live");
    expect(board.scoped).toBe(false);
    expect(board.health).toEqual({ state: "degraded", reasons: ["queue_backlog"] });
    expect(board.stageSummaries).toEqual([
      {
        stage: "reduce",
        pending: 3,
        claimed: 2,
        running: 1,
        retrying: 0,
        succeeded: 100,
        failed: 0,
        deadLetter: 0,
      },
    ]);
    expect(board.queue).toEqual({
      outstanding: 12,
      inFlight: 3,
      retrying: 1,
      succeeded: 900,
      deadLetter: 2,
      failed: 0,
      overdueClaims: 0,
    });
    expect(board.truncated).toBe(false);
    expect(board.limit).toBe(50);

    // Git heartbeat is 30s old -> fresh; SBOM is 15min old -> stale.
    expect(board.collectors).toEqual([
      {
        instanceId: "git-1",
        kind: "git",
        displayName: "Git",
        mode: "poll",
        health: "healthy",
        lastObservedAt: "2026-06-21T11:59:30Z",
        freshness: "fresh",
      },
      {
        instanceId: "sbom-1",
        kind: "sbom_attestation",
        displayName: "SBOM",
        mode: "claim",
        health: "degraded",
        lastObservedAt: "2026-06-21T11:45:00Z",
        freshness: "stale",
      },
    ]);

    expect(board.liveActivity).toEqual([
      {
        workItemId: "wi-1",
        stage: "reduce",
        status: "running",
        domain: "repository:checkout-service",
        leaseOwner: "reducer-1",
        claimUntil: "2026-06-21T12:05:00Z",
        attemptCount: 1,
        updatedAt: "2026-06-21T11:59:00Z",
        createdAt: "2026-06-21T11:58:00Z",
        ageSeconds: 90,
        scopeKind: "repository",
        collectorKind: "git",
        sourceSystem: "github",
        sourceKey: "repository:r_ea78e8bb",
        sourceDisplay: "acme/checkout-service",
      },
    ]);
  });

  it("renders scoped rows safely with null lease_owner, source_key, and source_display", async () => {
    const client = mockClient({
      data: {
        ...wirePayload,
        scoped: true,
        live_activity: [
          {
            ...wirePayload.live_activity[0],
            lease_owner: null,
            source_key: null,
            source_display: null,
          },
        ],
      },
    });
    const board = await loadOperationsBoard(client);
    expect(board.scoped).toBe(true);
    expect(board.liveActivity[0]?.leaseOwner).toBeNull();
    expect(board.liveActivity[0]?.sourceKey).toBeNull();
    expect(board.liveActivity[0]?.sourceDisplay).toBeNull();
  });

  it("degrades to an unavailable board when the endpoint throws", async () => {
    const client = mockClient({ throws: true });
    const board = await loadOperationsBoard(client);
    expect(board.provenance).toBe("unavailable");
    expect(board.health.state).toBe("unknown");
    expect(board.collectors).toEqual([]);
    expect(board.liveActivity).toEqual([]);
  });

  it("degrades to an unavailable board on an envelope error", async () => {
    const client = mockClient({ error: { code: "not_found", message: "no reader configured" } });
    const board = await loadOperationsBoard(client);
    expect(board.provenance).toBe("unavailable");
  });

  it("degrades to an unavailable board when data is missing", async () => {
    const client = mockClient({ data: null });
    const board = await loadOperationsBoard(client);
    expect(board.provenance).toBe("unavailable");
  });
});

describe("humanizeAge", () => {
  it("renders seconds, minutes, hours, and days compactly", () => {
    expect(humanizeAge(40)).toBe("40s");
    expect(humanizeAge(125)).toBe("2m");
    expect(humanizeAge(7290)).toBe("2h 1m");
    expect(humanizeAge(90000)).toBe("1d");
  });
});

// repoLabel resolves the "Now processing" repo column: source_display (the
// operator-facing repo name, #5137 follow-up) when non-empty, else source_key
// as a fallback, else an em dash when both are redacted/absent (scoped token).
describe("repoLabel", () => {
  it("prefers the human-readable source_display over the raw source_key", () => {
    expect(
      repoLabel({ sourceDisplay: "acme/orders-api", sourceKey: "repository:r_ea78e8bb" }),
    ).toBe("acme/orders-api");
  });

  it("falls back to source_key when source_display is missing", () => {
    expect(repoLabel({ sourceDisplay: null, sourceKey: "repository:r_ea78e8bb" })).toBe(
      "repository:r_ea78e8bb",
    );
  });

  it("falls back to an em dash when both are redacted or absent", () => {
    expect(repoLabel({ sourceDisplay: null, sourceKey: null })).toBe("—");
  });
});
