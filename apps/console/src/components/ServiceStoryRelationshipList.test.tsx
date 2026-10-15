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
    roles: [role],
    scopeKey: id,
    scopeKeys: [id],
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
    evidenceHandles: [],
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
  const source = node("viznode:source", "api-demo-shop", "source_repository");
  const target = node("viznode:target", "buyer-web", "workload");

  it.each([
    [
      "CONSUMED_BY",
      "api-demo-shop (workload service) is consumed by buyer-web (downstream repository)",
    ],
    ["DEPENDS_ON", "api-demo-shop (source repository) depends on buyer-web (workload service)"],
    ["DEPLOYS_FROM", "api-demo-shop (source repository) deploys from buyer-web (workload service)"],
    [
      "PROVISIONING_SOURCE_CHAIN",
      "api-demo-shop (source repository) provisions buyer-web (workload service)",
    ],
    [
      "READS_CONFIG_FROM",
      "api-demo-shop (source repository) reads config from buyer-web (workload service)",
    ],
    ["RUNS_AS", "api-demo-shop (workload service) runs as buyer-web (runtime instance)"],
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
