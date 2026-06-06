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
      profile: "live",
      graphBackend: "nornicdb",
      snapshotAt: "2026-06-06T00:00:00Z",
      freshnessState: "fresh"
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
        services: ["api-node-boats"]
      }
    ],
    provenance: {
      services: "live",
      languages: "live",
      ingesters: "live",
      findings: "live",
      vulnerabilities: "live"
    }
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
            path="/vulnerabilities/:id"
            element={<VulnDetailPage model={modelWithVulnerability()} client={client} />}
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
