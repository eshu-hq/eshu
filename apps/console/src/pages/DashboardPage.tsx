import { useEffect, useState } from "react";
import { EshuApiClient } from "../api/client";
import { loadDashboardSnapshot } from "../api/dashboardSnapshot";
import type { DashboardMetric, DashboardSnapshot, EvidenceRow } from "../api/mockData";
import { loadConsoleEnvironment } from "../config/environment";
import { DeploymentGraphView } from "../visualization/DeploymentGraphView";

export function DashboardPage(): React.JSX.Element {
  const [snapshot, setSnapshot] = useState<DashboardSnapshot | undefined>();
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
        ? new EshuApiClient({
          apiKey: environment.apiKey,
          baseUrl: environment.apiBaseUrl
        })
        : undefined;
    void loadDashboardSnapshot({ client, mode: environment.mode })
      .then((loadedSnapshot) => {
        setSnapshot(loadedSnapshot);
        setMetrics(loadedSnapshot.metrics);
        setSelectedMetric(loadedSnapshot.metrics[0]);
        setLoadError("");
        setLoadState("ready");
      })
      .catch((error: unknown) => {
        setSnapshot(undefined);
        setMetrics([]);
        setLoadError(errorMessage(error));
        setLoadState("unavailable");
      });
  }, []);

  return (
    <section className="page-shell">
      <div className="page-intro">
        <h1>Dashboard</h1>
        <p>Read-only runtime, indexing, collector, and freshness status.</p>
      </div>
      {loadState === "loading" ? <p className="inline-state">Loading live data.</p> : null}
      {loadState === "unavailable" ? (
        <p className="inline-state">
          Local Eshu API unavailable{loadError.length > 0 ? `: ${loadError}` : ""}.
        </p>
      ) : null}
      {snapshot !== undefined ? (
        <section className="dashboard-atlas" aria-label="Deployment relationship atlas">
          <div className="dashboard-atlas-copy">
            <h2>Deployment relationship graph</h2>
            <p>{snapshot.story}</p>
            <RelationshipSummaryGroup
              relationships={snapshot.relationships.filter(
                (relationship) => relationship.layer !== "topology"
              )}
              title="Canonical verbs"
            />
            <RelationshipSummaryGroup
              relationships={snapshot.relationships.filter(
                (relationship) => relationship.layer === "topology"
              )}
              title="Runtime topology"
            />
          </div>
          {snapshot.graph.nodes.length > 1 ? (
            <DeploymentGraphView
              ariaLabel="Workspace relationship graph"
              detailTitle="Typed evidence"
              graph={snapshot.graph}
            />
          ) : (
            <p className="inline-state">No deployment relationship graph is available yet.</p>
          )}
        </section>
      ) : null}
      {snapshot !== undefined && snapshot.evidence.length > 0 ? (
        <section className="relationship-evidence" aria-label="Relationship evidence">
          <h2>Evidence trail</h2>
          <div className="relationship-evidence-list">
            {snapshot.evidence.map((row) => (
              <EvidenceTrailRow key={evidenceKey(row)} row={row} />
            ))}
          </div>
        </section>
      ) : null}
      <div className="dashboard-layout">
        <div className="dashboard-column">
          <section className="dashboard-story" aria-label="Runtime summary">
            <h2>Runtime summary</h2>
            <p>{dashboardSummary(metrics)}</p>
            <dl className="summary-metrics">
              {summaryMetrics(metrics).map((metric) => (
                <div key={metric.label}>
                  <dt>{metric.label}</dt>
                  <dd>{metric.value}</dd>
                </div>
              ))}
            </dl>
          </section>
          <section className="runtime-ledger" aria-label="Runtime metrics">
            <h2>Runtime metrics</h2>
            <table className="data-table status-table">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Value</th>
                  <th>Use</th>
                </tr>
              </thead>
              <tbody>
                {metrics.map((metric) => (
                  <tr key={metric.label}>
                    <td>
                      <button
                        aria-label={`Inspect ${metric.label} ${metric.value}`}
                        className="row-button"
                        onClick={() => setSelectedMetric(metric)}
                        type="button"
                      >
                        {metric.label}
                      </button>
                    </td>
                    <td>{metric.value}</td>
                    <td>{metricNarrative(metric)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        </div>
        <div className="dashboard-column">
          <section className="queue-ledger" aria-label="Queue ledger">
            <h2>Queue ledger</h2>
            {queueMetrics(metrics).map((metric) => (
              <button
                aria-label={`${metric.label} ${metric.value}`}
                className="ledger-row"
                key={metric.label}
                onClick={() => setSelectedMetric(metric)}
                type="button"
              >
                <span>{metric.label}</span>
                <strong>{metric.value}</strong>
                <span>{metricNarrative(metric)}</span>
              </button>
            ))}
          </section>
          {selectedMetric !== undefined ? (
            <section className="detail-panel" aria-label="Runtime dossier">
              <h2>Runtime dossier</h2>
              <dl>
                <div>
                  <dt>Selected</dt>
                  <dd>{selectedMetric.label}</dd>
                </div>
                <div>
                  <dt>Value</dt>
                  <dd>{selectedMetric.value}</dd>
                </div>
                <div>
                  <dt>Meaning</dt>
                  <dd>{metricNarrative(selectedMetric)}</dd>
                </div>
              </dl>
            </section>
          ) : null}
        </div>
      </div>
    </section>
  );
}

function RelationshipSummaryGroup({
  relationships,
  title
}: {
  readonly relationships: readonly DashboardSnapshot["relationships"][number][];
  readonly title: string;
}): React.JSX.Element {
  if (relationships.length === 0) {
    return <></>;
  }
  return (
    <div className="relationship-summary-section">
      <h3>{title}</h3>
      <div className="relationship-summary-grid">
        {relationships.map((relationship) => (
          <article
            key={relationship.verb}
            className={
              relationship.count === 0
                ? "relationship-summary relationship-summary-empty"
                : "relationship-summary"
            }
          >
            <strong>{relationship.verb}</strong>
            <span>{relationship.detail}</span>
            <b>{relationship.count}</b>
          </article>
        ))}
      </div>
    </div>
  );
}

function EvidenceTrailRow({ row }: { readonly row: EvidenceRow }): React.JSX.Element {
  return (
    <article className="relationship-evidence-row">
      <div>
        <strong>{row.title ?? row.basis}</strong>
        <p>{row.summary}</p>
      </div>
      <dl>
        <div>
          <dt>Verb</dt>
          <dd>{row.basis}</dd>
        </div>
        <div>
          <dt>Source</dt>
          <dd>{row.source}</dd>
        </div>
        <div>
          <dt>Path</dt>
          <dd>{row.detailPath ?? "evidence"}</dd>
        </div>
      </dl>
    </article>
  );
}

function evidenceKey(row: EvidenceRow): string {
  return `${row.source}:${row.basis}:${row.detailPath ?? row.summary}`;
}

function metricByLabel(
  metrics: readonly DashboardMetric[],
  label: string
): DashboardMetric | undefined {
  return metrics.find((metric) => metric.label === label);
}

function summaryMetrics(metrics: readonly DashboardMetric[]): readonly DashboardMetric[] {
  return [
    "Index status",
    "Graph repositories",
    "Catalog repositories",
    "Succeeded work"
  ].flatMap((label) => {
    const metric = metricByLabel(metrics, label);
    return metric === undefined ? [] : [metric];
  });
}

function queueMetrics(metrics: readonly DashboardMetric[]): readonly DashboardMetric[] {
  return ["Queue outstanding", "In flight", "Pending work", "Dead letters"].flatMap(
    (label) => {
      const metric = metricByLabel(metrics, label);
      return metric === undefined ? [] : [metric];
    }
  );
}

function dashboardSummary(metrics: readonly DashboardMetric[]): string {
  const status = metricByLabel(metrics, "Index status")?.value ?? "unknown";
  const graphRepos = metricByLabel(metrics, "Graph repositories")?.value ?? "0";
  const catalogRepos = metricByLabel(metrics, "Catalog repositories")?.value ?? "0";
  const outstanding = metricByLabel(metrics, "Queue outstanding")?.value ?? "0";
  const deadLetters = metricByLabel(metrics, "Dead letters")?.value ?? "0";
  return `${graphRepos} repositories indexed by graph status, ${catalogRepos} available in catalog drilldown, ${outstanding} outstanding queue item(s), and ${deadLetters} dead letter(s). Current index state is ${status}.`;
}

function metricNarrative(metric: DashboardMetric): string {
  if (metric.detail !== undefined && metric.detail.trim().length > 0) {
    return metric.detail;
  }
  if (metric.label === "Queue outstanding") {
    return metric.value === "0"
      ? "No queued work is waiting on reducers or projectors."
      : `${metric.value} work item(s) still need reducer or projector attention.`;
  }
  if (metric.label === "Catalog repositories") {
    return `${metric.value} repositories are available through catalog drilldown.`;
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
