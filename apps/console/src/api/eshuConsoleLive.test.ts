import { describe, expect, it } from "vitest";
import { loadConsoleSnapshot } from "./eshuConsoleLive";
import type { EshuApiClient } from "./client";

// The console adapter must match the real API response shapes:
// - ecosystem/overview is enveloped and uses repo_count (not repository_count)
// - index-status and status/ingesters return RAW JSON (no envelope), so they
//   must be read with getJson, not get
// - the language overview comes from repositories/language-inventory, not
//   repositories/by-language (which requires a ?language= and 400s without it)
describe("eshuConsoleLive", () => {
  function fakeClient(): EshuApiClient {
    return {
      get: async (path: string) => {
        if (path.includes("/ecosystem/overview")) {
          return {
            data: { repo_count: 33, workload_count: 21, platform_count: 7, instance_count: 92 },
            error: null,
            truth: { profile: "production", level: "exact", capability: "x", freshness: { state: "fresh" } }
          };
        }
        if (path.includes("/repositories/language-inventory")) {
          return {
            data: { languages: [{ language: "yaml", repository_count: 32 }, { language: "go", repository_count: 5 }] },
            error: null,
            truth: null
          };
        }
        if (path.includes("/repositories/by-language")) {
          throw new Error("by-language requires ?language= and must not be used for the overview");
        }
        if (path.includes("/catalog")) {
          // The same workload appears as a service and a workload (and twice
          // across environments) — the adapter must dedup by id.
          return {
            data: {
              services: [{ id: "workload:api", name: "api", kind: "deployment", repo_name: "api" }],
              workloads: [
                { id: "workload:api", name: "api", kind: "deployment", repo_name: "api" },
                { id: "workload:lib-config", name: "lib-config", kind: "library", repo_name: "lib-config" },
                { id: "workload:lib-config", name: "lib-config", kind: "library", repo_name: "lib-config" }
              ]
            },
            error: null,
            truth: { profile: "production", level: "exact", capability: "x", freshness: { state: "fresh" } }
          };
        }
        if (path.includes("/supply-chain/impact/findings")) {
          // List endpoints require an impact_status anchor; affected findings
          // carry a CVSS score but no severity label.
          if (path.includes("impact_status=affected_exact")) {
            return {
              data: { findings: [
                { advisory_id: "GHSA-aaaa", package_name: "serialize-javascript", cvss_score: 8.1, fixed_version: "7.0.3", repository_id: "r_1" },
                { advisory_id: "GHSA-bbbb", package_name: "lodash", cvss_score: 5.9, repository_id: "r_1" }
              ] },
              error: null, truth: null
            };
          }
          // affected_derived (and any unanchored call) returns nothing here.
          return { data: { findings: [] }, error: null, truth: null };
        }
        return { data: {}, error: null, truth: null };
      },
      getJson: async (path: string) => {
        if (path.includes("/index-status")) {
          return {
            status: "healthy", repository_count: 33,
            queue: { outstanding: 2, in_flight: 1, dead_letter: 0, succeeded: 333 },
            coordinator: { collector_instances: [
              { collector_kind: "grafana", instance_id: "remote-e2e-grafana", enabled: true, last_observed_at: "2026-06-07T05:00:00Z", deactivated_at: null },
              { collector_kind: "aws", instance_id: "remote-e2e-aws", enabled: true, last_observed_at: "2026-06-07T05:00:00Z", deactivated_at: null },
              { collector_kind: "loki", instance_id: "remote-e2e-loki", enabled: false, last_observed_at: null, deactivated_at: "2026-06-06T00:00:00Z" }
            ] }
          };
        }
        if (path.includes("/status/ingesters")) {
          return { ingesters: [{ name: "repository", health: "healthy", runtime_family: "ingester" }] };
        }
        return {};
      },
      post: async () => ({ data: {}, error: null, truth: null })
    } as unknown as EshuApiClient;
  }

  it("maps runtime counts and status from enveloped + raw endpoints", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.runtime.repositories).toBe(33);
    expect(snap.runtime.workloads).toBe(21);
    expect(snap.runtime.platforms).toBe(7);
    expect(snap.runtime.instances).toBe(92);
    expect(snap.runtime.indexStatus).toBe("healthy");
    expect(snap.runtime.queueOutstanding).toBe(2);
    expect(snap.runtime.succeeded).toBe(333);
    expect(snap.runtime.profile).toBe("production");
  });

  it("reads the language overview from language-inventory (repository_count)", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.languages).toEqual([
      { language: "yaml", count: 32 },
      { language: "go", count: 5 }
    ]);
  });

  it("merges the repository ingester with coordinator collector instances", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    // 1 ingester (status/ingesters) + 3 collectors (index-status.coordinator)
    expect(snap.ingesters).toHaveLength(4);
    expect(snap.ingesters[0]).toMatchObject({ id: "repository", state: "healthy", kind: "ingester" });
    const grafana = snap.ingesters.find((r) => r.id === "remote-e2e-grafana");
    expect(grafana).toMatchObject({ kind: "grafana", state: "active", facts: null, freshness: "fresh" });
    const loki = snap.ingesters.find((r) => r.id === "remote-e2e-loki");
    // disabled + deactivated collector renders as a stale, non-active row
    expect(loki).toMatchObject({ kind: "loki", state: "deactivated", facts: null, freshness: "stale" });
  });

  it("dedups the catalog by id across services and workloads", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    const ids = snap.services.map((s) => s.id);
    expect(ids).toEqual(["workload:api", "workload:lib-config"]);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("loads affected vulnerabilities with the impact_status anchor and derives severity from CVSS", async () => {
    const snap = await loadConsoleSnapshot(fakeClient());
    expect(snap.vulnerabilities).toHaveLength(2);
    expect(snap.vulnerabilities[0]).toMatchObject({
      id: "GHSA-aaaa", package: "serialize-javascript", severity: "high", cvss: 8.1, fixedVersion: "7.0.3"
    });
    // cvss 5.9 -> medium band, no fix -> null
    expect(snap.vulnerabilities[1]).toMatchObject({ id: "GHSA-bbbb", severity: "medium", fixedVersion: null });
  });
});
