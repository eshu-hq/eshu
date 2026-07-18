import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { fetchAdvisoryCatalogPage } from "./eshuConsoleAdvisories";

describe("fetchAdvisoryCatalogPage", () => {
  it("preserves the authoritative bounded-page summary", async () => {
    const client = {
      get: async () => ({
        data: {
          advisories: [
            {
              advisory_key: "CVE-2026-0001",
              cve_id: "CVE-2026-0001",
              cvss_score: 9.8,
              severity_label: "critical",
            },
          ],
          count: 1,
          limit: 1,
          truncated: true,
          next_cursor: {
            after_advisory_key: "CVE-2026-0001",
            after_cvss: 9.8,
          },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    const page = await fetchAdvisoryCatalogPage(client, { limit: 1 });

    expect(page.summary).toEqual({ count: 1, limit: 1, truncated: true });
    expect(page.nextCursor).toEqual({
      after_advisory_key: "CVE-2026-0001",
      after_cvss: 9.8,
    });
  });

  it("rejects malformed page metadata instead of presenting a false catalog count", async () => {
    const client = {
      get: async () => ({
        data: {
          advisories: [{ advisory_key: "CVE-2026-0001" }],
          count: 50,
          limit: 50,
          truncated: false,
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(fetchAdvisoryCatalogPage(client, { limit: 50 })).rejects.toThrow(
      "catalog page count",
    );
  });

  it("rejects a response that does not preserve the requested page bound", async () => {
    const client = {
      get: async () => ({
        data: {
          advisories: [{ advisory_key: "CVE-2026-0001" }],
          count: 1,
          limit: 200,
          truncated: false,
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(fetchAdvisoryCatalogPage(client, { limit: 50 })).rejects.toThrow(
      "requested limit 50",
    );
  });

  it("rejects Eshu error envelopes instead of returning an empty advisory catalog", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "advisory catalog unavailable",
        },
        truth: null,
      }),
    } as unknown as EshuApiClient;

    await expect(fetchAdvisoryCatalogPage(client, { limit: 50 })).rejects.toThrow(
      "unsupported_capability",
    );
  });
});
