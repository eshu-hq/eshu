import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import {
  inventoryMultiGroupNote,
  loadImpactInventory,
  sortInventoryBuckets,
  type InventoryBucket,
} from "./eshuConsoleImpactInventory";

function bucket(overrides: Partial<InventoryBucket>): InventoryBucket {
  return {
    dimension: "service_id",
    value: "svc",
    unattributed: false,
    count: 1,
    byImpactStatus: {},
    bySeverity: {},
    maxPriorityScore: null,
    completeness: "complete",
    missingEvidence: [],
    ...overrides,
  };
}

function clientReturning(data: unknown): { client: EshuApiClient; get: ReturnType<typeof vi.fn> } {
  const get = vi.fn(async () => ({ data, error: null, truth: null }));
  return { client: { get } as unknown as EshuApiClient, get };
}

describe("loadImpactInventory", () => {
  it("sends group_by, profile, limit and offset to the inventory route", async () => {
    const { client, get } = clientReturning({ buckets: [], detection_profile: "comprehensive" });
    await loadImpactInventory(client, {
      groupBy: "workload_id",
      profile: "comprehensive",
      limit: 25,
      offset: 50,
    });
    const [path] = get.mock.calls[0] as unknown as [string];
    const url = new URL(path, "http://x");
    expect(url.pathname).toBe("/api/v0/supply-chain/impact/inventory");
    expect(url.searchParams.get("group_by")).toBe("workload_id");
    expect(url.searchParams.get("profile")).toBe("comprehensive");
    expect(url.searchParams.get("limit")).toBe("25");
    expect(url.searchParams.get("offset")).toBe("50");
  });

  it("omits offset on the first page", async () => {
    const { client, get } = clientReturning({ buckets: [], detection_profile: "precise" });
    await loadImpactInventory(client, { groupBy: "service_id", profile: "precise" });
    const [path] = get.mock.calls[0] as unknown as [string];
    expect(new URL(path, "http://x").searchParams.has("offset")).toBe(false);
  });

  it("parses buckets and reports the echoed profile", async () => {
    const { client } = clientReturning({
      buckets: [
        {
          dimension: "service_id",
          value: "checkout",
          unattributed: false,
          count: 3,
          by_impact_status: { affected_exact: 2, possibly_affected: 1 },
          by_severity: { critical: 1, high: 2 },
          max_priority_score: 91.5,
          completeness: "partial",
          missing_evidence: ["deploy_evidence"],
        },
      ],
      count: 1,
      limit: 50,
      offset: 0,
      group_by: "service_id",
      detection_profile: "precise",
      distinct_findings: 3,
      truncated: true,
      next_offset: 50,
    });
    const page = await loadImpactInventory(client, {
      groupBy: "service_id",
      profile: "precise",
    });
    expect(page.detectionProfile).toBe("precise");
    expect(page.truncated).toBe(true);
    expect(page.nextOffset).toBe(50);
    expect(page.distinctFindings).toBe(3);
    expect(page.buckets[0]).toMatchObject({
      value: "checkout",
      count: 3,
      byImpactStatus: { affected_exact: 2, possibly_affected: 1 },
      bySeverity: { critical: 1, high: 2 },
      maxPriorityScore: 91.5,
      completeness: "partial",
      missingEvidence: ["deploy_evidence"],
    });
  });

  it("never reports an unattributed bucket as complete, even if the wire says so", async () => {
    const { client } = clientReturning({
      buckets: [
        {
          dimension: "service_id",
          value: "",
          unattributed: true,
          count: 4,
          completeness: "complete",
        },
      ],
      detection_profile: "precise",
    });
    const page = await loadImpactInventory(client, { groupBy: "service_id", profile: "precise" });
    expect(page.buckets[0].unattributed).toBe(true);
    expect(page.buckets[0].completeness).toBe("unattributed");
  });

  it("treats an unknown completeness as partial, not complete", async () => {
    const { client } = clientReturning({
      buckets: [{ value: "x", count: 1, completeness: "fresh-new-state" }],
      detection_profile: "precise",
    });
    const page = await loadImpactInventory(client, { groupBy: "service_id", profile: "precise" });
    expect(page.buckets[0].completeness).toBe("partial");
  });

  it("fails when the server answers on a different profile than requested", async () => {
    const { client } = clientReturning({ buckets: [], detection_profile: "precise" });
    await expect(
      loadImpactInventory(client, { groupBy: "service_id", profile: "comprehensive" }),
    ).rejects.toThrow(/requested comprehensive.*precise/i);
  });

  it("fails when the server does not report a detection profile", async () => {
    const { client } = clientReturning({ buckets: [] });
    await expect(
      loadImpactInventory(client, { groupBy: "service_id", profile: "precise" }),
    ).rejects.toThrow(/did not report/i);
  });

  it("throws the envelope error instead of returning an empty page", async () => {
    const get = vi.fn(async () => ({
      data: null,
      error: { code: "unavailable", message: "winners read disabled" },
      truth: null,
    }));
    await expect(
      loadImpactInventory({ get } as unknown as EshuApiClient, {
        groupBy: "service_id",
        profile: "precise",
      }),
    ).rejects.toThrow("winners read disabled");
  });
});

describe("sortInventoryBuckets", () => {
  it("sorts by max priority desc, then count desc, then value, with missing scores last", () => {
    const sorted = sortInventoryBuckets([
      bucket({ value: "none", count: 99, maxPriorityScore: null }),
      bucket({ value: "low", count: 50, maxPriorityScore: 10 }),
      bucket({ value: "high-few", count: 1, maxPriorityScore: 90 }),
      bucket({ value: "high-many", count: 5, maxPriorityScore: 90 }),
    ]);
    expect(sorted.map((item) => item.value)).toEqual(["high-many", "high-few", "low", "none"]);
  });

  it("does not mutate its input", () => {
    const input = [bucket({ value: "a", count: 1 }), bucket({ value: "b", count: 2 })];
    sortInventoryBuckets(input);
    expect(input.map((item) => item.value)).toEqual(["a", "b"]);
  });
});

describe("inventoryMultiGroupNote", () => {
  it("explains the overlap when group counts exceed distinct findings", () => {
    const note = inventoryMultiGroupNote(
      [bucket({ count: 3 }), bucket({ value: "b", count: 2 })],
      3,
      "service_id",
    );
    expect(note).toContain("3 distinct findings");
    expect(note).toContain("5");
    expect(note).toMatch(/counted in each/i);
  });

  it("is silent when the counts do not overlap", () => {
    expect(inventoryMultiGroupNote([bucket({ count: 3 })], 3, "service_id")).toBeNull();
    expect(inventoryMultiGroupNote([], 0, "service_id")).toBeNull();
  });
});
