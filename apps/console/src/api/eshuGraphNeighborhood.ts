import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import { kindFor, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface EntityMapRel {
  readonly entity_id?: string;
  readonly entity_name?: string;
  readonly entity_labels?: readonly string[];
  readonly direction?: string;
  readonly relationship_type?: string;
  readonly relationship_types?: readonly string[];
  readonly relationship_source?: string;
  readonly repo_id?: string;
  readonly environment?: string;
  readonly depth?: number;
}

interface EntityMapResponse {
  readonly from?: string;
  readonly resolution?: {
    readonly candidates?: readonly {
      readonly id?: string;
      readonly name?: string;
      readonly labels?: readonly string[];
    }[];
  };
  readonly evidence?: { readonly relationships?: readonly EntityMapRel[] };
}

// Maps entity-map evidence into a center-and-neighbours graph.
export function entityMapToGraph(data: EntityMapResponse, fallbackName: string): GraphModel {
  const candidate = data.resolution?.candidates?.[0];
  const centerId = candidate?.id ?? data.from ?? fallbackName;
  const centerType = candidate?.labels?.[0];
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, {
    id: centerId,
    kind: kindFor(centerType),
    label: candidate?.name ?? fallbackName,
    sub: centerType,
    col: 1,
    hero: true,
    truth: "exact",
  });
  const edges: GraphEdge[] = [];
  (data.evidence?.relationships ?? []).forEach((relationship) => {
    const label = (relationship.entity_name ?? relationship.entity_id ?? "").trim();
    const id = (relationship.entity_id ?? relationship.entity_name ?? "").trim();
    if (id === "" || id === centerId) return;
    const verb = (
      relationship.relationship_type ??
      relationship.relationship_types?.[0] ??
      "RELATED"
    ).toUpperCase();
    const type = relationship.entity_labels?.[0];
    const incoming = (relationship.direction ?? "outgoing").toLowerCase() === "incoming";
    if (!nodes.has(id)) {
      nodes.set(id, {
        id,
        kind: kindFor(type),
        label: label || id,
        sub: type,
        col: incoming ? 0 : 2,
        truth: "exact",
      });
    }
    const edge = incoming
      ? {
          s: id,
          t: centerId,
          verb,
          layer: layerFor(verb),
          evidence: edgeEvidence(relationship, true),
        }
      : {
          s: centerId,
          t: id,
          verb,
          layer: layerFor(verb),
          evidence: edgeEvidence(relationship, false),
        };
    edges.push(edge);
  });
  return { nodes: [...nodes.values()], edges };
}

function edgeEvidence(relationship: EntityMapRel, incoming: boolean): readonly string[] {
  const labels = (relationship.entity_labels ?? []).filter(Boolean).join(", ");
  return [
    `relationship source: ${relationship.relationship_source ?? "graph"}`,
    `direction: ${incoming ? "incoming" : "outgoing"}`,
    labels ? `entity labels: ${labels}` : "",
    relationship.repo_id ? `repo: ${relationship.repo_id}` : "",
    relationship.environment ? `environment: ${relationship.environment}` : "",
    relationship.depth !== undefined ? `depth: ${relationship.depth}` : "",
  ].filter((value): value is string => value !== "");
}

export async function loadEntityMapGraph(client: EshuApiClient, name: string): Promise<GraphModel> {
  const env = await client.post<EntityMapResponse>("/api/v0/impact/entity-map", {
    from: name,
    depth: 2,
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  return entityMapToGraph(env.data ?? {}, name);
}
