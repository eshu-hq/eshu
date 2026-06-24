import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadDependencies } from "./eshuDependencies";

describe("loadDependencies", () => {
  it("propagates Eshu error envelopes instead of rendering an empty dependency graph", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_runtime_profile",
          message: "dependency graph is unavailable in this profile",
          capability: "dependencies.list"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadDependencies(client, { direction: "forward", limit: 50 })).rejects.toThrow(
      "unsupported_runtime_profile"
    );
  });
});
