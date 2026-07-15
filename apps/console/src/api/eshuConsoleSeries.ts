// Trend-metric loading for the console snapshot. Keeping the concurrent metric
// bundle in its own module preserves the per-section loader's bounded size.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { SeriesBundle } from "./eshuConsoleLive";

interface MetricsTimeSeriesResponse {
  readonly points?: readonly { readonly t?: string; readonly v?: number }[];
}

// emptySeries is the all-empty trend baseline the series bundle starts from so
// any unavailable metric reports an empty series rather than fabricated points.
export const emptySeries: SeriesBundle = {
  ingestRate: [],
  queueDepth: [],
  deadLetters: [],
  graphNodes: [],
  graphEdges: [],
  queryP50: [],
  queryP95: [],
  queryP99: [],
  newVulns: [],
  metricsConfigured: true,
};

// MetricSeriesResult carries the point values and whether the metrics source
// is configured. The configured flag is derived from the truth envelope: a
// freshness state of "unavailable" means no Prometheus/Mimir collector is
// wired up, while "building" means the source exists but has no history yet.
interface MetricSeriesResult {
  readonly points: readonly number[];
  readonly configured: boolean;
}

// loadSeriesBundle fetches every dashboard trend metric concurrently and folds
// them into the series bundle. section() (passed in) records per-metric
// provenance; failures degrade to an empty series. metricsConfigured is
// derived from the queue_depth probe: if its truth freshness is "unavailable"
// then no Prometheus/Mimir source is wired up and all chart placeholders
// switch to an explicit "not configured" message.
export async function loadSeriesBundle(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>,
): Promise<SeriesBundle> {
  const [
    ingestRate,
    queueDepth,
    deadLetters,
    graphNodes,
    graphEdges,
    queryP50,
    queryP95,
    queryP99,
  ] = await Promise.all([
    loadMetricSeries(client, section, "ingestRate", "ingest_rate"),
    loadMetricSeries(client, section, "queueDepth", "queue_depth"),
    loadMetricSeries(client, section, "deadLetters", "dead_letters"),
    loadMetricSeries(client, section, "graphNodes", "graph_nodes"),
    loadMetricSeries(client, section, "graphEdges", "graph_edges"),
    loadMetricSeries(client, section, "queryP50", "query_p50"),
    loadMetricSeries(client, section, "queryP95", "query_p95"),
    loadMetricSeries(client, section, "queryP99", "query_p99"),
  ]);
  // Use the queue_depth probe to derive metricsConfigured. Any metric that
  // returns configured=false means the source is missing; all charts should
  // show "not configured" rather than "no history yet".
  const metricsConfigured = [
    ingestRate,
    queueDepth,
    deadLetters,
    graphNodes,
    graphEdges,
    queryP50,
    queryP95,
    queryP99,
  ].every((r) => r.configured);
  return {
    ...emptySeries,
    ingestRate: ingestRate.points,
    queueDepth: queueDepth.points,
    deadLetters: deadLetters.points,
    graphNodes: graphNodes.points,
    graphEdges: graphEdges.points,
    queryP50: queryP50.points,
    queryP95: queryP95.points,
    queryP99: queryP99.points,
    metricsConfigured,
  };
}

async function loadMetricSeries(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>,
  key: keyof Omit<SeriesBundle, "newVulns" | "metricsConfigured">,
  metric: string,
): Promise<MetricSeriesResult> {
  let configured = true;
  const values = await section(`series.${key}`, async () => {
    const env = await client.get<MetricsTimeSeriesResponse>(
      `/api/v0/metrics/timeseries?metric=${metric}&window=24h&step=30m`,
    );
    if (env.error) throw new EshuEnvelopeError(env.error);
    // A freshness state of "unavailable" means the Prometheus/Mimir source is
    // not configured; "building" means the source exists but has no samples yet.
    if (env.truth?.freshness.state === "unavailable") {
      configured = false;
    }
    const points = (env.data?.points ?? []).map((point) => point.v).filter(isFiniteNumber);
    return points.length > 0 ? points : null;
  });
  return { points: values ?? [], configured };
}

function isFiniteNumber(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}
