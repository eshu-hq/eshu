import { describe, expect, it, vi } from "vitest";
import { loadCloudInventory } from "./cloudInventory";
import type { EshuApiClient } from "./client";

describe("loadCloudInventory", () => {
  function fakeClient(capture: { path?: string }): EshuApiClient {
    return {
      get: async (path: string) => {
        capture.path = path;
        return {
          data: {
            resources: [{
              cloud_resource_uid: "aws:111122223333:AWS::S3::Bucket:acme-prod",
              provider: "aws",
              resource_type: "AWS::S3::Bucket",
              management_origin: "declared",
              scope_id: "cloud-scope:aws:111122223333",
              generation_id: "aws-gen-1",
              source_state: "exact",
              evidence: { declared: true, applied: true, observed: false },
              tag_value_fingerprints: { owner: "fp-owner" }
            }],
            count: 1,
            limit: 50,
            truncated: true,
            next_cursor: "50"
          },
          error: null,
          truth: {
            capability: "cloud_inventory.readback.list",
            freshness: { state: "fresh" },
            level: "exact",
            profile: "production"
          }
        };
      }
    } as unknown as EshuApiClient;
  }

  it("maps canonical inventory rows with evidence and truth", async () => {
    const capture: { path?: string } = {};
    const page = await loadCloudInventory(fakeClient(capture), { limit: 50 });

    expect(page.rows).toHaveLength(1);
    expect(page.rows[0]).toMatchObject({
      cloudResourceUid: "aws:111122223333:AWS::S3::Bucket:acme-prod",
      provider: "aws",
      resourceType: "AWS::S3::Bucket",
      managementOrigin: "declared",
      scopeId: "cloud-scope:aws:111122223333",
      sourceState: "exact",
      evidence: { declared: true, applied: true, observed: false },
      tagValueFingerprints: { owner: "fp-owner" }
    });
    expect(page.nextCursor).toBe("50");
    expect(page.truncated).toBe(true);
    expect(page.truth.level).toBe("exact");
    expect(capture.path).toBe("/api/v0/cloud/inventory?limit=50");
  });

  it("forwards canonical filters and offset cursor", async () => {
    const capture: { path?: string } = {};
    await loadCloudInventory(fakeClient(capture), {
      accountId: "111122223333",
      cursor: "50",
      limit: 100,
      managementOrigin: "observed",
      provider: "aws"
    });

    const path = capture.path ?? "";
    expect(path).toContain("/api/v0/cloud/inventory");
    expect(path).toContain("account_id=111122223333");
    expect(path).toContain("cursor=50");
    expect(path).toContain("limit=100");
    expect(path).toContain("management_origin=observed");
    expect(path).toContain("provider=aws");
    expect(path).not.toContain("resource_type=");
  });

  it("propagates endpoint failures instead of fabricating canonical rows", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new Error("unsupported capability");
      })
    } as unknown as EshuApiClient;

    await expect(loadCloudInventory(client, { limit: 50 })).rejects.toThrow("unsupported capability");
  });

  it("propagates Eshu error envelopes instead of fabricating canonical rows", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "cloud inventory is unavailable in this profile",
          capability: "cloud_inventory.readback.list"
        },
        truth: null
      }))
    } as unknown as EshuApiClient;

    await expect(loadCloudInventory(client, { limit: 50 })).rejects.toThrow("unsupported_runtime_profile");
  });
});
