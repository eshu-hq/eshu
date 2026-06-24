import { render, screen, fireEvent, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { RelationshipsPage } from "./RelationshipsPage";
import type { EshuApiClient } from "../api/client";
import { emptyConsoleModel } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

function liveModel(): ConsoleModel {
  const base = emptyConsoleModel();
  return { ...base, runtime: { ...base.runtime, repositories: 42 } };
}

const catalogEnvelope = {
  data: {
    verbs: [
      { verb: "IMPORTS", layer: "code", count: 2800, evidence: "npm Registry", detail: "File imports a module" },
      { verb: "RUNS_ON", layer: "runtime", count: 46, evidence: "Runtime placement", detail: "Instance runs on a platform" }
    ],
    verb_count: 2,
    total_edges: 2846,
    layer_count: 2
  },
  error: null,
  truth: { level: "exact", basis: "authoritative_graph", freshness: { state: "fresh" } }
};

const edgesEnvelope = {
  data: {
    verb: "IMPORTS",
    layer: "code",
    evidence: "npm Registry",
    detail: "File imports a module",
    edges: [
      { source_id: "f1", source_name: "server/index.ts", target_id: "m1", target_name: "express", evidence: "import statement" }
    ],
    truncated: false,
    limit: 50
  },
  error: null,
  truth: null
};

describe("RelationshipsPage", () => {
  it("renders verb tiles and catalog rows from the live catalog envelope", async () => {
    const client = {
      post: async (path: string) => (path.endsWith("/catalog") ? catalogEnvelope : edgesEnvelope)
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText("IMPORTS")).toBeInTheDocument();
    expect(screen.getByText("RUNS_ON")).toBeInTheDocument();
    expect(screen.getByText("npm Registry")).toBeInTheDocument();
  });

  it("lists concrete edges with endpoints after selecting a verb", async () => {
    const client = {
      post: async (path: string) => (path.endsWith("/catalog") ? catalogEnvelope : edgesEnvelope)
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>
    );

    fireEvent.click(await screen.findByText("IMPORTS"));

    expect(await screen.findByText("server/index.ts")).toBeInTheDocument();
    expect(screen.getByText("express")).toBeInTheDocument();
    expect(screen.getByText("import statement")).toBeInTheDocument();
  });

  it("filters the verb list by layer", async () => {
    const client = {
      post: async () => catalogEnvelope
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>
    );

    await screen.findByText("IMPORTS");
    // Target the layer toggle in the filter bar specifically; verb rows also
    // contain their layer name, so scope the query to the filter container.
    const filterBar = document.querySelector(".rel-layer-filter") as HTMLElement;
    const runtimeToggle = within(filterBar).getByRole("button", { name: /runtime/i });
    fireEvent.click(runtimeToggle);

    expect(screen.queryByText("IMPORTS")).not.toBeInTheDocument();
    expect(screen.getByText("RUNS_ON")).toBeInTheDocument();
  });

  it("surfaces an honest error banner when the catalog load fails", async () => {
    const client = {
      post: async () => ({ data: null, error: { code: "internal", message: "boom" }, truth: null })
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>
    );

    expect(await screen.findByText(/Relationships unavailable/)).toBeInTheDocument();
  });
});
