import { useEffect, useState } from "react";
import { EshuApiClient } from "../api/client";
import { loadDashboardMetrics } from "../api/liveData";
import type { DashboardMetric } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";

export function DashboardPage(): React.JSX.Element {
  const [metrics, setMetrics] = useState<readonly DashboardMetric[]>([]);
  const [selectedMetric, setSelectedMetric] = useState<DashboardMetric | undefined>();
  const [loadError, setLoadError] = useState<string>("");
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
    const environment = loadConsoleEnvironment();
    const client =
      environment.mode === "private"
        ? new EshuApiClient({ baseUrl: environment.apiBaseUrl })
        : undefined;
    void loadDashboardMetrics({ client, mode: environment.mode })
      .then((loadedMetrics) => {
        setMetrics(loadedMetrics);
        setSelectedMetric(loadedMetrics[0]);
        setLoadError("");
        setLoadState("ready");
      })
      .catch((error: unknown) => {
        setMetrics([]);
        setLoadError(errorMessage(error));
        setLoadState("unavailable");
      });
  }, []);

  return (
    <section className="page-shell">
      <h1>Dashboard</h1>
      <p>Read-only runtime, indexing, collector, and freshness status.</p>
      {loadState === "loading" ? <p className="inline-state">Loading live data.</p> : null}
      {loadState === "unavailable" ? (
        <p className="inline-state">
          Local Eshu API unavailable{loadError.length > 0 ? `: ${loadError}` : ""}.
        </p>
      ) : null}
      <div className="metric-grid">
        {metrics.map((metric) => (
          <button
            aria-label={`${metric.label} ${metric.value}`}
            className="metric-card"
            key={metric.label}
            onClick={() => setSelectedMetric(metric)}
            type="button"
          >
            <h2>{metric.label}</h2>
            <strong>{metric.value}</strong>
          </button>
        ))}
      </div>
      {selectedMetric !== undefined ? (
        <section className="runtime-detail" aria-label="Runtime timeline">
          <h2>Runtime timeline</h2>
          <p>{metricNarrative(selectedMetric)}</p>
          <div className="runtime-bar">
            <span />
            <span />
            <span />
          </div>
        </section>
      ) : null}
    </section>
  );
}

function metricNarrative(metric: DashboardMetric): string {
  if (metric.label === "Queue outstanding") {
    return `${metric.value} outstanding. Queue is drained.`;
  }
  if (metric.label === "Repositories") {
    return `${metric.value} repositories are indexed and ready for workspace drilldown.`;
  }
  if (metric.label === "Index status") {
    return `Index status is ${metric.value}.`;
  }
  return `${metric.label}: ${metric.value}.`;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message.trim().length > 0) {
    return error.message.trim();
  }
  return "";
}
