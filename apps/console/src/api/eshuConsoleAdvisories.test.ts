import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { fetchAdvisoryCatalogPage } from "./eshuConsoleAdvisories";

describe("fetchAdvisoryCatalogPage", () => {
  it("rejects Eshu error envelopes instead of returning an empty advisory catalog", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "advisory catalog unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(fetchAdvisoryCatalogPage(client, { limit: 50 })).rejects.toThrow(
      "unsupported_capability"
    );
  });
});
