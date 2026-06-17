import type { EshuApiClient } from "./client";
import type { EshuError, EshuTruth } from "./envelope";
import type { GraphLayer, GraphModel, GraphNode } from "../console/types";
import { uiTruth } from "../console/types";
import {
  emptyAnswerGraph,
  normalizeVisualizationPacket,
  serviceStoryVisualizationRequest,
  type VisualizationDeriveResponseWire,
  type VisualizationPacket
} from "./answerVisualization";

// ServiceEvidenceGraphResult is the bounded, source-backed view the console
// renders for a single service. The packet is null whenever the story or derive
// route failed, so the page surfaces the error rather than an invented graph.
export interface ServiceEvidenceGraphResult {
  readonly graph: GraphModel;
  readonly packet: VisualizationPacket | null;
  readonly serviceName: string;
  readonly storyError: EshuError | null;
  readonly truth: EshuTruth | null;
}

// Column lanes for the service-story layout: upstream dependencies feed the
// service, downstream consumers flow out of it, and anything the dossier did
// not categorize sits in a trailing lane. The center service lane keeps the
// anchor visually distinct.
const CATEGORY_COLUMN: Record<string, number> = {
  upstream: 0,
  service: 1,
  downstream: 2
};
const UNCATEGORIZED_COLUMN = 3;

// Node types the derive route emits map onto the console KIND_COLOR vocabulary.
// "repository" is the dossier's spelling; the console palette keys it as "repo".
const KIND_ALIASES: Record<string, string> = {
  repository: "repo"
};

// loadServiceEvidenceGraph fetches the authorized service-story dossier, then
// asks the side-effect-free derive route to fold it into a bounded
// visualization packet. It performs no client-side graph synthesis: when either
// call fails it returns the error and an empty graph so the console never
// presents fabricated topology.
export async function loadServiceEvidenceGraph(
  client: EshuApiClient,
  serviceName: string
): Promise<ServiceEvidenceGraphResult> {
  const trimmed = serviceName.trim();
  const story = await client.get<Record<string, unknown>>(
    `/api/v0/services/${encodeURIComponent(trimmed)}/story`
  );
  if (story.error !== null || story.data === null) {
    return emptyResult(trimmed, story.error);
  }

  const derived = await client.post<VisualizationDeriveResponseWire>(
    "/api/v0/visualizations/derive",
    serviceStoryVisualizationRequest(story.data, story.truth)
  );
  if (derived.error !== null || derived.data === null) {
    return emptyResult(trimmed, derived.error);
  }

  const packet = normalizeVisualizationPacket(derived.data, derived.truth ?? story.truth);
  return {
    graph: serviceStoryGraph(packet),
    packet,
    serviceName: trimmed,
    storyError: null,
    truth: derived.truth ?? packet?.truth ?? story.truth
  };
}

// serviceStoryGraph projects a derived packet onto the console graph model,
// grouping nodes into upstream/service/downstream lanes. It reads only fields
// the packet already carries and drops edges whose endpoints were not returned,
// so it cannot render a relationship the source did not support.
export function serviceStoryGraph(packet: VisualizationPacket | null): GraphModel {
  if (packet === null || !packet.supported) {
    return emptyAnswerGraph();
  }
  const nodes: GraphNode[] = packet.nodes
    .filter((node) => node.id.length > 0)
    .map((node) => ({
      col: CATEGORY_COLUMN[node.category] ?? UNCATEGORIZED_COLUMN,
      hero: node.category === "service",
      id: node.id,
      kind: KIND_ALIASES[node.type] ?? node.type ?? "repo",
      label: node.label || node.id,
      sub: node.category || node.type,
      truth: node.truthLabel.length > 0 ? uiTruth(node.truthLabel) : undefined
    }));
  if (nodes.length > 0 && !nodes.some((node) => node.hero === true)) {
    nodes[0] = { ...nodes[0], hero: true };
  }
  const nodeIds = new Set(nodes.map((node) => node.id));
  const edges = packet.edges
    .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
    .map((edge) => ({
      layer: "code" satisfies GraphLayer as GraphLayer,
      s: edge.source,
      t: edge.target,
      verb: edge.relationship || "RELATED"
    }));
  return { edges, nodes };
}

function emptyResult(serviceName: string, error: EshuError | null): ServiceEvidenceGraphResult {
  return {
    graph: emptyAnswerGraph(),
    packet: null,
    serviceName,
    storyError: error,
    truth: null
  };
}
