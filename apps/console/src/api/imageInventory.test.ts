import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadImages } from "./imageInventory";

// The image inventory loader must:
// - read GET /api/v0/images with limit/offset and optional exact-match filters
// - map snake_case API fields into camelCase view rows (registry/repository split
//   is server-side; size_bytes -> sizeBytes with null when absent)
// - surface next_cursor.offset only when the page is truncated
// - degrade to an "unavailable" page (not throw) when the endpoint fails
describe("loadImages", () => {
  it("maps a truncated page and exposes the next cursor offset", async () => {
    const requested: string[] = [];
    const client = {
      get: async (path: string) => {
        requested.push(path);
        return {
          data: {
            images: [
              {
                id: "oci-image://reg/team/api@sha256:aaa", digest: "sha256:aaa",
                repository_id: "oci-registry://reg/team/api", registry: "reg", repository: "team/api",
                name: "api", tag: "1.2.3", media_type: "application/vnd.oci.image.manifest.v1+json",
                config_digest: "sha256:cfg", size_bytes: 1234567, source_system: "oci_registry"
              },
              // a record with no id must be dropped so React keys stay stable
              { digest: "sha256:bbb" }
            ],
            count: 1, limit: 50, offset: 0, truncated: true, next_cursor: { offset: 50 }
          },
          error: null,
          truth: { profile: "production", level: "exact", capability: "platform_impact.container_image_list", freshness: { state: "fresh" } }
        };
      }
    } as unknown as EshuApiClient;

    const page = await loadImages(client, { limit: 50, offset: 0, repositoryId: "oci-registry://reg/team/api" });
    expect(page.images).toHaveLength(1);
    expect(page.images[0]).toMatchObject({
      id: "oci-image://reg/team/api@sha256:aaa", registry: "reg", repository: "team/api",
      tag: "1.2.3", sizeBytes: 1234567, mediaType: "application/vnd.oci.image.manifest.v1+json"
    });
    expect(page.nextOffset).toBe(50);
    expect(page.provenance).toBe("live");
    expect(page.truth?.level).toBe("exact");
    expect(requested[0]).toContain("/api/v0/images?");
    expect(requested[0]).toContain("limit=50");
    expect(requested[0]).toContain("repository_id=oci-registry");
  });

  it("returns null next cursor when the page is not truncated and null size when absent", async () => {
    const client = {
      get: async () => ({
        data: {
          images: [{ id: "img:1", digest: "sha256:ccc", repository: "team/svc" }],
          count: 1, limit: 50, offset: 0, truncated: false
        },
        error: null, truth: null
      })
    } as unknown as EshuApiClient;

    const page = await loadImages(client);
    expect(page.nextOffset).toBeNull();
    expect(page.images[0].sizeBytes).toBeNull();
    expect(page.provenance).toBe("live");
  });

  it("degrades to an unavailable page when the endpoint fails", async () => {
    const client = {
      get: async () => { throw new Error("HTTP 503"); }
    } as unknown as EshuApiClient;

    const page = await loadImages(client);
    expect(page.images).toEqual([]);
    expect(page.nextOffset).toBeNull();
    expect(page.provenance).toBe("unavailable");
  });

  it("degrades to an unavailable page when the endpoint returns an Eshu error envelope", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "image inventory is unavailable in this profile",
          capability: "platform_impact.container_image_list"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    const page = await loadImages(client);
    expect(page.images).toEqual([]);
    expect(page.nextOffset).toBeNull();
    expect(page.provenance).toBe("unavailable");
  });
});
