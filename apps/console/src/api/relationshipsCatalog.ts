// api/relationshipsCatalog.ts
// Loaders for the typed-edge relationships surface: the verb catalog
// (POST /api/v0/relationships/catalog) and the per-verb concrete edge slice
// (POST /api/v0/relationships/edges). Both are bounded, source-label-anchored
// reads on the Go side; these loaders only normalize the envelope payloads.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { GraphLayer } from "../console/types";

const GRAPH_LAYERS: readonly GraphLayer[] = ["code", "deploy", "infra", "runtime", "security", "ops"];

export interface RelationshipVerbTile {
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly count: number;
  readonly evidence: string;
  readonly detail: string;
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
}

export interface RelationshipEdges {
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly evidence: string;
  readonly detail: string;
  readonly edges: readonly RelationshipEdge[];
  readonly truncated: boolean;
  readonly limit: number;
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
}

interface EdgesResponse {
  readonly verb?: string;
  readonly layer?: string;
  readonly evidence?: string;
  readonly detail?: string;
  readonly edges?: readonly EdgeRecord[];
  readonly truncated?: boolean;
  readonly limit?: number;
}

interface EdgeRecord {
  readonly source_id?: string;
  readonly source_name?: string;
  readonly target_id?: string;
  readonly target_name?: string;
  readonly evidence?: string;
}

// loadRelationshipsCatalog fetches the typed-edge verb catalog with bounded
// per-verb whole-graph counts.
export async function loadRelationshipsCatalog(client: EshuApiClient): Promise<RelationshipsCatalog> {
  const env = await client.post<CatalogResponse>("/api/v0/relationships/catalog", {});
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const verbs = (data.verbs ?? []).map(normalizeVerbTile);
  return {
    verbs,
    verbCount: num(data.verb_count) ?? verbs.length,
    totalEdges: num(data.total_edges) ?? verbs.reduce((sum, verb) => sum + verb.count, 0),
    layerCount: num(data.layer_count) ?? new Set(verbs.map((verb) => verb.layer)).size
  };
}

// loadRelationshipEdges fetches the bounded concrete-edge slice for one verb.
export async function loadRelationshipEdges(
  client: EshuApiClient,
  verb: string,
  limit = 50
): Promise<RelationshipEdges> {
  const env = await client.post<EdgesResponse>("/api/v0/relationships/edges", { verb, limit });
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  return {
    verb: str(data.verb) || verb,
    layer: layer(data.layer),
    evidence: str(data.evidence),
    detail: str(data.detail),
    edges: (data.edges ?? []).map(normalizeEdge),
    truncated: data.truncated === true,
    limit: num(data.limit) ?? limit
  };
}

function normalizeVerbTile(record: VerbTileRecord): RelationshipVerbTile {
  return {
    verb: str(record.verb),
    layer: layer(record.layer),
    count: num(record.count) ?? 0,
    evidence: str(record.evidence),
    detail: str(record.detail)
  };
}

function normalizeEdge(record: EdgeRecord): RelationshipEdge {
  return {
    sourceId: str(record.source_id),
    sourceName: str(record.source_name) || str(record.source_id),
    targetId: str(record.target_id),
    targetName: str(record.target_name) || str(record.target_id),
    evidence: str(record.evidence)
  };
}

// layer coerces an API layer string to a known GraphLayer, defaulting to "code"
// for an unexpected value so the UI palette lookup never returns undefined.
function layer(value: string | undefined): GraphLayer {
  const normalized = str(value).toLowerCase();
  return (GRAPH_LAYERS as readonly string[]).includes(normalized) ? (normalized as GraphLayer) : "code";
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function num(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
