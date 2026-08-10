import { describe, expect, it } from "vitest";

import { sameApiBase, seedRetryKey } from "./appConnectSeed";

const configured = { apiBaseUrl: "/eshu-api/", apiKey: "configured-shared-key" };

describe("sameApiBase", () => {
  it("ignores a trailing slash and surrounding whitespace", () => {
    expect(sameApiBase("/eshu-api", "/eshu-api/")).toBe(true);
    expect(sameApiBase("  /eshu-api/  ", "/eshu-api")).toBe(true);
  });

  it("treats a different origin as different", () => {
    expect(sameApiBase("https://elsewhere.example/", "/eshu-api/")).toBe(false);
    expect(sameApiBase("http://127.0.0.1:9999/", "http://127.0.0.1:8080/")).toBe(false);
  });

  it("never matches an empty base", () => {
    expect(sameApiBase("", "")).toBe(false);
    expect(sameApiBase("   ", "/eshu-api/")).toBe(false);
  });
});

describe("seedRetryKey", () => {
  it("returns the seed for a credential-less attempt against the configured base", () => {
    expect(seedRetryKey({ base: "/eshu-api/", key: "" }, configured)).toBe("configured-shared-key");
  });

  it("tolerates a trailing-slash difference against the configured base", () => {
    expect(seedRetryKey({ base: "/eshu-api", key: "" }, configured)).toBe("configured-shared-key");
  });

  // The popover lets an operator type any origin. Handing the build-time seed
  // to one would send `Authorization: Bearer <seed>` to a server the operator
  // never configured.
  it("never returns the seed for a base the operator typed", () => {
    expect(seedRetryKey({ base: "https://elsewhere.example/", key: "" }, configured)).toBe("");
    expect(seedRetryKey({ base: "http://127.0.0.1:9999/", key: "" }, configured)).toBe("");
  });

  it("returns nothing when the attempt already carried a key", () => {
    expect(seedRetryKey({ base: "/eshu-api/", key: "some-key" }, configured)).toBe("");
  });

  it("returns nothing when no seed is configured", () => {
    expect(seedRetryKey({ base: "/eshu-api/", key: "" }, { ...configured, apiKey: "" })).toBe("");
    expect(seedRetryKey({ base: "/eshu-api/", key: "" }, { ...configured, apiKey: "   " })).toBe(
      "",
    );
  });
});
