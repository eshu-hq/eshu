// Core builds its OIDC provider snapshot at boot and refreshes it in the
// background at most every 30 seconds, on a request. A provider created in the
// console after boot is therefore invisible to the first JWT request that
// reaches core, which answers `unauthenticated` and starts the refresh. These
// tests pin the one retry the E2E allows for that warm-up and prove nothing
// else is retried, so the retry cannot hide a real denial.
import { describe, expect, it } from "vitest";

import { isUnauthenticatedEnvelope, retryUntilAuthenticated } from "./authMcpE2EWarmup.ts";

const okEnvelope = { data: { repositories: [], total: 0 }, truth: {}, error: null };
const unauthenticated = { data: null, truth: null, error: { code: "unauthenticated", message: "authentication is required" } };
const forbidden = { data: null, truth: null, error: { code: "forbidden", message: "not allowed" } };

// recordingSleep records every delay it is asked for and returns at once.
function recordingSleep(): { sleep: (ms: number) => Promise<void>; delays: number[] } {
  const delays: number[] = [];
  return {
    delays,
    sleep: async (ms: number) => {
      delays.push(ms);
    },
  };
}

// sequence returns the envelopes in order, then keeps returning the last one.
function sequence(...envelopes: unknown[]): { call: () => Promise<unknown>; calls: () => number } {
  let n = 0;
  return {
    call: async () => envelopes[Math.min(n++, envelopes.length - 1)],
    calls: () => n,
  };
}

describe("isUnauthenticatedEnvelope", () => {
  it("recognises core's unauthenticated error and nothing else", () => {
    expect(isUnauthenticatedEnvelope(unauthenticated)).toBe(true);
    expect(isUnauthenticatedEnvelope(forbidden)).toBe(false);
    expect(isUnauthenticatedEnvelope(okEnvelope)).toBe(false);
    expect(isUnauthenticatedEnvelope(null)).toBe(false);
    expect(isUnauthenticatedEnvelope("unauthenticated")).toBe(false);
    expect(isUnauthenticatedEnvelope({ error: "unauthenticated" })).toBe(false);
  });
});

describe("retryUntilAuthenticated", () => {
  it("returns an authenticated first answer without sleeping", async () => {
    const { sleep, delays } = recordingSleep();
    const src = sequence(okEnvelope);
    const result = await retryUntilAuthenticated(src.call, { attempts: 5, delayMs: 2000, sleep });
    expect(result).toEqual({ structured: okEnvelope, attempts: 1 });
    expect(delays).toEqual([]);
  });

  it("retries while core answers unauthenticated, then returns the first authenticated answer", async () => {
    const { sleep, delays } = recordingSleep();
    const src = sequence(unauthenticated, unauthenticated, okEnvelope);
    const result = await retryUntilAuthenticated(src.call, { attempts: 5, delayMs: 2000, sleep });
    expect(result).toEqual({ structured: okEnvelope, attempts: 3 });
    expect(delays).toEqual([2000, 2000]);
  });

  it("gives up after the attempt budget and returns the last unauthenticated answer for the caller to fail on", async () => {
    const { sleep, delays } = recordingSleep();
    const src = sequence(unauthenticated);
    const result = await retryUntilAuthenticated(src.call, { attempts: 3, delayMs: 500, sleep });
    expect(result).toEqual({ structured: unauthenticated, attempts: 3 });
    expect(src.calls()).toBe(3);
    expect(delays).toEqual([500, 500]);
  });

  it("does not retry any other error", async () => {
    const { sleep, delays } = recordingSleep();
    const src = sequence(forbidden, okEnvelope);
    const result = await retryUntilAuthenticated(src.call, { attempts: 5, delayMs: 2000, sleep });
    expect(result).toEqual({ structured: forbidden, attempts: 1 });
    expect(src.calls()).toBe(1);
    expect(delays).toEqual([]);
  });

  it("rejects an attempt budget below one instead of never calling", async () => {
    const { sleep } = recordingSleep();
    await expect(retryUntilAuthenticated(async () => okEnvelope, { attempts: 0, delayMs: 1, sleep })).rejects.toThrow(/attempts/);
  });
});
