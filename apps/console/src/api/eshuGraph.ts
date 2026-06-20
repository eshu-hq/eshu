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
import { codeRelationshipsToGraph, type CodeRelationshipsResponse } from "./eshuGraphCode";

export { loadBlastGraph, blastFromModel } from "./eshuGraphImpact";
export {
  codeRelationshipStoryToGraph,
  codeRelationshipsToGraph,
  mergeGraphSourceMetadata,
  type CodeRelationshipStoryCoverage,
  type CodeRelationshipStoryResponse,
  type CodeRelationshipsResponse
} from "./eshuGraphCode";

const VERB_LAYER: Record<string, GraphLayer> = {
  CALLS: "code", IMPORTS: "code", INHERITS: "code", OVERRIDES: "code", REFERENCES: "code",
  DEPLOYS_FROM: "deploy", DEPLOYS_HELM: "deploy", PACKAGES: "deploy",
  BUILDS: "deploy", DISCOVERS_CONFIG_IN: "deploy",
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

// Resolve + expand one entity into a center-and-neighbours graph via direct code
// relationships (max_depth 1). code/relationships only matches on `entity_id`,
// so we resolve the query to a graph entity id first (falling back to the raw query).
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
    env = await client.post<CodeRelationshipsResponse>("/api/v0/code/relationships", { entity_id: entityID, max_depth: 1 });
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
  readonly relationship_source?: string; readonly repo_id?: string; readonly environment?: string; readonly depth?: number;
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
      ? { s: id, t: centerId, verb, layer: layerFor(verb), evidence: entityMapEdgeEvidence(r, incoming) }
      : { s: centerId, t: id, verb, layer: layerFor(verb), evidence: entityMapEdgeEvidence(r, incoming) });
  });
  return { nodes: [...nodes.values()], edges };
}

function entityMapEdgeEvidence(r: EntityMapRel, incoming: boolean): readonly string[] {
  const labels = (r.entity_labels ?? []).filter(Boolean).join(", ");
  return [
    `relationship source: ${r.relationship_source ?? "graph"}`,
    `direction: ${incoming ? "incoming" : "outgoing"}`,
    labels ? `entity labels: ${labels}` : "",
    r.repo_id ? `repo: ${r.repo_id}` : "",
    r.environment ? `environment: ${r.environment}` : "",
    r.depth !== undefined ? `depth: ${r.depth}` : ""
  ].filter((value): value is string => value !== "");
}

interface DeploymentArtifactRecord {
  readonly source_repo_id?: string;
  readonly source_repo_name?: string;
  readonly target_repo_id?: string;
  readonly target_repo_name?: string;
  readonly relationship_type?: string;
  readonly artifact_family?: string;
  readonly evidence_kind?: string;
  readonly environment?: string;
  readonly path?: string;
}
/**
 * Minimal service-context payload needed to render a typed deployment story
 * without fetching raw source bodies.
 */
export interface ServiceDeploymentContextResponse {
  readonly name?: string;
  readonly repo_name?: string;
  readonly deployment_evidence?: {
    readonly artifacts?: readonly DeploymentArtifactRecord[];
  };
}
interface RepositoryDeploymentContextResponse {
  readonly repository?: {
    readonly id?: string;
    readonly name?: string;
  };
  readonly deployment_evidence?: ServiceDeploymentContextResponse["deployment_evidence"];
}
interface RepoRef { readonly id: string; readonly name: string; }

/**
 * Builds the user-facing controller repo -> chart repo -> source repo ->
 * workload story from service-context deployment artifacts. The graph is
 * derived from artifact evidence instead of the lossy impact/entity-map fanout.
 */
