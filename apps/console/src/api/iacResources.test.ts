import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { iacResourcesPath, loadIacResourcesPage } from "./iacResources";

function envelope(
  resources: readonly Record<string, unknown>[],
  opts: { readonly truncated?: boolean } = {},
) {
  return {
    data: {
      count: resources.length,
      kind: "resource",
      limit: 50,
      resources,
      truncated: opts.truncated === true,
      next_cursor:
        opts.truncated === true ? { after_name: "aws_s3_bucket.logs", after_id: "r2" } : undefined,
    },
    error: null,
    truth: {
      capability: "iac_inventory.resources.list",
      freshness: { state: "fresh" },
      level: "derived",
      profile: "production",
    },
  };
}

describe("iacResources", () => {
  it("builds the bounded keyset path without offset pagination", () => {
    const path = iacResourcesPath({
      cursor: { afterId: "content-entity:e1", afterName: "aws_iam_role.app" },
      kind: "resource",
      limit: 25,
      module: "api-node",
      provider: "aws",
      type: "aws_iam_role",
    });

    expect(path).toBe(
      "/api/v0/iac/resources?limit=25&kind=resource&type=aws_iam_role&provider=aws&module=api-node&after_name=aws_iam_role.app&after_id=content-entity%3Ae1",
    );
    expect(path).not.toContain("offset");
  });

  it("binds full-inventory search, repository, and authoritative facets", () => {
    expect(
      iacResourcesPath({
        includeFacets: true,
        kind: "data-source",
        limit: 50,
        query: "caller identity",
        repository: "repository:r1",
      }),
    ).toBe(
      "/api/v0/iac/resources?limit=50&kind=data-source&q=caller+identity&repository=repository%3Ar1&include_facets=true",
    );
  });

  it("maps IaC resource rows, paging metadata, and truth from the envelope", async () => {
    const get = vi.fn(async () =>
      envelope(
        [
          {
            id: "content-entity:e1",
            kind: "resource",
            name: "aws_s3_bucket.logs",
            resource_name: "logs",
            type: "aws_s3_bucket",
            provider: "aws",
            resource_service: "s3",
            resource_category: "storage",
            module: "audit",
            repo_id: "repository:r1",
            relative_path: "logging.tf",
            line_number: 12,
          },
        ],
        { truncated: true },
      ),
    );
    const client = { get } as unknown as EshuApiClient;

    const page = await loadIacResourcesPage(client, { limit: 50 });

    expect(get).toHaveBeenCalledWith("/api/v0/iac/resources?limit=50");
    expect(page).toMatchObject({
      count: 1,
      kind: "resource",
      limit: 50,
      nextCursor: { afterName: "aws_s3_bucket.logs", afterId: "r2" },
      truncated: true,
      truth: {
        freshness: "fresh",
        level: "derived",
        profile: "production",
      },
    });
    expect(page.rows[0]).toMatchObject({
      category: "storage",
      lineNumber: 12,
      name: "aws_s3_bucket.logs",
      resourceName: "logs",
      service: "s3",
    });
  });

  it("maps authoritative totals and bounded selector facets", async () => {
    const client = {
      get: vi.fn(async () => ({
        ...envelope([]),
        data: {
          ...envelope([]).data,
          summary: {
            total: 24610,
            by_kind: { resource: 17117, module: 612, "data-source": 6881 },
            types: [{ kind: "resource", value: "aws_s3_bucket", count: 500 }],
            providers: [{ kind: "resource", value: "aws", count: 1000 }],
            modules: [{ value: "vpc", count: 25 }],
            repositories: [{ value: "repository:r1", count: 100 }],
            facet_limit: 200,
            truncated: { types: true },
          },
        },
      })),
    } as unknown as EshuApiClient;

    const page = await loadIacResourcesPage(client, { includeFacets: true, limit: 50 });

    expect(page.summary).toMatchObject({
      total: 24610,
      byKind: { resource: 17117, module: 612, "data-source": 6881 },
      types: [{ kind: "resource", value: "aws_s3_bucket", count: 500 }],
      truncated: { types: true },
    });
  });

  it("does not fabricate authoritative zero counts from a partial summary", async () => {
    const client = {
      get: vi.fn(async () => ({
        ...envelope([]),
        data: {
          ...envelope([]).data,
          summary: {
            by_kind: { resource: 10, module: 6, "data-source": 2 },
            types: [],
            providers: [],
            modules: [],
            repositories: [],
            facet_limit: 200,
            truncated: {},
          },
        },
      })),
    } as unknown as EshuApiClient;

    const page = await loadIacResourcesPage(client, { includeFacets: true, limit: 50 });

    expect(page.summary).toBeNull();
  });

  it("does not fabricate hybrid inventory truth when metadata is absent", async () => {
    const client = {
      get: vi.fn(async () => ({ ...envelope([]), truth: null })),
    } as unknown as EshuApiClient;

    const page = await loadIacResourcesPage(client, { limit: 50 });

    expect(page.truth).toBeNull();
  });

  it("propagates Eshu error envelopes instead of fabricating an empty inventory", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "IaC resource inventory is unavailable in this profile",
          capability: "iac_inventory.resources.list",
        },
        truth: null,
      })),
    } as unknown as EshuApiClient;

    await expect(loadIacResourcesPage(client, { limit: 50 })).rejects.toThrow(
      "unsupported_runtime_profile",
    );
  });

  it("passes the caller abort signal to the bounded API request", async () => {
    const get = vi.fn(async () => envelope([]));
    const client = { get } as unknown as EshuApiClient;
    const controller = new AbortController();

    await loadIacResourcesPage(client, { limit: 50 }, { signal: controller.signal });

    expect(get).toHaveBeenCalledWith("/api/v0/iac/resources?limit=50", {
      signal: controller.signal,
    });
  });
});
