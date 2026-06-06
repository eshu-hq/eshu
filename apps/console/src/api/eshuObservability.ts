// api/eshuObservability.ts
// Observability coverage loader. Reads reducer-owned coverage correlations from
// GET /api/v0/observability/coverage/correlations for the four observability
// collectors (grafana, prometheus/mimir, loki, tempo). That endpoint requires an
// anchor (provider/scope_id/coverage_signal/...), so we fan out one request per
// provider and merge. The response is RAW JSON (no eshu envelope), so we read it
// with getJson. Live-API data only — no fabricated rows.

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
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function mapRow(rec: CorrelationRecord, fallbackProvider: string): CoverageRow {
  const status = str(rec.coverage_status);
  return {
    id: str(rec.correlation_id) || `${str(rec.provider) || fallbackProvider}:${str(rec.coverage_signal)}:${str(rec.observability_object_ref)}`,
    provider: str(rec.provider) || fallbackProvider,
    signal: str(rec.coverage_signal) || "unknown",
    object: str(rec.observability_object_ref),
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

// loadObservabilityCoverage fans out one coverage request per provider and
// merges the results. A provider whose request fails is skipped; the snapshot is
// "unavailable" only when every provider request fails, "empty" when all succeed
// with no rows, and "live" when any rows are returned.
export async function loadObservabilityCoverage(client: EshuApiClient): Promise<ObservabilitySnapshot> {
  const settled = await Promise.allSettled(
    OBSERVABILITY_PROVIDERS.map(async (provider) => {
      // The endpoint caps limit at 200 (400s above that); 200 is the max page.
      const body = await client.getJson<CoverageResponse>(
        `/api/v0/observability/coverage/correlations?provider=${provider}&limit=200`
      );
      const recs = body.correlations ?? body.results ?? [];
      return recs.map((rec) => mapRow(rec, provider));
    })
  );

  const byId = new Map<string, CoverageRow>();
  let anyOk = false;
  for (const result of settled) {
    if (result.status !== "fulfilled") continue;
    anyOk = true;
    for (const row of result.value) {
      if (!byId.has(row.id)) byId.set(row.id, row);
    }
  }
  const rows = [...byId.values()];

  const providers: ProviderSummary[] = OBSERVABILITY_PROVIDERS.map((provider) => {
    const owned = rows.filter((r) => r.provider === provider);
    return {
      provider,
      total: owned.length,
      covered: owned.filter((r) => r.covered).length,
      gaps: owned.filter((r) => !r.covered).length
    };
  });

  const signalCounts = new Map<string, number>();
  for (const r of rows) signalCounts.set(r.signal, (signalCounts.get(r.signal) ?? 0) + 1);
  const signals = [...signalCounts.entries()]
    .map(([signal, count]) => ({ signal, count }))
    .sort((a, b) => b.count - a.count);

  const source: ObservabilitySnapshot["source"] = rows.length > 0 ? "live" : anyOk ? "empty" : "unavailable";
  return { rows, providers, signals, source };
}
