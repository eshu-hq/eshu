import type { EvidenceSelection } from "./EvidenceDrawer";
import type { EvidencePanelData, EvidencePanelFact, EvidencePanelSection } from "./EvidencePanel";
import {
  buildSourceCitationHref,
  sourceCitationLabel,
  type AnswerEvidenceHandle,
} from "../api/answerPacket";
import type { VisualizationPacket } from "../api/answerVisualization";

// visualizationEvidencePanelData maps a selected node or edge from a
// visualization packet into the packet-agnostic EvidencePanelData contract so the
// shared inline EvidencePanel primitive can render service-story evidence-lane
// pills and graph nodes. It returns null when the selected id is not in the
// packet, mirroring EvidenceDrawer so a stale selection renders nothing rather
// than an empty panel.
export function visualizationEvidencePanelData(
  packet: VisualizationPacket,
  selection: EvidenceSelection,
): EvidencePanelData | null {
  if (selection.kind === "node") {
    const node = packet.nodes.find((row) => row.id === selection.id);
    if (node === undefined) {
      return null;
    }
    const facts: EvidencePanelFact[] = [
      { label: "Type", value: node.type },
      { label: "Category", value: node.category },
      { label: "Role", value: node.role },
      { label: "Canonical repository", value: node.canonicalKey },
      { label: "Observation scope", value: node.scopeKey },
    ];
    return {
      kindLabel: "Node evidence",
      title: node.label || node.id,
      truthLabel: node.truthLabel,
      truth: packet.truth,
      facts,
      sections: [
        ...handleSections(node.evidenceHandle),
        ...observationHandleSections(node.evidenceHandles),
      ],
      limitations: packet.limitations,
      ...sourceLinkFields(node.evidenceHandle),
    };
  }
  const edge = packet.edges.find((row) => row.id === selection.id);
  if (edge === undefined) {
    return null;
  }
  const facts: EvidencePanelFact[] = [
    { label: "From", value: nodeLabel(packet, edge.source) },
    { label: "To", value: nodeLabel(packet, edge.target) },
  ];
  return {
    kindLabel: "Edge evidence",
    title: edge.relationship || "relationship",
    truthLabel: edge.truthLabel,
    truth: packet.truth,
    facts,
    sections: handleSections(edge.evidenceHandle),
    limitations: packet.limitations,
    ...sourceLinkFields(edge.evidenceHandle),
  };
}

function observationHandleSections(
  handles: readonly AnswerEvidenceHandle[],
): readonly EvidencePanelSection[] {
  if (handles.length <= 1) {
    return [];
  }
  return [
    {
      title: "Repository observations",
      rows: handles.map((handle, index) => ({
        label: `Observation ${index + 1}`,
        value: handle.entityId ?? handle.repoId ?? "",
      })),
    },
  ];
}

function handleSections(handle: AnswerEvidenceHandle | null): readonly EvidencePanelSection[] {
  if (handle === null) {
    return [];
  }
  const rows: EvidencePanelFact[] = [
    { label: "Kind", value: handle.kind },
    { label: "Family", value: handle.evidenceFamily },
    { label: "Entity", value: handle.entityId ?? "" },
    { label: "Reason", value: handle.reason },
  ];
  return [{ title: "Evidence handle", rows }];
}

function sourceLinkFields(handle: AnswerEvidenceHandle | null): {
  sourceHref?: string;
  sourceLabel?: string;
} {
  if (handle === null || handle.relativePath === undefined || handle.repoId === undefined) {
    return {};
  }
  return {
    sourceHref: buildSourceCitationHref(handle),
    sourceLabel: sourceCitationLabel(handle),
  };
}

function nodeLabel(packet: VisualizationPacket, id: string): string {
  return packet.nodes.find((node) => node.id === id)?.label || id;
}
