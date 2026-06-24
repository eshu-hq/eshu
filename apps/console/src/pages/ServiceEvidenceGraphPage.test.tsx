import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { vi } from "vitest";

import { ServiceEvidenceGraphPage } from "./ServiceEvidenceGraphPage";
import type { EshuApiClient } from "../api/client";
import type { EshuEnvelope } from "../api/envelope";
import { emptySnapshot, modelFromSnapshot } from "../console/liveModel";

function liveModel() {
  return modelFromSnapshot(emptySnapshot("live"));
}

function modelWithService(name: string) {
  return modelFromSnapshot({
    ...emptySnapshot("live"),
    services: [{ id: `svc:${name}`, name, kind: "service", repo: `${name}-repo`, environments: [], truth: "exact", freshness: "fresh" }]
  });
}

function storyEnvelope(): EshuEnvelope<Record<string, unknown>> {
  return {
    data: {
      service_identity: { service_id: "svc-1", service_name: "payments", repo_id: "svc-repo" },
      upstream_dependencies: [
        { source: "billing", source_repo_id: "up-1", target_repo_id: "svc-repo", relationship_type: "DEPENDS_ON", confidence: 0.9 }
      ],
      downstream_consumers: {}
    },
    error: null,
    truth: { capability: "service.story.read", freshness: { state: "fresh" }, level: "exact", profile: "local_authoritative" }
  };
}

function deriveEnvelope(packet: Record<string, unknown>): EshuEnvelope<Record<string, unknown>> {
  return {
    data: { visualization_packet: packet },
    error: null,
    truth: { capability: "visualization.derive", freshness: { state: "fresh" }, level: "exact", profile: "local_authoritative" }
  };
}

function supportedPacket(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    view: "service_story",
    title: "payments",
    supported: true,
    nodes: [
      { id: "viznode:service", type: "service", label: "payments", category: "service", evidence_handle: { kind: "entity", repo_id: "svc-repo", entity_id: "svc-1" } },
      { id: "viznode:up-1", type: "repository", label: "billing", category: "upstream", truth_label: "exact", evidence_handle: { kind: "entity", repo_id: "up-1", entity_id: "up-1", evidence_family: "repository" } }
    ],
    edges: [
      { id: "vizedge:1", source: "viznode:up-1", target: "viznode:service", relationship: "DEPENDS_ON", truth_label: "exact" }
    ],
    truth: { level: "exact", basis: "authoritative_graph", freshness: { state: "fresh" } },
    limits: { max_nodes: 60, max_edges: 120, ordering: "stable_id", node_count: 2, edge_count: 1 },
    truncation: { truncated: false, dropped_node_count: 0, dropped_edge_count: 0 },
    limitations: [],
    recommended_next_calls: [],
    ...overrides
  };
}

function clientFor(deriveData: EshuEnvelope<Record<string, unknown>>): { client: EshuApiClient; paths: string[] } {
  const paths: string[] = [];
  const client = {
    get: vi.fn(async (path: string) => {
      paths.push(path);
      return storyEnvelope();
    }),
    post: vi.fn(async (path: string) => {
      paths.push(path);
      return deriveData;
    })
  } as unknown as EshuApiClient;
  return { client, paths };
}

function renderAt(path: string, client: EshuApiClient) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/service-story" element={<ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} />} />
        <Route path="/service-story/:serviceName" element={<ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} />} />
      </Routes>
    </MemoryRouter>
  );
}

