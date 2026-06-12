import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TopologyPage } from "./TopologyPage";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";
import { emptyConsoleModel } from "../console/liveModel";

function model(): ConsoleModel {
  return {
    ...emptyConsoleModel(),
    services: [
      {
        freshness: "fresh",
        id: "svc-api",
        kind: "service",
        name: "api-node-boats",
        repo: "boats/api-node-boats",
        environments: ["bg-prod"],
        truth: "exact"
      }
    ]
  };
}

function storyEnvelope() {
  return {
    data: {
      edge_runtime_evidence: {
        cloudfront_distributions: [{
          aliases: ["www.boats.com"],
          domain_name: "d123.cloudfront.net",
          id: "E2BGBOATS",
          origins: [{ id: "origin-alb-boats" }]
        }]
      },
      service_identity: {
        repo_name: "boats/api-node-boats",
        service_name: "api-node-boats"
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

describe("TopologyPage", () => {
  it("loads the selected service topology from live story evidence", async () => {
    const client = {
      get: vi.fn(async (path: string) => {
        if (path.includes("/story")) return storyEnvelope();
        return { data: {}, error: null, truth: { freshness: { state: "fresh" }, level: "exact" } };
      })
    } as unknown as EshuApiClient;

    render(<TopologyPage client={client} model={model()} onOpenService={vi.fn()} />);

    expect(screen.getByRole("heading", { name: "Topology" })).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/services/{name}/story")).toBeInTheDocument();
    expect(screen.getByText("GET /api/v0/services/{name}/context")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("www.boats.com")).toBeInTheDocument());
    expect(screen.getByText("E2BGBOATS")).toBeInTheDocument();
    expect(screen.getByText("boats/api-node-boats")).toBeInTheDocument();
  });

  it("switches services through the picker without mutating when no live client exists", async () => {
    render(<TopologyPage client={undefined} model={model()} onOpenService={vi.fn()} />);

    expect(screen.getByText("Entry evidence pending")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Service"), { target: { value: "api-node-boats" } });
    expect(screen.getByText("Entry evidence pending")).toBeInTheDocument();
  });
});
