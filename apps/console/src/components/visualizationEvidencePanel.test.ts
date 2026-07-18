import { visualizationEvidencePanelData } from "./visualizationEvidencePanel";
import { normalizeVisualizationPacket, type VisualizationPacket } from "../api/answerVisualization";

function packetFrom(): VisualizationPacket {
  const packet = normalizeVisualizationPacket(
    {
      visualization_packet: {
        view: "service_story",
        title: "payments",
        supported: true,
        nodes: [
          {
            id: "viznode:service",
            type: "service",
            label: "payments",
            category: "service",
            truth_label: "exact",
          },
          {
            id: "viznode:up-1",
            type: "repository",
            label: "billing",
            category: "upstream",
            role: "deployment_configuration",
            canonical_key: "repository:r_billing",
            scope_key: "scope:s_primary",
            truth_label: "fallback",
            evidence_handle: {
              kind: "file",
              repo_id: "up-1",
              relative_path: "go.mod",
              start_line: 12,
              evidence_family: "repository",
              reason: "import edge",
            },
            evidence_handles: [
              {
                kind: "entity",
                repo_id: "up-1",
                entity_id: "repository:observation-1",
                evidence_family: "repository",
              },
              {
                kind: "entity",
                repo_id: "up-2",
                entity_id: "repository:observation-2",
                evidence_family: "repository",
              },
            ],
          },
        ],
        edges: [
          {
            id: "vizedge:1",
            source: "viznode:up-1",
            target: "viznode:service",
            relationship: "IMPORTS",
            truth_label: "exact",
          },
        ],
        truth: {
          capability: "visualization.derive",
          profile: "local_authoritative",
          level: "derived",
          basis: "authoritative_graph",
          freshness: { state: "fresh" },
          reason: "graph projection",
        },
        limits: {
          max_nodes: 60,
          max_edges: 120,
          ordering: "stable_id",
          node_count: 2,
          edge_count: 1,
        },
        truncation: { truncated: false },
        limitations: ["bounded subset"],
        recommended_next_calls: [],
      },
    },
    null,
  );
  if (packet === null) {
    throw new Error("packet should normalize");
  }
  return packet;
}

describe("visualizationEvidencePanelData", () => {
  it("maps a node selection into evidence panel data with facts and source", () => {
    const data = visualizationEvidencePanelData(packetFrom(), { kind: "node", id: "viznode:up-1" });
    expect(data).not.toBeNull();
    expect(data?.kindLabel).toBe("Node evidence");
    expect(data?.title).toBe("billing");
    expect(data?.truthLabel).toBe("fallback");
    expect(data?.facts).toContainEqual({ label: "Type", value: "repository" });
    expect(data?.facts).toContainEqual({ label: "Category", value: "upstream" });
    expect(data?.facts).toContainEqual({ label: "Role", value: "deployment_configuration" });
    expect(data?.facts).toContainEqual({
      label: "Canonical repository",
      value: "repository:r_billing",
    });
    expect(data?.sourceHref).toBe("/repositories/up-1/source?path=go.mod&lineStart=12");
    expect(data?.limitations).toContain("bounded subset");
    expect(data?.sections).toContainEqual({
      title: "Repository observations",
      rows: [
        { label: "Observation 1", value: "repository:observation-1" },
        { label: "Observation 2", value: "repository:observation-2" },
      ],
    });
  });

  it("maps an edge selection into endpoints resolved to node labels", () => {
    const data = visualizationEvidencePanelData(packetFrom(), { kind: "edge", id: "vizedge:1" });
    expect(data?.kindLabel).toBe("Edge evidence");
    expect(data?.title).toBe("IMPORTS");
    expect(data?.facts).toContainEqual({ label: "From", value: "billing" });
    expect(data?.facts).toContainEqual({ label: "To", value: "payments" });
  });

  it("carries the packet truth onto the panel data", () => {
    const data = visualizationEvidencePanelData(packetFrom(), { kind: "edge", id: "vizedge:1" });
    expect(data?.truth?.basis).toBe("authoritative_graph");
    expect(data?.truth?.freshness.state).toBe("fresh");
  });

  it("returns null when the selected id is absent from the packet", () => {
    expect(
      visualizationEvidencePanelData(packetFrom(), { kind: "node", id: "missing" }),
    ).toBeNull();
  });
});
