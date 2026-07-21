import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import { consoleRoutes, type NetworkObservation } from "../src/e2e/routeAssertions";
import {
  RouteResponseEvidenceCache,
  routeResponseEvidenceKey,
  type RouteInputState,
} from "./routeResponseEvidenceCache";
import { executeRouteWorkflow } from "./routeWorkflowProbes";
import { locatorStub } from "./routeWorkflowProbesTestSupport";

const impactNetwork = [
  successfulResponse("/api/v0/impact/change-surface/investigate"),
  successfulResponse("/api/v0/impact/trace-deployment-chain"),
] as const;

const impactInput: RouteInputState = {
  controls: [
    { identity: "aria-label:Entity type", kind: "select", value: "workload" },
    { identity: "aria-label:Entity target", kind: "text", value: "android-github-runner" },
    { identity: "aria-label:Repository scope", kind: "text", value: "repository:r_mobile" },
    { identity: "aria-label:Environment", kind: "text", value: "production" },
  ],
  pathname: "/impact",
  search: "",
};

function successfulResponse(path: string): NetworkObservation {
  return {
    failureText: null,
    method: "POST",
    status: 200,
    url: `http://host/eshu-api${path}`,
  };
}

function impactWorkflow() {
  const workflow = consoleRoutes.find((route) => route.path === "/impact")?.workflow;
  if (!workflow || workflow.kind !== "state") {
    throw new Error("Impact state workflow is missing");
  }
  return workflow;
}

function impactPage(): Page {
  const main = locatorStub();
  const truth = locatorStub({ count: vi.fn().mockResolvedValue(2) });
  const absent = locatorStub({ count: vi.fn().mockResolvedValue(0) });
  return {
    locator: vi.fn((selector: string) => {
      if (selector === ".main") return main;
      if (selector === ".impact-truth") return truth;
      return absent;
    }),
  } as unknown as Page;
}

describe("same-session route response evidence", () => {
  it("proves a cold then warm repeated Impact route from one successful response set", async () => {
    const workflow = impactWorkflow();
    const cache = new RouteResponseEvidenceCache();
    const key = routeResponseEvidenceKey("/impact", workflow.id, impactInput);

    const coldEvidence = cache.select(key, impactNetwork);
    const cold = await executeRouteWorkflow(
      impactPage(),
      workflow,
      vi.fn(),
      coldEvidence.network,
      [],
      undefined,
      null,
      undefined,
      coldEvidence.source,
    );
    cache.remember(key, coldEvidence.network, cold.passed);

    const warmEvidence = cache.select(key, []);
    const warm = await executeRouteWorkflow(
      impactPage(),
      workflow,
      vi.fn(),
      warmEvidence.network,
      [],
      undefined,
      null,
      undefined,
      warmEvidence.source,
    );

    expect(cold).toMatchObject({ passed: true, routeResponseEvidence: "fresh" });
    expect(warm).toMatchObject({ passed: true, routeResponseEvidence: "same_session_cache" });
    expect(warm.requests).toHaveLength(2);
  });

  it("keeps a changed Impact input strict instead of reusing another scope", async () => {
    const workflow = impactWorkflow();
    const cache = new RouteResponseEvidenceCache();
    const originalKey = routeResponseEvidenceKey("/impact", workflow.id, impactInput);
    cache.remember(originalKey, impactNetwork, true);
    const changedKey = routeResponseEvidenceKey("/impact", workflow.id, {
      ...impactInput,
      controls: impactInput.controls.map((control) =>
        control.identity === "aria-label:Environment" ? { ...control, value: "staging" } : control,
      ),
    });

    const evidence = cache.select(changedKey, []);
    const result = await executeRouteWorkflow(
      impactPage(),
      workflow,
      vi.fn(),
      evidence.network,
      [],
      undefined,
      null,
      undefined,
      evidence.source,
    );

    expect(changedKey).not.toBe(originalKey);
    expect(evidence.source).toBe("fresh");
    expect(result.passed).toBe(false);
    expect(result.detail).toContain("required route response");
  });

  it("keys an exact URL query independently from matching rendered controls", () => {
    const base = routeResponseEvidenceKey("/impact", "impact-live-evidence", impactInput);
    const deepLink = routeResponseEvidenceKey("/impact", "impact-live-evidence", {
      ...impactInput,
      search: "?kind=workload&target=android-github-runner",
    });

    expect(deepLink).not.toBe(base);
  });

  it("does not carry forward a failed response set", async () => {
    const workflow = impactWorkflow();
    const cache = new RouteResponseEvidenceCache();
    const key = routeResponseEvidenceKey("/impact", workflow.id, impactInput);
    const failedNetwork = impactNetwork.map((response) => ({ ...response, status: 500 }));
    const failed = await executeRouteWorkflow(impactPage(), workflow, vi.fn(), failedNetwork);
    cache.remember(key, failedNetwork, failed.passed);

    expect(failed.passed).toBe(false);
    expect(cache.select(key, [])).toEqual({ network: [], source: "fresh" });
  });

  it("prefers any fresh capture over cached evidence without blending the sets", () => {
    const cache = new RouteResponseEvidenceCache();
    const key = routeResponseEvidenceKey("/impact", "impact-live-evidence", impactInput);
    cache.remember(key, impactNetwork, true);
    const partialFresh = [impactNetwork[0]];

    expect(cache.select(key, partialFresh)).toEqual({
      network: partialFresh,
      source: "fresh",
    });
  });

  it("invalidates older evidence after a fresh probe fails", () => {
    const cache = new RouteResponseEvidenceCache();
    const key = routeResponseEvidenceKey("/impact", "impact-live-evidence", impactInput);
    cache.remember(key, impactNetwork, true);

    cache.remember(key, [impactNetwork[0]], false);

    expect(cache.select(key, [])).toEqual({ network: [], source: "fresh" });
  });
});
