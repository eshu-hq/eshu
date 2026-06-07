// api/eshuGraph.ts
// Live graph loader for the Explorer. Resolves an entity and expands its
// neighbourhood from POST /api/v0/code/relationships, mapping verbs onto the
// console's relationship layers. Defensive over response shape — see
// GET /api/v0/openapi.json for the authoritative schema; adjust the readers below
// if your build's payload differs.

import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { GraphModel, GraphNode, GraphEdge, GraphLayer } from "../console/types";
import { resolveEntity } from "./entityResolution";

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

// relationshipsToGraph maps a relationship-style response (center entity plus
// edge records) into a center-and-neighbours graph. Shared by the direct
// (code/relationships) and neighborhood (impact/entity-map) loaders, both of
// which return the same defensive shape.
export function relationshipsToGraph(data: RelationshipsResponse, name: string): GraphModel {
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

// --- code/relationships (Direct mode) ---------------------------------------
// POST /api/v0/code/relationships resolves edges by `entity_id` only — a `name`
// body returns nothing — and answers with split incoming/outgoing edge lists,
// not the generic relationships[] shape. See codeRelationshipsToGraph below.
interface CodeRelEdge {
  readonly type?: string;
  readonly source_id?: string; readonly source_name?: string;
  readonly target_id?: string; readonly target_name?: string;
}
interface CodeRelationshipsResponse {
  readonly entity_id?: string; readonly name?: string; readonly labels?: readonly string[];
  readonly incoming?: readonly CodeRelEdge[]; readonly outgoing?: readonly CodeRelEdge[];
}

// codeRelationshipsToGraph maps the code/relationships incoming/outgoing edge
// lists into a center-and-neighbours graph.
export function codeRelationshipsToGraph(data: CodeRelationshipsResponse, fallback: { id: string; name: string }): GraphModel {
  const centerId = data.entity_id ?? fallback.id;
  const centerType = data.labels?.[0];
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, { id: centerId, kind: kindFor(centerType), label: data.name ?? fallback.name, sub: centerType, col: 1, hero: true, truth: "exact" });
  const edges: GraphEdge[] = [];
  (data.incoming ?? []).forEach((e) => {
    const id = e.source_id ?? e.source_name;
    if (!id) return;
    const verb = (e.type ?? "RELATED").toUpperCase();
    if (id !== centerId && !nodes.has(id)) nodes.set(id, { id, kind: kindFor(undefined), label: e.source_name ?? id, col: 0, truth: "exact" });
    edges.push({ s: id, t: centerId, verb, layer: layerFor(verb) });
  });
  (data.outgoing ?? []).forEach((e) => {
    const id = e.target_id ?? e.target_name;
    if (!id) return;
    const verb = (e.type ?? "RELATED").toUpperCase();
    if (id !== centerId && !nodes.has(id)) nodes.set(id, { id, kind: kindFor(undefined), label: e.target_name ?? id, col: 2, truth: "exact" });
    edges.push({ s: centerId, t: id, verb, layer: layerFor(verb) });
  });
  return { nodes: [...nodes.values()], edges };
}

// Resolve + expand one entity into a center-and-neighbours graph via direct code
// relationships (depth 1). code/relationships only matches on `entity_id`, so we
// resolve the query to a graph entity id first (falling back to the raw query).
export async function loadEntityGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  let entityID = "";
  let displayName = name;
  let centerType: string | undefined;
  try {
    const resolved = await resolveEntity({ client, name, limit: 1 });
    const top = resolved.candidates[0];
    if (top?.id) { entityID = top.id; displayName = top.name ?? name; centerType = top.type; }
  } catch { /* resolution unavailable — handled below */ }
  if (entityID === "") {
    // code/relationships matches on entity_id only; a raw query string is not a
    // valid id, so render the searched node alone instead of forcing a 404.
    return centerOnlyGraph(name, name, undefined);
  }
  let env;
  try {
    env = await client.post<CodeRelationshipsResponse>("/api/v0/code/relationships", { entity_id: entityID, depth: 1 });
  } catch (e) {
    // code/relationships is keyed to code entities (Function/File/Class…). A
    // service/workload/infra entity has none, so the endpoint answers 404 — a
    // category mismatch, not a failure. Degrade to the resolved node alone so the
    // Explorer can show a clean "no direct code relationships" empty state and
    // invite the Neighborhood mode. Any other status (500/timeout) still surfaces.
    // See issue #1725.
    if (e instanceof EshuApiHttpError && e.status === 404) {
      return centerOnlyGraph(entityID, displayName, centerType);
    }
    throw e;
  }
  if (env.error) throw new EshuEnvelopeError(env.error);
  return codeRelationshipsToGraph(env.data ?? {}, { id: entityID, name: displayName });
}

