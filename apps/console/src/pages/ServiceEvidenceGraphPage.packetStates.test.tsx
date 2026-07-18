import { screen } from "@testing-library/react";

import {
  clientFor,
  deriveEnvelope,
  renderServiceEvidenceGraphAt,
  supportedPacket,
} from "./ServiceEvidenceGraphPage.testSupport";

describe("ServiceEvidenceGraphPage packet states", () => {
  it("keeps known truncation counts visible", async () => {
    const packet = supportedPacket({
      truncation: {
        truncated: true,
        dropped_node_count: 4,
        dropped_edge_count: 7,
        dropped_node_ids: ["viznode:z"],
      },
      limitations: [
        "source story response was already truncated; visualized subgraph is a bounded subset",
      ],
    });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/Subgraph truncated/i)).toBeInTheDocument();
    expect(screen.getByText(/4 nodes/)).toBeInTheDocument();
    expect(screen.getByText(/7 edges/)).toBeInTheDocument();
    expect(screen.getByText(/bounded subset/)).toBeInTheDocument();
  });

  it("does not invent zero drop counts for an already-truncated source story", async () => {
    const packet = supportedPacket({
      truncation: { truncated: true, dropped_node_count: 0, dropped_edge_count: 0 },
      limitations: [
        "source story response was already truncated; visualized subgraph is a bounded subset",
      ],
    });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/Source story was already truncated/i)).toBeInTheDocument();
    expect(screen.queryByText(/0 nodes and 0 edges dropped/i)).not.toBeInTheDocument();
  });

  it("keeps truncation visible even when every node was dropped", async () => {
    const packet = supportedPacket({
      nodes: [],
      edges: [],
      limits: {
        max_nodes: 60,
        max_edges: 120,
        ordering: "stable_id",
        node_count: 0,
        edge_count: 0,
      },
      truncation: { truncated: true, dropped_node_count: 5, dropped_edge_count: 9 },
    });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/No graph rows/i)).toBeInTheDocument();
    expect(screen.getByText(/Subgraph truncated/i)).toBeInTheDocument();
    expect(screen.getByText(/5 nodes/)).toBeInTheDocument();
    expect(screen.getByText(/9 edges/)).toBeInTheDocument();
  });

  it("never asserts an up-to-zero cap when the server omits limits", async () => {
    const packet = supportedPacket({ limits: undefined });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/Showing 2 nodes and 1 edges/)).toBeInTheDocument();
    expect(screen.queryByText(/up to 0/)).not.toBeInTheDocument();
  });

  it("surfaces an unsupported packet with its limitations and next calls", async () => {
    const packet = {
      view: "service_story",
      supported: false,
      nodes: [],
      edges: [],
      limitations: [
        "service story response carried no identity, evidence graph, or dependency topology to visualize",
      ],
      recommended_next_calls: [
        { tool: "get_service_story", reason: "fetch a service story dossier first" },
      ],
    };
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/No renderable subgraph/i)).toBeInTheDocument();
    expect(screen.getByText(/carried no identity/)).toBeInTheDocument();
    expect(screen.getByText("get_service_story")).toBeInTheDocument();
  });

  it("shows an empty state when a supported packet has no nodes", async () => {
    const packet = supportedPacket({
      nodes: [],
      edges: [],
      limits: {
        max_nodes: 60,
        max_edges: 120,
        ordering: "stable_id",
        node_count: 0,
        edge_count: 0,
      },
    });
    const { client } = clientFor(deriveEnvelope(packet));
    renderServiceEvidenceGraphAt("/service-story/payments", client);

    expect(await screen.findByText(/No graph rows/i)).toBeInTheDocument();
  });
});
