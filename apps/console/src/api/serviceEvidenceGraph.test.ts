import { describe, expect, it, vi } from "vitest";

import { normalizeVisualizationPacket } from "./answerVisualization";
import { EshuApiHttpError, type EshuApiClient } from "./client";
import type { EshuEnvelope } from "./envelope";
import {
  loadServiceEvidenceGraph,
  serviceStoryGraph
} from "./serviceEvidenceGraph";

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

function packetEnvelope(packet: unknown): EshuEnvelope<Record<string, unknown>> {
  return {
    data: { visualization_packet: packet },
    error: null,
    truth: { capability: "visualization.derive", freshness: { state: "fresh" }, level: "exact", profile: "local_authoritative" }
  };
}

function supportedPacket(): Record<string, unknown> {
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
    recommended_next_calls: []
  };
}

describe("normalizeVisualizationPacket limits and truncation", () => {
  it("preserves bounded limits and truncation counts", () => {
    const packet = normalizeVisualizationPacket(
      {
        visualization_packet: {
          ...supportedPacket(),
          truncation: { truncated: true, dropped_node_count: 3, dropped_edge_count: 5, dropped_node_ids: ["viznode:x"] },
          limits: { max_nodes: 60, max_edges: 120, ordering: "stable_id", node_count: 2, edge_count: 1 }
        }
      },
      null
    );
    expect(packet?.truncation.truncated).toBe(true);
    expect(packet?.truncation.droppedNodeCount).toBe(3);
    expect(packet?.truncation.droppedEdgeCount).toBe(5);
    expect(packet?.truncation.droppedNodeIds).toEqual(["viznode:x"]);
    expect(packet?.limits.maxNodes).toBe(60);
    expect(packet?.limits.nodeCount).toBe(2);
  });

  it("defaults limits and truncation when the wire omits them", () => {
    const packet = normalizeVisualizationPacket(
      { visualization_packet: { view: "service_story", supported: true, nodes: [], edges: [] } },
      null
    );
    expect(packet?.truncation.truncated).toBe(false);
    expect(packet?.truncation.droppedNodeIds).toEqual([]);
    expect(packet?.limits.maxNodes).toBe(0);
  });
});

describe("serviceStoryGraph", () => {
  it("lays out upstream, service, and downstream into distinct columns", () => {
    const packet = normalizeVisualizationPacket({ visualization_packet: supportedPacket() }, null);
    const graph = serviceStoryGraph(packet);
    expect(graph.nodes).toHaveLength(2);
    const service = graph.nodes.find((node) => node.id === "viznode:service");
    const upstream = graph.nodes.find((node) => node.id === "viznode:up-1");
    expect(service?.hero).toBe(true);
    expect(service?.col).toBeGreaterThan(upstream?.col ?? -1);
    expect(graph.edges).toHaveLength(1);
    expect(graph.edges[0]?.verb).toBe("DEPENDS_ON");
  });

  it("returns an empty graph for an unsupported packet", () => {
    const packet = normalizeVisualizationPacket(
      { visualization_packet: { view: "service_story", supported: false, nodes: [], edges: [] } },
      null
    );
    expect(serviceStoryGraph(packet).nodes).toHaveLength(0);
  });

  it("drops edges that reference a missing endpoint", () => {
    const packet = normalizeVisualizationPacket(
      {
        visualization_packet: {
          view: "service_story",
          supported: true,
          nodes: [{ id: "viznode:service", type: "service", label: "payments", category: "service" }],
          edges: [{ id: "vizedge:dangling", source: "viznode:service", target: "viznode:ghost", relationship: "DEPENDS_ON" }]
        }
      },
      null
    );
    expect(serviceStoryGraph(packet).edges).toHaveLength(0);
  });
});

describe("loadServiceEvidenceGraph", () => {
  it("fetches the story then derives a service_story packet", async () => {
    const paths: string[] = [];
    const bodies: unknown[] = [];
    const client = {
      get: vi.fn(async (path: string) => {
        paths.push(path);
        return storyEnvelope();
      }),
      post: vi.fn(async (path: string, body: unknown) => {
        paths.push(path);
        bodies.push(body);
        return packetEnvelope(supportedPacket());
      })
    } as unknown as EshuApiClient;

    const result = await loadServiceEvidenceGraph(client, "payments");

    expect(paths).toEqual([
      "/api/v0/services/payments/story",
      "/api/v0/visualizations/derive"
    ]);
    expect(bodies[0]).toMatchObject({ view: "service_story" });
    expect(result.storyError).toBeNull();
    expect(result.packet?.supported).toBe(true);
    expect(result.graph.nodes).toHaveLength(2);
    expect(result.truth?.level).toBe("exact");
  });

  it("encodes the service name in the story path", async () => {
    const paths: string[] = [];
    const client = {
      get: vi.fn(async (path: string) => {
        paths.push(path);
        return storyEnvelope();
      }),
      post: vi.fn(async () => packetEnvelope(supportedPacket()))
    } as unknown as EshuApiClient;

    await loadServiceEvidenceGraph(client, "team/payments");
    expect(paths[0]).toBe("/api/v0/services/team%2Fpayments/story");
  });

  it("surfaces a story error without deriving or inventing a graph", async () => {
    const post = vi.fn();
    const client = {
      get: vi.fn(async () => ({
        data: null,
        error: { code: "not_found", message: "service not found" },
        truth: null
      })),
      post
    } as unknown as EshuApiClient;

    const result = await loadServiceEvidenceGraph(client, "ghost");
    expect(result.storyError?.code).toBe("not_found");
    expect(result.packet).toBeNull();
    expect(result.graph.nodes).toHaveLength(0);
    expect(post).not.toHaveBeenCalled();
  });

  it("surfaces a derive error without inventing a graph", async () => {
    const client = {
      get: vi.fn(async () => storyEnvelope()),
      post: vi.fn(async () => ({
        data: null,
        error: { code: "unsupported_capability", message: "derive disabled" },
        truth: null
      }))
    } as unknown as EshuApiClient;

    const result = await loadServiceEvidenceGraph(client, "payments");
    expect(result.storyError?.code).toBe("unsupported_capability");
    expect(result.packet).toBeNull();
    expect(result.graph.nodes).toHaveLength(0);
  });

  it("converts a thrown story error (non-2xx) into an error result without deriving", async () => {
    const post = vi.fn();
    const client = {
      get: vi.fn(async () => {
        throw new EshuApiHttpError(404, { code: "not_found", message: "service not found" });
      }),
      post
    } as unknown as EshuApiClient;

    const result = await loadServiceEvidenceGraph(client, "ghost");
    expect(result.storyError?.code).toBe("not_found");
    expect(result.packet).toBeNull();
    expect(result.graph.nodes).toHaveLength(0);
    expect(post).not.toHaveBeenCalled();
  });

  it("converts a thrown derive error (non-2xx) into an error result", async () => {
    const client = {
      get: vi.fn(async () => storyEnvelope()),
      post: vi.fn(async () => {
        throw new EshuApiHttpError(503);
      })
    } as unknown as EshuApiClient;

    const result = await loadServiceEvidenceGraph(client, "payments");
    expect(result.storyError?.code).toBe("http_503");
    expect(result.packet).toBeNull();
    expect(result.graph.nodes).toHaveLength(0);
  });
});
