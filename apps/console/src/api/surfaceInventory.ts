// api/surfaceInventory.ts
// Loader for the surface inventory readiness catalog served at
// GET /api/v0/surface-inventory. The same embedded artifact backs the HTTP API,
// so the console surface inventory is in parity with that surface. The loader
// never fabricates data: on any error it returns an "unavailable" provenance so
// the page can render a truthful empty state, and it shows each readiness lane
// exactly as the backend classified it — never implying readiness a surface
// does not have.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

// SurfaceRow is the console view-model for one surface inventory entry.
export interface SurfaceRow {
  readonly category: string;
  readonly name: string;
  readonly readiness: string;
  readonly owner: string;
  readonly proof: string;
  readonly docs: readonly string[];
  readonly notes: string;
  readonly collectorContract: SurfaceCollectorContract | null;
}

// SurfaceCollectorContract is the collector-only provenance contract that links
// emitted fact kinds to projection and read surfaces.
export interface SurfaceCollectorContract {
  readonly fact_kinds?: readonly string[];
  readonly projection_surfaces?: readonly string[];
  readonly read_surfaces?: readonly string[];
  readonly proof_gates?: readonly string[];
  readonly fixture_refs?: readonly string[];
  readonly truth_profile?: string;
}

// SurfaceInventoryPage is one loaded page of the inventory plus its truth and
// provenance metadata.
export interface SurfaceInventoryPage {
  readonly rows: readonly SurfaceRow[];
  readonly total: number;
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "empty" | "unavailable";
}

interface SurfaceWireEntry {
  readonly category?: string;
  readonly name?: string;
  readonly readiness?: string;
  readonly owner?: string;
  readonly proof?: string;
  readonly docs?: readonly string[];
  readonly notes?: string;
  readonly collector_contract?: SurfaceCollectorContract;
}

interface SurfaceListResponse {
  readonly version?: string;
  readonly total?: number;
  readonly limit?: number;
  readonly offset?: number;
  readonly truncated?: boolean;
  readonly surfaces?: readonly SurfaceWireEntry[];
}

function rowFromEntry(entry: SurfaceWireEntry): SurfaceRow {
  return {
    category: entry.category ?? "",
    name: entry.name ?? "",
    readiness: entry.readiness ?? "",
    owner: entry.owner ?? "",
    proof: entry.proof ?? "",
    docs: entry.docs ?? [],
    notes: entry.notes ?? "",
    collectorContract: entry.collector_contract ?? null,
  };
}

// loadSurfaceInventory fetches the surface inventory readiness catalog. Optional
// filters narrow by category or readiness; paging is bounded by limit/offset.
export async function loadSurfaceInventory(
  client: EshuApiClient,
  opts: { category?: string; readiness?: string; limit?: number; offset?: number } = {},
): Promise<SurfaceInventoryPage> {
  const params = new URLSearchParams();
  if (opts.category) params.set("category", opts.category);
  if (opts.readiness) params.set("readiness", opts.readiness);
  if (opts.limit !== undefined) params.set("limit", String(opts.limit));
  if (opts.offset !== undefined) params.set("offset", String(opts.offset));
  const query = params.toString();
  const path = query === "" ? "/api/v0/surface-inventory" : `/api/v0/surface-inventory?${query}`;
  try {
    const env = await client.get<SurfaceListResponse>(path);
    if (env.error) throw new EshuEnvelopeError(env.error);
    const rows = (env.data?.surfaces ?? []).map(rowFromEntry).filter((row) => row.name !== "");
    return {
      rows,
      total: env.data?.total ?? rows.length,
      truth: env.truth ?? null,
      provenance: rows.length > 0 ? "live" : "empty",
    };
  } catch {
    return { rows: [], total: 0, truth: null, provenance: "unavailable" };
  }
}
