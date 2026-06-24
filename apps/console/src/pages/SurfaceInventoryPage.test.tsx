import { render, screen, waitFor } from "@testing-library/react";

import { SurfaceInventoryPage } from "./SurfaceInventoryPage";
import type { EshuApiClient } from "../api/client";

// SurfaceInventoryPage renders the surface inventory readiness catalog. It must:
// - show a loading state until the first page resolves
// - render surface rows (name, category, readiness lane, owner, proof, docs)
// - show each readiness lane honestly: at least one implemented, one gated, and
//   one foundation_only surface each render with their correct lane label
// - render an explicit unavailable state when the endpoint fails
// - never fabricate surfaces
describe("SurfaceInventoryPage", () => {
  it("shows the loading state until the inventory resolves", () => {
    const client = { get: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<SurfaceInventoryPage client={client} />);
    expect(screen.getByText("Loading surface inventory…")).toBeInTheDocument();
  });

  it("renders surfaces with their honest readiness lanes", async () => {
    const client = {
      get: async () => ({
        data: {
          version: "v1",
          total: 3,
          limit: 500,
          offset: 0,
          truncated: false,
          surfaces: [
            {
              category: "api_route",
              name: "GET /api/v0/capabilities",
              readiness: "implemented",
              owner: "internal/query",
              proof: "go test ./internal/query",
              docs: ["docs/public/reference/http-api.md"],
              notes: "reconciled catalog endpoint"
            },
            {
              category: "collector",
              name: "azure_cloud",
              readiness: "gated",
              owner: "internal/collector/azure",
              proof: "fixture replay",
              docs: [],
              notes: "live transport behind flag"
            },
            {
              category: "reducer_domain",
              name: "incidents_support",
              readiness: "foundation_only",
              owner: "cmd/reducer",
              proof: "",
              docs: [],
              notes: "loader seam, unwired"
            }
          ]
        },
        error: null,
        truth: { profile: "production", level: "exact", capability: "surface_inventory.list", freshness: { state: "fresh" } }
      })
    } as unknown as EshuApiClient;

    render(<SurfaceInventoryPage client={client} />);

    expect(await screen.findByText("GET /api/v0/capabilities")).toBeInTheDocument();
    expect(screen.getByText("azure_cloud")).toBeInTheDocument();
    expect(screen.getByText("incidents_support")).toBeInTheDocument();
    // Each readiness lane renders as a Badge with its honest label. "implemented"
    // also appears in the page intro and StatTile, so assert on the badge element
    // (a span carrying the badge class) to prove the lane is shown truthfully.
    const isBadge = (content: string) => (text: string, node: Element | null): boolean =>
      text === content && node !== null && node.tagName === "SPAN" && node.className.includes("badge");
    expect(screen.getByText(isBadge("implemented"))).toBeInTheDocument();
    expect(screen.getByText(isBadge("gated"))).toBeInTheDocument();
    expect(screen.getByText(isBadge("foundation only"))).toBeInTheDocument();
    expect(screen.getByText("docs/public/reference/http-api.md")).toBeInTheDocument();
    expect(screen.getByText("live transport behind flag")).toBeInTheDocument();
  });

  it("renders an explicit unavailable state when the endpoint fails", async () => {
    const client = { get: async () => { throw new Error("HTTP 503"); } } as unknown as EshuApiClient;
    render(<SurfaceInventoryPage client={client} />);
    await waitFor(() =>
      expect(screen.getByText("Surface inventory unavailable from this source.")).toBeInTheDocument()
    );
  });
});
