import { render, screen, within } from "@testing-library/react";
import { vi } from "vitest";

import { ServiceStoryRelationshipList } from "./ServiceStoryRelationshipList";
import type { VisualizationEdge, VisualizationNode } from "../api/answerVisualization";

function node(
  id: string,
  label: string,
  role: string,
  overrides: Partial<VisualizationNode> = {},
): VisualizationNode {
  return {
    canonicalKey: id,
    category: "repository",
    evidenceHandle: null,
    evidenceHandles: [],
    id,
    label,
    role,
    scopeKey: id,
    truthLabel: "exact",
    type: "repository",
    ...overrides,
  };
}

function edge(
  relationship: string,
  source = "viznode:source",
  target = "viznode:target",
): VisualizationEdge {
  return {
    evidenceHandle: null,
    id: `vizedge:${relationship}`,
    relationship,
    source,
    target,
    truthLabel: "exact",
  };
}

function renderList(edges: readonly VisualizationEdge[], nodes: readonly VisualizationNode[]) {
  return render(
    <ServiceStoryRelationshipList edges={edges} nodes={nodes} onSelect={vi.fn()} selected={null} />,
  );
}

describe("ServiceStoryRelationshipList", () => {
  const source = node("viznode:source", "api-node-boats", "source_repository");
  const target = node("viznode:target", "trader-web", "workload");

  it.each([
    [
      "CONSUMED_BY",
      "api-node-boats (workload service) is consumed by trader-web (downstream repository)",
    ],
    ["DEPENDS_ON", "api-node-boats (source repository) depends on trader-web (workload service)"],
    [
      "DEPLOYS_FROM",
      "api-node-boats (source repository) deploys from trader-web (workload service)",
    ],
    [
      "PROVISIONING_SOURCE_CHAIN",
      "api-node-boats (source repository) provisions trader-web (workload service)",
    ],
    [
      "READS_CONFIG_FROM",
      "api-node-boats (source repository) reads config from trader-web (workload service)",
    ],
    ["RUNS_AS", "api-node-boats (workload service) runs as trader-web (runtime instance)"],
  ])("renders the %s relationship as a human narrative", (relationship, expected) => {
    renderList([edge(relationship)], [source, target]);

    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it.each([
    ["CONSUMED_BY", "payments (workload service) is consumed by checkout (downstream repository)"],
    ["RUNS_AS", "payments (workload service) runs as checkout (runtime instance)"],
  ])(
    "derives endpoint roles for %s instead of trusting conflicting node roles",
    (relationship, expected) => {
      const conflictingSource = node("viznode:source", "payments", "downstream_consumer");
      const conflictingTarget = node("viznode:target", "checkout", "source_repository");

      renderList([edge(relationship)], [conflictingSource, conflictingTarget]);

      expect(screen.getByText(expected)).toBeInTheDocument();
    },
  );

  it("uses missing endpoint IDs as diagnostic narrative fallbacks", () => {
    const relationship = edge("DEPENDS_ON", "viznode:missing-source", "viznode:missing-target");

    renderList([relationship], []);

    const button = screen.getByRole("button");
    expect(
      within(button).getByText("viznode:missing-source depends on viznode:missing-target"),
    ).toBeInTheDocument();
    expect(within(button).getByLabelText("Relationship diagnostic IDs")).toHaveTextContent(
      "viznode:missing-source → viznode:missing-target",
    );
  });

  it("renders nothing when no relationships are present", () => {
    const { container } = renderList([], [source, target]);

    expect(container).toBeEmptyDOMElement();
  });
});
