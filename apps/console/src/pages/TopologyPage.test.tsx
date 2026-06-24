import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TopologyPage } from "./TopologyPage";
import type { EshuApiClient } from "../api/client";
import { emptyConsoleModel } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

function modelWithServices(): ConsoleModel {
  return {
    ...emptyConsoleModel(),
    services: [
      {
        freshness: "fresh",
        id: "svc-alpha",
        kind: "service",
        name: "alpha-service",
        repo: "org/alpha-service",
        environments: ["prod"],
        truth: "exact"
      }
    ]
  };
}

function emptyModel(): ConsoleModel {
  return emptyConsoleModel();
}

function storyEnvelope() {
  return {
    data: {
      edge_runtime_evidence: {
        cloudfront_distributions: [{
          aliases: ["alpha.example.com"],
          domain_name: "d123.cloudfront.net",
          id: "EDIST-ALPHA",
          origins: [{ id: "origin-alb-alpha" }]
        }]
      },
      service_identity: {
        repo_name: "org/alpha-service",
        service_name: "alpha-service"
      }
    },
    error: null,
    truth: {
      level: "exact",
      profile: "local_authoritative",
      freshness: { state: "fresh" }
    }
  };
}

function catalogWithRepositories() {
  return {
    repositories: [
      { id: "repo-gamma", name: "gamma-service", repo_slug: "org/gamma-service" },
      { id: "repo-delta", name: "delta-service", repo_slug: "org/delta-service" }
    ]
  };
}

describe("TopologyPage", () => {
  it("loads the selected service topology from live story evidence", async () => {
    const client = {
      get: vi.fn(async (path: string) => {
        if (path.includes("/story")) return storyEnvelope();
        return { data: {}, error: null, truth: { freshness: { state: "fresh" }, level: "exact" } };
      }),
      getJson: vi.fn(async () => ({ services: [], workloads: [], repositories: [] }))
    } as unknown as EshuApiClient;

    render(<TopologyPage client={client} model={modelWithServices()} onOpenService={vi.fn()} />);

    expect(screen.getByRole("heading", { name: "Topology" })).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/services/{name}/story")).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/services/{name}/context")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("alpha.example.com")).toBeInTheDocument());
    expect(screen.getByText("EDIST-ALPHA")).toBeInTheDocument();
    expect(screen.getByText("org/alpha-service")).toBeInTheDocument();
  });

  it("switches services through the picker without mutating when no live client exists", async () => {
    render(<TopologyPage client={undefined} model={modelWithServices()} onOpenService={vi.fn()} />);

    expect(screen.getByText("Entry evidence pending")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Service"), { target: { value: "alpha-service" } });
    expect(screen.getByText("Entry evidence pending")).toBeInTheDocument();
  });

  it("fetches services from the catalog when model.services is empty", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: {},
        error: null,
        truth: { freshness: { state: "fresh" }, level: "exact" }
      })),
      getJson: vi.fn(async (path: string) => {
        if (path.includes("/catalog")) return catalogWithRepositories();
        return {};
      })
    } as unknown as EshuApiClient;

    render(<TopologyPage client={client} model={emptyModel()} onOpenService={vi.fn()} />);

    // Picker should populate from the catalog repository list.
    await waitFor(() =>
      expect(screen.getByRole("option", { name: "gamma-service" })).toBeInTheDocument()
    );
    expect(screen.getByRole("option", { name: "delta-service" })).toBeInTheDocument();
    // First entry (sorted: delta < gamma) should be selected by default.
    expect((screen.getByLabelText("Service") as HTMLSelectElement).value).toBe("delta-service");
  });

  it("shows loading state while catalog is being fetched", async () => {
    let resolveCatalog!: (v: unknown) => void;
    const catalogPromise = new Promise((resolve) => { resolveCatalog = resolve; });

    const client = {
      get: vi.fn(async () => ({
        data: {}, error: null, truth: { freshness: { state: "fresh" }, level: "exact" }
      })),
      getJson: vi.fn(async (path: string) => {
        if (path.includes("/catalog")) return catalogPromise;
        return {};
      })
    } as unknown as EshuApiClient;

    render(<TopologyPage client={client} model={emptyModel()} onOpenService={vi.fn()} />);

    // While the catalog fetch is pending, the badge shows "loading".
    expect(screen.getByText("loading")).toBeInTheDocument();

    resolveCatalog(catalogWithRepositories());
    await waitFor(() =>
      expect(screen.getByRole("option", { name: "gamma-service" })).toBeInTheDocument()
    );
  });
});
