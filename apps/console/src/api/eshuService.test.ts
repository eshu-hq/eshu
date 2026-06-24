import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadServiceSpotlight } from "./eshuService";

describe("loadServiceSpotlight", () => {
  it("rejects story error envelopes instead of fabricating a live service story", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "service story unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadServiceSpotlight(client, "catalog-api")).rejects.toThrow(
      "unsupported_capability"
    );
  });
});
