import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { loadVulnerabilityStatusPage } from "./eshuConsoleVulnerabilities";

describe("impact finding page contract", () => {
  it("shows CVE, GHSA aliases, source, EPSS including zero, and KEV", async () => {
    const get = vi.fn(async () => ({
      data: {
        findings: [
          {
            finding_id: "f-1",
            cve_id: "CVE-2026-12345",
            advisory_id: "GHSA-aaaa-bbbb-cccc",
            provenance: {
              advisory_sources: [
                { advisory_id: "GHSA-dddd-eeee-ffff", source: "OSV" },
                { source: "NVD" },
              ],
            },
            epss_probability: 0,
            known_exploited: true,
          },
        ],
        truncated: false,
      },
      error: null,
      truth: null,
    }));
    const page = await loadVulnerabilityStatusPage(
      { get } as unknown as EshuApiClient,
      new Map(),
      "affected_exact",
    );
    expect(page.rows[0]).toMatchObject({
      id: "CVE-2026-12345",
      ghsaIds: ["GHSA-aaaa-bbbb-cccc", "GHSA-dddd-eeee-ffff"],
      sources: ["OSV", "NVD"],
      epssProbability: 0,
      kev: true,
    });
  });

  it("does not call an alias a CVE or accept a non-advancing cursor", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: {
          findings: [{ finding_id: "f-2", cve_id: "GHSA-aaaa-bbbb-cccc" }],
          truncated: true,
          next_cursor: { after_finding_id: "same" },
        },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;
    await expect(
      loadVulnerabilityStatusPage(client, new Map(), "affected_exact", "same"),
    ).rejects.toThrow("no advancing cursor");
  });
});
