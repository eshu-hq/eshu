// api/capabilityCatalog.ts
// Loader for the capability maturity catalog served at GET /api/v0/capabilities.
// The same embedded artifact backs the HTTP API and the MCP get_capability_catalog
// tool, so the console capability matrix is in parity with those surfaces. The
// loader never fabricates data: on any error it returns an "unavailable"
// provenance so the page can render a truthful empty state.
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

// CapabilitySurface is one classified public surface for a capability.
export interface CapabilitySurface {
  readonly tool: string;
  readonly kind: string;
}

// CapabilityProofSignal is one verification signal for a capability.
export interface CapabilityProofSignal {
  readonly kind: string;
  readonly ref: string;
}

// CapabilityRow is the console view-model for one catalog entry.
export interface CapabilityRow {
  readonly capability: string;
  readonly displayName: string;
  readonly ownerPackage: string;
  readonly maturity: string;
  readonly derivedMaturity: string;
  readonly maturityReason: string;
  readonly surfaces: readonly CapabilitySurface[];
  readonly proofSignals: readonly CapabilityProofSignal[];
  readonly knownGaps: readonly string[];
  readonly linkedIssues: readonly number[];
  readonly console: boolean;
}

// CapabilityCatalogPage is one loaded page of the catalog plus its truth and
// provenance metadata.
export interface CapabilityCatalogPage {
  readonly rows: readonly CapabilityRow[];
  readonly total: number;
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "empty" | "unavailable";
}

interface CapabilityWireEntry {
  readonly capability?: string;
  readonly display_name?: string;
  readonly owner_package?: string;
  readonly maturity?: string;
  readonly derived_maturity?: string;
  readonly maturity_reason?: string;
  readonly surfaces?: readonly CapabilitySurface[];
  readonly proof_signals?: readonly CapabilityProofSignal[];
  readonly known_gaps?: readonly string[];
  readonly linked_issues?: readonly number[];
  readonly console?: boolean;
}

interface CapabilityListResponse {
  readonly version?: string;
  readonly total?: number;
  readonly capabilities?: readonly CapabilityWireEntry[];
}

function rowFromEntry(entry: CapabilityWireEntry): CapabilityRow {
  return {
    capability: entry.capability ?? "",
    displayName: entry.display_name ?? entry.capability ?? "",
    ownerPackage: entry.owner_package ?? "",
    maturity: entry.maturity ?? "",
    derivedMaturity: entry.derived_maturity ?? "",
    maturityReason: entry.maturity_reason ?? "",
    surfaces: entry.surfaces ?? [],
    proofSignals: entry.proof_signals ?? [],
    knownGaps: entry.known_gaps ?? [],
    linkedIssues: entry.linked_issues ?? [],
    console: entry.console ?? false
  };
}

// loadCapabilityCatalog fetches the capability catalog. Optional filters narrow
// by maturity or owner_package; paging is bounded by limit/offset.
export async function loadCapabilityCatalog(
  client: EshuApiClient,
  opts: { maturity?: string; owner?: string; limit?: number; offset?: number } = {}
): Promise<CapabilityCatalogPage> {
  const params = new URLSearchParams();
  if (opts.maturity) params.set("maturity", opts.maturity);
  if (opts.owner) params.set("owner", opts.owner);
  if (opts.limit !== undefined) params.set("limit", String(opts.limit));
  if (opts.offset !== undefined) params.set("offset", String(opts.offset));
  const query = params.toString();
  const path = query === "" ? "/api/v0/capabilities" : `/api/v0/capabilities?${query}`;
  try {
    const env = await client.get<CapabilityListResponse>(path);
    if (env.error) throw new EshuEnvelopeError(env.error);
    const rows = (env.data?.capabilities ?? []).map(rowFromEntry).filter((row) => row.capability !== "");
    return {
      rows,
      total: env.data?.total ?? rows.length,
      truth: env.truth ?? null,
      provenance: rows.length > 0 ? "live" : "empty"
    };
  } catch {
    return { rows: [], total: 0, truth: null, provenance: "unavailable" };
  }
}
