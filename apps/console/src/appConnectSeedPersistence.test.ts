// Integration proof for the seed trust anchor.
//
// Unlike the component tests, this exercises the REAL appBoot and REAL
// config/environment against jsdom localStorage, because the defect it guards
// is a read-after-write across those two modules: bootFromKey persists an
// operator-typed base even when the attempt fails, and a later boot reads that
// base back. A test that mocks either module cannot observe it.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { bootFromKey } from "./appBoot";
import { sameApiBase, seedRetryKey } from "./appConnectSeed";
import {
  consoleStorageKeys,
  defaultApiBaseUrl,
  loadConsoleEnvironment,
  saveConsoleEnvironment,
} from "./config/environment";

const buildSeed = "build-time-seed";
const hostileBase = "https://elsewhere.example/";

describe("seed trust anchor across a real persistence round-trip", () => {
  beforeEach(() => {
    window.localStorage.clear();
    // Every API call in this scenario is unauthenticated, so the console has
    // no browser session anywhere. 401 is what the real API returns.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 401 })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    window.localStorage.clear();
  });

  it("withholds the seed even after a failed attempt persists a hostile base", async () => {
    saveConsoleEnvironment({
      mode: "private",
      apiBaseUrl: defaultApiBaseUrl,
      apiKey: "",
      recentApiBaseUrls: [defaultApiBaseUrl],
    });

    // Step 1: the operator types a hostile origin and connects with no
    // credential. The attempt fails, and bootFromKey persists that base anyway.
    const attempt = await bootFromKey(hostileBase, "");
    expect(attempt).toBeNull();

    // Step 2: the saved environment is now poisoned. This is the precondition
    // the exploit needs, so assert it rather than assuming it.
    const poisoned = loadConsoleEnvironment();
    expect(poisoned.apiBaseUrl).toBe(hostileBase);
    expect(window.localStorage.getItem(consoleStorageKeys.environment)).toContain(hostileBase);

    // The counterfactual, stated so this test explains itself: anchoring the
    // comparison on the saved base would match here, which is precisely how
    // the seed would have been released to the hostile origin.
    expect(sameApiBase(poisoned.apiBaseUrl, poisoned.apiBaseUrl)).toBe(true);
    expect(sameApiBase(poisoned.apiBaseUrl, defaultApiBaseUrl)).toBe(false);

    // Step 3: the decision that matters. A boot or reconnect against the saved
    // base must still refuse the seed, because the saved base is not the base
    // this build ships with.
    expect(seedRetryKey({ base: poisoned.apiBaseUrl, key: "" }, { apiKey: buildSeed })).toBe("");

    // The build's own base still works, so refusing above is a trust decision
    // and not a blanket disabling of the fallback.
    expect(seedRetryKey({ base: defaultApiBaseUrl, key: "" }, { apiKey: buildSeed })).toBe(
      buildSeed,
    );
  });
});
