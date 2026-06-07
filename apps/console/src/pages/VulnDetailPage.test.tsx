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
      graphNodes: [],
      graphEdges: [],
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
    advisories: [],
    provenance: {
      services: "live",
      languages: "live",
      ingesters: "live",
      findings: "live",
      vulnerabilities: "live",
      sbom: "empty",
      dependencies: "empty",
      advisories: "empty"
    },
    truth: {}
  };
}

describe("VulnDetailPage", () => {
  it("shows an unavailable state when the live advisory response is empty", async () => {
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
      expect(screen.getByRole("heading", { name: "Advisory unavailable" })).toBeInTheDocument();
    });
    expect(screen.queryByText("Severity")).not.toBeInTheDocument();
  });
});
