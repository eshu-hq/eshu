import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { ExplorerPage } from "./ExplorerPage";
import { demoModel } from "../console/demoModel";
import { EshuApiHttpError } from "../api/client";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";

const liveModel: ConsoleModel = { ...demoModel, source: "live" };

function renderExplorer(client: EshuApiClient, q: string): void {
  render(
    <MemoryRouter initialEntries={[`/explorer?q=${encodeURIComponent(q)}`]}>
      <ExplorerPage model={liveModel} client={client} />
    </MemoryRouter>
  );
}

describe("ExplorerPage mode-by-kind (issue #1725)", () => {
  it("auto-selects Neighborhood for a service kind and loads the entity map", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [{ id: "workload:api-node-boats", name: "api-node-boats", labels: ["Workload"], type: "Workload" }]
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            from: "api-node-boats",
            resolution: { candidates: [{ id: "workload:api-node-boats", name: "api-node-boats", labels: ["Workload"] }] },
            evidence: { relationships: [{ entity_id: "repository:r1", entity_name: "boats", entity_labels: ["Repository"], direction: "incoming", relationship_type: "DEFINES" }] }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    renderExplorer(client, "api-node-boats");

    // The service search must route to impact/entity-map, never code/relationships.
    await waitFor(() => expect(calls).toContain("/api/v0/impact/entity-map"));
    expect(calls).not.toContain("/api/v0/code/relationships");
    // The Neighborhood toggle is now active.
    expect(screen.getByRole("button", { name: "Neighborhood" }).className).toContain("active");
  });

  it("keeps Direct for a code (Function) kind and loads code/relationships", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [{ id: "content-entity:e1", name: "createNewVersion", labels: ["Function"], type: "Function" }]
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            entity_id: "content-entity:e1", name: "createNewVersion", labels: ["Function"],
            incoming: [{ type: "CALLS", source_id: "content-entity:e2", source_name: "main" }], outgoing: []
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    renderExplorer(client, "createNewVersion");

    await waitFor(() => expect(calls).toContain("/api/v0/code/relationships"));
    expect(calls).not.toContain("/api/v0/impact/entity-map");
    expect(screen.getByRole("button", { name: "Direct" }).className).toContain("active");
  });

  it("degrades a 404 from code/relationships to a clean empty-state hint, no error banner", async () => {
    // A function-kind resolve keeps Direct, but the endpoint 404s (category
    // mismatch). The page must show the empty-state hint, not the HTTP error.
    const client = {
      postJson: async () => ({
        entities: [{ id: "content-entity:e1", name: "orphan", labels: ["Function"], type: "Function" }]
      }),
      post: async () => { throw new EshuApiHttpError(404); }
    } as unknown as EshuApiClient;

    renderExplorer(client, "orphan");

    await waitFor(() =>
      expect(screen.getByText("No direct code relationships for this entity — try Neighborhood.")).toBeInTheDocument()
    );
    expect(screen.queryByText(/HTTP 404/)).not.toBeInTheDocument();
  });

  it("surfaces a real server error (500) from code/relationships", async () => {
    const client = {
      postJson: async () => ({
        entities: [{ id: "content-entity:e1", name: "boom", labels: ["Function"], type: "Function" }]
      }),
      post: async () => { throw new EshuApiHttpError(500); }
    } as unknown as EshuApiClient;

    renderExplorer(client, "boom");

    await waitFor(() => expect(screen.getByText(/HTTP 500/)).toBeInTheDocument());
  });
});