// centerOnlyGraph renders a single hero node — used when an entity resolves but
// has no direct code relationships (or could not be resolved to an id).
function centerOnlyGraph(id: string, label: string, type: string | undefined): GraphModel {
  return { nodes: [{ id, kind: kindFor(type), label, sub: type, col: 1, hero: true, truth: "exact" }], edges: [] };
}

// recommendedModeForKind chooses the Explorer mode that has data for a resolved
// entity kind: code entities (Function/File/Class/Method/Symbol) expand through
// Direct (code/relationships); service/workload/repo/infra/cloud entities expand
// through Neighborhood (impact/entity-map). Unknown kinds keep Direct so existing
// code-search behaviour is unchanged. See issue #1725.
export function recommendedModeForKind(kind: string | undefined): "direct" | "neighborhood" {
  const k = (kind ?? "").toLowerCase();
  if (k === "") return "direct";
  const codeKind = ["function", "file", "class", "method", "symbol", "interface", "field", "variable"].some((c) => k.includes(c));
  if (codeKind) return "direct";
  const neighborhoodKind = ["service", "workload", "deployment", "repo", "resource", "aws", "infra", "cloud", "module", "package", "library", "endpoint", "queue", "topic", "bucket", "database", "table"].some((c) => k.includes(c));
  if (neighborhoodKind) return "neighborhood";
  return "direct";
}

// --- impact/entity-map (Neighborhood mode) ----------------------------------
// POST /api/v0/impact/entity-map requires `from` (not `name`); it resolves the
// handle itself and returns neighbours under evidence.relationships[].
interface EntityMapRel {
  readonly entity_id?: string; readonly entity_name?: string; readonly entity_labels?: readonly string[];
  readonly direction?: string;
  readonly relationship_type?: string; readonly relationship_types?: readonly string[];
}
interface EntityMapResponse {
  readonly from?: string;
  readonly resolution?: { readonly candidates?: readonly { readonly id?: string; readonly name?: string; readonly labels?: readonly string[] }[] };
  readonly evidence?: { readonly relationships?: readonly EntityMapRel[] };
}

// entityMapToGraph maps the entity-map evidence.relationships[] into a
// center-and-neighbours graph, using the resolved candidate as the center.
export function entityMapToGraph(data: EntityMapResponse, fallbackName: string): GraphModel {
  const candidate = data.resolution?.candidates?.[0];
  const centerId = candidate?.id ?? data.from ?? fallbackName;
  const centerType = candidate?.labels?.[0];
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, { id: centerId, kind: kindFor(centerType), label: candidate?.name ?? fallbackName, sub: centerType, col: 1, hero: true, truth: "exact" });
  const edges: GraphEdge[] = [];
  (data.evidence?.relationships ?? []).forEach((r) => {
    const label = (r.entity_name ?? r.entity_id ?? "").trim();
    // Prefer the stable entity_id for the node identity; fall back to the name
    // when the backend omits it. Keying by id avoids collapsing distinct nodes
    // that share a display name.
    const id = (r.entity_id ?? r.entity_name ?? "").trim();
    if (id === "" || id === centerId) return;
    const verb = (r.relationship_type ?? r.relationship_types?.[0] ?? "RELATED").toUpperCase();
    const type = r.entity_labels?.[0];
    const incoming = (r.direction ?? "outgoing").toLowerCase() === "incoming";
    if (!nodes.has(id)) nodes.set(id, { id, kind: kindFor(type), label: label || id, sub: type, col: incoming ? 0 : 2, truth: "exact" });
    edges.push(incoming
      ? { s: id, t: centerId, verb, layer: layerFor(verb) }
      : { s: centerId, t: id, verb, layer: layerFor(verb) });
  });
  return { nodes: [...nodes.values()], edges };
}

