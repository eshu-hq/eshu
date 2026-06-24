import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

import { EvidenceDrawer } from "./EvidenceDrawer";
import { normalizeVisualizationPacket, type VisualizationPacket } from "../api/answerVisualization";

function packetFrom(overrides: Record<string, unknown> = {}): VisualizationPacket {
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
            evidence_handle: { kind: "entity", repo_id: "svc-repo", entity_id: "svc-1", evidence_family: "repository", reason: "service identity" }
          },
          {
            id: "viznode:up-1",
            type: "repository",
            label: "billing",
            category: "upstream",
            truth_label: "fallback",
            evidence_handle: { kind: "file", repo_id: "up-1", relative_path: "go.mod", start_line: 12, evidence_family: "repository", reason: "import edge" }
          },
          {
            id: "viznode:bare",
            type: "repository",
            label: "ghost",
            category: "downstream"
          }
        ],
        edges: [
          { id: "vizedge:1", source: "viznode:up-1", target: "viznode:service", relationship: "DEPENDS_ON", truth_label: "exact" }
        ],
        truth: { capability: "visualization.derive", profile: "local_authoritative", level: "derived", basis: "authoritative_graph", freshness: { state: "fresh" }, reason: "graph projection" },
        limits: { max_nodes: 60, max_edges: 120, ordering: "stable_id", node_count: 3, edge_count: 1 },
        truncation: { truncated: false },
        limitations: ["bounded subset"],
        recommended_next_calls: [{ tool: "get_service_story", reason: "fetch the full dossier" }],
        ...overrides
      }
    },
    null
  );
  if (packet === null) {
    throw new Error("packet should normalize");
  }
  return packet;
}

function renderDrawer(packet: VisualizationPacket, selection: Parameters<typeof EvidenceDrawer>[0]["selection"], onClose = vi.fn()) {
  render(
    <MemoryRouter>
      <EvidenceDrawer packet={packet} selection={selection} onClose={onClose} />
    </MemoryRouter>
  );
  return onClose;
}

describe("EvidenceDrawer", () => {
  it("renders a dialog for a selected node with an exact truth label and source link", () => {
    renderDrawer(packetFrom(), { kind: "node", id: "viznode:up-1" });
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("billing")).toBeInTheDocument();
    expect(within(dialog).getByText("upstream")).toBeInTheDocument();
    // fallback maps to the console "inferred" vocabulary, never silently dropped.
    expect(within(dialog).getAllByText("inferred").length).toBeGreaterThan(0);
    expect(within(dialog).getByText(/go\.mod/)).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: /Open source/i })).toHaveAttribute(
      "href",
      "/repositories/up-1/source?path=go.mod&lineStart=12"
    );
  });

  it("renders the packet truth basis and freshness", () => {
    renderDrawer(packetFrom(), { kind: "node", id: "viznode:service" });
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/authoritative_graph/)).toBeInTheDocument();
    expect(within(dialog).getByText("fresh")).toBeInTheDocument();
    expect(within(dialog).getByText(/graph projection/)).toBeInTheDocument();
  });

  it("shows a stale freshness state without hiding it", () => {
    const packet = packetFrom({ truth: { capability: "visualization.derive", profile: "local_authoritative", level: "derived", basis: "content_index", freshness: { state: "stale" } } });
    renderDrawer(packet, { kind: "node", id: "viznode:service" });
    expect(within(screen.getByRole("dialog")).getByText("stale")).toBeInTheDocument();
  });

  it("renders an unknown/ambiguous truth label literally, preserving uncertainty", () => {
    const packet = packetFrom({
      nodes: [{ id: "viznode:amb", type: "service", label: "payments", category: "service", truth_label: "ambiguous" }]
    });
    renderDrawer(packet, { kind: "node", id: "viznode:amb" });
    expect(within(screen.getByRole("dialog")).getByText("ambiguous")).toBeInTheDocument();
  });

  it("stays open and explicit when a node has no evidence handle or truth label", () => {
    renderDrawer(packetFrom(), { kind: "node", id: "viznode:bare" });
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/No evidence handle returned/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/truth label not provided/i)).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("renders an edge selection with its endpoints and relationship", () => {
    renderDrawer(packetFrom(), { kind: "edge", id: "vizedge:1" });
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("DEPENDS_ON")).toBeInTheDocument();
    expect(within(dialog).getByText(/billing/)).toBeInTheDocument();
    expect(within(dialog).getByText(/payments/)).toBeInTheDocument();
  });

  it("surfaces limitations and recommended next calls", () => {
    renderDrawer(packetFrom(), { kind: "node", id: "viznode:service" });
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("bounded subset")).toBeInTheDocument();
    expect(within(dialog).getByText("get_service_story")).toBeInTheDocument();
  });

  it("closes on the close button and on Escape", () => {
    const onClose = renderDrawer(packetFrom(), { kind: "node", id: "viznode:service" });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("traps Tab focus within the modal drawer", () => {
    renderDrawer(packetFrom(), { kind: "node", id: "viznode:up-1" });
    const dialog = screen.getByRole("dialog");
    const close = within(dialog).getByRole("button", { name: "Close" });
    const link = within(dialog).getByRole("link", { name: /Open source/i });

    // Tab from the last focusable (the source link) wraps back to the first (close).
    link.focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(close).toHaveFocus();

    // Shift+Tab from the first focusable wraps to the last.
    close.focus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(link).toHaveFocus();
  });

  it("renders nothing when the selected id is absent from the packet", () => {
    const { container } = render(
      <MemoryRouter>
        <EvidenceDrawer packet={packetFrom()} selection={{ kind: "node", id: "viznode:missing" }} onClose={vi.fn()} />
      </MemoryRouter>
    );
    expect(container.querySelector("[role=dialog]")).toBeNull();
  });
});
