import { describe, expect, it } from "vitest";
import type { EshuApiClient } from "./client";
import { loadFindings, loadVulnerabilities, type SectionContext } from "./eshuConsoleSections";

describe("eshuConsoleSections findings", () => {
  it("falls back from an empty dead-code repo name to the repo id", async () => {
    const client = {
      post: async () => ({
        data: {
          results: [{
            classification: "unused",
            file_path: "server/src/api/itemsClient.ts",
            name: "parseRange",
            repo_id: "repository:r_1",
            repo_name: ""
          }]
        },
        error: null,
        truth: {
          capability: "code_quality.dead_code",
          freshness: { state: "fresh" },
          level: "derived",
          profile: "local_authoritative"
        }
      })
    } as unknown as EshuApiClient;

    const rows = await loadFindings(client);

    expect(rows?.[0]?.entity).toBe("repository:r_1");
  });

  it("resolves dead-code repo ids through the catalog repo name map", async () => {
    const client = {
      post: async () => ({
        data: {
          results: [{
            classification: "unused",
            file_path: "server/src/api/itemsClient.ts",
            name: "parseRange",
            repo_id: "repository:r_1",
            repo_name: ""
          }]
        },
        error: null,
        truth: {
          capability: "code_quality.dead_code",
          freshness: { state: "fresh" },
          level: "derived",
          profile: "local_authoritative"
        }
      })
    } as unknown as EshuApiClient;
    const ctx: SectionContext = {
      truth: {},
      repoNames: new Map([["repository:r_1", "api-node-boats"]])
    };

    const rows = await loadFindings(client, ctx);

    expect(rows?.[0]?.entity).toBe("api-node-boats");
  });

  it("rejects dead-code error envelopes so snapshot provenance marks findings unavailable", async () => {
    const client = {
      post: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "dead-code analysis unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;

    await expect(loadFindings(client)).rejects.toThrow(
      "unsupported_capability: dead-code analysis unavailable"
    );
  });
});

describe("eshuConsoleSections vulnerabilities", () => {
  it("rejects impact-finding error envelopes so snapshot provenance marks vulnerabilities unavailable", async () => {
    const client = {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "impact findings unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;
    const ctx: SectionContext = {
      truth: {},
      repoNames: new Map()
    };

    await expect(loadVulnerabilities(client, ctx)).rejects.toThrow(
      "unsupported_capability: impact findings unavailable"
    );
  });
});
