import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { NodesPage } from "./NodesPage";

// NodesPage renders the browsable graph-entity explorer backed by
// GET /api/v0/graph/entities. It must:
// - render KIND filter chips with live counts and stat tiles
// - list a kind's entities only after the chip is selected
// - filter by the name/account search
// - render an explicit unavailable state when the endpoint fails

const KIND_COUNTS = [
  { kind: "services", label: "Workload", count: 15 },
  { kind: "repositories", label: "Repository", count: 21 },
  { kind: "libraries", label: "Module", count: 6 }
];

function envelope(data: unknown): unknown {
  return {
    data,
    error: null,
    truth: { level: "exact", capability: "platform_impact.context_overview", freshness: { state: "fresh" }, profile: "production" }
  };
}

function mockClient(byKind: Record<string, unknown[]>): EshuApiClient {
  return {
    get: async (path: string) => {
      const url = new URL(path, "http://x");
      const kind = url.searchParams.get("kind");
      const q = (url.searchParams.get("q") ?? "").toLowerCase();
      if (!kind) {
        return envelope({ kinds: KIND_COUNTS, total: 42, entities: [], count: 0, limit: 50, offset: 0, truncated: false });
      }
      const rows = (byKind[kind] ?? []).filter((row) => {
        const name = (row as { name?: string }).name ?? "";
        return q === "" || name.toLowerCase().includes(q);
      });
      return envelope({ kinds: KIND_COUNTS, total: 42, entities: rows, count: rows.length, limit: 50, offset: 0, truncated: false });
    }
  } as unknown as EshuApiClient;
}

describe("NodesPage", () => {
  it("renders stat tiles and KIND chips with live counts", async () => {
    const client = mockClient({});
    render(<NodesPage client={client} sourceLabel="live" />, { wrapper: MemoryRouter });

    expect(screen.getByRole("heading", { name: "Nodes" })).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Services/ })).toBeInTheDocument();
    });
    // Chip count and stat-tile facet count.
    expect(screen.getByRole("button", { name: /Services 15/ })).toBeInTheDocument();
    expect(screen.getByText("Browsable entities")).toBeInTheDocument();
  });

  it("lists a kind's entities only after the chip is selected", async () => {
    const client = mockClient({
      services: [
        { id: "workload:eshu-api", name: "eshu-api", kind: "services", account: "repo://eshu" }
      ]
    });
    render(<NodesPage client={client} sourceLabel="live" />, { wrapper: MemoryRouter });

    // Before selection: prompt to pick a kind, no entity rows.
    await waitFor(() => {
      expect(screen.getByText("Select a kind above to browse its entities.")).toBeInTheDocument();
    });
    expect(screen.queryByText("eshu-api")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Services 15/ }));

    await waitFor(() => {
      expect(screen.getByText("eshu-api")).toBeInTheDocument();
    });
    expect(screen.getByText("repo://eshu")).toBeInTheDocument();
  });

  it("filters listed entities by the search box", async () => {
    const client = mockClient({
      services: [
        { id: "workload:eshu-api", name: "eshu-api", kind: "services", account: "repo://eshu" },
        { id: "workload:eshu-mcp", name: "eshu-mcp", kind: "services", account: "repo://eshu" }
      ]
    });
    render(<NodesPage client={client} sourceLabel="live" />, { wrapper: MemoryRouter });

    fireEvent.click(await screen.findByRole("button", { name: /Services 15/ }));
    await waitFor(() => expect(screen.getByText("eshu-api")).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText(/Find a node/i), { target: { value: "mcp" } });

    await waitFor(() => {
      expect(screen.getByText("eshu-mcp")).toBeInTheDocument();
    });
    expect(screen.queryByText("eshu-api")).not.toBeInTheDocument();
  });

  it("renders an unavailable state when the endpoint errors", async () => {
    const client = {
      get: async () => ({ data: null, error: { code: "unsupported_capability", message: "no graph" }, truth: null })
    } as unknown as EshuApiClient;
    render(<NodesPage client={client} sourceLabel="live" />, { wrapper: MemoryRouter });

    await waitFor(() => {
      expect(screen.getByText(/Graph entity inventory unavailable/)).toBeInTheDocument();
    });
  });
});
