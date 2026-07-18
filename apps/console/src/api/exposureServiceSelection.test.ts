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

  it("resolves a catalog display name to its canonical workload handle without an API call", async () => {
    const postJson = vi.fn();
    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      options: exposureServiceOptions(services),
      query: "Payments API",
    });

    expect(result).toMatchObject({
      option: { canonicalId: "workload:payments-api", displayName: "Payments API" },
      source: "catalog",
      status: "resolved",
    });
    expect(postJson).not.toHaveBeenCalled();
  });

  it("uses one bounded workload resolver call for a supported human alias", async () => {
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
      options: [],
      query: "payments",
    });

    expect(postJson).toHaveBeenCalledWith("/api/v0/entities/resolve", {
      limit: 10,
      name: "payments",
      type: "workload",
    });
    expect(result).toMatchObject({
      option: { canonicalId: "workload:payments-api", displayName: "Payments API" },
      source: "resolver",
      status: "resolved",
    });
  });

  it("refuses to guess when a display name maps to multiple canonical workloads", async () => {
    const result = await resolveExposureServiceSelection({
      client: { postJson: vi.fn() } as unknown as EshuApiClient,
      options: exposureServiceOptions([
        { id: "workload:payments-us", kind: "service", name: "Payments", repo: "payments-us" },
        { id: "workload:payments-eu", kind: "service", name: "Payments", repo: "payments-eu" },
      ]),
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
      options: [],
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
      options: [],
      query: "restricted-service",
    });

    expect(result).toEqual({ query: "restricted-service", status: "not_authorized" });
  });

  it("accepts an explicitly pasted canonical workload handle without guessing", async () => {
    const postJson = vi.fn();
    const result = await resolveExposureServiceSelection({
      client: { postJson } as unknown as EshuApiClient,
      options: [],
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
