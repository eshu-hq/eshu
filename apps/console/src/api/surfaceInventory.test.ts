import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadSurfaceInventory } from "./surfaceInventory";

describe("surfaceInventory", () => {
  it("maps collector provenance contracts from the generated surface inventory", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: {
            total: 1,
            surfaces: [
              {
                category: "collector",
                name: "git",
                readiness: "implemented",
                owner: "internal/collector",
                proof: "reference/collector-reducer-readiness.md#promotion-proof",
                docs: ["reference/collector-reducer-readiness.md"],
                collector_contract: {
                  fact_kinds: ["documentation_document"],
                  projection_surfaces: ["documentation_evidence"],
                  read_surfaces: ["GET /api/v0/documentation/facts"],
                  proof_gates: ["go test ./internal/collector ./internal/query -count=1"],
                  fixture_refs: ["go/internal/collector/testdata"],
                  truth_profile: "deterministic",
                },
              },
            ],
          },
          error: null,
          truth: {
            capability: "surface_inventory.list",
            freshness: { state: "fresh" },
            level: "exact",
            profile: "production",
          },
        };
      },
    } as unknown as EshuApiClient;

    const result = await loadSurfaceInventory(client, { category: "collector", limit: 1 });

    expect(calls).toEqual(["/api/v0/surface-inventory?category=collector&limit=1"]);
    expect(result.rows[0]?.collectorContract).toEqual({
      fact_kinds: ["documentation_document"],
      projection_surfaces: ["documentation_evidence"],
      read_surfaces: ["GET /api/v0/documentation/facts"],
      proof_gates: ["go test ./internal/collector ./internal/query -count=1"],
      fixture_refs: ["go/internal/collector/testdata"],
      truth_profile: "deterministic",
    });
  });
});
