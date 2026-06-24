import type {
  AnswerEvidenceHandle,
  AnswerEvidenceHandleWire,
  AnswerNextCall,
  AnswerNextCallWire,
  EvidenceCitationPacket,
  EvidenceCitationResponseWire
} from "./answerPacket";
import type { EshuTruth } from "./envelope";
import type { GraphModel, GraphLayer, GraphNode } from "../console/types";
import { uiTruth } from "../console/types";

export interface VisualizationDeriveRequest {
  readonly source_response: EvidenceCitationResponseWire;
  readonly source_truth?: EshuTruth;
  readonly view: "evidence_citation";
}

export interface ServiceStoryVisualizationRequest {
  readonly source_response: Record<string, unknown>;
  readonly source_truth?: EshuTruth;
  readonly view: "service_story";
}

export interface VisualizationDeriveResponseWire {
  readonly visualization_packet?: VisualizationPacketWire;
}

export interface VisualizationPacket {
  readonly edges: readonly VisualizationEdge[];
  readonly limitations: readonly string[];
  readonly limits: VisualizationLimits;
  readonly nodes: readonly VisualizationNode[];
  readonly recommendedNextCalls: readonly AnswerNextCall[];
  readonly supported: boolean;
  readonly title: string;
  readonly truncation: VisualizationTruncation;
  readonly truth: EshuTruth | null;
  readonly view: string;
}

export interface VisualizationPacketWire {
  readonly edges?: readonly VisualizationEdgeWire[];
  readonly limitations?: readonly string[];
  readonly limits?: VisualizationLimitsWire;
  readonly nodes?: readonly VisualizationNodeWire[];
  readonly recommended_next_calls?: readonly AnswerNextCallWire[];
  readonly supported?: boolean;
  readonly title?: string;
  readonly truncation?: VisualizationTruncationWire;
  readonly truth?: EshuTruth | null;
  readonly view?: string;
}

// VisualizationLimits mirrors the bounded-payload counts the derive route
// returns so the console can report how much of the subgraph is shown and what
// the server-side caps are, instead of silently presenting a partial graph as
// complete.
export interface VisualizationLimits {
  readonly edgeCount: number;
  readonly maxEdges: number;
  readonly maxNodes: number;
  readonly nodeCount: number;
  readonly ordering: string;
}

export interface VisualizationLimitsWire {
  readonly edge_count?: number;
  readonly max_edges?: number;
  readonly max_nodes?: number;
  readonly node_count?: number;
  readonly ordering?: string;
}

// VisualizationTruncation captures what the derive route dropped to stay within
// bounds. The console must keep this visible so a truncated subgraph is never
// mistaken for the full evidence picture.
export interface VisualizationTruncation {
  readonly droppedEdgeCount: number;
  readonly droppedNodeCount: number;
  readonly droppedNodeIds: readonly string[];
  readonly truncated: boolean;
}

export interface VisualizationTruncationWire {
  readonly dropped_edge_count?: number;
  readonly dropped_node_count?: number;
  readonly dropped_node_ids?: readonly string[];
  readonly truncated?: boolean;
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

// serviceStoryVisualizationRequest derives the request body for the
// service_story view from an already-fetched service-story dossier response.
// The derive route is a side-effect-free transformation of source_response, so
// the console passes the raw dossier through unchanged and never synthesizes
// graph topology client-side.
export function serviceStoryVisualizationRequest(
  story: Record<string, unknown>,
  sourceTruth: EshuTruth | null
): ServiceStoryVisualizationRequest {
  return {
    source_response: story,
    ...(sourceTruth === null ? {} : { source_truth: sourceTruth }),
    view: "service_story"
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
    limits: normalizeLimits(packet.limits),
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
    truncation: normalizeTruncation(packet.truncation),
    truth: packet.truth ?? routeTruth,
    view: clean(packet.view)
  };
}

function normalizeLimits(limits: VisualizationLimitsWire | undefined): VisualizationLimits {
  return {
    edgeCount: limits?.edge_count ?? 0,
    maxEdges: limits?.max_edges ?? 0,
    maxNodes: limits?.max_nodes ?? 0,
    nodeCount: limits?.node_count ?? 0,
    ordering: clean(limits?.ordering)
  };
}

function normalizeTruncation(truncation: VisualizationTruncationWire | undefined): VisualizationTruncation {
  return {
    droppedEdgeCount: truncation?.dropped_edge_count ?? 0,
    droppedNodeCount: truncation?.dropped_node_count ?? 0,
    droppedNodeIds: (truncation?.dropped_node_ids ?? []).filter((id) => id.trim().length > 0),
    truncated: truncation?.truncated ?? false
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
