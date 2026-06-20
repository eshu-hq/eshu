import type {
  GraphEdge, GraphLayer, GraphModel, GraphNode, GraphSourceLocation,
  RelationshipConfidenceTier, RelationshipTruthState
} from "../console/types";

const VERB_LAYER: Record<string, GraphLayer> = {
  CALLS: "code", IMPORTS: "code", INHERITS: "code", OVERRIDES: "code", REFERENCES: "code",
  DEPLOYS_FROM: "deploy", BUILDS: "deploy", DISCOVERS_CONFIG_IN: "deploy",
  DECLARED_BY: "infra", STORES_IN: "infra", ASSUMES_ROLE: "infra",
  RUNS_IN: "runtime", RUNS_AS: "runtime", DEPENDS_ON: "runtime", EXPOSES: "runtime",
  AFFECTED_BY: "security", OBSERVED_INCIDENT: "ops", TRACKED_BY: "ops"
};

interface CodeRelEdge {
  readonly type?: string; readonly direction?: string;
  readonly source_id?: string; readonly source_name?: string;
  readonly target_id?: string; readonly target_name?: string;
  readonly repo_id?: string; readonly repo_name?: string; readonly file_path?: string;
  readonly start_line?: number; readonly end_line?: number;
  readonly source_repo_id?: string; readonly source_repo_name?: string; readonly source_file_path?: string;
  readonly source_start_line?: number; readonly source_end_line?: number; readonly source_type?: string;
  readonly target_repo_id?: string; readonly target_repo_name?: string; readonly target_file_path?: string;
  readonly target_start_line?: number; readonly target_end_line?: number; readonly target_type?: string;
  readonly provenance?: CodeRelationshipProvenance;
}

export interface CodeRelationshipProvenance {
  readonly confidence?: number;
  readonly confidence_tier?: RelationshipConfidenceTier;
  readonly truth_state?: RelationshipTruthState;
  readonly source_family?: string;
  readonly method?: string;
  readonly reason?: string;
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

export interface CodeRelationshipStoryCoverage {
  readonly missing_edge_reason?: string;
  readonly truncation_state?: string;
  readonly evidence_explanation?: string;
}

export interface CodeRelationshipStoryResponse {
  readonly entity_id?: string; readonly name?: string; readonly labels?: readonly string[];
  readonly relationships?: readonly CodeRelEdge[];
  readonly coverage?: CodeRelationshipStoryCoverage;
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

export function codeRelationshipStoryToGraph(
  data: CodeRelationshipStoryResponse,
  fallback: { readonly id: string; readonly name: string }
): { readonly graph: GraphModel; readonly coverage?: CodeRelationshipStoryCoverage } {
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
    truth: "exact"
  });
  const edges: GraphEdge[] = [];
  (data.relationships ?? []).forEach((edge) => {
    const direction = (edge.direction ?? "outgoing").toLowerCase() === "incoming" ? "incoming" : "outgoing";
    const otherId = direction === "incoming" ? edge.source_id ?? edge.source_name : edge.target_id ?? edge.target_name;
    if (!otherId) return;
    const verb = (edge.type ?? "RELATED").toUpperCase();
    if (otherId !== centerId && !nodes.has(otherId)) {
      nodes.set(otherId, {
        id: otherId,
        kind: storyRelationshipNodeKind(edge, direction, verb),
        label: direction === "incoming" ? edge.source_name ?? otherId : edge.target_name ?? otherId,
        sub: storyRelationshipNodeSub(edge, direction, verb),
        col: direction === "incoming" ? 0 : 2,
        truth: "exact",
        source: sourceLocationFromEdge(edge, direction === "incoming" ? "source" : "target")
      });
    }
    edges.push(withRelationshipProvenance(direction === "incoming"
      ? { s: otherId, t: centerId, verb, layer: layerFor(verb) }
      : { s: centerId, t: otherId, verb, layer: layerFor(verb) }, edge.provenance));
  });
  return { graph: { nodes: [...nodes.values()], edges }, coverage: data.coverage };
}

export function mergeGraphSourceMetadata(primary: GraphModel, sourceGraph: GraphModel): GraphModel {
  const sourcesById = new Map(sourceGraph.nodes.map((node) => [node.id, node.source]));
  return {
    nodes: primary.nodes.map((node) => node.source ? node : { ...node, source: sourcesById.get(node.id) }),
    edges: primary.edges
  };
}

function storyRelationshipNodeKind(edge: CodeRelEdge, direction: "incoming" | "outgoing", verb: string): string {
  const type = direction === "incoming" ? edge.source_type : edge.target_type;
  return type ? kindFor(type) : relationshipNodeKind(verb);
}

function storyRelationshipNodeSub(edge: CodeRelEdge, direction: "incoming" | "outgoing", verb: string): string {
  const type = direction === "incoming" ? edge.source_type : edge.target_type;
  return type ?? relationshipNodeSub(verb, direction);
}

function withRelationshipProvenance(edge: GraphEdge, provenance: CodeRelationshipProvenance | undefined): GraphEdge {
  if (!provenance) return edge;
  return {
    ...edge,
    confidenceTier: provenance.confidence_tier,
    truthState: provenance.truth_state,
    sourceFamily: provenance.source_family,
    method: provenance.method,
    evidence: relationshipEvidence(provenance)
  };
}

function relationshipEvidence(provenance: CodeRelationshipProvenance): readonly string[] {
  return [
    provenance.confidence_tier ? `confidence: ${provenance.confidence_tier}` : "",
    provenance.truth_state ? `truth: ${provenance.truth_state}` : "",
    provenance.source_family ? `source: ${provenance.source_family}` : "",
    provenance.method ? `method: ${provenance.method}` : "",
    provenance.reason ? `reason: ${provenance.reason}` : ""
  ].filter((value): value is string => value !== "");
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
