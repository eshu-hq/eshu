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
      {
        verb: "IMPORTS",
        layer: "code",
        count: 2800,
        evidence: "npm Registry",
        detail: "File imports a module",
      },
      {
        verb: "DEPENDS_ON",
        layer: "deploy",
        count: 280,
        evidence: "infrastructure tooling",
        detail: "Service depends on another",
        source_tools: { terraform: 215, helm: 65 },
      },
      {
        verb: "RUNS_ON",
        layer: "runtime",
        count: 46,
        evidence: "Runtime placement",
        detail: "Instance runs on a platform",
      },
    ],
    verb_count: 3,
    total_edges: 3126,
    layer_count: 3,
  },
  error: null,
  truth: { level: "exact", basis: "authoritative_graph", freshness: { state: "fresh" } },
};

const edgesEnvelope = {
  data: {
    verb: "IMPORTS",
    layer: "code",
    evidence: "npm Registry",
    detail: "File imports a module",
    edges: [
      {
        source_id: "f1",
        source_name: "server/index.ts",
        target_id: "m1",
        target_name: "express",
        evidence: "import statement",
      },
      {
        source_id: "f2",
        source_name: "src/auth.ts",
        target_id: "m2",
        target_name: "jsonwebtoken",
        evidence: "import statement",
        source_tool: "terraform",
      },
    ],
    truncated: false,
    limit: 50,
  },
  error: null,
  truth: null,
};

const filteredEdgesEnvelope = {
  data: {
    verb: "DEPENDS_ON",
    layer: "deploy",
    evidence: "infrastructure tooling",
    detail: "Service depends on another",
    edges: [
      {
        source_id: "r1",
        source_name: "checkout-service",
        target_id: "r2",
        target_name: "payments-api",
        evidence: "tf resource",
        source_tool: "terraform",
      },
    ],
    truncated: false,
    limit: 50,
    source_tool: "terraform",
  },
  error: null,
  truth: null,
};

describe("RelationshipsPage", () => {
  it("renders verb tiles and catalog rows from the live catalog envelope", async () => {
    const client = {
      post: async (path: string) => (path.endsWith("/catalog") ? catalogEnvelope : edgesEnvelope),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByText("IMPORTS")).toBeInTheDocument();
    expect(screen.getByText("RUNS_ON")).toBeInTheDocument();
    expect(screen.getByText("npm Registry")).toBeInTheDocument();
  });

  it("lists concrete edges with endpoints after selecting a verb", async () => {
    const client = {
      post: async (path: string) => (path.endsWith("/catalog") ? catalogEnvelope : edgesEnvelope),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByText("IMPORTS"));

    expect(await screen.findByText("server/index.ts")).toBeInTheDocument();
    expect(screen.getByText("express")).toBeInTheDocument();
    expect(screen.getByText("import statement")).toBeInTheDocument();
  });

  it("renders source_tool badge on edges that carry it", async () => {
    const client = {
      post: async (path: string) => (path.endsWith("/catalog") ? catalogEnvelope : edgesEnvelope),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByText("IMPORTS"));

    // The second edge carries source_tool: "terraform"; a Badge should appear.
    expect(await screen.findByText("terraform")).toBeInTheDocument();
    // The first edge has no source_tool; no badge for it (only one badge total).
    expect(screen.getAllByText("terraform")).toHaveLength(1);
  });

  it("renders source_tools breakdown under verb tiles that carry it", async () => {
    const client = {
      post: async () => catalogEnvelope,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    // DEPENDS_ON tile has source_tools: { terraform: 215, helm: 65 }
    expect(await screen.findByText(/terraform/)).toBeInTheDocument();
    expect(screen.getByText(/helm/)).toBeInTheDocument();
    // IMPORTS has no source_tools — no breakdown rendered for it.
  });

  it("renders the tool filter when source_tools are present", async () => {
    const client = {
      post: async () => catalogEnvelope,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    await screen.findByText("IMPORTS");
    const filterRegion = document.querySelector(".rel-tool-filter") as HTMLElement;
    expect(filterRegion).not.toBeNull();
    expect(within(filterRegion).getByRole("button", { name: /terraform/i })).toBeInTheDocument();
    expect(within(filterRegion).getByRole("button", { name: /helm/i })).toBeInTheDocument();
  });

  it("re-fetches edges with source_tool when a tool chip is clicked", async () => {
    const postCalls: Array<{ path: string; body: unknown }> = [];
    const client = {
      post: async (path: string, body: unknown) => {
        postCalls.push({ path, body });
        if (path.endsWith("/catalog")) return catalogEnvelope;
        return filteredEdgesEnvelope;
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    // Select a verb first.
    fireEvent.click(await screen.findByText("DEPENDS_ON"));
    await screen.findByText("checkout-service");

    // Click the terraform tool chip.
    const filterRegion = document.querySelector(".rel-tool-filter") as HTMLElement;
    fireEvent.click(within(filterRegion).getByRole("button", { name: /terraform/i }));

    // The next edges POST must include source_tool.
    const edgesCall = postCalls.filter((c) => c.path.endsWith("/edges")).at(-1);
    expect((edgesCall?.body as Record<string, unknown>)?.source_tool).toBe("terraform");

    // The active-filter note must appear.
    expect(await screen.findByText(/Filtered to/)).toBeInTheDocument();
  });

  it("filters the verb list by layer", async () => {
    const client = {
      post: async () => catalogEnvelope,
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
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
      post: async () => ({ data: null, error: { code: "internal", message: "boom" }, truth: null }),
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <RelationshipsPage model={liveModel()} client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByText(/Relationships unavailable/)).toBeInTheDocument();
  });
});
