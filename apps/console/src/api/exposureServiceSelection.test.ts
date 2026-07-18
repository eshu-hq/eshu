import { describe, expect, it, vi } from "vitest";

import { EshuApiHttpError, type EshuApiClient } from "./client";
import {
  exposureServiceOptions,
  filterExposureServiceOptions,
  resolveExposureServiceSelection,
} from "./exposureServiceSelection";

describe("Exposure service selection", () => {
  const services = [
    {
      id: "workload:checkout-api",
      kind: "service",
      name: "Checkout API",
      repo: "checkout-service",
    },
    {
      id: "workload:payments-api",
      kind: "Deployment",
      name: "Payments API",
      repo: "payments-service",
    },
  ];

  it("builds searchable canonical options without treating repository context as an alias", () => {
    const options = exposureServiceOptions(services);

    expect(options).toEqual([
      {
        aliases: ["checkout-api"],
        canonicalId: "workload:checkout-api",
        displayName: "Checkout API",
        kind: "service",
        repoName: "checkout-service",
      },
      {
        aliases: ["payments-api"],
        canonicalId: "workload:payments-api",
        displayName: "Payments API",
        kind: "Deployment",
        repoName: "payments-service",
      },
    ]);
    expect(filterExposureServiceOptions(options, "payments-service")).toEqual([options[1]]);
  });

  it("resolves an exact catalog display name through the authoritative bounded resolver", async () => {
    const postJson = vi.fn(async () => ({
      count: 1,
      entities: [
        {
          id: "workload:payments-api",
          labels: ["Workload"],
          name: "Payments API",
          repo_id: "repository:r_payments",
          repo_name: "payments-service",
        },
      ],
      limit: 10,
      truncated: false,
    }));
    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      query: "Payments API",
    });

    expect(result).toMatchObject({
      option: { canonicalId: "workload:payments-api", displayName: "Payments API" },
      source: "resolver",
      status: "resolved",
    });
    expect(postJson).toHaveBeenCalledWith("/api/v0/entities/resolve", {
      limit: 10,
      name: "Payments API",
      type: "workload",
    });
  });

  it("uses one bounded workload resolver call for free text", async () => {
    const postJson = vi.fn(async () => ({
      count: 1,
      entities: [
        {
          id: "workload:payments-api",
          labels: ["Workload"],
          name: "payments",
          repo_id: "repository:r_payments",
          repo_name: "payments-service",
        },
      ],
      limit: 10,
      truncated: false,
    }));

    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      query: "payments",
    });

    expect(postJson).toHaveBeenCalledWith("/api/v0/entities/resolve", {
      limit: 10,
      name: "payments",
      type: "workload",
    });
    expect(result).toMatchObject({
      option: { canonicalId: "workload:payments-api", displayName: "payments" },
      source: "resolver",
      status: "resolved",
    });
  });

  it("submits an exact catalog handle alias as its authorized canonical workload", async () => {
    const postJson = vi.fn(async () => ({ count: 0, entities: [], limit: 10, truncated: false }));
    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      options: exposureServiceOptions(services),
      query: "payments-api",
    });

    expect(result).toMatchObject({
      option: { canonicalId: "workload:payments-api", displayName: "Payments API" },
      source: "catalog_alias",
      status: "resolved",
    });
    expect(postJson).not.toHaveBeenCalled();
  });

  it("passes the caller abort signal into the bounded resolver request", async () => {
    const controller = new AbortController();
    const postJson = vi.fn(async () => ({
      count: 0,
      entities: [],
      limit: 10,
      truncated: false,
    }));

    await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      query: "checkout",
      signal: controller.signal,
    });

    expect(postJson).toHaveBeenCalledWith(
      "/api/v0/entities/resolve",
      { limit: 10, name: "checkout", type: "workload" },
      { signal: controller.signal },
    );
  });

  it("refuses to auto-select a single visible candidate from a truncated resolver page", async () => {
    const result = await resolveExposureServiceSelection({
      client: {
        postJson: vi.fn(async () => ({
          count: 11,
          entities: [
            {
              id: "workload:payments-api",
              labels: ["Workload"],
              name: "Payments API",
              repo_id: "repository:r_payments",
              repo_name: "payments-service",
            },
          ],
          limit: 1,
          truncated: true,
        })),
      } as unknown as EshuApiClient,
      limit: 1,
      query: "payments",
    });

    expect(result).toEqual({
      candidates: [
        {
          aliases: [],
          canonicalId: "workload:payments-api",
          displayName: "Payments API",
          kind: "workload",
          repoName: "payments-service",
        },
      ],
      query: "payments",
      status: "ambiguous",
      truncated: true,
    });
  });

  it("refuses to guess when a display name maps to multiple canonical workloads", async () => {
    const result = await resolveExposureServiceSelection({
      client: {
        postJson: vi.fn(async () => ({
          count: 2,
          entities: [
            {
              id: "workload:payments-us",
              labels: ["Workload"],
              name: "Payments",
              repo_name: "payments-us",
            },
            {
              id: "workload:payments-eu",
              labels: ["Workload"],
              name: "Payments",
              repo_name: "payments-eu",
            },
          ],
          limit: 10,
          truncated: false,
        })),
      } as unknown as EshuApiClient,
      query: "Payments",
    });

    expect(result.status).toBe("ambiguous");
    if (result.status === "ambiguous") {
      expect(result.candidates.map((candidate) => candidate.canonicalId)).toEqual([
        "workload:payments-eu",
        "workload:payments-us",
      ]);
    }
  });

  it("returns a precise no-match state when the bounded resolver has no workload", async () => {
    const result = await resolveExposureServiceSelection({
      client: {
        postJson: vi.fn(async () => ({ count: 0, entities: [], limit: 10, truncated: false })),
      } as unknown as EshuApiClient,
      query: "missing-service",
    });

    expect(result).toEqual({ query: "missing-service", status: "not_found" });
  });

  it("distinguishes an authorization rejection from no match", async () => {
    const result = await resolveExposureServiceSelection({
      client: {
        postJson: vi.fn(async () => {
          throw new EshuApiHttpError(403);
        }),
      } as unknown as EshuApiClient,
      query: "restricted-service",
    });

    expect(result).toEqual({ query: "restricted-service", status: "not_authorized" });
  });

  it("accepts an explicitly pasted canonical workload handle without guessing", async () => {
    const postJson = vi.fn();
    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      query: "workload:external-api",
    });

    expect(result).toMatchObject({
      option: {
        canonicalId: "workload:external-api",
        displayName: "external-api",
      },
      source: "canonical_handle",
      status: "resolved",
    });
    expect(postJson).not.toHaveBeenCalled();
  });
});
