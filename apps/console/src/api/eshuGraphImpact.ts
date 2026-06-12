import type { EshuApiClient } from "./client";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface RelEntity {
  readonly id?: string;
  readonly name?: string;
  readonly type?: string;
  readonly entity_type?: string;
}

interface ImpactEntity {
  readonly id?: string;
  readonly name?: string;
  readonly type?: string;
  readonly distance?: number;
  readonly hops?: number;
  readonly repo?: string;
  readonly repo_id?: string;
}

interface BlastResponse {
  readonly target?: RelEntity | string;
  readonly entity?: RelEntity;
  readonly affected?: readonly ImpactEntity[];
  readonly impacted?: readonly ImpactEntity[];
  readonly dependents?: readonly ImpactEntity[];
  readonly results?: readonly ImpactEntity[];
}

// Blast radius: dependents that break if `name` fails. `loadBlastGraph` queries
// the live API and throws if it is unavailable; callers that need an offline
// path fall back to `blastFromModel`, which reverse-walks the in-memory graph.
export async function loadBlastGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  // POST /api/v0/impact/blast-radius requires `target` + `target_type`; a
  // service/workload is anchored on its repository. The response is
  // `{ target, target_type, affected: [{repo, repo_id?, hops?}], ... }`.
  const env = await client.post<BlastResponse>("/api/v0/impact/blast-radius", {
    target: name,
    target_type: "repository",
    limit: 50
  });
  const data = env.data ?? {};
  const center = ident(typeof data.target === "object" ? data.target : data.entity, name);
  const affected = data.affected ?? data.impacted ?? data.dependents ?? data.results ?? [];
  const nodes = new Map<string, GraphNode>();
  nodes.set(center.id, {
    id: center.id,
    kind: "service",
    label: center.name,
    sub: "impact origin",
    hero: true,
    col: 0,
    truth: "exact"
  });
  const edges: GraphEdge[] = [];
  affected.forEach((entity) => {
    const label = (entity.repo ?? entity.name ?? entity.id ?? "").trim();
    // Skip empty or non-entity rows (a real repo name has no whitespace), so a
    // target with no indexed dependents renders cleanly as the origin alone.
    if (label === "" || /\s/.test(label)) return;
    const id = entity.repo_id ?? entity.id ?? label;
    if (id === center.id) return;
    const hop = entity.distance ?? entity.hops ?? 1;
    if (!nodes.has(id)) {
      nodes.set(id, { id, kind: kindFor(entity.type), label, sub: `hop ${hop}`, col: hop, truth: "exact" });
    }
    edges.push({ s: id, t: center.id, verb: "DEPENDS_ON", layer: "runtime" });
  });
  return { nodes: [...nodes.values()], edges };
}

// Fallback: reverse-walk the loaded model graph to find dependents of `name`.
export function blastFromModel(graph: GraphModel, name: string): GraphModel {
  const center = graph.nodes.find((node) => node.label === name || node.id === name);
  if (!center) return { nodes: [], edges: [] };
  const dependents = new Set<string>([center.id]);
  const edges: GraphEdge[] = [];
  const seen = new Set<string>();
  let frontier = [center.id];
  for (let depth = 0; depth < 3 && frontier.length > 0; depth += 1) {
    const next: string[] = [];
    graph.edges.forEach((edge) => {
      if (!frontier.includes(edge.t)) return;
      if (!["DEPENDS_ON", "IMPORTS", "CALLS", "RUNS_AS"].includes(edge.verb.toUpperCase())) return;
      const edgeKey = `${edge.s}>${edge.t}`;
      if (!seen.has(edgeKey)) {
        edges.push({ s: edge.s, t: edge.t, verb: edge.verb, layer: edge.layer });
        seen.add(edgeKey);
      }
      if (!dependents.has(edge.s)) {
        dependents.add(edge.s);
        next.push(edge.s);
      }
    });
    frontier = next;
  }
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const nodes: GraphNode[] = [...dependents].map((id) => {
    const node = byId.get(id);
    return {
      id,
      kind: node?.kind ?? "service",
      label: node?.label ?? id,
      sub: id === center.id ? "impact origin" : node?.sub,
      hero: id === center.id,
      col: 0,
      truth: node?.truth
    };
  });
  return { nodes, edges };
}

function ident(entity: RelEntity | undefined, fallback: string): { readonly id: string; readonly name: string } {
  const name = entity?.name ?? entity?.id ?? fallback;
  return { id: entity?.id ?? name, name };
}

function kindFor(type: string | undefined): string {
  const normalized = (type ?? "").toLowerCase();
  if (normalized.includes("service")) return "service";
  if (normalized.includes("workload") || normalized.includes("deployment")) return "workload";
  if (normalized.includes("repo")) return "repo";
  if (normalized.includes("module") || normalized.includes("package") || normalized.includes("library")) return "library";
  if (normalized.includes("function") || normalized.includes("class") || normalized.includes("symbol")) return "client";
  if (normalized.includes("resource") || normalized.includes("aws")) return "aws";
  return "service";
}