describe("ServiceEvidenceGraphPage", () => {
  it("renders the heading and a service input", () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderAt("/service-story", client);
    expect(screen.getByRole("heading", { name: "Service evidence graph" })).toBeInTheDocument();
    expect(screen.getByLabelText("Service name")).toBeInTheDocument();
  });

  it("auto-loads a default catalog service on open when none is selected", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    render(
      <MemoryRouter initialEntries={["/service-story"]}>
        <Routes>
          <Route path="/service-story" element={<ServiceEvidenceGraphPage client={client} model={modelWithService("acme-app")} onOpenService={vi.fn()} />} />
          <Route path="/service-story/:serviceName" element={<ServiceEvidenceGraphPage client={client} model={modelWithService("acme-app")} onOpenService={vi.fn()} />} />
        </Routes>
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(paths).toEqual([
        "/api/v0/services/acme-app/story",
        "/api/v0/visualizations/derive"
      ]);
    });
    expect(await screen.findByText("billing")).toBeInTheDocument();
  });

  it("deep-loads a service story packet and renders nodes with truth labels", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    renderAt("/service-story/payments", client);

    await waitFor(() => {
      expect(paths).toEqual([
        "/api/v0/services/payments/story",
        "/api/v0/visualizations/derive"
      ]);
    });
    expect(await screen.findByText("billing")).toBeInTheDocument();
    expect(screen.getAllByText("payments").length).toBeGreaterThan(0);
    // Truth label is rendered as text, not color alone.
    expect(screen.getByText("visualization.derive")).toBeInTheDocument();
    expect(screen.getAllByText("exact").length).toBeGreaterThan(0);
    expect(screen.getByText("fresh")).toBeInTheDocument();
    // Bounded counts are visible so a partial subgraph is never read as complete.
    expect(screen.getByText(/of up to 60 nodes/)).toBeInTheDocument();
  });

  it("submits the form into a deep-linkable service story route", async () => {
    const { client, paths } = clientFor(deriveEnvelope(supportedPacket()));
    renderAt("/service-story", client);

    fireEvent.change(screen.getByLabelText("Service name"), { target: { value: "payments" } });
    fireEvent.click(screen.getByRole("button", { name: "Show evidence graph" }));

    await waitFor(() => {
      expect(paths).toEqual([
        "/api/v0/services/payments/story",
        "/api/v0/visualizations/derive"
      ]);
    });
  });

  it("keeps truncation visible when the subgraph is bounded", async () => {
    const truncated = supportedPacket({
      truncation: { truncated: true, dropped_node_count: 4, dropped_edge_count: 7, dropped_node_ids: ["viznode:z"] },
      limitations: ["source story response was already truncated; visualized subgraph is a bounded subset"]
    });
    const { client } = clientFor(deriveEnvelope(truncated));
    renderAt("/service-story/payments", client);

    expect(await screen.findByText(/Subgraph truncated/i)).toBeInTheDocument();
    expect(screen.getByText(/4 nodes/)).toBeInTheDocument();
    expect(screen.getByText(/7 edges/)).toBeInTheDocument();
    // The bounded-subset limitation also stays visible.
    expect(screen.getByText(/bounded subset/)).toBeInTheDocument();
  });

  it("keeps truncation visible even when every node was dropped", async () => {
    const allDropped = {
      view: "service_story",
      title: "payments",
      supported: true,
      nodes: [],
      edges: [],
      limits: { max_nodes: 60, max_edges: 120, ordering: "stable_id", node_count: 0, edge_count: 0 },
      truncation: { truncated: true, dropped_node_count: 5, dropped_edge_count: 9 }
    };
    const { client } = clientFor(deriveEnvelope(allDropped));
    renderAt("/service-story/payments", client);

    expect(await screen.findByText(/No graph rows/i)).toBeInTheDocument();
    expect(screen.getByText(/Subgraph truncated/i)).toBeInTheDocument();
    expect(screen.getByText(/5 nodes/)).toBeInTheDocument();
    expect(screen.getByText(/9 edges/)).toBeInTheDocument();
  });

  it("never asserts an 'up to 0' cap when the server omits limits", async () => {
    const noLimits = {
      view: "service_story",
      title: "payments",
      supported: true,
      nodes: [
        { id: "viznode:service", type: "service", label: "payments", category: "service" },
        { id: "viznode:up-1", type: "repository", label: "billing", category: "upstream" }
      ],
      edges: [{ id: "vizedge:1", source: "viznode:up-1", target: "viznode:service", relationship: "DEPENDS_ON" }]
    };
    const { client } = clientFor(deriveEnvelope(noLimits));
    renderAt("/service-story/payments", client);

    expect(await screen.findByText(/Showing 2 nodes and 1 edges/)).toBeInTheDocument();
    expect(screen.queryByText(/up to 0/)).not.toBeInTheDocument();
  });

  it("surfaces an unsupported packet with its limitations and next calls", async () => {
    const unsupported = {
      view: "service_story",
      supported: false,
      nodes: [],
      edges: [],
      limitations: ["service story response carried no identity, evidence graph, or dependency topology to visualize"],
      recommended_next_calls: [{ tool: "get_service_story", reason: "fetch a service story dossier first" }]
    };
    const { client } = clientFor(deriveEnvelope(unsupported));
    renderAt("/service-story/payments", client);

    expect(await screen.findByText(/No renderable subgraph/i)).toBeInTheDocument();
    expect(screen.getByText(/carried no identity/)).toBeInTheDocument();
    expect(screen.getByText("get_service_story")).toBeInTheDocument();
  });

  it("shows an empty state when a supported packet has no nodes", async () => {
    const empty = {
      view: "service_story",
      title: "payments",
      supported: true,
      nodes: [],
      edges: [],
      limits: { max_nodes: 60, max_edges: 120, ordering: "stable_id", node_count: 0, edge_count: 0 },
      truncation: { truncated: false }
    };
    const { client } = clientFor(deriveEnvelope(empty));
    renderAt("/service-story/payments", client);

    expect(await screen.findByText(/No graph rows/i)).toBeInTheDocument();
  });

  it("shows a story error without rendering a stale graph", async () => {
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "service not found" },
        truth: null
      })),
      post: vi.fn()
    } as unknown as EshuApiClient;

    renderAt("/service-story/ghost", client);
    expect(await screen.findByText("not_found: service not found")).toBeInTheDocument();
    expect(screen.queryByText("billing")).not.toBeInTheDocument();
  });

  it("clears a stale graph when navigating back to the bare route", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    function Nav(): React.JSX.Element {
      const navigate = useNavigate();
      return <button onClick={() => navigate("/service-story")} type="button">to bare</button>;
    }
    render(
      <MemoryRouter initialEntries={["/service-story/payments"]}>
        <Routes>
          <Route path="/service-story" element={<><Nav /><ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} /></>} />
          <Route path="/service-story/:serviceName" element={<><Nav /><ServiceEvidenceGraphPage client={client} model={liveModel()} onOpenService={vi.fn()} /></>} />
        </Routes>
      </MemoryRouter>
    );
    expect(await screen.findByText("billing")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "to bare" }));
    await waitFor(() => expect(screen.queryByText("billing")).not.toBeInTheDocument());
  });

  it("selects a node and opens the inline evidence panel", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderAt("/service-story/payments", client);

    const billing = await screen.findByText("billing");
    fireEvent.click(billing);

    const panel = await screen.findByRole("region", { name: /Evidence for billing/i });
    expect(within(panel).getByText("billing")).toBeInTheDocument();
    expect(within(panel).getByText(/upstream/)).toBeInTheDocument();
  });

  it("selects an evidence-lane pill and opens the inline evidence panel", async () => {
    const { client } = clientFor(deriveEnvelope(supportedPacket()));
    renderAt("/service-story/payments", client);

    await screen.findByText("billing");
    fireEvent.click(screen.getByRole("button", { name: /DEPENDS_ON/ }));

    const panel = await screen.findByRole("region", { name: /Evidence for DEPENDS_ON/i });
    expect(within(panel).getByText("DEPENDS_ON")).toBeInTheDocument();
  });
});