export function deploymentStoryToGraph(data: ServiceDeploymentContextResponse, fallbackName: string): GraphModel {
  const serviceName = cleanText(data.name) || fallbackName;
  const sourceRepoName = cleanText(data.repo_name) || serviceName;
  const deployArtifacts = (data.deployment_evidence?.artifacts ?? []).filter((artifact) =>
    cleanText(artifact.relationship_type).toUpperCase() === "DEPLOYS_FROM"
  );
  const artifactTargetRepo = deployArtifacts
    .map((artifact) => repoFromArtifact(artifact, "target"))
    .find((repo) => repo?.name === sourceRepoName);
  const sourceRepo = artifactTargetRepo ?? { id: `repository:${sourceRepoName}`, name: sourceRepoName };
  const serviceID = `workload:${serviceName}`;
  const nodes = new Map<string, GraphNode>();
  const edges: GraphEdge[] = [];
  const edgeKeys = new Set<string>();
  const chartRepoIDs = new Set<string>();

  addStoryNode(nodes, { id: serviceID, label: serviceName, kind: "workload", sub: "Workload", col: 3, hero: true, truth: "derived" });
  addStoryNode(nodes, { id: sourceRepo.id, label: sourceRepo.name, kind: "repo", sub: "Source repository", col: 2, truth: "derived" });

  const chartRepos = uniqueRepos(deployArtifacts
    .filter(isHelmChartArtifact)
    .map((artifact) => repoFromArtifact(artifact, "source"))
    .filter((repo): repo is RepoRef => !!repo && repo.id !== sourceRepo.id));
  chartRepos.forEach((repo) => {
    chartRepoIDs.add(repo.id);
    addStoryNode(nodes, { id: repo.id, label: repo.name, kind: "repo", sub: "Helm chart", col: 1, truth: "derived" });
  });
  deployArtifacts.filter(isHelmChartArtifact).forEach((artifact) => {
    const repo = repoFromArtifact(artifact, "source");
    if (repo && repo.id !== sourceRepo.id) addStoryEdge(edges, edgeKeys, repo.id, sourceRepo.id, "PACKAGES", artifactEdgeEvidence(artifact));
  });

  const controllerArtifacts = deployArtifacts.filter(isDeploymentControllerArtifact);
  const controllerRepos = uniqueRepos(controllerArtifacts.map((artifact) => repoFromArtifact(artifact, "source"))
    .filter((repo): repo is RepoRef => !!repo && repo.id !== sourceRepo.id && !chartRepoIDs.has(repo.id)));
  controllerRepos.forEach((repo) => {
    addStoryNode(nodes, { id: repo.id, label: repo.name, kind: "repo", sub: "Deployment controller", col: 0, truth: "derived" });
  });
  controllerArtifacts.forEach((artifact) => {
    const repo = repoFromArtifact(artifact, "source");
    if (!repo || repo.id === sourceRepo.id || chartRepoIDs.has(repo.id)) return;
    if (chartRepos.length === 0) {
      addStoryEdge(edges, edgeKeys, repo.id, sourceRepo.id, "DEPLOYS_FROM", artifactEdgeEvidence(artifact));
      return;
    }
    chartRepos.forEach((chartRepo) => addStoryEdge(edges, edgeKeys, repo.id, chartRepo.id, "DEPLOYS_HELM", artifactEdgeEvidence(artifact)));
  });

  if (deployArtifacts.length > 0) {
    addStoryEdge(edges, edgeKeys, sourceRepo.id, serviceID, "DEPLOYS_FROM", artifactEdgeEvidence(deployArtifacts[0]));
  }
  return { nodes: [...nodes.values()], edges };
}

/**
 * Loads the best neighborhood graph for a handle, preferring service deployment
 * context and falling back to impact/entity-map when no story evidence exists.
 */
export async function loadEntityStoryGraph(client: EshuApiClient, name: string, repoID?: string): Promise<GraphModel> {
  const storyClient = client as EshuApiClient & { readonly get?: EshuApiClient["get"] };
  if (typeof storyClient.get === "function") {
    try {
      const env = await storyClient.get<ServiceDeploymentContextResponse>(`/api/v0/services/${encodeURIComponent(name)}/context`);
      if (env.error) throw new EshuEnvelopeError(env.error);
      const graph = deploymentStoryToGraph(env.data ?? {}, name);
      if (graph.edges.length > 0) return graph;
    } catch (error) {
      if (!shouldFallbackFromServiceContext(error)) throw error;
    }
    const repositoryGraph = await loadRepositoryDeploymentStoryGraph(storyClient, name, repoID);
    if (repositoryGraph !== null) return repositoryGraph;
  }
  return loadEntityMapGraph(client, name);
}

