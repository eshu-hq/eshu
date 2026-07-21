import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { resolveEntity } from "./entityResolution";
import { EshuEnvelopeError } from "./envelope";
import { codeRelationshipsToGraph, type CodeRelationshipsResponse } from "./eshuGraphCode";
import { cleanText, kindFor, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface RelEntity {
  readonly id?: string;
  readonly name?: string;
  readonly type?: string;
  readonly entity_type?: string;
}

interface RelRecord {
  readonly verb?: string;
  readonly relationship?: string;
  readonly type?: string;
  readonly direction?: string;
  readonly target?: RelEntity;
  readonly entity?: RelEntity;
  readonly node?: RelEntity;
  readonly source?: RelEntity;
}

interface RelationshipsResponse {
  readonly target?: RelEntity;
  readonly entity?: RelEntity;
  readonly relationships?: readonly RelRecord[];
  readonly edges?: readonly RelRecord[];
  readonly results?: readonly RelRecord[];
}

function ident(
  entity: RelEntity | undefined,
  fallback: string,
): { id: string; name: string; type?: string } {
  const name = entity?.name ?? entity?.id ?? fallback;
  return { id: entity?.id ?? name, name, type: entity?.type ?? entity?.entity_type };
}

// Maps a relationship-style response into a center-and-neighbours graph.
export function relationshipsToGraph(data: RelationshipsResponse, name: string): GraphModel {
  const center = ident(data.target ?? data.entity, name);
  const records = data.relationships ?? data.edges ?? data.results ?? [];
  const nodes = new Map<string, GraphNode>();
  nodes.set(center.id, {
    id: center.id,
    kind: kindFor(center.type),
    label: center.name,
    sub: center.type,
    col: 1,
    hero: true,
    truth: "exact",
  });
  const edges: GraphEdge[] = [];

  records.forEach((record) => {
    const verb = (record.verb ?? record.relationship ?? record.type ?? "RELATED").toUpperCase();
    const other = ident(record.target ?? record.entity ?? record.node, "unknown");
    const incoming = (record.direction ?? "outgoing").toLowerCase() === "incoming";
    if (!nodes.has(other.id)) {
      nodes.set(other.id, {
        id: other.id,
        kind: kindFor(other.type),
        label: other.name,
        sub: other.type,
        col: incoming ? 0 : 2,
        truth: "exact",
      });
    }
    edges.push(
      incoming
        ? { s: other.id, t: center.id, verb, layer: layerFor(verb) }
        : { s: center.id, t: other.id, verb, layer: layerFor(verb) },
    );
  });

  return { nodes: [...nodes.values()], edges };
}

// Resolves one entity and expands direct code relationships around it.
export async function loadEntityGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  const resolved = await resolveEntity({ client, name, limit: 1 });
  const top = resolved.candidates[0];
  const entityID = top?.id ?? "";
  const displayName = top?.name ?? name;
  const centerType = top?.type;
  if (entityID === "") return centerOnlyGraph(name, name, undefined);

  let env;
  try {
    env = await client.post<CodeRelationshipsResponse>("/api/v0/code/relationships", {
      entity_id: entityID,
      max_depth: 1,
    });
  } catch (error) {
    if (error instanceof EshuApiHttpError && error.status === 404) {
      return centerOnlyGraph(entityID, displayName, centerType);
    }
    throw error;
  }
  if (env.error) throw new EshuEnvelopeError(env.error);
  return codeRelationshipsToGraph(env.data ?? {}, { id: entityID, name: displayName });
}

function centerOnlyGraph(id: string, label: string, type: string | undefined): GraphModel {
  return {
    nodes: [{ id, kind: kindFor(type), label, sub: type, col: 1, hero: true, truth: "exact" }],
    edges: [],
  };
}

// Chooses the Explorer mode that has data for a resolved entity kind.
export function recommendedModeForKind(kind: string | undefined): "direct" | "neighborhood" {
  const normalized = (kind ?? "").toLowerCase();
  if (normalized === "") return "direct";
  const codeKind = [
    "function",
    "file",
    "class",
    "method",
    "symbol",
    "interface",
    "field",
    "variable",
  ].some((candidate) => normalized.includes(candidate));
  if (codeKind) return "direct";
  const neighborhoodKind = [
    "service",
    "workload",
    "deployment",
    "repo",
    "resource",
    "aws",
    "infra",
    "cloud",
    "module",
    "package",
    "library",
    "endpoint",
    "queue",
    "topic",
    "bucket",
    "database",
    "table",
  ].some((candidate) => normalized.includes(candidate));
  return neighborhoodKind ? "neighborhood" : "direct";
}

export async function resolveEntityName(client: EshuApiClient, query: string): Promise<string> {
  return (await resolveEntityHandle(client, query)).name;
}

export interface ResolvedHandle {
  readonly id: string;
  readonly name: string;
  readonly kind: string;
  readonly mode: "direct" | "neighborhood";
  readonly repoId: string;
  readonly repoName: string;
}

export async function resolveEntityHandle(
  client: EshuApiClient,
  query: string,
): Promise<ResolvedHandle> {
  const canonical = canonicalResolutionQuery(query);
  const result = await resolveEntity({
    client,
    name: canonical.name,
    limit: 1,
    type: canonical.type,
  });
  const top = result.candidates[0];
  const kind = top?.type ?? top?.labels[0] ?? "";
  return {
    id: top?.id ?? "",
    name: top?.name ?? query,
    kind,
    mode: recommendedModeForKind(kind),
    repoId: repositoryIDForResolved(top?.id, top?.repoId, kind),
    repoName: top?.repoName ?? "",
  };
}

function canonicalResolutionQuery(query: string): { name: string; type?: string } {
  const trimmed = query.trim();
  const prefix = "workload:";
  if (trimmed.toLowerCase().startsWith(prefix)) {
    return { name: trimmed.slice(prefix.length), type: "workload" };
  }
  return { name: query };
}

function repositoryIDForResolved(
  id: string | undefined,
  repoID: string | undefined,
  kind: string,
): string {
  const resolvedRepoID = cleanText(repoID);
  if (resolvedRepoID !== "") return resolvedRepoID;
  const resolvedID = cleanText(id);
  if (resolvedID !== "" && kind.toLowerCase().includes("repo")) return resolvedID;
  return "";
}
