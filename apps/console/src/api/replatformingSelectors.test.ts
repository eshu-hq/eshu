import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { loadReplatformingSelectors } from "./replatformingSelectors";

describe("replatforming selector adapter", () => {
  it("maps bounded selector inventory including authoritative-empty scopes", async () => {
    const get = vi.fn(async () => ({
      data: {
        scopes: [
          {
            scope_id: "aws:123456789012:us-east-1:lambda",
            account_id: "123456789012",
            region: "us-east-1",
            service: "lambda",
            label: "lambda in us-east-1 (account ...9012)",
            finding_count: 3,
          },
          {
            scope_id: "aws:123456789012:us-west-2:s3",
            account_id: "123456789012",
            region: "us-west-2",
            service: "s3",
            label: "s3 in us-west-2 (account ...9012)",
            finding_count: 0,
          },
        ],
        count: 2,
        limit: 100,
        truncated: false,
        empty_scope_count: 1,
        supported_scope_kinds: ["account", "region", "service"],
        finding_kinds: ["unmanaged_cloud_resource", "orphaned_cloud_resource"],
        page_sizes: [25, 50, 100, 200],
        readiness: {
          state: "ready",
          detail: "2 active AWS collector scope(s) are available.",
          next_action: "Choose a scope.",
        },
      },
      error: null,
      truth: {
        capability: "replatforming.selector_inventory",
        freshness: { state: "fresh" },
        level: "derived",
        profile: "local_authoritative",
      },
    }));
    const client = { get } as unknown as EshuApiClient;

    const inventory = await loadReplatformingSelectors(client);

    expect(get).toHaveBeenCalledWith("/api/v0/replatforming/selectors?limit=200");
    expect(inventory.scopes).toHaveLength(2);
    expect(inventory.scopes[1]).toMatchObject({
      findingCount: 0,
      label: "s3 in us-west-2 (account ...9012)",
      scopeId: "aws:123456789012:us-west-2:s3",
    });
    expect(inventory.supportedScopeKinds).toEqual(["account", "region", "service"]);
    expect(inventory.readiness.state).toBe("ready");
    expect(inventory.truth?.capability).toBe("replatforming.selector_inventory");
  });

  it("propagates selector inventory failures instead of showing a false empty state", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: {
          code: "backend_unavailable",
          message: "selector inventory unavailable",
          capability: "replatforming.selector_inventory",
        },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    await expect(loadReplatformingSelectors(client)).rejects.toThrow("backend_unavailable");
  });
});
