import { useEffect, useState } from "react";
import { EshuApiClient } from "../api/client";
import { loadDashboardSnapshot } from "../api/dashboardSnapshot";
import type { DashboardMetric, DashboardSnapshot, EvidenceRow } from "../api/mockData";
import { loadServiceSpotlight } from "../api/serviceSpotlight";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import { loadConsoleEnvironment } from "../config/environment";
import { DeploymentGraphView } from "../visualization/DeploymentGraphView";
import { ServiceSpotlightPanel } from "./ServiceSpotlightPanel";

export function DashboardPage(): React.JSX.Element {
  const [snapshot, setSnapshot] = useState<DashboardSnapshot | undefined>();
  const [metrics, setMetrics] = useState<readonly DashboardMetric[]>([]);
  const [selectedEvidence, setSelectedEvidence] = useState<EvidenceRow | undefined>();
  const [selectedMetric, setSelectedMetric] = useState<DashboardMetric | undefined>();
  const [serviceSpotlight, setServiceSpotlight] = useState<ServiceSpotlight | undefined>();
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
        setSelectedEvidence(loadedSnapshot.evidence[0]);
        setSelectedMetric(loadedSnapshot.metrics[0]);
        setServiceSpotlight(loadedSnapshot.serviceSpotlight);
        setLoadError("");
        setLoadState("ready");
        if (client !== undefined && loadedSnapshot.repositories !== undefined) {
          void loadServiceSpotlight(client, loadedSnapshot.repositories)
            .then(setServiceSpotlight)
            .catch(() => setServiceSpotlight(undefined));
        }
      })
      .catch((error: unknown) => {
        setSnapshot(undefined);
        setMetrics([]);
        setSelectedEvidence(undefined);
        setServiceSpotlight(undefined);
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
      {metrics.length > 0 ? <RunReadiness metrics={metrics} /> : null}
      {serviceSpotlight !== undefined ? (
        <ServiceSpotlightPanel spotlight={serviceSpotlight} />
      ) : null}
      {snapshot !== undefined ? (
        <section className="dashboard-atlas" aria-label="Deployment relationship atlas">
          <div className="dashboard-atlas-copy">
            <h2>Deployment relationship graph</h2>
            <p>{snapshot.story}</p>
            <p className="atlas-note">
              Read this from left to right: source evidence, controller evidence, workload,
              runtime placement, then supporting artifacts. Pick a node or edge to inspect the
              exact relationship.
            </p>
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
      {snapshot !== undefined ? (
        <RelationshipCoverage relationships={snapshot.relationships} />
      ) : null}
      {snapshot !== undefined && snapshot.evidence.length > 0 ? (
        <section className="relationship-evidence" aria-label="Relationship evidence">
          <h2>Evidence trail</h2>
          <div className="relationship-evidence-grid">
            <div className="relationship-evidence-list">
              {snapshot.evidence.map((row) => (
                <EvidenceTrailRow
                  isSelected={evidenceKey(row) === evidenceKey(selectedEvidence)}
                  key={evidenceKey(row)}
                  onSelect={setSelectedEvidence}
                  row={row}
                />
              ))}
            </div>
            <EvidenceDossier row={selectedEvidence ?? snapshot.evidence[0]} />
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

function RunReadiness({
  metrics
}: {
  readonly metrics: readonly DashboardMetric[];
}): React.JSX.Element {
  const readinessMetrics = [
    "Index status",
    "Graph repositories",
    "Catalog repositories",
    "Queue outstanding",
    "Dead letters"
  ].flatMap((label) => {
    const metric = metricByLabel(metrics, label);
    return metric === undefined ? [] : [metric];
  });

  return (
    <section aria-label="Run readiness" className="run-readiness">
      <div>
        <h2>Run readiness</h2>
        <p>{readinessSummary(metrics)}</p>
      </div>
      <dl>
        {readinessMetrics.map((metric) => (
          <div key={metric.label}>
            <dt>{metric.label}</dt>
            <dd>{metric.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function RelationshipCoverage({
  relationships
}: {
  readonly relationships: readonly DashboardSnapshot["relationships"][number][];
}): React.JSX.Element {
  const canonicalRelationships = relationships.filter(
    (relationship) => relationship.layer !== "topology"
  );
  const topologyRelationships = relationships.filter(
    (relationship) => relationship.layer === "topology"
  );
  return (
    <section aria-label="Relationship coverage" className="relationship-coverage">
      <div className="section-heading-row">
        <div>
          <h2>Relationship coverage</h2>
          <p>Known Eshu verbs, separated into canonical deployment truth and topology hints.</p>
        </div>
        <span>{relationships.filter((relationship) => relationship.count > 0).length} observed</span>
      </div>
      <CoverageGroup relationships={canonicalRelationships} title="Canonical verbs" />
      <CoverageGroup relationships={topologyRelationships} title="Runtime topology" />
    </section>
  );
}

function CoverageGroup({
  relationships,
  title
}: {
  readonly relationships: readonly DashboardSnapshot["relationships"][number][];
  readonly title: string;
}): React.JSX.Element {
  return (
    <div className="coverage-group">
      <h3>{title}</h3>
      <div className="coverage-table" role="table">
        {relationships.map((relationship) => (
          <div className="coverage-row" key={relationship.verb} role="row">
            <strong role="cell">{relationship.verb}</strong>
            <span role="cell">{relationship.detail}</span>
            <b role="cell">{relationship.count}</b>
            <em role="cell">{relationship.count > 0 ? "Observed" : "Missing"}</em>
          </div>
        ))}
      </div>
    </div>
  );
}

function EvidenceTrailRow({
  isSelected,
  onSelect,
  row
}: {
  readonly isSelected: boolean;
  readonly onSelect: (row: EvidenceRow) => void;
  readonly row: EvidenceRow;
}): React.JSX.Element {
  return (
    <article
      className={
        isSelected ? "relationship-evidence-row relationship-evidence-row-selected" : "relationship-evidence-row"
      }
    >
      <button
        aria-pressed={isSelected}
        className="evidence-select"
        onClick={() => onSelect(row)}
        type="button"
      >
        <strong>{row.title ?? row.basis}</strong>
        <p>{row.summary}</p>
      </button>
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

function EvidenceDossier({ row }: { readonly row: EvidenceRow | undefined }): React.JSX.Element {
  if (row === undefined) {
    return (
      <aside aria-label="Evidence dossier" className="evidence-dossier">
        <h2>Evidence dossier</h2>
        <p>No relationship evidence has been selected yet.</p>
      </aside>
    );
  }
  return (
    <aside aria-label="Evidence dossier" className="evidence-dossier">
      <h2>Evidence dossier</h2>
      <p>{row.summary}</p>
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
          <dt>Category</dt>
          <dd>{row.category ?? "relationship evidence"}</dd>
        </div>
        <div>
          <dt>Path</dt>
          <dd>{row.detailPath ?? "evidence"}</dd>
        </div>
      </dl>
    </aside>
  );
}

function evidenceKey(row: EvidenceRow | undefined): string {
  if (row === undefined) {
    return "";
  }
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

function readinessSummary(metrics: readonly DashboardMetric[]): string {
  const status = metricByLabel(metrics, "Index status")?.value ?? "unknown";
  const outstanding = metricByLabel(metrics, "Queue outstanding")?.value ?? "0";
  const deadLetters = metricByLabel(metrics, "Dead letters")?.value ?? "0";
  return `Current run is ${status}; queue has ${outstanding} outstanding item(s) and ${deadLetters} dead letter(s).`;
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
