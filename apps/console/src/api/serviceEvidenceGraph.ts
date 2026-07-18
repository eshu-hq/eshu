import {
  emptyAnswerGraph,
  normalizeVisualizationPacket,
  serviceStoryVisualizationRequest,
  type VisualizationDeriveResponseWire,
  type VisualizationPacket,
} from "./answerVisualization";
import { EshuApiHttpError, type EshuApiClient } from "./client";
import type { EshuError, EshuTruth } from "./envelope";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";
import { uiTruth } from "../console/types";

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
  source: 0,
  deployment: 1,
  service: 2,
  runtime: 3,
  downstream: 4,
};
const ROLE_COLUMN: Record<string, number> = {
  source_repository: 0,
  deployment_configuration: 1,
  workload: 2,
  runtime_instance: 3,
  downstream_consumer: 4,
};
const ROLE_LABEL: Record<string, string> = {
  source_repository: "source repository",
  deployment_configuration: "deployment configuration repository",
  workload: "workload service",
  runtime_instance: "runtime instance",
  downstream_consumer: "downstream repository",
};
const UNCATEGORIZED_COLUMN = 5;

// Node types the derive route emits map onto the console KIND_COLOR vocabulary.
// "repository" is the dossier's spelling; the console palette keys it as "repo".
const KIND_ALIASES: Record<string, string> = {
  repository: "repo",
};

// loadServiceEvidenceGraph fetches the authorized service-story dossier, then
// asks the side-effect-free derive route to fold it into a bounded
// visualization packet. It performs no client-side graph synthesis: when either
// call fails it returns the error and an empty graph so the console never
// presents fabricated topology. The client throws EshuApiHttpError on non-2xx
// (e.g. 404 for a missing service); that is caught and converted to an error
// result so the page never leaves a stale graph on screen after a failed load.
export async function loadServiceEvidenceGraph(
  client: EshuApiClient,
  serviceName: string,
): Promise<ServiceEvidenceGraphResult> {
  const trimmed = serviceName.trim();
  try {
    const story = await client.get<Record<string, unknown>>(
      `/api/v0/services/${encodeURIComponent(trimmed)}/story`,
    );
    if (story.error !== null || story.data === null) {
      return emptyResult(trimmed, story.error ?? requestFailedError());
    }

    const derived = await client.post<VisualizationDeriveResponseWire>(
      "/api/v0/visualizations/derive",
      serviceStoryVisualizationRequest(story.data, story.truth),
    );
    if (derived.error !== null || derived.data === null) {
      return emptyResult(trimmed, derived.error ?? requestFailedError());
    }

    const packet = normalizeVisualizationPacket(derived.data, derived.truth ?? story.truth);
    return {
      graph: serviceStoryGraph(packet),
      packet,
      serviceName: trimmed,
      storyError: null,
      truth: derived.truth ?? packet?.truth ?? story.truth,
    };
  } catch (error) {
    return emptyResult(trimmed, eshuErrorFromThrown(error));
  }
}

function eshuErrorFromThrown(error: unknown): EshuError {
  if (error instanceof EshuApiHttpError) {
    return error.error ?? { code: `http_${error.status}`, message: error.message };
  }
  return requestFailedError(error);
}

function requestFailedError(error?: unknown): EshuError {
  return {
    code: "request_failed",
    message: error instanceof Error ? error.message : "service evidence graph request failed",
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
      col: ROLE_COLUMN[node.role] ?? CATEGORY_COLUMN[node.category] ?? UNCATEGORIZED_COLUMN,
      hero:
        node.role === "workload" ||
        (node.role.length === 0 && node.type === "service" && node.category === "service"),
      id: node.id,
      kind: KIND_ALIASES[node.type] ?? node.type ?? "repo",
      label: node.label || node.id,
      sub: nodeSubLabel(node.roles, node.scopeKeys, node.category || node.type),
      truth: node.truthLabel.length > 0 ? uiTruth(node.truthLabel) : undefined,
    }));
  const nodeIds = new Set(nodes.map((node) => node.id));
  const edges = packet.edges
    .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
    .map(
      (edge): GraphEdge => ({
        layer: "code",
        s: edge.source,
        t: edge.target,
        verb: edge.relationship || "RELATED",
      }),
    );
  return { edges, nodes };
}

function nodeSubLabel(
  roles: readonly string[],
  scopeKeys: readonly string[],
  fallback: string,
): string {
  const roleLabels = roles.map((role) => ROLE_LABEL[role] ?? role.replaceAll("_", " "));
  const parts = [roleLabels.filter((role) => role.length > 0).join(" + ") || fallback];
  if (scopeKeys.length > 0) {
    const observations = scopeKeys.map((scopeKey) =>
      scopeKey.startsWith("scope:") ? scopeKey.slice("scope:".length) : scopeKey,
    );
    parts.push(
      `${observations.length === 1 ? "observation" : "observations"} ${observations.join(", ")}`,
    );
  }
  return parts.filter((part) => part.length > 0).join(" · ");
}

function emptyResult(serviceName: string, error: EshuError | null): ServiceEvidenceGraphResult {
  return {
    graph: emptyAnswerGraph(),
    packet: null,
    serviceName,
    storyError: error,
    truth: null,
  };
}
