import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { loadCollectorReadiness } from "./collectorReadiness";

describe("collectorReadiness", () => {
  it("maps the generated collector readiness catalog without collapsing closed states", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        return {
          data: {
            readiness: [
              readinessRow("git", "Git Repository", "implemented", { evidence_sources: ["source_facts", "reducer_facts"], observation_count: 42, reducer_readback: "available" }),
              readinessRow("aws", "AWS Cloud", "failed", { health: "degraded", blockers: ["runtime health degraded: credential denied"] }),
              readinessRow("pagerduty", "PagerDuty", "gated", { claim_state: "direct", blockers: ["claim-driven collector registered with claims disabled"] }),
              readinessRow("jira", "Jira", "stale", { last_proof_at: "2026-06-18T12:00:00Z", observation_count: 4 }),
              readinessRow("sbom_attestation", "SBOM Attestation", "disabled", { claim_state: "registration_only" }),
              readinessRow("kubernetes_live", "Kubernetes Live", "permission_hidden", { blockers: ["hidden by active permission scope"] }),
              readinessRow("vault_live", "Vault Live", "unsupported", { blockers: ["no configured instance for this collector family"] }),
              readinessRow("grafana", "Grafana", "partial", { evidence_sources: ["source_facts"], reducer_readback: "pending" })
            ]
          },
          error: null,
          truth: {
            capability: "collector_readiness",
            freshness: { state: "fresh" },
            level: "exact",
            profile: "local_full_stack"
          }
        };
      }
    } as unknown as EshuApiClient;

    const result = await loadCollectorReadiness(client);

    expect(calls).toEqual(["/api/v0/status/collector-readiness"]);
    expect(result.truth?.level).toBe("exact");
    expect(result.rows.map((row) => row.state)).toEqual([
      "implemented",
      "failed",
      "gated",
      "stale",
      "disabled",
      "permission_hidden",
      "unsupported",
      "partial"
    ]);
    expect(result.rows.find((row) => row.kind === "aws")?.blockingGate).toBe("runtime health degraded: credential denied");
    expect(result.rows.find((row) => row.kind === "git")?.lastProof).toBe("42 observations");
    expect(result.rows.find((row) => row.kind === "jira")?.lastProof).toBe("4 observations at 2026-06-18T12:00:00Z");
    expect(result.rows.find((row) => row.kind === "kubernetes_live")?.stateLabel).toBe("permission hidden");
    expect(result.rows.find((row) => row.kind === "vault_live")?.stateLabel).toBe("unsupported");
  });
});

function readinessRow(
  collectorKind: string,
  displayName: string,
  promotionState: string,
  extra: Record<string, unknown> = {}
): Record<string, unknown> {
  return {
    claim_driven: true,
    claim_state: "claim_driven",
    collector_kind: collectorKind,
    display_name: displayName,
    evidence_sources: [],
    health: "healthy",
    observation_count: 0,
    promotion_state: promotionState,
    recommended_next_action: "review",
    reducer_readback: "unavailable",
    source_scope: "scope",
    source_systems: [],
    telemetry_handles: ["collector.observe"],
    ...extra
  };
}
