import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadConsoleSnapshot } from "./eshuConsoleLive";
import { vulnerabilityRowKey } from "./eshuConsoleVulnerabilities";

describe("eshuConsoleLive vulnerability snapshot", () => {
  function vulnerabilityClient(): EshuApiClient {
    return {
      get: async (path: string) => {
        if (path.includes("/catalog")) {
          return {
            data: {
              services: [
                {
                  id: "workload:catalog-api",
                  kind: "service",
                  name: "catalog-api",
                  repo_id: "repository:r_1",
                  repo_name: "catalog-api",
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        if (path.includes("/supply-chain/impact/findings")) {
          if (path.includes("impact_status=affected_exact")) {
            return {
              data: {
                findings: [
                  {
                    advisory_id: "GHSA-aaaa",
                    cvss_score: 8.1,
                    fixed_version: "7.0.3",
                    package_name: "serialize-javascript",
                    repository_id: "repository:r_1",
                  },
                  {
                    advisory_id: "GHSA-bbbb",
                    cvss_score: 5.9,
                    package_name: "lodash",
                    repository_id: "repository:r_1",
                  },
                ],
              },
              error: null,
              truth: null,
            };
          }
          return { data: { findings: [] }, error: null, truth: null };
        }
        if (path.includes("/supply-chain/advisories")) {
          return {
            data: {
              advisories: [
                {
                  advisory_key: "CVE-2021-44228",
                  canonical_id: "CVE-2021-44228",
                  cve_id: "CVE-2021-44228",
                  cvss_score: 10,
                  ecosystems: ["maven"],
                  kev: true,
                  package_ids: ["pkg:maven/org.apache.logging.log4j/log4j-core"],
                  severity_label: "CRITICAL",
                },
                {
                  advisory_key: "CVE-2021-45046",
                  canonical_id: "CVE-2021-45046",
                  cve_id: "CVE-2021-45046",
                  cvss_score: 9,
                  ecosystems: ["maven"],
                  kev: false,
                },
              ],
              next_cursor: {
                after_advisory_key: "CVE-2021-45046",
                after_cvss: 9,
              },
              count: 2,
              limit: 50,
              truncated: true,
            },
            error: null,
            truth: {
              capability: "supply_chain.advisory_catalog.list",
              freshness: { state: "fresh" },
              level: "exact",
              profile: "production",
            },
          };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async () => ({ queue: {}, status: "healthy" }),
      post: async () => ({ data: {}, error: null, truth: null }),
    } as unknown as EshuApiClient;
  }

  it("loads affected vulnerabilities with the impact_status anchor and derives severity", async () => {
    const snap = await loadConsoleSnapshot(vulnerabilityClient());

    expect(snap.vulnerabilities).toHaveLength(2);
    expect(snap.vulnerabilities[0]).toMatchObject({
      cvss: 8.1,
      fixedVersion: "7.0.3",
      id: "GHSA-aaaa",
      package: "serialize-javascript",
      severity: "high",
    });
    expect(snap.vulnerabilities[1]).toMatchObject({
      fixedVersion: null,
      id: "GHSA-bbbb",
      severity: "medium",
    });
  });

  it("marks vulnerabilities unavailable when either impact-status request fails", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (path.includes("impact_status=affected_derived")) {
          throw new Error("derived impact request timed out");
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities).toEqual([]);
    expect(snap.provenance.vulnerabilities).toBe("unavailable");
  });

  it("does not fabricate affected services from repository ownership", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (
          path.includes("/supply-chain/impact/findings") &&
          path.includes("impact_status=affected_exact")
        ) {
          return {
            data: {
              findings: [
                {
                  advisory_id: "GHSA-aaaa",
                  cvss_score: 8.1,
                  package_name: "lodash",
                  repository_id: "repository:r_1",
                },
                {
                  advisory_id: "GHSA-cccc",
                  cvss_score: 6.1,
                  package_name: "axios",
                  repository_id: "repository:r_unmapped",
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities.find((item) => item.id === "GHSA-aaaa")?.services).toEqual([]);
    expect(snap.vulnerabilities.find((item) => item.id === "GHSA-cccc")?.services).toEqual([]);
  });

  it("preserves explicit legacy service evidence without repository fallback", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (
          path.includes("/supply-chain/impact/findings") &&
          path.includes("impact_status=affected_exact")
        ) {
          return {
            data: {
              findings: [
                {
                  finding_id: "finding:legacy-service",
                  advisory_id: "GHSA-legacy",
                  cvss_score: 8.1,
                  package_name: "lodash",
                  repository_id: "repository:r_unmapped",
                  service_id: "service:checkout",
                  service_ids: ["service:checkout", "service:billing", "service:checkout"],
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities).toHaveLength(1);
    expect(snap.vulnerabilities[0]?.services).toEqual(["checkout", "billing"]);
    expect(snap.vulnerabilities[0]?.serviceIds).toEqual(["service:checkout", "service:billing"]);
  });

  it("preserves explicit affected_services labels without using repository identity", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (
          path.includes("/supply-chain/impact/findings") &&
          path.includes("impact_status=affected_exact")
        ) {
          return {
            data: {
              findings: [
                {
                  finding_id: "finding:affected-services",
                  advisory_id: "GHSA-affected-services",
                  affected_services: ["catalog-api", "catalog-api", "worker"],
                  repository_id: "repository:r_unmapped",
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities[0]?.affectedServices).toEqual(["catalog-api", "worker"]);
    expect(snap.vulnerabilities[0]?.serviceIds).toEqual([]);
    expect(snap.vulnerabilities[0]?.services).toEqual(["catalog-api", "worker"]);
  });

  it("preserves and deduplicates production service_ids across impact statuses", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (path.includes("/supply-chain/impact/findings")) {
          const serviceIds = path.includes("impact_status=affected_exact")
            ? ["service:checkout", "service:billing", "service:checkout"]
            : ["service:billing", "service:notifications"];
          return {
            data: {
              findings: [
                {
                  finding_id: "finding:shared-library:catalog",
                  advisory_id: "GHSA-services",
                  cvss_score: 8.1,
                  package_name: "shared-library",
                  repository_id: "repository:r_1",
                  service_ids: serviceIds,
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities.filter((item) => item.id === "GHSA-services")).toHaveLength(1);
    expect(snap.vulnerabilities.find((item) => item.id === "GHSA-services")?.services).toEqual([
      "checkout",
      "billing",
      "notifications",
    ]);
    expect(snap.vulnerabilities.find((item) => item.id === "GHSA-services")?.serviceIds).toEqual([
      "service:checkout",
      "service:billing",
      "service:notifications",
    ]);
  });

  it("keeps distinct production findings for the same advisory separate", async () => {
    const base = vulnerabilityClient();
    const client = {
      ...base,
      get: async (path: string) => {
        if (
          path.includes("/supply-chain/impact/findings") &&
          path.includes("impact_status=affected_exact")
        ) {
          return {
            data: {
              findings: [
                {
                  finding_id: "finding:GHSA-shared:lodash:catalog:4.17.20",
                  advisory_id: "GHSA-shared",
                  cvss_score: 8.1,
                  fixed_version: "4.17.22",
                  observed_version: "4.17.20",
                  package_id: "pkg:npm/lodash@4.17.20",
                  package_name: "lodash",
                  repository_id: "repository:r_1",
                  service_ids: ["service:catalog"],
                },
                {
                  finding_id: "finding:GHSA-shared:underscore:worker:1.13.6",
                  advisory_id: "GHSA-shared",
                  cvss_score: 8.1,
                  fixed_version: "1.13.7",
                  observed_version: "1.13.6",
                  package_id: "pkg:npm/underscore@1.13.6",
                  package_name: "underscore",
                  repository_id: "repository:r_worker",
                  service_ids: ["service:worker"],
                },
              ],
            },
            error: null,
            truth: null,
          };
        }
        return base.get(path);
      },
    } as unknown as EshuApiClient;

    const snap = await loadConsoleSnapshot(client);

    expect(snap.vulnerabilities).toHaveLength(2);
    expect(snap.vulnerabilities.map((item) => item.id)).toEqual(["GHSA-shared", "GHSA-shared"]);
    expect(snap.vulnerabilities).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          findingId: "finding:GHSA-shared:lodash:catalog:4.17.20",
          services: ["catalog"],
        }),
        expect.objectContaining({
          findingId: "finding:GHSA-shared:underscore:worker:1.13.6",
          package: "underscore",
          services: ["worker"],
        }),
      ]),
    );
    expect(new Set(snap.vulnerabilities.map(vulnerabilityRowKey)).size).toBe(2);
  });

  it("loads the advisory catalog as known intelligence rather than impact", async () => {
    const snap = await loadConsoleSnapshot(vulnerabilityClient());

    expect(snap.advisories).toHaveLength(2);
    expect(snap.advisories[0]).toMatchObject({
      cveId: "CVE-2021-44228",
      cvss: 10,
      id: "CVE-2021-44228",
      kev: true,
      severity: "critical",
    });
    expect(snap.advisories[0].ecosystems).toEqual(["maven"]);
    expect(snap.advisories[1]).toMatchObject({
      id: "CVE-2021-45046",
      kev: false,
      severity: "critical",
    });
    expect(snap.advisoryCatalogSummary).toEqual({ count: 2, limit: 50, truncated: true });
    expect(snap.advisoryCatalogNextCursor).toEqual({
      after_advisory_key: "CVE-2021-45046",
      after_cvss: 9,
    });
    expect(snap.provenance.advisories).toBe("live");
    expect(snap.truth.advisories?.capability).toBe("supply_chain.advisory_catalog.list");
  });
});