async function loadRepositoryDeploymentStoryGraph(
  client: EshuApiClient & { readonly get: EshuApiClient["get"] },
  name: string,
  repoID: string | undefined
): Promise<GraphModel | null> {
  const id = cleanText(repoID);
  if (id === "") return null;
  try {
    const env = await client.get<RepositoryDeploymentContextResponse>(`/api/v0/repositories/${encodeURIComponent(id)}/context`);
    if (env.error) throw new EshuEnvelopeError(env.error);
    const data = env.data ?? {};
    const repoName = cleanText(data.repository?.name) || name;
    const graph = deploymentStoryToGraph({
      name,
      repo_name: repoName,
      deployment_evidence: data.deployment_evidence
    }, name);
    return graph.edges.length > 0 ? graph : null;
  } catch (error) {
    if (shouldFallbackFromServiceContext(error)) return null;
    throw error;
  }
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

function cleanText(value: string | undefined): string {
  return value?.trim() ?? "";
}

function repoFromArtifact(artifact: DeploymentArtifactRecord, side: "source" | "target"): RepoRef | undefined {
  const id = cleanText(side === "source" ? artifact.source_repo_id : artifact.target_repo_id);
  const name = cleanText(side === "source" ? artifact.source_repo_name : artifact.target_repo_name);
  if (id === "" && name === "") return undefined;
  return { id: id || `repository:${name}`, name: name || id };
}

function uniqueRepos(repos: readonly RepoRef[]): RepoRef[] {
  const seen = new Set<string>();
  const unique: RepoRef[] = [];
  repos.forEach((repo) => {
    if (seen.has(repo.id)) return;
    seen.add(repo.id);
    unique.push(repo);
  });
  return unique;
}

function isHelmChartArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = cleanText(artifact.artifact_family).toLowerCase();
  const path = cleanText(artifact.path).toLowerCase();
  const sourceRepo = cleanText(artifact.source_repo_name).toLowerCase();
  if (family !== "helm") return false;
  return path.endsWith("/chart.yaml") || sourceRepo.includes("helm") || sourceRepo.includes("chart");
}

function isDeploymentControllerArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = cleanText(artifact.artifact_family).toLowerCase();
  return family === "argocd" || family === "kustomize";
}

function addStoryNode(nodes: Map<string, GraphNode>, node: GraphNode): void {
  if (!nodes.has(node.id)) nodes.set(node.id, node);
}

function addStoryEdge(edges: GraphEdge[], seen: Set<string>, s: string, t: string, verb: string, evidence?: readonly string[]): void {
  const key = `${s}\u0000${t}\u0000${verb}`;
  if (seen.has(key)) return;
  seen.add(key);
  edges.push({ s, t, verb, layer: layerFor(verb), evidence });
}

function artifactEdgeEvidence(artifact: DeploymentArtifactRecord): readonly string[] {
  return [
    cleanText(artifact.artifact_family) ? `artifact family: ${cleanText(artifact.artifact_family)}` : "",
    cleanText(artifact.evidence_kind) ? `evidence kind: ${cleanText(artifact.evidence_kind)}` : "",
    cleanText(artifact.path) ? `path: ${cleanText(artifact.path)}` : "",
    cleanText(artifact.environment) ? `environment: ${cleanText(artifact.environment)}` : ""
  ].filter((value): value is string => value !== "");
}

function shouldFallbackFromServiceContext(error: unknown): boolean {
  if (error instanceof EshuApiHttpError) return error.status === 404;
  if (!(error instanceof EshuEnvelopeError)) return false;
  const code = error.error.code.toLowerCase();
  return code === "not_found" || code === "service_not_found" || code === "unknown_service";
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
  readonly id: string;
  readonly name: string;
  readonly kind: string;
  readonly mode: "direct" | "neighborhood";
  readonly repoId: string;
  readonly repoName: string;
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
    return {
      id: top?.id ?? "",
      name: top?.name ?? query,
      kind,
      mode: recommendedModeForKind(kind),
      repoId: repositoryIDForResolved(top?.id, top?.repoId, kind),
      repoName: top?.repoName ?? ""
    };
  } catch {
    return { id: "", name: query, kind: "", mode: "direct", repoId: "", repoName: "" };
  }
}

function repositoryIDForResolved(id: string | undefined, repoID: string | undefined, kind: string): string {
  const resolvedRepoID = cleanText(repoID);
  if (resolvedRepoID !== "") return resolvedRepoID;
  const resolvedID = cleanText(id);
  if (resolvedID !== "" && kind.toLowerCase().includes("repo")) return resolvedID;
  return "";
}
