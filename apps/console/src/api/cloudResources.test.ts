import { describe, expect, it, vi } from "vitest";
import { loadCloudResources } from "./cloudResources";
import type { EshuApiClient } from "./client";

// The cloud-resource adapter must:
// - map the real GET /api/v0/cloud/resources envelope into typed rows
// - forward provider/resource_type/region/account_id filters and the keyset
//   cursor as query params (after_resource_type + after_id), not SKIP/offset
// - surface next_cursor + truncated for pagination
// - capture truth.level / freshness so the page can render chips
// - never fabricate rows: an unavailable endpoint propagates as a thrown error
describe("loadCloudResources", () => {
  function fakeClient(capture: { path?: string }): EshuApiClient {
    return {
      get: async (path: string) => {
        capture.path = path;
        return {
          data: {
            resources: [
              {
                id: "aws:iam-role:r1",
                resource_type: "aws_iam_role",
                name: "role-a",
                provider: "aws",
                region: "us-east-1",
                account_id: "123456789012",
                arn: "arn:aws:iam::123456789012:role/role-a",
                service_name: "iam",
                state: "active"
              }
            ],
            count: 1,
            limit: 50,
            truncated: true,
            next_cursor: { after_resource_type: "aws_iam_role", after_id: "aws:iam-role:r1" },
            scope: { provider: "aws" }
          },
          error: null,
          truth: {
            profile: "production",
            level: "exact",
            capability: "platform_impact.cloud_resource_list",
            freshness: { state: "fresh" }
          }
        };
      }
    } as unknown as EshuApiClient;
  }

  it("maps the envelope into typed rows with truth and pagination", async () => {
    const capture: { path?: string } = {};
    const page = await loadCloudResources(fakeClient(capture), { limit: 50 });

    expect(page.rows).toHaveLength(1);
    expect(page.rows[0]).toMatchObject({
      id: "aws:iam-role:r1",
      resourceType: "aws_iam_role",
      name: "role-a",
      provider: "aws",
      region: "us-east-1",
      accountId: "123456789012",
      serviceName: "iam",
      state: "active"
    });
    expect(page.truncated).toBe(true);
    expect(page.nextCursor).toEqual({ afterResourceType: "aws_iam_role", afterId: "aws:iam-role:r1" });
    expect(page.truth.level).toBe("exact");
    expect(page.truth.freshness).toBe("fresh");
  });

  it("forwards filters and keyset cursor as query params, never offset", async () => {
    const capture: { path?: string } = {};
    await loadCloudResources(fakeClient(capture), {
      limit: 25,
      provider: "aws",
      resourceType: "aws_s3_bucket",
      region: "us-west-2",
      accountId: "1234",
      cursor: { afterResourceType: "aws_iam_role", afterId: "aws:iam-role:r1" }
    });

    const path = capture.path ?? "";
    expect(path).toContain("/api/v0/cloud/resources");
    expect(path).toContain("limit=25");
    expect(path).toContain("provider=aws");
    expect(path).toContain("resource_type=aws_s3_bucket");
    expect(path).toContain("region=us-west-2");
    expect(path).toContain("account_id=1234");
    expect(path).toContain("after_resource_type=aws_iam_role");
    expect(path).toContain("after_id=aws%3Aiam-role%3Ar1");
    expect(path).not.toContain("offset");
    expect(path).not.toContain("skip");
  });

  it("omits empty filters from the query string", async () => {
    const capture: { path?: string } = {};
    await loadCloudResources(fakeClient(capture), { limit: 50 });
    const path = capture.path ?? "";
    expect(path).not.toContain("provider=");
    expect(path).not.toContain("after_id=");
  });

  it("omits an incomplete keyset cursor", async () => {
    const capture: { path?: string } = {};
    await loadCloudResources(fakeClient(capture), {
      limit: 50,
      cursor: { afterResourceType: "", afterId: "aws:iam-role:r1" }
    });
    const path = capture.path ?? "";
    expect(path).not.toContain("after_resource_type=");
    expect(path).not.toContain("after_id=");
  });

  it("propagates a thrown error rather than fabricating an empty page", async () => {
    const client = {
      get: vi.fn(async () => {
        throw new Error("HTTP 503");
      })
    } as unknown as EshuApiClient;
    await expect(loadCloudResources(client, { limit: 50 })).rejects.toThrow("HTTP 503");
  });

  it("propagates an Eshu error envelope rather than rendering an empty page", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "cloud resource list is unavailable in this profile",
          capability: "platform_impact.cloud_resource_list"
        },
        truth: null
      }))
    } as unknown as EshuApiClient;

    await expect(loadCloudResources(client, { limit: 50 })).rejects.toThrow("unsupported_runtime_profile");
  });
});
