import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { ExplorerPage } from "./ExplorerPage";
import { EshuApiHttpError, type EshuApiClient } from "../api/client";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

const liveModel: ConsoleModel = { ...demoModel, source: "live" };

function renderExplorer(client: EshuApiClient, q: string): void {
  render(
    <MemoryRouter initialEntries={[`/explorer?q=${encodeURIComponent(q)}`]}>
      <ExplorerPage model={liveModel} client={client} />
    </MemoryRouter>,
  );
}

describe("ExplorerPage failure and stale-result handling", () => {
  it("degrades a 404 from code/relationships to a clean empty-state hint, no error banner", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          { id: "content-entity:e1", name: "orphan", labels: ["Function"], type: "Function" },
        ],
      }),
      post: async () => {
        throw new EshuApiHttpError(404);
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "orphan");

    await waitFor(() =>
      expect(
        screen.getByText("No direct code relationships for this entity — try Neighborhood."),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByText(/HTTP 404/)).not.toBeInTheDocument();
  });

  it("surfaces a real server error (500) from code/relationships", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          { id: "content-entity:e1", name: "boom", labels: ["Function"], type: "Function" },
        ],
      }),
      post: async () => {
        throw new EshuApiHttpError(500);
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "boom");
    await waitFor(() => expect(screen.getByText(/HTTP 500/)).toBeInTheDocument());
  });

  it("surfaces resolver authorization failure instead of rendering a center-only success", async () => {
    const client = {
      postJson: async () => {
        throw new EshuApiHttpError(403);
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "private-service");

    expect(await screen.findByRole("alert")).toHaveTextContent("HTTP 403");
    expect(screen.queryByRole("button", { name: "Current center" })).not.toBeInTheDocument();
    expect(screen.getByText("Search for an entity to begin.")).toBeInTheDocument();
  });

  it("surfaces resolver timeout instead of rendering a center-only success", async () => {
    const client = {
      postJson: async () => {
        throw new Error("request timed out");
      },
    } as unknown as EshuApiClient;

    renderExplorer(client, "slow-service");

    expect(await screen.findByRole("alert")).toHaveTextContent("timed out");
    expect(screen.queryByRole("button", { name: "Current center" })).not.toBeInTheDocument();
  });

  it("renders a valid resolver no-match distinctly from an unavailable error", async () => {
    const client = {
      postJson: async () => ({ entities: [] }),
    } as unknown as EshuApiClient;

    renderExplorer(client, "missing-entity");

    expect(
      await screen.findByText('No indexed entity matched "missing-entity".'),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Current center" })).not.toBeInTheDocument();
  });

  it("keeps the newest search result when an older resolver completes last", async () => {
    let resolveOld!: (value: unknown) => void;
    const oldResolution = new Promise((resolve) => {
      resolveOld = resolve;
    });
    const client = {
      postJson: vi.fn(async (_path: string, body: unknown) => {
        const name = (body as { readonly name: string }).name;
        if (name === "old-service") return oldResolution;
        return {
          entities: [
            {
              id: "content-entity:new",
              name: "new-service",
              labels: ["Function"],
              type: "Function",
            },
          ],
        };
      }),
      post: vi.fn(async () => ({
        data: {
          entity_id: "content-entity:new",
          name: "new-service",
          labels: ["Function"],
          incoming: [],
          outgoing: [],
        },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;

    renderExplorer(client, "old-service");
    const input = screen.getByPlaceholderText("Entity / symbol / service name…");
    fireEvent.change(input, { target: { value: "new-service" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect((await screen.findAllByText("new-service")).length).toBeGreaterThan(0);

    resolveOld({ entities: [] });
    await waitFor(() =>
      expect(
        screen.queryByText('No indexed entity matched "old-service".'),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getAllByText("new-service").length).toBeGreaterThan(0);
  });

  it("discards the previous source and reloads when the live client changes", async () => {
    let resolveOld!: (value: unknown) => void;
    const oldResolution = new Promise((resolve) => {
      resolveOld = resolve;
    });
    const oldClient = {
      postJson: vi.fn(async () => oldResolution),
    } as unknown as EshuApiClient;
    const newClient = {
      postJson: vi.fn(async () => ({
        entities: [
          {
            id: "content-entity:new-source",
            name: "new-source-service",
            labels: ["Function"],
            type: "Function",
          },
        ],
      })),
      post: vi.fn(async () => ({
        data: {
          entity_id: "content-entity:new-source",
          name: "new-source-service",
          labels: ["Function"],
          incoming: [],
          outgoing: [],
        },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;

    const { rerender } = render(
      <MemoryRouter initialEntries={["/explorer?q=shared-service"]}>
        <ExplorerPage model={liveModel} client={oldClient} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(oldClient.postJson).toHaveBeenCalledOnce());

    rerender(
      <MemoryRouter initialEntries={["/explorer?q=shared-service"]}>
        <ExplorerPage model={liveModel} client={newClient} />
      </MemoryRouter>,
    );

    expect((await screen.findAllByText("new-source-service")).length).toBeGreaterThan(0);
    resolveOld({
      entities: [
        {
          id: "content-entity:old-source",
          name: "old-source-service",
          labels: ["Function"],
          type: "Function",
        },
      ],
    });
    await oldResolution;

    await waitFor(() => expect(screen.queryByText("old-source-service")).not.toBeInTheDocument());
    expect(screen.getAllByText("new-source-service").length).toBeGreaterThan(0);
    expect(newClient.postJson).toHaveBeenCalled();
  });

  it("clears a rendered graph while replacement-source truth is loading", async () => {
    let resolveNew!: (value: unknown) => void;
    const newResolution = new Promise((resolve) => {
      resolveNew = resolve;
    });
    const oldClient = {
      postJson: vi.fn(async () => ({
        entities: [
          {
            id: "content-entity:old-source",
            name: "old-source-service",
            labels: ["Function"],
            type: "Function",
          },
        ],
      })),
      post: vi.fn(async () => ({
        data: {
          entity_id: "content-entity:old-source",
          name: "old-source-service",
          labels: ["Function"],
          incoming: [],
          outgoing: [],
        },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;
    const newClient = {
      postJson: vi.fn(async () => newResolution),
      post: vi.fn(async () => ({
        data: {
          entity_id: "content-entity:new-source",
          name: "new-source-service",
          labels: ["Function"],
          incoming: [],
          outgoing: [],
        },
        error: null,
        truth: null,
      })),
    } as unknown as EshuApiClient;

    const { rerender } = render(
      <MemoryRouter initialEntries={["/explorer?q=shared-service"]}>
        <ExplorerPage model={liveModel} client={oldClient} />
      </MemoryRouter>,
    );
    expect((await screen.findAllByText("old-source-service")).length).toBeGreaterThan(0);

    rerender(
      <MemoryRouter initialEntries={["/explorer?q=shared-service"]}>
        <ExplorerPage model={liveModel} client={newClient} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.queryByText("old-source-service")).not.toBeInTheDocument());
    expect(screen.getByText("No graph rows returned from this source yet.")).toBeInTheDocument();
    await act(async () => {
      resolveNew({
        entities: [
          {
            id: "content-entity:new-source",
            name: "new-source-service",
            labels: ["Function"],
            type: "Function",
          },
        ],
      });
      await newResolution;
    });
    expect((await screen.findAllByText("new-source-service")).length).toBeGreaterThan(0);
    expect(screen.queryByText("old-source-service")).not.toBeInTheDocument();
  });
});

describe("ExplorerPage default auto-load (issue #3405)", () => {
  it("auto-loads the first catalog service when opened with no query", async () => {
    const calls: string[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:checkout-service",
            name: "checkout-service",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async (path: string) => {
        calls.push(path);
        return {
          data: {
            from: "checkout-service",
            resolution: {
              candidates: [
                { id: "workload:checkout-service", name: "checkout-service", labels: ["Workload"] },
              ],
            },
            evidence: {
              relationships: [
                {
                  entity_id: "repository:r1",
                  entity_name: "orders",
                  entity_labels: ["Repository"],
                  direction: "incoming",
                  relationship_type: "DEFINES",
                },
              ],
            },
          },
          error: null,
          truth: null,
        };
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter initialEntries={["/explorer"]}>
        <ExplorerPage model={liveModel} client={client} />
      </MemoryRouter>,
    );

    await waitFor(() => expect(calls).toContain("/api/v0/impact/entity-map"));
    expect(screen.getByPlaceholderText("Entity / symbol / service name…")).toHaveValue(
      "checkout-service",
    );
  });

  it("finishes the default load under StrictMode instead of staying busy", async () => {
    const client = {
      postJson: async () => ({
        entities: [
          {
            id: "workload:checkout-service",
            name: "checkout-service",
            labels: ["Workload"],
            type: "Workload",
          },
        ],
      }),
      post: async () => ({
        data: {
          from: "checkout-service",
          resolution: { candidates: [] },
          evidence: { relationships: [] },
        },
        error: null,
        truth: null,
      }),
    } as unknown as EshuApiClient;

    render(
      <StrictMode>
        <MemoryRouter initialEntries={["/explorer"]}>
          <ExplorerPage model={liveModel} client={client} />
        </MemoryRouter>
      </StrictMode>,
    );

    await waitFor(() => expect(screen.getByRole("button", { name: "Load" })).toBeEnabled());
  });
});
