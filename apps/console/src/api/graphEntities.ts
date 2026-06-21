// api/graphEntities.ts
// Loader for the browsable graph entity inventory that backs the Nodes page.
// Wraps GET /api/v0/graph/entities: per-kind facet counts always, plus a
// bounded, name-searchable, paginated slice of one kind's entities when a kind
// is selected.
import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";
import { EshuEnvelopeError } from "./envelope";
import type { SectionProvenance } from "./eshuConsoleLive";

// GraphEntityKindCount is one KIND filter chip: its facet key, the underlying
// graph label, and the live node count.
export interface GraphEntityKindCount {
  readonly kind: string;
  readonly label: string;
  readonly count: number;
}

// GraphEntityRow is one browsable node row in the inventory table.
export interface GraphEntityRow {
  readonly id: string;
  readonly name: string;
  readonly kind: string;
  readonly account: string;
}

// GraphEntityPage is the normalized inventory response for one request.
export interface GraphEntityPage {
  readonly kinds: readonly GraphEntityKindCount[];
  readonly total: number;
  readonly entities: readonly GraphEntityRow[];
  readonly nextOffset: number | null;
  readonly truth: EshuTruth | null;
  readonly provenance: SectionProvenance;
}

// GraphEntityQuery selects the facet, name filter, and page window.
export interface GraphEntityQuery {
  readonly kind?: string;
  readonly q?: string;
  readonly limit?: number;
  readonly offset?: number;
}

interface GraphEntityKindRecord {
  readonly kind?: string;
  readonly label?: string;
  readonly count?: number;
}

interface GraphEntityRecord {
  readonly id?: string;
  readonly name?: string;
  readonly kind?: string;
  readonly account?: string;
}

interface GraphEntityResponse {
  readonly kinds?: readonly GraphEntityKindRecord[];
  readonly total?: number;
  readonly entities?: readonly GraphEntityRecord[];
  readonly limit?: number;
  readonly offset?: number;
  readonly truncated?: boolean;
}

const DEFAULT_LIMIT = 50;

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function buildPath(query: GraphEntityQuery): string {
  const params = new URLSearchParams();
  const limit = query.limit ?? DEFAULT_LIMIT;
  params.set("limit", String(limit));
  params.set("offset", String(query.offset ?? 0));
  if (query.kind) params.set("kind", query.kind);
  if (query.q && query.q.trim() !== "") params.set("q", query.q.trim());
  return `/api/v0/graph/entities?${params.toString()}`;
}

function kindFromRecord(record: GraphEntityKindRecord): GraphEntityKindCount {
  return { kind: str(record.kind), label: str(record.label), count: num(record.count) };
}

function rowFromRecord(record: GraphEntityRecord): GraphEntityRow {
  return {
    id: str(record.id),
    name: str(record.name),
    kind: str(record.kind),
    account: str(record.account)
  };
}

// loadGraphEntities fetches one inventory page. On any transport or envelope
// error it returns an "unavailable" page rather than throwing, so the Nodes
// page renders a truthful empty/unavailable state instead of crashing.
export async function loadGraphEntities(
  client: EshuApiClient,
  query: GraphEntityQuery = {}
): Promise<GraphEntityPage> {
  const limit = query.limit ?? DEFAULT_LIMIT;
  const offset = query.offset ?? 0;
  try {
    const env = await client.get<GraphEntityResponse>(buildPath(query));
    if (env.error) throw new EshuEnvelopeError(env.error);
    const data = env.data ?? {};
    const kinds = (data.kinds ?? []).map(kindFromRecord).filter((k) => k.kind !== "");
    const entities = (data.entities ?? []).map(rowFromRecord).filter((row) => row.id !== "" || row.name !== "");
    const nextOffset = data.truncated === true ? offset + limit : null;
    return {
      kinds,
      total: num(data.total),
      entities,
      nextOffset,
      truth: env.truth ?? null,
      provenance: "live"
    };
  } catch {
    return {
      kinds: [],
      total: 0,
      entities: [],
      nextOffset: null,
      truth: null,
      provenance: "unavailable"
    };
  }
}