// Expand one entity into a broader neighbourhood via POST impact/entity-map.
// Returns the same center-and-neighbours graph shape as loadEntityGraph.
export async function loadEntityMapGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  // The endpoint's request field is `depth` (1-4); `max_depth` is ignored by the
  // Go decoder and silently defaults the traversal to depth 1.
  const env = await client.post<EntityMapResponse>("/api/v0/impact/entity-map", { from: name, depth: 2 });
  if (env.error) throw new EshuEnvelopeError(env.error);
  return entityMapToGraph(env.data ?? {}, name);
}

// resolveEntityName resolves a typed query to a canonical entity name via
// entities/resolve, returning the best candidate. Falls back to the raw query
// when nothing resolves, so search still works against exact names.
export async function resolveEntityName(client: EshuApiClient, query: string): Promise<string> {
  return (await resolveEntityHandle(client, query)).name;
}

// ResolvedHandle is the canonical name plus the resolved kind and the Explorer
// mode that has data for that kind. The Explorer uses `mode` to land a search on
// the right view (Direct for code, Neighborhood for service/infra) before the
// user toggles. See issue #1725.
export interface ResolvedHandle {
  readonly name: string;
  readonly kind: string;
  readonly mode: "direct" | "neighborhood";
}

// resolveEntityHandle resolves a typed query to its canonical name and kind via
// entities/resolve, then derives the recommended Explorer mode. Falls back to the
// raw query (and Direct mode) when nothing resolves or resolution is unavailable,
// so exact-name code search is unchanged.
export async function resolveEntityHandle(client: EshuApiClient, query: string): Promise<ResolvedHandle> {
  try {
    const result = await resolveEntity({ client, name: query, limit: 1 });
    const top = result.candidates[0];
    const kind = top?.type ?? top?.labels[0] ?? "";
    return { name: top?.name ?? query, kind, mode: recommendedModeForKind(kind) };
  } catch {
    return { name: query, kind: "", mode: "direct" };
  }
}

// Blast radius: dependents that break if `name` fails. `loadBlastGraph` queries
// the live API and throws if it is unavailable; callers that need an offline
// path fall back to `blastFromModel`, which reverse-walks the in-memory graph.
// One affected entity from the blast-radius response. The live endpoint returns
// `affected: [{repo, repo_id?, hops?, ...}]`; the other keys are kept for forward
// compatibility with alternate response shapes.
interface ImpactEntity {
  readonly id?: string; readonly name?: string; readonly type?: string;
  readonly distance?: number; readonly hops?: number;
  readonly repo?: string; readonly repo_id?: string;
}
interface BlastResponse {
  readonly target?: RelEntity | string; readonly entity?: RelEntity;
  readonly affected?: readonly ImpactEntity[];
  readonly impacted?: readonly ImpactEntity[]; readonly dependents?: readonly ImpactEntity[]; readonly results?: readonly ImpactEntity[];
}

export async function loadBlastGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  // POST /api/v0/impact/blast-radius requires `target` + `target_type`; a
  // service/workload is anchored on its repository. The response is
  // `{ target, target_type, affected: [{repo, repo_id?, hops?}], ... }`.
  const env = await client.post<BlastResponse>("/api/v0/impact/blast-radius", {
    target: name, target_type: "repository", limit: 50
  });
  const data = env.data ?? {};
  const center = ident(typeof data.target === "object" ? data.target : data.entity, name);
  const affected = data.affected ?? data.impacted ?? data.dependents ?? data.results ?? [];
  const nodes = new Map<string, GraphNode>();
  nodes.set(center.id, { id: center.id, kind: "service", label: center.name, sub: "impact origin", hero: true, col: 0, truth: "exact" });
  const edges: GraphEdge[] = [];
  affected.forEach((e) => {
    const label = (e.repo ?? e.name ?? e.id ?? "").trim();
    // Skip empty or non-entity rows (a real repo name has no whitespace), so a
    // target with no indexed dependents renders cleanly as the origin alone.
    if (label === "" || /\s/.test(label)) return;
    const id = e.repo_id ?? e.id ?? label;
    if (id === center.id) return;
    const hop = e.distance ?? e.hops ?? 1;
    if (!nodes.has(id)) nodes.set(id, { id, kind: kindFor(e.type), label, sub: `hop ${hop}`, col: hop, truth: "exact" });
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
