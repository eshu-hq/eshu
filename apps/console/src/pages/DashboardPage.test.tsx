import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import { DashboardPage } from "./DashboardPage";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import { demoModel } from "../console/demoModel";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";
import type { ConsoleModel } from "../console/types";

describe("DashboardPage", () => {
  it("renders runtime stat tiles and panels from the model", () => {
    render(<DashboardPage model={demoModel} />);

    const atlasTitle = screen.getByText("Code-to-cloud topology");
    const statTitle = screen.getByText("Graph nodes");
    expect(Boolean(atlasTitle.compareDocumentPosition(statTitle) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);

    expect(screen.getByText("Graph nodes")).toBeInTheDocument();
    expect(screen.getByText("Relationships")).toBeInTheDocument();
    expect(screen.getByText("Indexed repos")).toBeInTheDocument();
    expect(screen.getByText("Queue outstanding")).toBeInTheDocument();

    expect(screen.getByText("Hot entities")).toBeInTheDocument();
    expect(screen.getByText("Seeded from the live graph neighbourhood (probes capped at 8).")).toBeInTheDocument();
    expect(screen.getByText(/click any node or relationship edge to read its evidence/i)).toBeInTheDocument();
    expect(screen.getByText("Relationship coverage")).toBeInTheDocument();
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
  });

  it("filters the landing topology by layer without fetching new data", () => {
    render(<DashboardPage model={demoModel} />);

    expect(screen.getByText("9 nodes · 8 edges")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "runtime 4" }));

    expect(screen.getByText("5 nodes · 4 edges")).toBeInTheDocument();
    expect(screen.queryByText("9 nodes · 8 edges")).not.toBeInTheDocument();
  });

  it("marks graph count metrics unavailable instead of fabricating demo numbers", () => {
    render(<DashboardPage model={modelFromSnapshot(emptySnapshot("live"))} />);

    expect(screen.getByText("node count metric unavailable")).toBeInTheDocument();
    expect(screen.getByText("relationship count metric unavailable")).toBeInTheDocument();
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
    expect(screen.getByText("1 typed verbs observed")).toBeInTheDocument();
    expect(screen.getByText("DEPENDS_ON → payments-api")).toBeInTheDocument();
    expect(screen.queryByText("DEPENDS_ON → workload:payments-api")).not.toBeInTheDocument();
    expect(calls).toEqual([
      {
        path: "/api/v0/impact/entity-map",
        body: { from: "checkout-service", depth: 2 }
      }
    ]);
  });

  it("skips a trivial seed and lands the atlas on a higher-degree service", async () => {
    // First catalog service resolves to a trivial self-edge graph (2 nodes /
    // 1 edge); the second resolves to a meaningful neighbourhood. The atlas
    // must fall through to the meaningful one instead of opening on the weak
    // landing graph.
    const calls: unknown[] = [];
    const client = {
      postJson: async () => ({ entities: [] }),
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        const from = requestFrom(body);
        if (from === "trivial-service") {
          return {
            data: {
              from,
              resolution: { candidates: [{ id: "workload:trivial-service", labels: ["Workload"], name: "trivial-service" }] },
              // Self-edge only: the lone neighbour carries the same name as the
              // center, producing the weak 2-node / 1-edge landing graph.
              evidence: { relationships: [{ direction: "incoming", entity_name: "trivial-service", entity_labels: ["Repository"], relationship_types: [] }] }
            },
            error: null,
            truth: null
          };
        }
        return {
          data: {
            from,
            resolution: { candidates: [{ id: "workload:hub-service", labels: ["Repository"], name: "hub-service" }] },
            evidence: { relationships: [
              { direction: "incoming", entity_id: "workload:a", entity_name: "neighbour-a", entity_labels: ["Repository"], relationship_types: ["DEPENDS_ON"] },
              { direction: "incoming", entity_id: "workload:b", entity_name: "neighbour-b", entity_labels: ["Repository"], relationship_types: ["DEPENDS_ON"] }
            ] }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(<DashboardPage model={liveModelWithTrivialThenHub()} client={client} />);

    await waitFor(() => {
      expect(screen.getByText("3 nodes · 2 edges")).toBeInTheDocument();
    });
    expect(await screen.findByText("neighbour-a")).toBeInTheDocument();
    expect(await screen.findByText("neighbour-b")).toBeInTheDocument();
    // It probed the trivial seed first, then settled on the meaningful one.
    expect(calls).toEqual([
      { path: "/api/v0/impact/entity-map", body: { from: "trivial-service", depth: 2 } },
      { path: "/api/v0/impact/entity-map", body: { from: "hub-service", depth: 2 } }
    ]);
  });

  it("keeps the best available seed when no candidate clears the threshold", async () => {
    // Every candidate is trivial (1 edge). The atlas must still render the
    // first candidate's graph rather than blanking — no fabricated edges.
    const calls: unknown[] = [];
    const client = {
      postJson: async () => ({ entities: [] }),
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        const from = requestFrom(body);
        return {
          data: {
            from,
            resolution: { candidates: [{ id: `workload:${from}`, labels: ["Workload"], name: from }] },
            evidence: { relationships: [{ direction: "incoming", entity_name: from, entity_labels: ["Repository"], relationship_types: [] }] }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(<DashboardPage model={liveModelWithTrivialThenHub()} client={client} />);

    await waitFor(() => {
      expect(screen.getByText("2 nodes · 1 edges")).toBeInTheDocument();
    });
    // Probed both candidates, neither cleared the bar, fell back to the first.
    expect(calls).toEqual([
      { path: "/api/v0/impact/entity-map", body: { from: "trivial-service", depth: 2 } },
      { path: "/api/v0/impact/entity-map", body: { from: "hub-service", depth: 2 } }
    ]);
    expect(await screen.findAllByText("trivial-service")).not.toHaveLength(0);
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

  it("keeps the latest clicked atlas node when live expansion responses finish out of order", async () => {
    const resolvers = new Map<string, (value: unknown) => void>();
    const client = {
      postJson: async (_path: string, body: unknown) => ({
        entities: [
          {
            entity_id: `workload:${requestName(body)}`,
            labels: ["Workload"],
            name: requestName(body)
          }
        ]
      }),
      post: (path: string, body: unknown) => new Promise((resolve) => {
        resolvers.set(requestFrom(body), resolve);
        if (path !== "/api/v0/impact/entity-map") {
          throw new Error(`unexpected path ${path}`);
        }
      })
    } as unknown as EshuApiClient;

    render(<DashboardPage model={liveModelWithServices()} client={client} />);

    await waitFor(() => {
      expect(resolvers.has("checkout-service")).toBe(true);
    });
    resolveEntityMap(resolvers, "checkout-service", "payments-api");
    await screen.findByText("payments-api");

    fireEvent.click(graphLabel("checkout-service"));
    await waitFor(() => {
      expect(resolvers.has("checkout-service")).toBe(true);
    });
    fireEvent.click(graphLabel("payments-api"));
    await waitFor(() => {
      expect(resolvers.has("payments-api")).toBe(true);
    });
    resolveEntityMap(resolvers, "payments-api", "ledger-service");
    await screen.findByText("ledger-service");
    resolveEntityMap(resolvers, "checkout-service", "stale-service");

    await waitFor(() => {
      expect(screen.getByText("ledger-service")).toBeInTheDocument();
    });
    expect(screen.queryByText("stale-service")).not.toBeInTheDocument();
  });

  it("seeds the live atlas from indexed repositories when the catalog has no services", async () => {
    // The live catalog (/api/v0/catalog) is empty, but 951 repositories are
    // indexed. The atlas must fall back to seeding from the repository
    // neighbourhood instead of blanking the landing visualization.
    const calls: unknown[] = [];
    const client = {
      postJson: async () => ({ entities: [] }),
      post: async (path: string, body: unknown) => {
        calls.push({ path, body });
        return {
          data: {
            from: "platform-repo",
            resolution: {
              candidates: [
                { id: "repository:platform-repo", labels: ["Repository"], name: "platform-repo" }
              ]
            },
            evidence: {
              relationships: [
                {
                  direction: "outgoing",
                  entity_id: "workload:checkout-service",
                  entity_labels: ["Workload"],
                  entity_name: "checkout-service",
                  relationship_type: "DEFINES"
                }
              ]
            }
          },
          error: null,
          truth: null
        };
      }
    } as unknown as EshuApiClient;

    render(
      <DashboardPage
        model={liveModelWithRepositoriesOnly()}
        client={client}
        repositories={[repoItem("repository:platform-repo", "platform-repo")]}
      />
    );

    await waitFor(() => {
      expect(screen.getByText("2 nodes · 1 edges")).toBeInTheDocument();
    });
    expect(await screen.findByText("checkout-service")).toBeInTheDocument();
    expect(await screen.findAllByText("platform-repo")).not.toHaveLength(0);
    expect(calls).toEqual([
      { path: "/api/v0/impact/entity-map", body: { from: "platform-repo", depth: 2 } }
    ]);
  });

  it("does not invent an atlas seed when live data has no entities", () => {
    const client = {
      postJson: vi.fn(),
      post: vi.fn()
    } as unknown as EshuApiClient;

    render(<DashboardPage model={modelFromSnapshot(emptySnapshot("empty"))} client={client} repositories={[]} />);

    expect(screen.getByText("No graph entities are available from the live model yet.")).toBeInTheDocument();
    expect(client.postJson).not.toHaveBeenCalled();
    expect(client.post).not.toHaveBeenCalled();
  });
});

function repoItem(id: string, name: string): RepoListItem {
  return {
    groupKey: "",
    groupKind: "",
    groupReason: "",
    groupSource: "",
    groupTruth: "",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: name
  };
}

function liveModelWithRepositoriesOnly(): ConsoleModel {
  return modelFromSnapshot({
    ...emptySnapshot(),
    provenance: { runtime: "live" },
    runtime: {
      deadLetters: 0, inFlight: 0, indexStatus: "complete", instances: 0, platforms: 0,
      profile: "local_full_stack", queueOutstanding: 0, repositories: 951, succeeded: 951, workloads: 0
    }
  });
}

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

function liveModelWithTrivialThenHub(): ConsoleModel {
  return modelFromSnapshot({
    ...emptySnapshot(),
    services: [
      { environments: [], freshness: "fresh", id: "svc-trivial", kind: "service", name: "trivial-service", repo: "trivial", truth: "exact" },
      { environments: [], freshness: "fresh", id: "svc-hub", kind: "service", name: "hub-service", repo: "hub", truth: "exact" }
    ],
    provenance: { services: "live" },
    runtime: {
      deadLetters: 0, inFlight: 0, indexStatus: "complete", instances: 0, platforms: 0,
      profile: "local_full_stack", queueOutstanding: 0, repositories: 2, succeeded: 2, workloads: 2
    }
  });
}

function resolveEntityMap(resolvers: Map<string, (value: unknown) => void>, from: string, related: string): void {
  const resolve = resolvers.get(from);
  if (!resolve) throw new Error(`missing resolver for ${from}`);
  resolvers.delete(from);
  resolve({
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
  });
}

function graphLabel(label: string): HTMLElement {
  const text = screen.getAllByText(label).find((element) => element.tagName.toLowerCase() === "text");
  if (!text) throw new Error(`missing graph label ${label}`);
  return text;
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
