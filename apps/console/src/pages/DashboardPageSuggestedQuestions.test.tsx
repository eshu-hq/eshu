import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { DashboardPage } from "./DashboardPage";
import type { EshuApiClient } from "../api/client";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

describe("DashboardPage suggested questions", () => {
  it("renders source-backed suggestions as query route links", async () => {
    const client = suggestionClient();

    render(
      <MemoryRouter>
        <DashboardPage model={modelWithSuggestions()} client={client} />
      </MemoryRouter>,
    );

    expect(screen.getByText("Suggested questions")).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: /Why is routeCheckout a hot graph entity/i }),
    ).toHaveAttribute("href", "/explorer?q=routeCheckout");
    expect(
      screen.getByRole("link", {
        name: /What changed in checkout-api since the prior generation/i,
      }),
    ).toHaveAttribute("href", "/repositories/repository%3Ar1/source");
    expect(
      screen.getByRole("link", { name: /Which services are exposed to CVE-2026-1234/i }),
    ).toHaveAttribute("href", "/vulnerabilities/CVE-2026-1234");
    expect(screen.getByText("POST /api/v0/ecosystem/graph-summary")).toBeInTheDocument();
  });

  it("renders an unavailable state when suggestion reads fail authorization", async () => {
    const client = {
      get: async () => {
        throw new Error("permission denied");
      },
      post: async () => {
        throw new Error("permission denied");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <DashboardPage model={modelWithSuggestions()} client={client} />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Suggested questions are unavailable",
    );
    expect(
      screen.queryByText("No source-backed suggestions from this snapshot."),
    ).not.toBeInTheDocument();
  });

  it("keeps successful suggestions and names a failed source", async () => {
    const base = suggestionClient();
    const client = {
      ...base,
      post: async () => {
        throw new Error("graph summary denied");
      },
    } as unknown as EshuApiClient;

    render(
      <MemoryRouter>
        <DashboardPage model={modelWithSuggestions()} client={client} />
      </MemoryRouter>,
    );

    expect(
      await screen.findByRole("link", { name: /Which services are exposed to CVE-2026-1234/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("graph summary");
  });

  it("clears prior-client suggestions while the replacement client loads", async () => {
    let resolveRepositories!: (value: unknown) => void;
    const delayed = {
      get: (path: string) =>
        path.startsWith("/api/v0/repositories")
          ? new Promise((resolve) => {
              resolveRepositories = resolve;
            })
          : Promise.resolve({ data: { findings: [] }, error: null, truth: null }),
      post: async () => ({ data: { hot_entities: [] }, error: null, truth: null }),
    } as unknown as EshuApiClient;
    const { rerender } = render(
      <MemoryRouter>
        <DashboardPage model={modelWithSuggestions()} client={suggestionClient()} />
      </MemoryRouter>,
    );
    expect(await screen.findByRole("link", { name: /Why is routeCheckout/i })).toBeInTheDocument();

    rerender(
      <MemoryRouter>
        <DashboardPage model={modelWithSuggestions()} client={delayed} />
      </MemoryRouter>,
    );

    expect(screen.queryByRole("link", { name: /Why is routeCheckout/i })).not.toBeInTheDocument();
    resolveRepositories({ data: { repositories: [] }, error: null, truth: null });
  });
});

function modelWithSuggestions(): ConsoleModel {
  return modelFromSnapshot(emptySnapshot("live"));
}

function suggestionClient(): EshuApiClient {
  return {
    get: async (path: string) => {
      if (path.startsWith("/api/v0/repositories")) {
        return {
          data: { repositories: [{ id: "repository:r1", name: "checkout-api" }] },
          error: null,
          truth: null,
        };
      }
      if (path.startsWith("/api/v0/freshness/generations")) {
        return {
          data: {
            generations: [
              { generation_id: "gen-current", is_active: true, status: "active" },
              { generation_id: "gen-prior", is_active: false, status: "superseded" },
            ],
          },
          error: null,
          truth: null,
        };
      }
      if (path.startsWith("/api/v0/freshness/changed-since")) {
        return {
          data: { categories: [{ name: "facts", counts: { added: 1, updated: 1 } }] },
          error: null,
          truth: null,
        };
      }
      if (path.includes("severity=critical")) {
        return { data: { findings: [] }, error: null, truth: null };
      }
      if (path.includes("severity=high")) {
        return {
          data: {
            findings: [
              {
                advisory_id: "CVE-2026-1234",
                cvss_score: 8.4,
                package_name: "openssl",
                service_ids: ["workload:checkout-api"],
                severity: "high",
              },
            ],
          },
          error: null,
          truth: null,
        };
      }
      throw new Error(`unexpected GET ${path}`);
    },
    post: async (path: string) => {
      if (path === "/api/v0/ecosystem/graph-summary") {
        return {
          data: {
            hot_entities: [
              {
                file_path: "src/router.ts",
                function_id: "content-entity:routeCheckout",
                function_name: "routeCheckout",
                incoming_calls: 2,
                outgoing_calls: 5,
                total_degree: 7,
              },
            ],
          },
          error: null,
          truth: null,
        };
      }
      throw new Error(`unexpected POST ${path}`);
    },
  } as unknown as EshuApiClient;
}
