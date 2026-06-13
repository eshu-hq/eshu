import type { GraphModel, GraphLayer, GraphNode } from "../console/types";
import { uiTruth } from "../console/types";
import type { EshuTruth } from "./envelope";
import type {
  AnswerEvidenceHandle,
  AnswerEvidenceHandleWire,
  AnswerNextCall,
  AnswerNextCallWire,
  EvidenceCitationPacket,
  EvidenceCitationResponseWire
} from "./answerPacket";

export interface VisualizationDeriveRequest {
  readonly source_response: EvidenceCitationResponseWire;
  readonly source_truth?: EshuTruth;
  readonly view: "evidence_citation";
}

export interface VisualizationDeriveResponseWire {
  readonly visualization_packet?: VisualizationPacketWire;
}

export interface VisualizationPacket {
  readonly edges: readonly VisualizationEdge[];
  readonly limitations: readonly string[];
  readonly nodes: readonly VisualizationNode[];
  readonly recommendedNextCalls: readonly AnswerNextCall[];
  readonly supported: boolean;
  readonly title: string;
  readonly truth: EshuTruth | null;
  readonly view: string;
}

export interface VisualizationPacketWire {
  readonly edges?: readonly VisualizationEdgeWire[];
  readonly limitations?: readonly string[];
  readonly nodes?: readonly VisualizationNodeWire[];
  readonly recommended_next_calls?: readonly AnswerNextCallWire[];
  readonly supported?: boolean;
  readonly title?: string;
  readonly truth?: EshuTruth | null;
  readonly view?: string;
}

export interface VisualizationNode {
  readonly category: string;
  readonly evidenceHandle: AnswerEvidenceHandle | null;
  readonly id: string;
  readonly label: string;
  readonly truthLabel: string;
  readonly type: string;
}

export interface VisualizationNodeWire {
  readonly category?: string;
  readonly evidence_handle?: AnswerEvidenceHandleWire;
  readonly id?: string;
  readonly label?: string;
  readonly truth_label?: string;
  readonly type?: string;
}

export interface VisualizationEdge {
  readonly evidenceHandle: AnswerEvidenceHandle | null;
  readonly id: string;
  readonly relationship: string;
  readonly source: string;
  readonly target: string;
  readonly truthLabel: string;
}

export interface VisualizationEdgeWire {
  readonly evidence_handle?: AnswerEvidenceHandleWire;
  readonly id?: string;
  readonly relationship?: string;
  readonly source?: string;
  readonly target?: string;
  readonly truth_label?: string;
}

export function visualizationRequest(
  citationPacket: EvidenceCitationPacket,
  sourceTruth: EshuTruth | null
): VisualizationDeriveRequest {
  return {
    source_response: citationPacket.raw,
    ...(sourceTruth === null ? {} : { source_truth: sourceTruth }),
    view: "evidence_citation"
  };
}

export function normalizeVisualizationPacket(
  response: VisualizationDeriveResponseWire,
  routeTruth: EshuTruth | null
): VisualizationPacket | null {
  const packet = response.visualization_packet;
  if (packet === undefined) {
    return null;
  }
  return {
    edges: (packet.edges ?? []).map((edge) => ({
      evidenceHandle: handleRow(edge.evidence_handle),
      id: clean(edge.id),
      relationship: clean(edge.relationship),
      source: clean(edge.source),
      target: clean(edge.target),
      truthLabel: clean(edge.truth_label)
    })),
    limitations: packet.limitations ?? [],
    nodes: (packet.nodes ?? []).map((node) => ({
      category: clean(node.category),
      evidenceHandle: handleRow(node.evidence_handle),
      id: clean(node.id),
      label: clean(node.label),
      truthLabel: clean(node.truth_label),
      type: clean(node.type)
    })),
    recommendedNextCalls: nextCalls(packet.recommended_next_calls ?? []),
    supported: packet.supported ?? false,
    title: clean(packet.title),
    truth: packet.truth ?? routeTruth,
    view: clean(packet.view)
  };
}

export function graphFromVisualizationPacket(packet: VisualizationPacket | null): GraphModel {
  if (packet === null || !packet.supported) {
    return emptyAnswerGraph();
  }
  const nodes: GraphNode[] = packet.nodes
    .filter((node) => node.id.length > 0)
    .map((node, index) => ({
      col: index,
      hero: index === 0,
      id: node.id,
      kind: node.type || "citation",
      label: node.label || node.id,
      sub: node.category || node.type,
      truth: node.truthLabel.length > 0 ? uiTruth(node.truthLabel) : undefined
    }));
  const nodeIds = new Set(nodes.map((node) => node.id));
  return {
    edges: packet.edges
      .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
      .map((edge) => ({
        layer: "code" satisfies GraphLayer,
        s: edge.source,
        t: edge.target,
        verb: edge.relationship || "EVIDENCE"
      })),
    nodes
  };
}

export function emptyAnswerGraph(): GraphModel {
  return { edges: [], nodes: [] };
}

function handleRow(handle: AnswerEvidenceHandleWire | undefined): AnswerEvidenceHandle | null {
  if (handle === undefined) {
    return null;
  }
  const kind = clean(handle.kind);
  const repoId = optionalClean(handle.repo_id);
  const relativePath = optionalClean(handle.relative_path);
  const entityId = optionalClean(handle.entity_id);
  if (kind.length === 0 || (relativePath === undefined && entityId === undefined)) {
    return null;
  }
  return {
    endLine: handle.end_line,
    entityId,
    evidenceFamily: clean(handle.evidence_family),
    kind,
    reason: clean(handle.reason),
    relativePath,
    repoId,
    startLine: handle.start_line
  };
}

function nextCalls(calls: readonly AnswerNextCallWire[]): readonly AnswerNextCall[] {
  return calls
    .map((call) => ({
      args: call.args,
      params: call.params,
      reason: clean(call.reason),
      route: optionalClean(call.route),
      tool: clean(call.tool)
    }))
    .filter((call) => call.tool.length > 0 || call.route !== undefined);
}

function optionalClean(value: string | undefined): string | undefined {
  const cleaned = clean(value);
  return cleaned.length === 0 ? undefined : cleaned;
}

function clean(value: string | undefined): string {
  return value?.trim() ?? "";
}
