import { describe, expect, it } from "vitest";

import { defaultChangedSinceParams, defaultServiceName } from "./defaultEntity";
import { emptySnapshot, modelFromSnapshot } from "./liveModel";
import type { ConsoleModel, ServiceRow } from "./types";

function row(overrides: Partial<ServiceRow>): ServiceRow {
  return {
    id: "svc:1",
    name: "service-1",
    kind: "service",
    repo: "repo-1",
    environments: [],
    truth: "exact",
    freshness: "fresh",
    ...overrides
  };
}

function modelWith(services: readonly ServiceRow[]): ConsoleModel {
  return modelFromSnapshot({ ...emptySnapshot("live"), services });
}

describe("defaultServiceName", () => {
  it("returns an empty string when the catalog has no services", () => {
    expect(defaultServiceName(modelWith([]))).toBe("");
  });

  it("prefers a service-kind row over a workload-kind row", () => {
    const model = modelWith([
      row({ id: "w:1", name: "workload-1", kind: "workload" }),
      row({ id: "s:1", name: "service-1", kind: "service" })
    ]);
    expect(defaultServiceName(model)).toBe("service-1");
  });

  it("falls back to the first row when no service-kind row exists", () => {
    const model = modelWith([row({ id: "w:1", name: "workload-1", kind: "workload" })]);
    expect(defaultServiceName(model)).toBe("workload-1");
  });

  it("skips rows with an empty name", () => {
    const model = modelWith([
      row({ id: "s:0", name: "  ", kind: "service" }),
      row({ id: "s:1", name: "service-2", kind: "service" })
    ]);
    expect(defaultServiceName(model)).toBe("service-2");
  });
});

describe("defaultChangedSinceParams", () => {
  const now = new Date("2026-06-21T00:00:00.000Z");

  it("returns null when no service carries a repository", () => {
    const model = modelWith([row({ repo: "" })]);
    expect(defaultChangedSinceParams(model, now)).toBeNull();
  });

  it("defaults to the first repository with a seven-day observed-at window", () => {
    const model = modelWith([row({ repo: "repo-alpha" })]);
    const params = defaultChangedSinceParams(model, now);
    expect(params).toEqual({
      repository: "repo-alpha",
      sinceObservedAt: "2026-06-14T00:00:00.000Z"
    });
  });

  it("skips rows without a repository to find the first usable scope", () => {
    const model = modelWith([
      row({ id: "s:0", repo: "  " }),
      row({ id: "s:1", repo: "repo-beta" })
    ]);
    expect(defaultChangedSinceParams(model, now)?.repository).toBe("repo-beta");
  });
});
