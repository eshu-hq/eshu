import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";
import { VulnDetailPage } from "./VulnDetailPage";

function modelWithVulnerability(): ConsoleModel {
  return {
    source: "live",
    graph: { nodes: [], edges: [] },
    relationships: [],
    series: {
      ingestRate: [],
      queueDepth: [],
      deadLetters: [],
      graphNodes: [],
      graphEdges: [],
      queryP50: [],
      queryP95: [],
      queryP99: [],
      newVulns: []
    },
    runtime: {
      indexStatus: "healthy",
      repositories: 0,
      workloads: 0,
      platforms: 0,
      instances: 0,
      queueOutstanding: 0,
      inFlight: 0,
      deadLetters: 0,
      succeeded: 0,
      profile: "live"
    },
    services: [],
    languages: [],
    ingesters: [],
    collectorReadiness: [],
    findings: [],
    vulnerabilities: [
      {
        id: "CVE-2025-13465",
        package: "serialize-javascript",
        severity: "high",
        cvss: 8.1,
        kev: false,
        fixedVersion: "7.0.3",
        services: ["checkout-api"]
      }
    ],
    sbom: null,
    dependencies: [],
    images: [],
    iacResources: [],
    advisories: [],
    provenance: {
      services: "live",
      languages: "live",
      ingesters: "live",
      findings: "live",
      vulnerabilities: "live",
      sbom: "empty",
      dependencies: "empty",
      images: "empty",
      iacResources: "empty",
      advisories: "empty",
      collectorReadiness: "empty"
    },
    truth: {}
  };
}

describe("VulnDetailPage", () => {
  it("shows an unavailable state when the advisory and impact rows are empty", async () => {
    const client = {
      get: async () => ({ data: null, truth: null, error: null })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/vulnerabilities/CVE-2025-00000"]}>
        <Routes>
          <Route
            element={<VulnDetailPage client={client} model={modelWithVulnerability()} />}
            path="/vulnerabilities/:id"
          />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByRole("heading", { name: "Advisory unavailable" })).toBeInTheDocument();
    });
    expect(screen.queryByText("Severity")).not.toBeInTheDocument();
  });

  it("falls back to source-backed impact facts when extended advisory evidence is empty", async () => {
    const client = {
      get: async () => ({ data: null, truth: null, error: null })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/vulnerabilities/CVE-2025-13465"]}>
        <Routes>
          <Route
            element={<VulnDetailPage client={client} model={modelWithVulnerability()} />}
            path="/vulnerabilities/:id"
          />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText("Severity")).toBeInTheDocument();
    });
    expect(screen.getByText("high")).toBeInTheDocument();
    expect(screen.getByText("serialize-javascript")).toBeInTheDocument();
    expect(screen.getByText(/Showing reachable impact facts from/)).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/supply-chain/impact/findings")).toBeInTheDocument();
  });

  it("loads the supply-chain impact investigation packet for the selected advisory", async () => {
    const calls: string[] = [];
    const client = {
      get: async (path: string) => {
        calls.push(path);
        if (path.startsWith("/api/v0/supply-chain/vulnerabilities/")) {
          return { data: null, truth: null, error: null };
        }
        if (path.startsWith("/api/v0/investigations/supply-chain/impact/packet")) {
          return {
            data: {
              answer: { summary: "Advisory impact packet is supported.", supported: true, truth_class: "exact" },
              bounds: { max_source_facts: 200, truncated: false },
              graph_answers: [{ id: "graph:supply:1", present: true, relation: "AFFECTED_BY" }],
              identity: { family: "supply_chain_impact", scope: { cve_id: "CVE-2025-13465" } },
              missing_evidence: [],
              packet_id: "investigation-evidence-packet:supply-demo",
              reducer_decisions: [{ id: "decision:supply:1", state: "admitted" }],
              redaction: { profile: "share_safe_v2" },
              reproduce: [{ kind: "http", route: "/api/v0/investigations/supply-chain/impact/packet" }],
              schema: "investigation_evidence_packet.v2",
              source_facts: [{ evidence_family: "supply_chain", fact_id: "fact:supply:1" }],
              validation: { valid: true }
            },
            error: null,
            truth: {
              capability: "supply_chain.impact_explanation.read",
              freshness: { state: "fresh" },
              level: "exact",
              profile: "production"
            }
          };
        }
        throw new Error(`unexpected path ${path}`);
      }
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/vulnerabilities/CVE-2025-13465"]}>
        <Routes>
          <Route
            element={<VulnDetailPage client={client} model={modelWithVulnerability()} />}
            path="/vulnerabilities/:id"
          />
        </Routes>
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(screen.getByText("investigation-evidence-packet:supply-demo")).toBeInTheDocument();
    });
    expect(screen.getByText("fact:supply:1")).toBeInTheDocument();
    expect(calls).toContain(
      "/api/v0/investigations/supply-chain/impact/packet?cve_id=CVE-2025-13465&package_id=serialize-javascript&max_source_facts=50"
    );
  });
});
