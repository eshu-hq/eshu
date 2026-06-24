import { describe, expect, it } from "vitest";
import { loadObservabilityCoverage } from "./eshuObservability";
import type { EshuApiClient } from "./client";

// The coverage endpoint requires a provider anchor, so the adapter must fan out
// one getJson request per provider and merge paginated results.
describe("eshuObservability", () => {
  function fakeClient(opts?: { failTempo?: boolean; failAll?: boolean }): { client: EshuApiClient; calls: string[] } {
    const calls: string[] = [];
    const client = {
      getJson: async (path: string) => {
        calls.push(path);
        if (opts?.failAll) throw new Error("down");
        if (path.includes("provider=tempo") && opts?.failTempo) throw new Error("tempo down");
        if (path.includes("provider=grafana")) {
          if (!path.includes("after_correlation_id=")) {
            return { correlations: [
              { correlation_id: "g1", provider: "grafana", coverage_signal: "dashboard", observability_object_ref: "grafana:checkout-dashboard", target_service_ref: "checkout-api", coverage_status: "covered", resource_class: "dashboard", source_kind: "kubernetes", freshness_state: "current" }
            ], truncated: true, next_cursor: { after_correlation_id: "g1" } };
          }
          return { correlations: [
            { correlation_id: "g2", provider: "grafana", coverage_signal: "datasource", observability_object_ref: "tempo", coverage_status: "gap", resource_class: "datasource", source_kind: "grafana", freshness_state: "current" }
          ], truncated: false };
        }
        if (path.includes("provider=loki")) {
          return { correlations: [
            { correlation_id: "l1", provider: "loki", coverage_signal: "log", observability_object_ref: "platform-prod", coverage_status: "covered", resource_class: "log_source", source_kind: "loki", freshness_state: "current" }
          ] };
        }
        if (path.includes("provider=tempo")) {
          return { correlations: [
            { correlation_id: "t1", provider: "tempo", coverage_signal: "trace", observability_object_ref: "platform-prod", coverage_status: "covered" }
          ] };
        }
        // prometheus
        return { correlations: [] };
      }
    } as unknown as EshuApiClient;
    return { client, calls };
  }

  it("fans out one anchored request per provider and merges rows", async () => {
    const { client, calls } = fakeClient();
    const snap = await loadObservabilityCoverage(client);
    expect(calls).toHaveLength(5);
    expect(calls.every((c) => c.includes("provider="))).toBe(true);
    expect(calls.some((c) => c.includes("after_correlation_id=g1"))).toBe(true);
    expect(snap.source).toBe("live");
    expect(snap.rows).toHaveLength(4); // g1, g2, l1, t1
    expect(snap.rows.find((row) => row.id === "g1")).toMatchObject({
      object: "grafana:checkout-dashboard",
      target: "checkout-api"
    });
  });

  it("rolls up covered vs gap per provider", async () => {
    const { client } = fakeClient();
    const snap = await loadObservabilityCoverage(client);
    const grafana = snap.providers.find((p) => p.provider === "grafana");
    expect(grafana).toMatchObject({ total: 2, covered: 1, gaps: 1 });
    const loki = snap.providers.find((p) => p.provider === "loki");
    expect(loki).toMatchObject({ total: 1, covered: 1, gaps: 0 });
    const prometheus = snap.providers.find((p) => p.provider === "prometheus");
    expect(prometheus).toMatchObject({ total: 0, covered: 0, gaps: 0 });
  });

  it("keeps fallback row identity distinct by target when correlation IDs are absent", async () => {
    const client = {
      getJson: async (path: string) => {
        if (!path.includes("provider=grafana")) return { correlations: [], truncated: false };
        return {
          correlations: [
            { provider: "grafana", coverage_signal: "dashboard", observability_object_ref: "shared-dashboard", target_service_ref: "checkout-api", coverage_status: "covered" },
            { provider: "grafana", coverage_signal: "dashboard", observability_object_ref: "shared-dashboard", target_service_ref: "payments-api", coverage_status: "covered" }
          ],
          truncated: false
        };
      }
    } as unknown as EshuApiClient;

    const snap = await loadObservabilityCoverage(client);

    expect(snap.rows.map((row) => row.target).sort()).toEqual(["checkout-api", "payments-api"]);
  });

  it("marks a failed provider unavailable while staying live when others succeed", async () => {
    const { client } = fakeClient({ failTempo: true });
    const snap = await loadObservabilityCoverage(client);
    expect(snap.source).toBe("live");
    expect(snap.rows.some((r) => r.provider === "tempo")).toBe(false);
    expect(snap.rows.some((r) => r.provider === "grafana")).toBe(true);
    const tempo = snap.providers.find((p) => p.provider === "tempo");
    expect(tempo).toMatchObject({ provider: "tempo", source: "unavailable", total: 0 });
  });

  it("reports unavailable when every provider request fails", async () => {
    const { client } = fakeClient({ failAll: true });
    const snap = await loadObservabilityCoverage(client);
    expect(snap.source).toBe("unavailable");
    expect(snap.rows).toHaveLength(0);
    expect(snap.providers.every((p) => p.source === "unavailable")).toBe(true);
  });
});
