import type { GraphEdge, GraphLayer, GraphModel, GraphNode, GraphSourceLocation } from "../console/types";

const VERB_LAYER: Record<string, GraphLayer> = {
  CALLS: "code", IMPORTS: "code", INHERITS: "code", OVERRIDES: "code", REFERENCES: "code",
  DEPLOYS_FROM: "deploy", BUILDS: "deploy", DISCOVERS_CONFIG_IN: "deploy",
  DECLARED_BY: "infra", STORES_IN: "infra", ASSUMES_ROLE: "infra",
  RUNS_IN: "runtime", RUNS_AS: "runtime", DEPENDS_ON: "runtime", EXPOSES: "runtime",
  AFFECTED_BY: "security", OBSERVED_INCIDENT: "ops", TRACKED_BY: "ops"
};

interface CodeRelEdge {
  readonly type?: string;
  readonly source_id?: string; readonly source_name?: string;
  readonly target_id?: string; readonly target_name?: string;
  readonly repo_id?: string; readonly repo_name?: string; readonly file_path?: string;
  readonly start_line?: number; readonly end_line?: number;
  readonly source_repo_id?: string; readonly source_repo_name?: string; readonly source_file_path?: string;
  readonly source_start_line?: number; readonly source_end_line?: number; readonly source_type?: string;
  readonly target_repo_id?: string; readonly target_repo_name?: string; readonly target_file_path?: string;
  readonly target_start_line?: number; readonly target_end_line?: number; readonly target_type?: string;
}

/**
 * Live code/relationships envelope payload used by Direct mode after resolving
 * a searched symbol to a graph entity id.
 */
export interface CodeRelationshipsResponse {
  readonly entity_id?: string; readonly name?: string; readonly labels?: readonly string[];
  readonly repo_id?: string; readonly repo_name?: string; readonly file_path?: string;
  readonly start_line?: number; readonly end_line?: number;
  readonly incoming?: readonly CodeRelEdge[]; readonly outgoing?: readonly CodeRelEdge[];
}

/**
 * Maps `/api/v0/code/relationships` incoming/outgoing edge lists into the
 * console's center-and-neighbours graph model, preserving source metadata for
 * the centered code entity when the endpoint returns it.
 */
export function codeRelationshipsToGraph(data: CodeRelationshipsResponse, fallback: { readonly id: string; readonly name: string }): GraphModel {
  const centerId = data.entity_id ?? fallback.id;
  const centerType = data.labels?.[0];
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, {
    id: centerId,
    kind: kindFor(centerType),
    label: data.name ?? fallback.name,
    sub: centerType,
    col: 1,
    hero: true,
    truth: "exact",
    source: sourceLocationFromCodeRelationships(data)
  });
  const edges: GraphEdge[] = [];
  (data.incoming ?? []).forEach((e) => {
    const id = e.source_id ?? e.source_name;
    if (!id) return;
    const verb = (e.type ?? "RELATED").toUpperCase();
    if (id !== centerId && !nodes.has(id)) {
      nodes.set(id, {
        id,
        kind: e.source_type ? kindFor(e.source_type) : relationshipNodeKind(verb),
        label: e.source_name ?? id,
        sub: e.source_type ?? relationshipNodeSub(verb, "incoming"),
        col: 0,
        truth: "exact",
        source: sourceLocationFromEdge(e, "source")
      });
    }
    edges.push({ s: id, t: centerId, verb, layer: layerFor(verb) });
  });
  (data.outgoing ?? []).forEach((e) => {
    const id = e.target_id ?? e.target_name;
    if (!id) return;
    const verb = (e.type ?? "RELATED").toUpperCase();
    if (id !== centerId && !nodes.has(id)) {
      nodes.set(id, {
        id,
        kind: e.target_type ? kindFor(e.target_type) : relationshipNodeKind(verb),
        label: e.target_name ?? id,
        sub: e.target_type ?? relationshipNodeSub(verb, "outgoing"),
        col: 2,
        truth: "exact",
        source: sourceLocationFromEdge(e, "target")
      });
    }
    edges.push({ s: centerId, t: id, verb, layer: layerFor(verb) });
  });
  return { nodes: [...nodes.values()], edges };
}

function layerFor(verb: string): GraphLayer {
  return VERB_LAYER[verb.toUpperCase()] ?? "runtime";
}

function relationshipNodeKind(verb: string): string {
  const normalized = verb.toUpperCase();
  if (normalized === "IMPORTS" || normalized === "REFERENCES") return "library";
  if (normalized === "CALLS") return "client";
  if (normalized === "INHERITS" || normalized === "OVERRIDES") return "client";
  return "client";
}

function relationshipNodeSub(verb: string, direction: "incoming" | "outgoing"): string {
  return `${direction} ${verb.toUpperCase()}`;
}

function kindFor(type: string | undefined): string {
  const t = (type ?? "").toLowerCase();
  if (t.includes("service")) return "service";
  if (t.includes("workload") || t.includes("deployment")) return "workload";
  if (t.includes("repo")) return "repo";
  if (t.includes("module") || t.includes("package") || t.includes("library")) return "library";
  if (t.includes("function") || t.includes("class") || t.includes("symbol")) return "client";
  if (t.includes("resource") || t.includes("aws")) return "aws";
  return "service";
}

function sourceLocationFromCodeRelationships(data: CodeRelationshipsResponse): GraphSourceLocation | undefined {
  const repoId = data.repo_id?.trim();
  const filePath = data.file_path?.trim();
  if (!repoId || !filePath) return undefined;
  return {
    repoId,
    repoName: data.repo_name?.trim() || undefined,
    filePath,
    startLine: data.start_line,
    endLine: data.end_line
  };
}

function sourceLocationFromEdge(edge: CodeRelEdge, side: "source" | "target"): GraphSourceLocation | undefined {
  const repoId = cleanText(side === "source" ? edge.source_repo_id : edge.target_repo_id) || cleanText(edge.repo_id);
  const filePath = cleanText(side === "source" ? edge.source_file_path : edge.target_file_path) || cleanText(edge.file_path);
  if (!repoId || !filePath) return undefined;
  return {
    repoId,
    repoName: cleanText(side === "source" ? edge.source_repo_name : edge.target_repo_name) || cleanText(edge.repo_name) || undefined,
    filePath,
    startLine: side === "source" ? edge.source_start_line ?? edge.start_line : edge.target_start_line ?? edge.start_line,
    endLine: side === "source" ? edge.source_end_line ?? edge.end_line : edge.target_end_line ?? edge.end_line
  };
}

function cleanText(value: string | undefined): string {
  return value?.trim() ?? "";
}
