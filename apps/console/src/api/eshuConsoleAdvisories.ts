// eshuConsoleAdvisories.ts
// Browsable vulnerability-intelligence catalog client. Split from
// eshuConsoleLive.ts so the catalog fetch/pagination contract stays focused and
// the live snapshot module stays under the file-size cap. Maps
// GET /api/v0/supply-chain/advisories into the AdvisoryRow shape the
// Vulnerabilities page renders. These rows are known source intelligence and do
// not imply service reachability or impact.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import { severityFromCvss } from "./eshuConsoleLive";
import type { AdvisoryRow } from "./eshuConsoleLive";

// AdvisoryCatalogCursor is the keyset continuation a catalog page returns when
// more rows exist. Pass both fields to the next request.
export interface AdvisoryCatalogCursor {
  readonly after_cvss: number;
  readonly after_advisory_key: string;
}

// AdvisoryCatalogPageResult is one fetched catalog page plus its truth envelope
// and the cursor for the next page (null when the page is the last).
export interface AdvisoryCatalogPageResult {
  readonly rows: readonly AdvisoryRow[];
  readonly nextCursor: AdvisoryCatalogCursor | null;
  readonly summary: AdvisoryCatalogSummary;
  readonly truth: EshuTruth | undefined;
}

// AdvisoryCatalogSummary preserves the authoritative bounded-page metadata.
// count is the number of rows in this page, never an assumed corpus total;
// truncated tells the UI when it must render the count with an open-ended cue.
export interface AdvisoryCatalogSummary {
  readonly count: number;
  readonly limit: number;
  readonly truncated: boolean;
}

// AdvisoryCatalogQuery bounds one catalog page request.
export interface AdvisoryCatalogQuery {
  readonly limit: number;
  readonly severity?: string;
  readonly ecosystem?: string;
  readonly kev?: boolean;
  readonly q?: string;
  readonly cursor?: AdvisoryCatalogCursor | null;
}

interface AdvisoryCatalogResponse {
  readonly advisories?: readonly Record<string, unknown>[];
  readonly count?: number;
  readonly limit?: number;
  readonly truncated?: boolean;
  readonly next_cursor?: { readonly after_cvss?: number; readonly after_advisory_key?: string };
}

// mapAdvisoryRow lifts one catalog API row into the UI AdvisoryRow shape. The
// API already canonicalizes ids and severity; this only narrows types and
// derives a severity band when the source omits a label.
export function mapAdvisoryRow(raw: Record<string, unknown>): AdvisoryRow {
  const cvss = Number(raw.cvss_score ?? 0);
  const cveId = stringField(raw.cve_id);
  const ghsaId = stringField(raw.ghsa_id);
  const id = stringField(raw.advisory_key) || stringField(raw.canonical_id) || cveId || ghsaId;
  const label = raw.severity_label ? stringField(raw.severity_label) : severityFromCvss(cvss);
  return {
    id,
    cveId,
    ghsaId,
    severity: label.toLowerCase(),
    cvss,
    kev: Boolean(raw.kev),
    ecosystems: Array.isArray(raw.ecosystems) ? (raw.ecosystems as string[]) : [],
    packageIds: Array.isArray(raw.package_ids) ? (raw.package_ids as string[]) : [],
    publishedAt: stringField(raw.published_at),
  };
}

// stringField narrows an `unknown` API value to a string. Anything that is
// not a string (including objects, which would otherwise stringify to
// "[object Object]") is treated as missing.
function stringField(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// fetchAdvisoryCatalogPage reads one bounded page of the browsable CVE
// intelligence catalog. It is independent of loadConsoleSnapshot so the
// Vulnerabilities page can paginate, filter, and refresh the catalog without
// reloading the whole console. Filters and the keyset cursor map directly onto
// GET /api/v0/supply-chain/advisories.
export async function fetchAdvisoryCatalogPage(
  client: EshuApiClient,
  query: AdvisoryCatalogQuery,
): Promise<AdvisoryCatalogPageResult> {
  const params = new URLSearchParams();
  params.set("limit", String(query.limit));
  if (query.severity) params.set("severity", query.severity);
  if (query.ecosystem) params.set("ecosystem", query.ecosystem);
  if (query.kev) params.set("kev", "true");
  if (query.q) params.set("q", query.q);
  if (query.cursor) {
    params.set("after_cvss", String(query.cursor.after_cvss));
    params.set("after_advisory_key", query.cursor.after_advisory_key);
  }
  const env = await client.get<AdvisoryCatalogResponse>(
    `/api/v0/supply-chain/advisories?${params.toString()}`,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const rows = (data.advisories ?? []).map(mapAdvisoryRow);
  const summary = catalogSummary(data, rows.length);
  const next = catalogCursor(data, summary.truncated);
  return { rows, nextCursor: next, summary, truth: env.truth ?? undefined };
}

function catalogSummary(data: AdvisoryCatalogResponse, rowCount: number): AdvisoryCatalogSummary {
  const count = data.count;
  const limit = data.limit;
  if (typeof count !== "number" || !Number.isInteger(count) || count < 0 || count !== rowCount) {
    throw new Error(
      `catalog page count must match returned rows; got ${String(count)} for ${rowCount} rows`,
    );
  }
  if (typeof limit !== "number" || !Number.isInteger(limit) || limit <= 0 || count > limit) {
    throw new Error(
      `catalog page limit must bound count; got limit ${String(limit)} and count ${count}`,
    );
  }
  if (typeof data.truncated !== "boolean") {
    throw new Error("catalog page truncated flag is required");
  }
  return { count, limit, truncated: data.truncated };
}

function catalogCursor(
  data: AdvisoryCatalogResponse,
  truncated: boolean,
): AdvisoryCatalogCursor | null {
  if (!truncated) return null;
  const key = data.next_cursor?.after_advisory_key;
  const cvss = data.next_cursor?.after_cvss;
  if (
    typeof key !== "string" ||
    key.length === 0 ||
    typeof cvss !== "number" ||
    !Number.isFinite(cvss)
  ) {
    throw new Error("truncated catalog page requires a complete next cursor");
  }
  return { after_cvss: cvss, after_advisory_key: key };
}
