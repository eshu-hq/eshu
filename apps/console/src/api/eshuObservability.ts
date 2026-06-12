// api/eshuObservability.ts
// Observability coverage loader. Reads reducer-owned coverage correlations from
// GET /api/v0/observability/coverage/correlations for the four observability
// collectors (grafana, prometheus/mimir, loki, tempo). That endpoint requires an
// anchor (provider/scope_id/coverage_signal/...), so we fan out one request per
// provider and merge. getJson handles both envelope and raw JSON responses.
// Live-API data only - no fabricated rows.

import type { EshuApiClient } from "./client";

// PROVIDERS are the observability source providers the coverage endpoint accepts
// as an anchor. prometheus_mimir reports under the `prometheus` provider.
export const OBSERVABILITY_PROVIDERS = ["grafana", "prometheus", "loki", "tempo"] as const;
export type ObservabilityProvider = (typeof OBSERVABILITY_PROVIDERS)[number];

// CoverageRow is one observability coverage correlation: whether a monitored
// resource/service or observability metadata identity has a coverage signal
// (dashboard, datasource, alarm, scrape, rule, log, trace, ...) versus a gap.
export interface CoverageRow {
  readonly id: string;
  readonly provider: string;
  readonly signal: string;
  readonly object: string;
  readonly target: string;
  readonly status: string;
  readonly covered: boolean;
  readonly resourceClass: string;
  readonly sourceKind: string;
  readonly freshness: string;
  readonly outcome: string;
  readonly reason: string;
}

// ProviderSummary aggregates coverage rows for one provider.
export interface ProviderSummary {
  readonly provider: string;
  readonly total: number;
  readonly covered: number;
  readonly gaps: number;
  readonly source: "live" | "empty" | "unavailable";
  readonly error: string;
}

// ObservabilitySnapshot is the full coverage view the page renders.
export interface ObservabilitySnapshot {
  readonly rows: readonly CoverageRow[];
  readonly providers: readonly ProviderSummary[];
  readonly signals: readonly { readonly signal: string; readonly count: number }[];
  readonly source: "live" | "empty" | "unavailable";
}

interface CorrelationRecord {
  readonly correlation_id?: string;
  readonly provider?: string;
  readonly coverage_signal?: string;
  readonly observability_object_ref?: string;
  readonly observability_resource_uid?: string;
  readonly target_service_ref?: string;
  readonly target_uid?: string;
  readonly coverage_status?: string;
  readonly resource_class?: string;
  readonly source_kind?: string;
  readonly freshness_state?: string;
  readonly outcome?: string;
  readonly reason?: string;
}
interface CoverageResponse {
  readonly correlations?: readonly CorrelationRecord[];
  readonly results?: readonly CorrelationRecord[];
  readonly truncated?: boolean;
  readonly next_cursor?: {
    readonly after_correlation_id?: string;
  };
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function mapRow(rec: CorrelationRecord, fallbackProvider: string): CoverageRow {
  const status = str(rec.coverage_status);
  const provider = str(rec.provider) || fallbackProvider;
  const signal = str(rec.coverage_signal) || "unknown";
  const object = str(rec.observability_object_ref) || str(rec.observability_resource_uid);
  const target = str(rec.target_service_ref) || str(rec.target_uid);
  return {
    id: str(rec.correlation_id) || `${provider}:${signal}:${object}:${target}`,
    provider,
    signal,
    object,
    target,
    status: status || "unknown",
    // Coverage is "covered" when a current signal exists; anything else (gap,
    // uncovered, stale) is treated as a gap for the rollup.
    covered: status.toLowerCase() === "covered",
    resourceClass: str(rec.resource_class),
    sourceKind: str(rec.source_kind),
    freshness: str(rec.freshness_state),
    outcome: str(rec.outcome),
    reason: str(rec.reason)
  };
}

interface ProviderLoadResult {
  readonly provider: ObservabilityProvider;
  readonly rows: readonly CoverageRow[];
  readonly source: ProviderSummary["source"];
  readonly error: string;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "failed";
}

async function loadProviderCoverage(client: EshuApiClient, provider: ObservabilityProvider): Promise<ProviderLoadResult> {
  const rows: CoverageRow[] = [];
  let afterCorrelationID = "";

  try {
    for (;;) {
      const cursor = afterCorrelationID === "" ? "" : `&after_correlation_id=${encodeURIComponent(afterCorrelationID)}`;
      const body = await client.getJson<CoverageResponse>(
        `/api/v0/observability/coverage/correlations?provider=${provider}&limit=200${cursor}`
      );
      const recs = body.correlations ?? body.results ?? [];
      rows.push(...recs.map((rec) => mapRow(rec, provider)));
      if (body.truncated !== true) break;
      const next = str(body.next_cursor?.after_correlation_id);
      if (next === "" || next === afterCorrelationID) {
        throw new Error(`provider ${provider} returned a truncated page without a usable next cursor`);
      }
      afterCorrelationID = next;
    }
  } catch (error) {
    return { provider, rows: [], source: "unavailable", error: errorMessage(error) };
  }

  return { provider, rows, source: rows.length > 0 ? "live" : "empty", error: "" };
}

// loadObservabilityCoverage fans out one coverage request per provider and
// merges the results. Failed providers stay visible as unavailable; the snapshot
// is "unavailable" when no provider returns rows and at least one provider
// fails, "empty" when all providers succeed with no rows, and "live" when any
// rows are returned.
export async function loadObservabilityCoverage(client: EshuApiClient): Promise<ObservabilitySnapshot> {
  const providerResults = await Promise.all(OBSERVABILITY_PROVIDERS.map((provider) => loadProviderCoverage(client, provider)));

  const byId = new Map<string, CoverageRow>();
  for (const result of providerResults) {
    for (const row of result.rows) {
      if (!byId.has(row.id)) byId.set(row.id, row);
    }
  }
  const rows = [...byId.values()];

  const providers: ProviderSummary[] = OBSERVABILITY_PROVIDERS.map((provider) => {
    const owned = rows.filter((r) => r.provider === provider);
    const result = providerResults.find((p) => p.provider === provider);
    return {
      provider,
      total: owned.length,
      covered: owned.filter((r) => r.covered).length,
      gaps: owned.filter((r) => !r.covered).length,
      source: result?.source ?? "unavailable",
      error: result?.error ?? "failed"
    };
  });

  const signalCounts = new Map<string, number>();
  for (const r of rows) signalCounts.set(r.signal, (signalCounts.get(r.signal) ?? 0) + 1);
  const signals = [...signalCounts.entries()]
    .map(([signal, count]) => ({ signal, count }))
    .sort((a, b) => b.count - a.count);

  const anyUnavailable = providerResults.some((p) => p.source === "unavailable");
  const source: ObservabilitySnapshot["source"] = rows.length > 0 ? "live" : anyUnavailable ? "unavailable" : "empty";
  return { rows, providers, signals, source };
}
