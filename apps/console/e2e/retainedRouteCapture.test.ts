import type { Page } from "playwright";
import { describe, expect, it, vi } from "vitest";

import {
  buildRouteReportObservation,
  consoleRoutes,
  type NetworkObservation,
} from "../src/e2e/routeAssertions";
import { RouteResponseEvidenceCache } from "./routeResponseEvidenceCache";
import { locatorStub } from "./routeWorkflowProbesTestSupport";
import { captureRoute } from "./runConsoleLiveE2E";

const inputState = {
  controls: [
    { identity: "aria-label:Entity type#0", kind: "select", value: "workload" },
    {
      identity: "aria-label:Entity target#1",
      kind: "text",
      value: "android-github-runner",
    },
  ],
  pathname: "/impact",
  search: "",
} as const;

function response(path: string): NetworkObservation {
  return {
    failureText: null,
    method: "POST",
    status: 200,
    url: `http://host/eshu-api${path}`,
  };
}

describe("captureRoute retained response evidence", () => {
  it("reports fresh then same-session cached proof for /impact,/impact", async () => {
    const route = consoleRoutes.find((candidate) => candidate.path === "/impact");
    if (route === undefined) throw new Error("Impact route is missing");
    const network: NetworkObservation[] = [];
    let navigationCount = 0;
    const truth = locatorStub({ count: vi.fn().mockResolvedValue(2) });
    const absent = locatorStub({ count: vi.fn().mockResolvedValue(0) });
    const page = {
      evaluate: vi.fn(async (_callback: unknown, argument?: string) => {
        if (argument !== undefined) {
          if (navigationCount === 0) {
            network.push(
              response("/api/v0/impact/change-surface/investigate"),
              response("/api/v0/impact/trace-deployment-chain"),
            );
          }
          navigationCount += 1;
          return undefined;
        }
        return {
          connected: true,
          demoBannerPresent: false,
          input: inputState,
          mainContentChars: 200,
          sourceMode: "live",
        };
      }),
      locator: vi.fn((selector: string) => (selector === ".impact-truth" ? truth : absent)),
      off: vi.fn(),
      on: vi.fn(),
      screenshot: vi.fn().mockResolvedValue(undefined),
      waitForFunction: vi.fn().mockResolvedValue(undefined),
      waitForSelector: vi.fn().mockResolvedValue(undefined),
      waitForTimeout: vi.fn().mockResolvedValue(undefined),
    } as unknown as Page;
    const cache = new RouteResponseEvidenceCache();
    const tracker = { inFlight: () => 0, lastChangeAt: () => Date.now() - 1_000 };

    const cold = await captureRoute(
      page,
      route,
      tracker,
      network,
      "/tmp/eshu-live-e2e-test-screenshots",
      [],
      null,
      undefined,
      undefined,
      undefined,
      cache,
    );
    const warm = await captureRoute(
      page,
      route,
      tracker,
      network,
      "/tmp/eshu-live-e2e-test-screenshots",
      [],
      null,
      undefined,
      undefined,
      undefined,
      cache,
    );

    const coldReport = buildRouteReportObservation(cold);
    const warmReport = buildRouteReportObservation(warm);
    expect(coldReport.workflow?.routeResponseEvidence).toBe("fresh");
    expect(warmReport.workflow?.routeResponseEvidence).toBe("same_session_cache");
    expect(JSON.stringify([coldReport, warmReport])).not.toContain("android-github-runner");
    expect(cold.network).toHaveLength(2);
    expect(warm.network).toHaveLength(0);
  });
});
