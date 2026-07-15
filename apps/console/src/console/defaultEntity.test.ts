import { describe, expect, it } from "vitest";

import { defaultChangedSinceParamsFromGenerations, defaultServiceName } from "./defaultEntity";
import { emptySnapshot, modelFromSnapshot } from "./liveModel";
import type { ConsoleModel, ServiceRow } from "./types";
import type { GenerationLifecycleRow } from "../api/changedSince";

function row(overrides: Partial<ServiceRow>): ServiceRow {
  return {
    id: "svc:1",
    name: "service-1",
    kind: "service",
    repo: "repo-1",
    environments: [],
    truth: "exact",
    freshness: "fresh",
    ...overrides,
  };
}

function modelWith(services: readonly ServiceRow[]): ConsoleModel {
  return modelFromSnapshot({ ...emptySnapshot("live"), services });
}

function generation(overrides: Partial<GenerationLifecycleRow>): GenerationLifecycleRow {
  return {
    activatedAt: null,
    collectorKind: "git",
    currentActiveGenerationId: "generation:current",
    freshnessHint: "",
    generationId: "generation:current",
    ingestedAt: null,
    isActive: true,
    latestFailure: "",
    observedAt: "2026-06-21T00:00:00Z",
    queueDeadLetter: 0,
    queueFailed: 0,
    queueInFlight: 0,
    queueOutstanding: 0,
    queueRetrying: 0,
    queueSucceeded: 1,
    queueTotal: 1,
    scopeId: "scope:repository:one",
    scopeKind: "repository",
    sourceSystem: "git",
    status: "active",
    supersededAt: null,
    triggerKind: "sync",
    ...overrides,
  };
}

describe("defaultServiceName", () => {
  it("returns an empty string when the catalog has no services", () => {
    expect(defaultServiceName(modelWith([]))).toBe("");
  });

  it("prefers a service-kind row over a workload-kind row", () => {
    const model = modelWith([
      row({ id: "w:1", name: "workload-1", kind: "workload" }),
      row({ id: "s:1", name: "service-1", kind: "service" }),
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
      row({ id: "s:1", name: "service-2", kind: "service" }),
    ]);
    expect(defaultServiceName(model)).toBe("service-2");
  });
});

describe("defaultChangedSinceParamsFromGenerations", () => {
  it("selects an exact active and prior generation pair from one repository scope", () => {
    const rows = [
      generation({ scopeId: "scope:without-prior" }),
      generation({ scopeId: "scope:usable" }),
      generation({
        scopeId: "scope:usable",
        generationId: "generation:prior",
        isActive: false,
        observedAt: "2026-06-20T00:00:00Z",
        status: "superseded",
      }),
    ];

    expect(defaultChangedSinceParamsFromGenerations(rows)).toEqual({
      scopeId: "scope:usable",
      sinceGenerationId: "generation:prior",
    });
  });

  it("fails closed when no repository has both an active and prior generation", () => {
    expect(defaultChangedSinceParamsFromGenerations([generation({})])).toBeNull();
  });

  it("uses a retained failed generation when it is the only exact prior baseline", () => {
    expect(
      defaultChangedSinceParamsFromGenerations([
        generation({}),
        generation({
          generationId: "generation:failed-prior",
          isActive: false,
          observedAt: "2026-06-20T00:00:00Z",
          status: "failed",
        }),
      ]),
    ).toEqual({
      scopeId: "scope:repository:one",
      sinceGenerationId: "generation:failed-prior",
    });
  });

  it("prefers a superseded baseline over a newer failed generation", () => {
    expect(
      defaultChangedSinceParamsFromGenerations([
        generation({}),
        generation({
          generationId: "generation:superseded-prior",
          isActive: false,
          observedAt: "2026-06-18T00:00:00Z",
          status: "superseded",
        }),
        generation({
          generationId: "generation:failed-newer",
          isActive: false,
          observedAt: "2026-06-20T00:00:00Z",
          status: "failed",
        }),
      ]),
    ).toEqual({
      scopeId: "scope:repository:one",
      sinceGenerationId: "generation:superseded-prior",
    });
  });

  it("ignores non-repository lifecycle rows", () => {
    expect(
      defaultChangedSinceParamsFromGenerations([
        generation({ scopeKind: "service" }),
        generation({
          scopeKind: "service",
          generationId: "generation:prior",
          isActive: false,
        }),
      ]),
    ).toBeNull();
  });
});
