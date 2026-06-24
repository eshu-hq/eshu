import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import {
  loadFindings,
  loadIacResources,
  loadImagesSection,
  loadLanguages,
  loadSbom,
  loadSeriesBundle,
  loadServices,
  loadVulnerabilities,
  type SectionContext
} from "./eshuConsoleSections";

describe("eshuConsoleSections findings", () => {
  it("falls back from an empty dead-code repo name to an unresolved label", async () => {
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

    expect(rows?.[0]?.entity).toBe("unresolved repository");
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
      repoNames: new Map([["repository:r_1", "svc-catalog"]])
    };

    const rows = await loadFindings(client, ctx);

    expect(rows?.[0]?.entity).toBe("svc-catalog");
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

describe("eshuConsoleSections snapshot envelopes", () => {
  function errorClient(): EshuApiClient {
    return {
      get: async () => ({
        data: null,
        error: {
          code: "unsupported_capability",
          message: "snapshot section unavailable"
        },
        truth: null
      })
    } as unknown as EshuApiClient;
  }

  function context(): SectionContext {
    return { truth: {}, repoNames: new Map() };
  }

  it("rejects catalog error envelopes so services are marked unavailable", async () => {
    await expect(loadServices(errorClient(), context())).rejects.toThrow("unsupported_capability");
  });

  it("rejects language inventory error envelopes so languages are marked unavailable", async () => {
    await expect(loadLanguages(errorClient())).rejects.toThrow("unsupported_capability");
  });

  it("rejects SBOM count error envelopes so SBOM evidence is marked unavailable", async () => {
    await expect(loadSbom(errorClient(), context())).rejects.toThrow("unsupported_capability");
  });

  it("rejects image inventory error envelopes so images are marked unavailable", async () => {
    await expect(loadImagesSection(errorClient(), context())).rejects.toThrow("unsupported_capability");
  });

  it("rejects IaC resource error envelopes so IaC is marked unavailable", async () => {
    await expect(loadIacResources(errorClient(), context())).rejects.toThrow("unsupported_capability");
  });

  it("rejects metric series error envelopes so series provenance is marked unavailable", async () => {
    await expect(loadSeriesBundle(errorClient(), async (_key, load) => load())).rejects.toThrow(
      "unsupported_capability"
    );
  });
});
