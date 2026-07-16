import { describe, expect, it, vi } from "vitest";

import { eshuDefaultTimeoutMs } from "../src/api/client";
import {
  apiQuietPolicy,
  awaitApiQuiet,
  filterAllowedResourceConsoleErrors,
  installNetworkObservationRecorder,
  isConsoleApiUrl,
  navigateClientRoute,
  traceCaptureEnabled,
} from "./liveE2EPolicy";
import { consoleRoutes } from "../src/e2e/consoleRouteCatalog";
import {
  authCoverageReport,
  createAuthRouteCoverage,
  formatAuthRouteCoverage,
} from "./liveE2EAuthCoverage";

describe("live E2E policy", () => {
  it("waits beyond the shared API client timeout before leaving a route", () => {
    expect(eshuDefaultTimeoutMs).toBe(15_000);
    expect(apiQuietPolicy.maxWaitMs).toBeGreaterThan(eshuDefaultTimeoutMs);
    expect(apiQuietPolicy.maxWaitMs - eshuDefaultTimeoutMs).toBeGreaterThanOrEqual(2_000);
    expect(apiQuietPolicy.pollMs).toBeLessThan(apiQuietPolicy.quietWindowMs);
  });

  it("classifies only proxied console API requests", () => {
    expect(isConsoleApiUrl("http://127.0.0.1:5180/eshu-api/api/v0/status")).toBe(true);
    expect(isConsoleApiUrl("http://127.0.0.1:5180/src/App.tsx")).toBe(false);
  });

  it("records completed and failed requests and detaches both listeners", () => {
    const listeners = new Map<string, (event: never) => void>();
    const on = vi.fn((event: string, listener: (event: never) => void) => {
      listeners.set(event, listener);
    });
    const off = vi.fn((event: string) => {
      listeners.delete(event);
    });
    const recorder = installNetworkObservationRecorder({ on, off } as never);

    listeners.get("response")?.({
      url: () => "http://127.0.0.1/eshu-api/api/v0/code/dead-code",
      status: () => 200,
      request: () => ({ method: () => "POST" }),
    } as never);
    listeners.get("requestfailed")?.({
      url: () => "http://127.0.0.1/eshu-api/api/v0/code/relationships",
      method: () => "POST",
      failure: () => ({ errorText: "net::ERR_ABORTED" }),
    } as never);

    expect(recorder.observations).toEqual([
      {
        url: "http://127.0.0.1/eshu-api/api/v0/code/dead-code",
        method: "POST",
        status: 200,
        failureText: null,
      },
      {
        url: "http://127.0.0.1/eshu-api/api/v0/code/relationships",
        method: "POST",
        status: 0,
        failureText: "net::ERR_ABORTED",
      },
    ]);

    recorder.stop();
    expect(off).toHaveBeenCalledTimes(2);
    expect(listeners).toHaveLength(0);
  });

  it("keeps authenticated trace capture off unless explicitly enabled", () => {
    for (const value of [undefined, "", "0", "false", "no"]) {
      expect(traceCaptureEnabled(value)).toBe(false);
    }
    expect(traceCaptureEnabled("1")).toBe(true);
    expect(traceCaptureEnabled("true")).toBe(true);
  });

  it("keeps browser-session-only surfaces out of the bearer verdict without counting them as passes", () => {
    const coverage = createAuthRouteCoverage(consoleRoutes, "bearer");

    expect(coverage.eligibleRoutes).toHaveLength(37);
    expect(coverage.excludedByAuth).toEqual([
      { path: "/profile", requiredAuthMode: "browser_session" },
      { path: "/admin", requiredAuthMode: "browser_session" },
    ]);
    expect(coverage.eligibleRoutes.some((route) => route.path === "/profile")).toBe(false);
    expect(coverage.eligibleRoutes.some((route) => route.path === "/admin")).toBe(false);
    expect(authCoverageReport(coverage)).toMatchObject({
      authMode: "bearer",
      catalogRouteCount: 39,
      eligibleRouteCount: 37,
    });
    expect(formatAuthRouteCoverage(coverage)).toContain(
      "37/39; 2 browser-session route(s) excluded from this verdict",
    );
  });

  it("runs every bearer-capable workflow through the browser-session verdict", () => {
    const coverage = createAuthRouteCoverage(consoleRoutes, "browser_session");

    expect(coverage.eligibleRoutes).toHaveLength(39);
    expect(coverage.excludedByAuth).toEqual([]);
    expect(coverage.eligibleRoutes.map((route) => route.path)).toEqual(
      consoleRoutes.map((route) => route.path),
    );
    expect(authCoverageReport(coverage)).toMatchObject({
      authMode: "browser_session",
      catalogRouteCount: 39,
      eligibleRouteCount: 39,
    });
  });

  it("navigates within the connected app before waiting for the route", async () => {
    const calls: string[] = [];

    await navigateClientRoute(
      "/topology",
      async (path) => {
        calls.push(`push:${path}`);
      },
      async (path) => {
        calls.push(`wait:${path}`);
      },
    );

    expect(calls).toEqual(["push:/topology", "wait:/topology"]);
  });

  it("extends request ownership from the latest activity and reports an explicit timeout", async () => {
    let now = 0;
    let inFlight = 1;
    let lastChangeAt = 0;
    const resultPromise = awaitApiQuiet(
      { inFlight: () => inFlight, lastChangeAt: () => lastChangeAt },
      async (duration) => {
        now += duration;
        if (now === apiQuietPolicy.maxWaitMs - 100) {
          lastChangeAt = now;
        }
      },
      () => now,
    );
    const result = await resultPromise;

    expect(result.settled).toBe(false);
    expect(result.inFlight).toBe(1);
    expect(result.waitedMs).toBeGreaterThan(apiQuietPolicy.maxWaitMs);
  });

  it("terminates when continuous API activity keeps resetting request ownership", async () => {
    let now = 0;
    let lastChangeAt = 0;
    const result = await awaitApiQuiet(
      { inFlight: () => 1, lastChangeAt: () => lastChangeAt },
      async (duration) => {
        now += duration;
        lastChangeAt = now;
      },
      () => now,
    );

    expect(result.settled).toBe(false);
    expect(result.inFlight).toBe(1);
    expect(result.waitedMs).toBe(apiQuietPolicy.absoluteMaxWaitMs);
  });

  it("suppresses a generic resource error only when an exact allowed status was observed", () => {
    const errors = [
      "Failed to load resource: the server responded with a status of 401 (Unauthorized)",
      "Failed to load resource: the server responded with a status of 400 (Bad Request)",
    ];

    expect(filterAllowedResourceConsoleErrors(errors, [401])).toEqual([errors[1]]);
    expect(filterAllowedResourceConsoleErrors(errors, [])).toEqual(errors);
  });
});
