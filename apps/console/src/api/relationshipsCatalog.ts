// api/relationshipsCatalog.ts
// Loaders for the typed-edge relationships surface: the verb catalog
// (POST /api/v0/relationships/catalog) and the per-verb concrete edge slice
// (POST /api/v0/relationships/edges). Both are bounded, source-label-anchored
// reads on the Go side; these loaders only normalize the envelope payloads.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { GraphLayer } from "../console/types";

const GRAPH_LAYERS: readonly GraphLayer[] = [
  "code",
  "deploy",
  "infra",
  "runtime",
  "security",
  "ops",
];

export interface RelationshipVerbTile {
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly count: number;
  readonly evidence: string;
  readonly detail: string;
  // sourceTools is the per-tool edge count breakdown for Tier-2 verbs that
  // carry source_tool. It is absent for Tier-1 self-labeling and Tier-3
  // code/structural verbs that do not stamp edges with a tool.
  readonly sourceTools?: Readonly<Record<string, number>>;
}

export interface RelationshipsCatalog {
  readonly verbs: readonly RelationshipVerbTile[];
  readonly verbCount: number;
  readonly totalEdges: number;
  readonly layerCount: number;
}

export interface RelationshipEdge {
  readonly sourceId: string;
  readonly sourceName: string;
  readonly targetId: string;
  readonly targetName: string;
  readonly evidence: string;
  // sourceTool is present on Tier-2 resolver edges and absent on Tier-1
  // self-labeling and Tier-3 code/structural edges.
  readonly sourceTool?: string;
}

export interface RelationshipEdges {
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly evidence: string;
  readonly detail: string;
  readonly edges: readonly RelationshipEdge[];
  readonly truncated: boolean;
  readonly limit: number;
  // sourceTool echoes the filter that was applied to fetch this slice,
  // or undefined when the slice was fetched without a source_tool filter.
  readonly sourceTool?: string;
}

interface CatalogResponse {
  readonly verbs?: readonly VerbTileRecord[];
  readonly verb_count?: number;
  readonly total_edges?: number;
  readonly layer_count?: number;
}

interface VerbTileRecord {
  readonly verb?: string;
  readonly layer?: string;
  readonly count?: number;
  readonly evidence?: string;
  readonly detail?: string;
  readonly source_tools?: Record<string, number>;
}

interface EdgesResponse {
  readonly verb?: string;
  readonly layer?: string;
  readonly evidence?: string;
  readonly detail?: string;
  readonly edges?: readonly EdgeRecord[];
  readonly truncated?: boolean;
  readonly limit?: number;
  readonly source_tool?: string;
}

interface EdgeRecord {
  readonly source_id?: string;
  readonly source_name?: string;
  readonly target_id?: string;
  readonly target_name?: string;
  readonly evidence?: string;
  readonly source_tool?: string;
}

// loadRelationshipsCatalog fetches the typed-edge verb catalog with bounded
// per-verb whole-graph counts.
export async function loadRelationshipsCatalog(
  client: EshuApiClient,
): Promise<RelationshipsCatalog> {
  const env = await client.post<CatalogResponse>("/api/v0/relationships/catalog", {});
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const verbs = (data.verbs ?? []).map(normalizeVerbTile);
  return {
    verbs,
    verbCount: num(data.verb_count) ?? verbs.length,
    totalEdges: num(data.total_edges) ?? verbs.reduce((sum, verb) => sum + verb.count, 0),
    layerCount: num(data.layer_count) ?? new Set(verbs.map((verb) => verb.layer)).size,
  };
}

// loadRelationshipEdges fetches the bounded concrete-edge slice for one verb.
// When sourceTool is provided it is sent as source_tool in the POST body to
// restrict the slice to edges stamped by that specific tool.
export async function loadRelationshipEdges(
  client: EshuApiClient,
  verb: string,
  limit = 50,
  sourceTool?: string,
): Promise<RelationshipEdges> {
  const body: Record<string, unknown> = { verb, limit };
  if (sourceTool) body.source_tool = sourceTool;
  const env = await client.post<EdgesResponse>("/api/v0/relationships/edges", body);
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const tool = str(data.source_tool) || undefined;
  return {
    verb: str(data.verb) || verb,
    layer: layer(data.layer),
    evidence: str(data.evidence),
    detail: str(data.detail),
    edges: (data.edges ?? []).map(normalizeEdge),
    truncated: data.truncated === true,
    limit: num(data.limit) ?? limit,
    sourceTool: tool,
  };
}

function normalizeVerbTile(record: VerbTileRecord): RelationshipVerbTile {
  const sourceTools = normalizeSourceTools(record.source_tools);
  return {
    verb: str(record.verb),
    layer: layer(record.layer),
    count: num(record.count) ?? 0,
    evidence: str(record.evidence),
    detail: str(record.detail),
    ...(sourceTools ? { sourceTools } : {}),
  };
}

function normalizeEdge(record: EdgeRecord): RelationshipEdge {
  const tool = str(record.source_tool) || undefined;
  return {
    sourceId: str(record.source_id),
    sourceName: str(record.source_name) || str(record.source_id),
    targetId: str(record.target_id),
    targetName: str(record.target_name) || str(record.target_id),
    evidence: str(record.evidence),
    ...(tool ? { sourceTool: tool } : {}),
  };
}

// normalizeSourceTools coerces the wire source_tools map to a plain
// Record<string,number>, filtering out any entries with invalid counts.
// Returns undefined when the map is empty or absent.
function normalizeSourceTools(
  raw: Record<string, number> | undefined,
): Readonly<Record<string, number>> | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const result: Record<string, number> = {};
  for (const [key, value] of Object.entries(raw)) {
    const count = num(value);
    if (key && count !== undefined) result[key] = count;
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

// layer coerces an API layer string to a known GraphLayer, defaulting to "code"
// for an unexpected value so the UI palette lookup never returns undefined.
function layer(value: string | undefined): GraphLayer {
  const normalized = str(value).toLowerCase();
  return (GRAPH_LAYERS as readonly string[]).includes(normalized)
    ? (normalized as GraphLayer)
    : "code";
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function num(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
