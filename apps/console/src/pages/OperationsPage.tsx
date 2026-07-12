// pages/OperationsPage.tsx
import { lazy, Suspense } from "react";

import type { EshuApiClient } from "../api/client";
import { Panel, StatTile, FreshDot, CollectorGlyph } from "../components/atoms";
import { AreaChart, BarRows } from "../components/charts";
import type { ConsoleModel } from "../console/types";
import { fmt } from "../console/types";

// OperationsLiveBoard (the health banner, stage tiles, collector heartbeat
// table, and "now processing" live_activity table backed by GET
// /api/v0/status/operations, issue #5137) is code-split via React.lazy so its
// code and CSS ship in a separate chunk rather than growing this eagerly
// loaded page past the console's main-bundle budget (mirrors WorkspacePage /
// SemanticSearchPage / GuidedQuestionsPage in appRoutes.tsx).
const OperationsLiveBoard = lazy(() =>
  import("./operations/OperationsLiveBoard").then((module) => ({
    default: module.OperationsLiveBoard,
  })),
);

export function OperationsPage({
  model,
  client,
  pollMs,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly pollMs?: number;
}): React.JSX.Element {
  const r = model.runtime;
  const langRows = model.languages
    .slice()
    .sort((a, b) => b.count - a.count)
    .map((l) => ({ label: l.language, value: l.count }));
  const queryLatencySummary = latencySummary(
    model.series.queryP50,
    model.series.queryP95,
    model.series.queryP99,
  );
  const graphGrowthSummary = graphSummary(model.series.graphNodes, model.series.graphEdges);
  const graphGrowthRows = graphRows(model.series.graphNodes, model.series.graphEdges);
  const graphGrowthTrend = model.series.graphEdges.length
    ? model.series.graphEdges
    : model.series.graphNodes;
  const graphGrowthTrendColor = model.series.graphEdges.length ? "var(--ember)" : "var(--teal)";
  const metricsConfigured = model.series.metricsConfigured;
  const argoCDIndexed = model.argoCDApps.filter((a) => a.sourceIndexed).length;
  return (
    <div className="page">
      <div className="page-intro">
        <h2>Operations</h2>
        <p>
          Eshu runtime &amp; NornicDB backend health. Source:{" "}
          <strong style={{ color: model.source === "live" ? "var(--teal)" : "var(--bone)" }}>
            {model.source === "live" ? "live API" : "demo"}
          </strong>
          .
        </p>
      </div>
      <Suspense fallback={<p className="empty mt">Loading live operations board…</p>}>
        <OperationsLiveBoard client={client} pollMs={pollMs} />
      </Suspense>
      <div className="grid g-4">
        <StatTile
          label="Index status"
          value={r.indexStatus}
          color="var(--teal)"
          sub={`profile ${r.profile}`}
        />
        <StatTile
          label="Queue outstanding"
          value={r.queueOutstanding}
          spark={model.series.queueDepth.length ? model.series.queueDepth : undefined}
          color="var(--violet)"
          sub={`${r.inFlight} in-flight`}
        />
        <StatTile
          label="Dead letters"
          value={r.deadLetters}
          spark={model.series.deadLetters.length ? model.series.deadLetters : undefined}
          color="var(--crit)"
          sub="needs replay"
        />
        <StatTile
          label="Succeeded"
          value={fmt(r.succeeded)}
          color="var(--blue)"
          sub="work items (run)"
        />
      </div>
      <div className="grid g-2 mt">
        <Panel title="Reducer queue depth" sub="Outstanding work items">
          {model.series.queueDepth.length ? (
            <AreaChart data={model.series.queueDepth} color="var(--violet)" h={180} unit=" items" />
          ) : metricsConfigured ? (
            <p className="empty" style={{ padding: "32px 12px" }}>
              Current depth above. Trend history appears when the metrics source has recent samples.
            </p>
          ) : (
            <p className="empty" style={{ padding: "32px 12px" }}>
              Metrics source not configured. Connect a Prometheus/Mimir collector to enable trend
              charts.
            </p>
          )}
        </Panel>
        <Panel title="Query latency" sub={queryLatencySummary ?? "GET /api/v0/metrics/timeseries"}>
          {model.series.queryP99.length ? (
            <AreaChart data={model.series.queryP99} color="var(--crit)" h={180} unit="ms" />
          ) : metricsConfigured ? (
            <p className="empty" style={{ padding: "32px 12px" }}>
              Query latency history appears when the metrics source has recent samples.
            </p>
          ) : (
            <p className="empty" style={{ padding: "32px 12px" }}>
              Metrics source not configured. Connect a Prometheus/Mimir collector to enable trend
              charts.
            </p>
          )}
        </Panel>
      </div>
      <Panel
        className="mt"
        title="Graph growth"
        sub={graphGrowthSummary ?? "GET /api/v0/metrics/timeseries"}
      >
        {graphGrowthTrend.length ? (
          <div className="grid g-2" style={{ alignItems: "center" }}>
            <AreaChart data={graphGrowthTrend} color={graphGrowthTrendColor} h={180} />
            <BarRows rows={graphGrowthRows} />
          </div>
        ) : metricsConfigured ? (
          <p className="empty" style={{ padding: "32px 12px" }}>
            Graph growth history appears when the metrics source has recent samples.
          </p>
        ) : (
          <p className="empty" style={{ padding: "32px 12px" }}>
            Metrics source not configured. Connect a Prometheus/Mimir collector to enable trend
            charts.
          </p>
        )}
      </Panel>
      {model.source === "live" ? (
        <Panel className="mt" title="Metric contract pending" sub="Tracked in issue #2216">
          <p className="empty" style={{ padding: "12px 0", textAlign: "left" }}>
            write-throughput, cache-hit, and vulnerability-feed intake decorations do not have named
            live metric series yet. Connected-live mode keeps those demo-only sparklines out of
            Operations until issue #2216 defines source-backed contracts.
          </p>
        </Panel>
      ) : null}
      <Panel
        className="mt"
        title="Repositories by language"
        sub="GET /api/v0/repositories/language-inventory"
      >
        {langRows.length ? (
          <BarRows rows={langRows} />
        ) : (
          <p className="empty">No language inventory from this source.</p>
        )}
      </Panel>
      {model.argoCDApps.length > 0 ? (
        <Panel
          className="mt"
          title="ArgoCD deployed workloads"
          sub={`${model.argoCDApps.length} apps · ${argoCDIndexed} source-indexed · POST /api/v0/infra/resources/search`}
        >
          <div className="argocd-grid">
            {model.argoCDApps.map((app) => (
              <div key={app.id} className={`argocd-app${app.sourceIndexed ? " indexed" : ""}`}>
                <span className="argocd-name" title={app.name}>
                  {app.name}
                </span>
                {app.sourceIndexed ? (
                  <span
                    className="argocd-tag"
                    style={{
                      color: "var(--teal)",
                      background: "color-mix(in oklab, var(--teal) 12%, transparent)",
                    }}
                  >
                    indexed
                  </span>
                ) : (
                  <span className="argocd-tag">not indexed</span>
                )}
              </div>
            ))}
          </div>
        </Panel>
      ) : model.source === "live" && model.provenance["argoCDApps"] === "live" ? (
        <Panel
          className="mt"
          title="ArgoCD deployed workloads"
          sub="POST /api/v0/infra/resources/search"
        >
          <p className="empty" style={{ padding: "32px 12px" }}>
            No ArgoCD Application or ApplicationSet nodes found in the graph. Index a GitOps
            repository to populate this view.
          </p>
        </Panel>
      ) : null}
      <Panel
        className="flush mt"
        title="Collectors / ingesters"
        sub={`${model.ingesters.length} fact sources`}
      >
        <table className="tbl">
          <thead>
            <tr>
              <th>Collector</th>
              <th>Instance</th>
              <th>State</th>
              <th>Facts</th>
              <th>Freshness</th>
            </tr>
          </thead>
          <tbody>
            {model.ingesters.map((c) => (
              <tr key={c.id}>
                <td>
                  <span className="row" style={{ gap: 10 }}>
                    <CollectorGlyph kind={c.kind} />
                    <span style={{ fontWeight: 600 }}>{c.kind}</span>
                  </span>
                </td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>
                  {c.id}
                </td>
                <td>
                  <span
                    className="status-pill"
                    style={{
                      color:
                        c.state === "healthy" || c.state === "active"
                          ? "var(--teal)"
                          : c.state === "degraded" || c.state === "disabled"
                            ? "var(--med)"
                            : c.state === "deactivated"
                              ? "var(--crit)"
                              : "var(--med)",
                    }}
                  >
                    <i style={{ background: "currentColor" }} />
                    {c.state}
                  </span>
                </td>
                <td className="mono" style={{ fontSize: ".82rem" }}>
                  {c.facts === null ? "—" : fmt(c.facts)}
                </td>
                <td>
                  <FreshDot
                    state={
                      c.freshness === "building"
                        ? "lagging"
                        : c.freshness === "stale" || c.freshness === "unavailable"
                          ? "stale"
                          : "fresh"
                    }
                  />
                </td>
              </tr>
            ))}
            {model.ingesters.length === 0 ? (
              <tr>
                <td colSpan={5} className="empty">
                  No ingester status from this source.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

function lastSeriesValue(values: readonly number[]): number | null {
  const value = values.at(-1);
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function latencySummary(
  queryP50: readonly number[],
  queryP95: readonly number[],
  queryP99: readonly number[],
): string | null {
  const p50 = lastSeriesValue(queryP50);
  const p95 = lastSeriesValue(queryP95);
  const p99 = lastSeriesValue(queryP99);
  if (p50 === null && p95 === null && p99 === null) return null;
  return `p50 ${p50 ?? "—"}ms · p95 ${p95 ?? "—"}ms · p99 ${p99 ?? "—"}ms`;
}

function graphSummary(graphNodes: readonly number[], graphEdges: readonly number[]): string | null {
  const nodes = lastSeriesValue(graphNodes);
  const edges = lastSeriesValue(graphEdges);
  if (nodes === null && edges === null) return null;
  return `${nodes === null ? "—" : fmt(nodes)} nodes · ${edges === null ? "—" : fmt(edges)} edges`;
}

function graphRows(
  graphNodes: readonly number[],
  graphEdges: readonly number[],
): readonly { readonly label: string; readonly value: number; readonly color: string }[] {
  const nodes = lastSeriesValue(graphNodes);
  const edges = lastSeriesValue(graphEdges);
  const rows: { label: string; value: number; color: string }[] = [];
  if (nodes !== null) rows.push({ label: "nodes", value: nodes, color: "var(--teal)" });
  if (edges !== null) rows.push({ label: "edges", value: edges, color: "var(--ember)" });
  return rows;
}
