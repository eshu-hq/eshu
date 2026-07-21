import type { EshuApiClient } from "../api/client";
import { resolveEntity } from "../api/entityResolution";
import { loadEntityMapGraph } from "../api/eshuGraph";
import type { RepoListItem } from "../api/repoCatalog";
import { LAYER_COLOR, uiTruth } from "../console/types";
import type {
  ConsoleModel,
  GraphLayer,
  GraphModel,
  GraphNode,
  RelationshipRow,
  ServiceRow,
} from "../console/types";

export const LANDING_LAYERS: readonly GraphLayer[] = [
  "code",
  "deploy",
  "infra",
  "runtime",
  "security",
  "ops",
];

export const MAX_SEED_PROBES = 8;

type RelationshipCoverageRow = {
  readonly label: string;
  readonly value: number;
  readonly color: string;
  readonly detail: string;
};

export function initialSelection(graph: GraphModel): GraphNode | undefined {
  return graph.nodes.find((node) => node.hero) ?? graph.nodes[0];
}

function lastNumber(values: readonly number[]): number | null {
  const value = values.at(-1);
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

export function graphNodeMetric(model: ConsoleModel): number | null {
  return lastNumber(model.series.graphNodes);
}

export function relationshipMetric(model: ConsoleModel, graph: GraphModel): number | null {
  const graphEdgeCount = lastNumber(model.series.graphEdges);
  if (graphEdgeCount !== null) return graphEdgeCount;
  const relationshipTotal = model.relationships.reduce((total, row) => total + row.count, 0);
  if (relationshipTotal > 0) return relationshipTotal;
  return graph.edges.length > 0 ? graph.edges.length : null;
}

export function relationshipRowsFor(
  rows: readonly RelationshipRow[],
  graph: GraphModel,
): readonly RelationshipCoverageRow[] {
  if (rows.length > 0) {
    return rows
      .slice()
      .sort((a, b) => b.count - a.count)
      .slice(0, 7)
      .map((row) => ({
        label: row.verb,
        value: row.count,
        color: LAYER_COLOR[row.layer],
        detail: row.detail,
      }));
  }
  const counts = new Map<string, RelationshipRow>();
  for (const edge of graph.edges) {
    const key = `${edge.verb}\u0000${edge.layer}`;
    const current = counts.get(key);
    counts.set(key, {
      verb: edge.verb,
      layer: edge.layer,
      count: (current?.count ?? 0) + 1,
      detail: "Live entity-map relationships",
    });
  }
  return [...counts.values()]
    .sort((a, b) => b.count - a.count)
    .slice(0, 7)
    .map((row) => ({
      label: row.verb,
      value: row.count,
      color: LAYER_COLOR[row.layer],
      detail: row.detail,
    }));
}

export function filterGraphByLayer(
  graph: GraphModel,
  enabledLayers: Readonly<Record<GraphLayer, boolean>>,
): GraphModel {
  const edges = graph.edges.filter((edge) => enabledLayers[edge.layer]);
  const keep = new Set<string>();
  for (const edge of edges) {
    keep.add(edge.s);
    keep.add(edge.t);
  }
  for (const node of graph.nodes) {
    if (node.hero) keep.add(node.id);
  }
  return {
    edges,
    nodes: graph.nodes.filter((node) => keep.has(node.id) || graph.edges.length === 0),
  };
}

export function layerCounts(
  graph: GraphModel,
): readonly { readonly count: number; readonly layer: GraphLayer }[] {
  return LANDING_LAYERS.map((layer) => ({
    count: graph.edges.filter((edge) => edge.layer === layer).length,
    layer,
  }));
}

export function hotEntityRows(
  model: ConsoleModel,
  atlasSeeds: readonly GraphNode[],
): readonly { readonly id: string; readonly name: string }[] {
  const services =
    atlasSeeds.length > 0
      ? atlasSeeds.map((seed) => ({ id: seed.id, name: seed.label }))
      : model.services.map((service) => ({ id: service.id, name: service.name }));
  return services.filter((row) => row.name.trim().length > 0).slice(0, MAX_SEED_PROBES);
}

// A single edge is the trivial workload-to-repository self-edge, so seed
// discovery continues until it finds a useful neighbourhood.
const MEANINGFUL_SEED_EDGES = 2;

export function liveAtlasSeeds(
  model: ConsoleModel,
  repositories: readonly RepoListItem[] | undefined,
): readonly GraphNode[] {
  if (model.source !== "live" || model.graph.nodes.length > 0) return [];
  const serviceSeeds = model.services
    .filter((service) => service.name.trim().length > 0)
    .map(serviceSeedNode);
  if (serviceSeeds.length > 0) return serviceSeeds;
  return (repositories ?? [])
    .filter((repository) => repository.name.trim().length > 0 && repository.id.trim().length > 0)
    .map(repoSeedNode);
}

export async function selectSeedGraph(
  client: EshuApiClient,
  seeds: readonly GraphNode[],
  isCancelled: () => boolean,
): Promise<{ readonly seed: GraphNode; readonly graph: GraphModel } | undefined> {
  let best: { readonly seed: GraphNode; readonly graph: GraphModel } | undefined;
  for (const seed of seeds.slice(0, MAX_SEED_PROBES)) {
    if (isCancelled()) return best;
    const resolution = atlasSeedResolution(seed);
    if (!resolution) continue;
    const result = await resolveEntity({ client, limit: 1, ...resolution });
    const resolved = result.candidates[0]?.name ?? resolution.name;
    const graph = await loadEntityMapGraph(client, resolved);
    if (graph.edges.length >= MEANINGFUL_SEED_EDGES) return { seed, graph };
    if (!best || graph.edges.length > best.graph.edges.length) best = { seed, graph };
  }
  return best;
}

function atlasSeedResolution(
  seed: GraphNode,
): { readonly name: string; readonly repoId?: string; readonly type?: string } | null {
  const name = seed.label.trim();
  if (name === "") return null;
  const kind = seed.kind.trim().toLowerCase();
  if (kind === "service" || kind === "workload") return { name, type: "workload" };
  if (kind !== "repo" && kind !== "repository") return null;
  const repoId = seed.id.trim();
  return repoId === "" ? null : { name, repoId };
}

function serviceSeedNode(service: ServiceRow): GraphNode {
  const label = service.name.trim();
  const id = service.id.trim() || label;
  const repo = service.repo.trim();
  return {
    col: 1,
    hero: true,
    id,
    kind: serviceKind(service.kind),
    label,
    sub: repo || undefined,
    truth: uiTruth(service.truth),
  };
}

function repoSeedNode(repository: RepoListItem): GraphNode {
  const label = repository.name.trim();
  const id = repository.id.trim();
  return {
    col: 1,
    hero: true,
    id,
    kind: "repo",
    label,
    sub: repository.repoSlug.trim() || undefined,
    truth: "exact",
  };
}

function serviceKind(kind: string): string {
  const lower = kind.toLowerCase();
  if (lower.includes("workload") || lower.includes("deployment")) return "workload";
  if (lower.includes("repo")) return "repo";
  return "service";
}

export function dashboardErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "failed";
}
