import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import {
  loadCloudRuntimeDriftPacket,
  loadDeployableUnitPacket,
  loadSupplyChainImpactPacket
} from "./investigationPacket";

function envelope(data: unknown) {
  return {
    data,
    error: null,
    truth: {
      capability: "investigation.packet.read",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production"
    }
  };
}

describe("investigation packet adapters", () => {
  it("loads a supply-chain impact packet with bounded query parameters", async () => {
    const get = vi.fn(async () => envelope(packetWire("supply-chain-packet")));
    const client = { get } as unknown as EshuApiClient;

    const result = await loadSupplyChainImpactPacket(client, {
      advisoryId: "GHSA-test",
      cveId: "CVE-2026-0001",
      maxSourceFacts: 12,
      packageId: "pkg:npm/left-pad",
      repositoryId: "repo://team/api"
    });

    expect(get).toHaveBeenCalledWith(
      "/api/v0/investigations/supply-chain/impact/packet?advisory_id=GHSA-test&cve_id=CVE-2026-0001&package_id=pkg%3Anpm%2Fleft-pad&repository_id=repo%3A%2F%2Fteam%2Fapi&max_source_facts=12"
    );
    expect(result.packet.packetId).toBe("supply-chain-packet");
    expect(result.truth.capability).toBe("investigation.packet.read");
  });

  it("loads deployable-unit and drift packets through their HTTP packet routes", async () => {
    const paths: string[] = [];
    const get = vi.fn(async (path: string) => {
      paths.push(path);
      return envelope(packetWire(path));
    });
    const client = { get } as unknown as EshuApiClient;

    await loadDeployableUnitPacket(client, {
      generationId: "generation-1",
      repositoryId: "repo://team/api",
      scopeId: "scope-1"
    });
    await loadCloudRuntimeDriftPacket(client, {
      cloudResourceUid: "cloud-resource:s3:bucket",
      provider: "aws",
      scopeId: "scope-1"
    });

    expect(paths).toEqual([
      "/api/v0/investigations/deployable-unit/packet?scope_id=scope-1&generation_id=generation-1&repository_id=repo%3A%2F%2Fteam%2Fapi",
      "/api/v0/investigations/drift/packet?scope_id=scope-1&provider=aws&cloud_resource_uid=cloud-resource%3As3%3Abucket"
    ]);
  });
});

function packetWire(packetId: string): Record<string, unknown> {
  return {
    answer: { summary: "packet summary", supported: true, truth_class: "exact" },
    bounds: { max_source_facts: 200, truncated: false },
    graph_answers: [{ id: "graph:1", present: true, relation: "AFFECTED_BY" }],
    identity: { family: "supply_chain_impact", scope: { cve_id: "CVE-2026-0001" } },
    missing_evidence: [],
    packet_id: packetId,
    reducer_decisions: [{ id: "decision:1", state: "admitted" }],
    redaction: { profile: "share_safe_v2" },
    reproduce: [{ kind: "http", route: "/api/v0/investigations/supply-chain/impact/packet" }],
    schema: "investigation_evidence_packet.v2",
    semantic_observations: [],
    source_facts: [{ fact_id: "fact:1", evidence_family: "supply_chain" }],
    validation: { valid: true }
  };
}
