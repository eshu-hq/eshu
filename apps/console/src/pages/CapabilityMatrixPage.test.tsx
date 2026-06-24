import { render, screen, waitFor } from "@testing-library/react";

import { CapabilityMatrixPage } from "./CapabilityMatrixPage";
import type { EshuApiClient } from "../api/client";

// CapabilityMatrixPage renders the reconciled capability catalog. It must:
// - show a loading state until the first page resolves
// - render capability rows (display name, maturity, surfaces, owner, issues)
// - render an explicit unavailable state when the endpoint fails
// - never fabricate capabilities
describe("CapabilityMatrixPage", () => {
  it("shows the loading state until the catalog resolves", () => {
    const client = { get: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<CapabilityMatrixPage client={client} />);
    expect(screen.getByText("Loading capability catalog…")).toBeInTheDocument();
  });

  it("renders capability rows with maturity and surfaces", async () => {
    const client = {
      get: async () => ({
        data: {
          version: "v1",
          total: 2,
          limit: 500,
          offset: 0,
          truncated: false,
          capabilities: [
            {
              capability: "code_search.exact_symbol",
              display_name: "Code Search Exact Symbol",
              owner_package: "internal/query",
              maturity: "general_availability",
              derived_maturity: "general_availability",
              surfaces: [{ tool: "find_code", kind: "mcp" }],
              proof_signals: [{ kind: "go_test", ref: "./internal/query" }],
              known_gaps: [],
              linked_issues: [],
              console: true
            },
            {
              capability: "platform_impact.cloud_resource_list",
              display_name: "Cloud Resource List",
              owner_package: "internal/query",
              maturity: "gated",
              derived_maturity: "general_availability",
              maturity_reason: "public chart support pending",
              surfaces: [{ tool: "list_cloud_resources", kind: "api" }],
              proof_signals: [],
              known_gaps: ["no live provider"],
              linked_issues: [2700],
              console: true
            }
          ]
        },
        error: null,
        truth: { profile: "production", level: "exact", capability: "capability_catalog.list", freshness: { state: "fresh" } }
      })
    } as unknown as EshuApiClient;

    render(<CapabilityMatrixPage client={client} />);

    expect(await screen.findByText("Code Search Exact Symbol")).toBeInTheDocument();
    expect(screen.getByText("Cloud Resource List")).toBeInTheDocument();
    expect(screen.getByText("general availability")).toBeInTheDocument();
    expect(screen.getByText("gated")).toBeInTheDocument();
    expect(screen.getByText("public chart support pending")).toBeInTheDocument();
    expect(screen.getByText("#2700")).toBeInTheDocument();
    expect(screen.getByText(/find_code \(mcp\)/)).toBeInTheDocument();
  });

  it("renders an explicit unavailable state when the endpoint fails", async () => {
    const client = { get: async () => { throw new Error("HTTP 503"); } } as unknown as EshuApiClient;
    render(<CapabilityMatrixPage client={client} />);
    await waitFor(() =>
      expect(screen.getByText("Capability catalog unavailable from this source.")).toBeInTheDocument()
    );
  });
});
