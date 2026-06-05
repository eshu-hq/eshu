// api/eshuGraph.ts
// Live graph loader for the Explorer. Resolves an entity and expands its
// neighbourhood from POST /api/v0/code/relationships, mapping verbs onto the
// console's relationship layers. Defensive over response shape — see
// GET /api/v0/openapi.json for the authoritative schema; adjust the readers below
// if your build's payload differs.

import type { EshuApiClient } from "./client";
import type { GraphModel, GraphNode, GraphEdge, GraphLayer } from "../console/types";

const VERB_LAYER: Record<string, GraphLayer> = {
  CALLS: "code", IMPORTS: "code", INHERITS: "code", OVERRIDES: "code", REFERENCES: "code",
  DEPLOYS_FROM: "deploy", BUILDS: "deploy", DISCOVERS_CONFIG_IN: "deploy",
  DECLARED_BY: "infra", STORES_IN: "infra", ASSUMES_ROLE: "infra",
  RUNS_IN: "runtime", RUNS_AS: "runtime", DEPENDS_ON: "runtime", EXPOSES: "runtime",
  AFFECTED_BY: "security", OBSERVED_INCIDENT: "ops", TRACKED_BY: "ops"
};
function layerFor(verb: string): GraphLayer { return VERB_LAYER[verb.toUpperCase()] ?? "runtime"; }
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

interface RelEntity { readonly id?: string; readonly name?: string; readonly type?: string; readonly entity_type?: string; }
interface RelRecord {
  readonly verb?: string; readonly relationship?: string; readonly type?: string;
  readonly direction?: string;
  readonly target?: RelEntity; readonly entity?: RelEntity; readonly node?: RelEntity;
  readonly source?: RelEntity;
}
interface RelationshipsResponse {
  readonly target?: RelEntity; readonly entity?: RelEntity;
  readonly relationships?: readonly RelRecord[]; readonly edges?: readonly RelRecord[]; readonly results?: readonly RelRecord[];
}

function ident(e: RelEntity | undefined, fallback: string): { id: string; name: string; type?: string } {
  const name = e?.name ?? e?.id ?? fallback;
  return { id: e?.id ?? name, name, type: e?.type ?? e?.entity_type };
}

// Resolve + expand one entity into a center-and-neighbours graph.
export async function loadEntityGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  const env = await client.post<RelationshipsResponse>("/api/v0/code/relationships", { name, depth: 1 });
  const data = env.data ?? {};
  const center = ident(data.target ?? data.entity, name);
  const records = data.relationships ?? data.edges ?? data.results ?? [];

  const nodes = new Map<string, GraphNode>();
  nodes.set(center.id, { id: center.id, kind: kindFor(center.type), label: center.name, sub: center.type, col: 1, hero: true, truth: "exact" });
  const edges: GraphEdge[] = [];

  records.forEach((r) => {
    const verb = (r.verb ?? r.relationship ?? r.type ?? "RELATED").toUpperCase();
    const other = ident(r.target ?? r.entity ?? r.node, "unknown");
    const incoming = (r.direction ?? "outgoing").toLowerCase() === "incoming";
    if (!nodes.has(other.id)) {
      nodes.set(other.id, { id: other.id, kind: kindFor(other.type), label: other.name, sub: other.type, col: incoming ? 0 : 2, truth: "exact" });
    }
    edges.push(incoming
      ? { s: other.id, t: center.id, verb, layer: layerFor(verb) }
      : { s: center.id, t: other.id, verb, layer: layerFor(verb) });
  });

  return { nodes: [...nodes.values()], edges };
}

// Blast radius: dependents that break if `name` fails. `loadBlastGraph` queries
// the live API and throws if it is unavailable; callers that need an offline
// path fall back to `blastFromModel`, which reverse-walks the in-memory graph.
interface ImpactEntity { readonly id?: string; readonly name?: string; readonly type?: string; readonly distance?: number; readonly hops?: number; }
interface BlastResponse {
  readonly target?: RelEntity; readonly entity?: RelEntity;
  readonly impacted?: readonly ImpactEntity[]; readonly dependents?: readonly ImpactEntity[]; readonly results?: readonly ImpactEntity[];
}

export async function loadBlastGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  const env = await client.post<BlastResponse>("/api/v0/impact/blast-radius", { name, max_depth: 3 });
  const data = env.data ?? {};
  const center = ident(data.target ?? data.entity, name);
  const impacted = data.impacted ?? data.dependents ?? data.results ?? [];
  const nodes = new Map<string, GraphNode>();
  nodes.set(center.id, { id: center.id, kind: "service", label: center.name, sub: "impact origin", hero: true, col: 0, truth: "exact" });
  const edges: GraphEdge[] = [];
  impacted.forEach((e) => {
    const id = e.id ?? e.name ?? "unknown";
    const hop = e.distance ?? e.hops ?? 1;
    if (!nodes.has(id)) nodes.set(id, { id, kind: kindFor(e.type), label: e.name ?? id, sub: `hop ${hop}`, col: hop, truth: "exact" });
    edges.push({ s: id, t: center.id, verb: "DEPENDS_ON", layer: "runtime" });
  });
  return { nodes: [...nodes.values()], edges };
}

// Fallback: reverse-walk the loaded model graph to find dependents of `name`.
export function blastFromModel(graph: GraphModel, name: string): GraphModel {
  const center = graph.nodes.find((n) => n.label === name || n.id === name);
  if (!center) return { nodes: [], edges: [] };
  const dependents = new Set<string>([center.id]);
  const edges: GraphEdge[] = [];
  const seen = new Set<string>();
  let frontier = [center.id];
  for (let depth = 0; depth < 3 && frontier.length; depth++) {
    const next: string[] = [];
    graph.edges.forEach((e) => {
      if (!frontier.includes(e.t)) return;
      if (!["DEPENDS_ON", "IMPORTS", "CALLS", "RUNS_AS"].includes(e.verb.toUpperCase())) return;
      const ek = `${e.s}>${e.t}`;
      if (!seen.has(ek)) { edges.push({ s: e.s, t: e.t, verb: e.verb, layer: e.layer }); seen.add(ek); }
      if (!dependents.has(e.s)) { dependents.add(e.s); next.push(e.s); }
    });
    frontier = next;
  }
  const byId = new Map(graph.nodes.map((n) => [n.id, n]));
  const nodes: GraphNode[] = [...dependents].map((id) => {
    const n = byId.get(id);
    return { id, kind: n?.kind ?? "service", label: n?.label ?? id, sub: id === center.id ? "impact origin" : n?.sub, hero: id === center.id, col: 0, truth: n?.truth };
  });
  return { nodes, edges };
}
