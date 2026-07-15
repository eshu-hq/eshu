import { describe, expect, it } from "vitest";

import { eshuDefaultTimeoutMs } from "../src/api/client";
import {
  apiQuietPolicy,
  awaitApiQuiet,
  filterAllowedResourceConsoleErrors,
  isConsoleApiUrl,
  traceCaptureEnabled,
} from "./liveE2EPolicy";

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

  it("keeps authenticated trace capture off unless explicitly enabled", () => {
    for (const value of [undefined, "", "0", "false", "no"]) {
      expect(traceCaptureEnabled(value)).toBe(false);
    }
    expect(traceCaptureEnabled("1")).toBe(true);
    expect(traceCaptureEnabled("true")).toBe(true);
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

  it("suppresses a generic resource error only when an exact allowed status was observed", () => {
    const errors = [
      "Failed to load resource: the server responded with a status of 401 (Unauthorized)",
      "Failed to load resource: the server responded with a status of 400 (Bad Request)",
    ];

    expect(filterAllowedResourceConsoleErrors(errors, [401])).toEqual([errors[1]]);
    expect(filterAllowedResourceConsoleErrors(errors, [])).toEqual(errors);
  });
});
