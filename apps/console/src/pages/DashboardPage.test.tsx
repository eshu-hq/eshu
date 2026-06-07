import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import type { EshuApiClient } from "../api/client";
import { DashboardPage } from "./DashboardPage";
import { demoModel } from "../console/demoModel";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

describe("DashboardPage", () => {
  it("renders runtime stat tiles and panels from the model", () => {
    render(<DashboardPage model={demoModel} />);

    expect(screen.getByText("Repositories")).toBeInTheDocument();
    expect(screen.getByText("Index status")).toBeInTheDocument();
    expect(screen.getByText("Queue outstanding")).toBeInTheDocument();
    expect(screen.getByText("Succeeded")).toBeInTheDocument();
    // Index status value from runtime.indexStatus.
    expect(screen.getByText("complete")).toBeInTheDocument();

    expect(
      screen.getByText("Code-to-cloud relationship atlas")
    ).toBeInTheDocument();
    expect(screen.getByText("Relationship coverage")).toBeInTheDocument();
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
  });

  it("opens the entity behind a finding row", () => {
    const onOpenService = vi.fn();
    render(<DashboardPage model={demoModel} onOpenService={onOpenService} />);

    fireEvent.click(screen.getByText("CVE-2024-0001 reachable in prod image"));

    expect(onOpenService).toHaveBeenCalledWith("checkout-service");
  });

  it("auto-seeds the live atlas from the first catalog service", async () => {
    const calls: unknown[] = [];
    const client = {
      postJson: async () => ({
        entities: [
          {
            entity_id: "workload:checkout-service",
            labels: ["Workload"],
            name: "checkout-service"
          }
        ]
      }),
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        return {
          data: {
            from: "checkout-service",
            resolution: {
              candidates: [
                {
                  id: "workload:checkout-service",
                  labels: ["Workload"],
                  name: "checkout-service"
                }
              ]
            },
            evidence: {
              relationships: [
                {
                  direction: "outgoing",
                  entity_id: "workload:payments-api",
                  entity_labels: ["Workload"],
                  entity_name: "payments-api",
                  relationship_type: "DEPENDS_ON"
                }
              ]
            }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(<DashboardPage model={liveModelWithServices()} client={client} />);

    await waitFor(() => {
      expect(screen.getByText("2 nodes · 1 edges")).toBeInTheDocument();
    });
    expect(await screen.findAllByText("checkout-service")).not.toHaveLength(0);
    expect(calls).toEqual([
      {
        path: "/api/v0/impact/entity-map",
        body: { from: "checkout-service", depth: 2 }
      }
    ]);
  });

  it("expands a clicked live atlas node through entity-map", async () => {
    const calls: unknown[] = [];
    const client = {
      postJson: async (_path: string, body: unknown) => {
        const name = requestName(body);
        return {
          entities: [
            {
              entity_id: `workload:${name}`,
              labels: ["Workload"],
              name
            }
          ]
        };
      },
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        const from = requestFrom(body);
        const related = from === "payments-api" ? "ledger-service" : "payments-api";
        return {
          data: {
            from,
            resolution: {
              candidates: [
                {
                  id: `workload:${from}`,
                  labels: ["Workload"],
                  name: from
                }
              ]
            },
            evidence: {
              relationships: [
                {
                  direction: "outgoing",
                  entity_id: `workload:${related}`,
                  entity_labels: ["Workload"],
                  entity_name: related,
                  relationship_type: "DEPENDS_ON"
                }
              ]
            }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(<DashboardPage model={liveModelWithServices()} client={client} />);

    await screen.findByText("payments-api");
    fireEvent.click(screen.getByText("payments-api"));

    expect(await screen.findByText("ledger-service")).toBeInTheDocument();
    expect(calls).toEqual([
      {
        path: "/api/v0/impact/entity-map",
        body: { from: "checkout-service", depth: 2 }
      },
      {
        path: "/api/v0/impact/entity-map",
        body: { from: "payments-api", depth: 2 }
      }
    ]);
  });

  it("does not invent an atlas seed when live data has no entities", () => {
    const client = {
      postJson: vi.fn(),
      post: vi.fn()
    } as unknown as EshuApiClient;

    render(<DashboardPage model={modelFromSnapshot(emptySnapshot("empty"))} client={client} />);

    expect(screen.getByText("No graph entities are available from the live model yet.")).toBeInTheDocument();
    expect(client.postJson).not.toHaveBeenCalled();
    expect(client.post).not.toHaveBeenCalled();
  });
});

function liveModelWithServices(): ConsoleModel {
  const model = modelFromSnapshot({
    ...emptySnapshot(),
    services: [
      {
        environments: ["prod"],
        freshness: "fresh",
        id: "svc-checkout",
        kind: "service",
        name: "checkout-service",
        repo: "checkout",
        truth: "exact"
      }
    ],
    provenance: { services: "live" },
    runtime: {
      deadLetters: 0,
      inFlight: 0,
      indexStatus: "complete",
      instances: 0,
      platforms: 0,
      profile: "local_full_stack",
      queueOutstanding: 0,
      repositories: 1,
      succeeded: 1,
      workloads: 1
    }
  });
  return model;
}

function requestFrom(body: unknown): string {
  if (typeof body === "object" && body !== null && "from" in body && typeof body.from === "string") {
    return body.from;
  }
  return "";
}

function requestName(body: unknown): string {
  if (typeof body === "object" && body !== null && "name" in body && typeof body.name === "string") {
    return body.name;
  }
  return "";
}
