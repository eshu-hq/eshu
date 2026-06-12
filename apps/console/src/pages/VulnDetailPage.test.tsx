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
      advisories: "empty"
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
});
