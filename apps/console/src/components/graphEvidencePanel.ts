import type { EvidencePanelData, EvidencePanelFact, EvidencePanelSection } from "./EvidencePanel";
import type { GraphEdge, GraphNode } from "../console/types";

// graphNodeEvidencePanelData maps a GraphModel node into the packet-agnostic
// EvidencePanelData contract so the Graph Explorer can reveal a node's identity,
// truth, and indexed source inline through the shared EvidencePanel primitive.
// The node truth (exact/derived/inferred) drives the truth chip; an absent truth
// renders an explicit "not provided" state rather than a fabricated level.
export function graphNodeEvidencePanelData(node: GraphNode): EvidencePanelData {
  const facts: EvidencePanelFact[] = [
    { label: "Kind", value: node.kind },
    { label: "Detail", value: node.sub ?? "" }
  ];
  return {
    kindLabel: "Node evidence",
    title: node.label || node.id,
    truthLabel: node.truth ?? "",
    truth: null,
    facts,
    ...sourceLinkFields(node)
  };
}

// graphEdgeEvidencePanelData maps a GraphModel edge into EvidencePanelData. The
// endpoint ids are resolved to human labels by the caller (which holds the node
// map). The relationship truth state, confidence tier, source family, and method
// become a "Relationship truth" provenance section, and supporting evidence
// strings become the evidence list. Empty provenance fields are dropped so an
// unsupported relationship stays explicit instead of rendering blank rows.
export function graphEdgeEvidencePanelData(
  edge: GraphEdge,
  fromLabel: string,
  toLabel: string
): EvidencePanelData {
  const facts: EvidencePanelFact[] = [
    { label: "From", value: fromLabel },
    { label: "To", value: toLabel },
    { label: "Layer", value: edge.layer }
  ];
  const provenanceRows: EvidencePanelFact[] = [
    { label: "Confidence", value: edge.confidenceTier ?? "" },
    { label: "Truth state", value: edge.truthState ?? "" },
    { label: "Source family", value: edge.sourceFamily ?? "" },
    { label: "Method", value: edge.method ?? "" }
  ];
  const sections: EvidencePanelSection[] = [];
  if (provenanceRows.some((row) => row.value.trim().length > 0)) {
    sections.push({ title: "Relationship truth", rows: provenanceRows });
  }
  return {
    kindLabel: "Edge evidence",
    title: edge.verb || "relationship",
    truthLabel: edge.truthState ?? "",
    truth: null,
    facts,
    sections,
    evidence: edge.evidence ?? []
  };
}

function sourceLinkFields(node: GraphNode): { sourceHref?: string; sourceLabel?: string } {
  const source = node.source;
  if (source === undefined) {
    return {};
  }
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine !== undefined) {
    params.set("lineStart", String(source.startLine));
  }
  if (source.endLine !== undefined) {
    params.set("lineEnd", String(source.endLine));
  }
  return {
    sourceHref: `/repositories/${encodeURIComponent(source.repoId)}/source?${params.toString()}`,
    sourceLabel: sourceLabel(source)
  };
}

function sourceLabel(source: NonNullable<GraphNode["source"]>): string {
  if (source.startLine !== undefined && source.endLine !== undefined) {
    return `${source.filePath}:${source.startLine}-${source.endLine}`;
  }
  if (source.startLine !== undefined) {
    return `${source.filePath}:${source.startLine}`;
  }
  return source.filePath;
}
