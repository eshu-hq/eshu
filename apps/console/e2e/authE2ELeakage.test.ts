// Unit tests for the item-6 negative-leakage scan primitives (issue #4971).
//
// These cover the pure detection/guard logic — the part where a false pass is
// most dangerous — without needing a live stack: a leakage scan that scans an
// empty haystack, or scans for an empty needle, silently "proves" absence of a
// secret it never actually looked for.
import { describe, expect, it } from "vitest";

import {
  assertProbesNonEmpty,
  findLeakedProbes,
  scanSurfacesForLeakage,
  stripBootstrapBanner,
} from "./authE2ELeakage.ts";

const bannerLog = [
  "eshu-1  | starting api",
  "eshu-1  | ================ ESHU BOOTSTRAP ADMIN CREDENTIAL (one-time) ================",
  "eshu-1  | username:      admin",
  "eshu-1  | password:      hunter2",
  "eshu-1  | recovery code: rec-code-xyz",
  "eshu-1  | This banner will not be shown again. Save these values now.",
  "eshu-1  | ==============================================================================",
  "eshu-1  | ready",
].join("\n");

describe("stripBootstrapBanner", () => {
  it("removes the one-time banner block so its sanctioned plaintext is not scanned", () => {
    const stripped = stripBootstrapBanner(bannerLog);
    expect(stripped).not.toContain("hunter2");
    expect(stripped).not.toContain("rec-code-xyz");
    expect(stripped).toContain("starting api");
    expect(stripped).toContain("ready");
    // The bootstrap credential inside the banner is no longer a finding...
    expect(findLeakedProbes(stripped, [{ label: "bootstrap password", value: "hunter2" }])).toEqual(
      [],
    );
  });

  it("still exposes the credential when it leaks OUTSIDE the banner (a real leak)", () => {
    const leaked = `${bannerLog}\neshu-1  | audit: password=hunter2 recorded`;
    const stripped = stripBootstrapBanner(leaked);
    expect(findLeakedProbes(stripped, [{ label: "bootstrap password", value: "hunter2" }])).toEqual(
      ["bootstrap password"],
    );
  });

  it("fails safe: a start marker with no terminating line leaves the log UNCHANGED", () => {
    const truncated = [
      "eshu-1  | ================ ESHU BOOTSTRAP ADMIN CREDENTIAL (one-time) ===",
      "eshu-1  | password:      hunter2",
    ].join("\n");
    expect(stripBootstrapBanner(truncated)).toBe(truncated);
  });

  it("returns the log unchanged when no banner is present", () => {
    expect(stripBootstrapBanner("eshu-1  | just normal logs\neshu-1  | ready")).toBe(
      "eshu-1  | just normal logs\neshu-1  | ready",
    );
  });
});

describe("findLeakedProbes", () => {
  it("returns the labels of every probe whose value occurs in the haystack", () => {
    const haystack = 'log line: password=hunter2 and client_secret="s3cr3t"';
    const leaked = findLeakedProbes(haystack, [
      { label: "bootstrap password", value: "hunter2" },
      { label: "client secret", value: "s3cr3t" },
      { label: "recovery code", value: "never-appears" },
    ]);
    expect(leaked).toEqual(["bootstrap password", "client secret"]);
  });

  it("finds nothing in a clean haystack", () => {
    expect(findLeakedProbes("nothing sensitive here", [{ label: "pw", value: "hunter2" }])).toEqual(
      [],
    );
  });

  it("never treats a blank probe value as a universal match", () => {
    // The critical false-pass guard: an empty needle must NOT 'match' every
    // surface — String.includes('') is always true, which would report every
    // secret as leaked (or, if inverted, mask a real leak).
    expect(findLeakedProbes("any text at all", [{ label: "empty", value: "" }])).toEqual([]);
    expect(findLeakedProbes("any text at all", [{ label: "whitespace", value: "   " }])).toEqual(
      [],
    );
  });
});

describe("assertProbesNonEmpty", () => {
  it("throws when any probe carries no real value, naming the offenders", () => {
    expect(() =>
      assertProbesNonEmpty([
        { label: "bootstrap password", value: "hunter2" },
        { label: "recovery code", value: "" },
        { label: "client secret", value: "   " },
      ]),
    ).toThrow(/recovery code, client secret/);
  });

  it("passes when every probe carries a real value", () => {
    expect(() => assertProbesNonEmpty([{ label: "pw", value: "hunter2" }])).not.toThrow();
  });
});

describe("scanSurfacesForLeakage", () => {
  it("reports each secret/surface pair where a secret leaked", () => {
    const findings = scanSurfacesForLeakage(
      [
        { name: "container logs", body: "started; password=hunter2" },
        { name: "status endpoint", body: '{"status":"ok"}' },
        { name: "audit events", body: '{"actor":"admin","secret":"s3cr3t"}' },
      ],
      [
        { label: "bootstrap password", value: "hunter2" },
        { label: "client secret", value: "s3cr3t" },
      ],
    );
    expect(findings).toEqual([
      "bootstrap password leaked into container logs",
      "client secret leaked into audit events",
    ]);
  });

  it("returns no findings when every surface is clean", () => {
    const findings = scanSurfacesForLeakage(
      [
        { name: "container logs", body: "started; all good" },
        { name: "DOM", body: "<html>dashboard</html>" },
      ],
      [{ label: "bootstrap password", value: "hunter2" }],
    );
    expect(findings).toEqual([]);
  });
});
