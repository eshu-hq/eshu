// pages/ObservabilityPage.tsx
// Observability coverage browser for the four observability collectors
// (grafana, prometheus/mimir, loki, tempo). Loads reducer-owned coverage
// correlations live from GET /api/v0/observability/coverage/correlations (one
// anchored request per provider) and renders covered-vs-gap rollups plus a
// coverage table. No fabricated rows - honest empty/error states.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import { loadObservabilityCoverage, OBSERVABILITY_PROVIDERS } from "../api/eshuObservability";
import type { CoverageRow, ObservabilitySnapshot } from "../api/eshuObservability";
import { Panel, StatTile, Badge } from "../components/atoms";

const PROVIDER_ANCHORS = OBSERVABILITY_PROVIDERS.join(", ");

export function ObservabilityPage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const [snap, setSnap] = useState<ObservabilitySnapshot | null>(null);
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) { setSnap({ rows: [], providers: [], signals: [], source: "unavailable" }); return; }
    setErr("");
    setSnap(null);
    void loadObservabilityCoverage(client)
      .then((s) => { if (!cancelled) setSnap(s); })
      .catch((e) => { if (!cancelled) { setSnap({ rows: [], providers: [], signals: [], source: "unavailable" }); setErr(e instanceof Error ? e.message : "failed"); } });
    return () => { cancelled = true; };
  }, [client]);

  const rows = (snap?.rows ?? []).filter((r) =>
    q === "" || `${r.provider} ${r.signal} ${r.object} ${r.target} ${r.resourceClass}`.toLowerCase().includes(q.toLowerCase())
  );
  const total = snap?.rows.length ?? 0;
  const covered = (snap?.rows ?? []).filter((r) => r.covered).length;
  const gaps = total - covered;
  const coverageSource = snap === null ? "loading" : snap.source;
  const providers = snap?.providers ?? [];
  const allProvidersUnavailable = providers.length > 0
    ? providers.every((p) => p.source === "unavailable")
    : snap?.source === "unavailable";
  const someProvidersUnavailable = providers.some((p) => p.source === "unavailable");
  const providerEmptyMessage = allProvidersUnavailable
    ? "Observability coverage is unavailable for every provider."
      : someProvidersUnavailable
      ? "Some observability providers are unavailable; no coverage rows were returned yet."
      : "No observability coverage from this source - requires the grafana/loki/tempo/mimir collectors.";
  const matrix = buildCoverageMatrix(rows);

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Observability</h2>
        <p>
          Live coverage correlations from{" "}
          <span className="mono">GET /api/v0/observability/coverage/correlations</span>.
          Provider anchors: {PROVIDER_ANCHORS}.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Coverage correlations" value={total} color="var(--blue)" sub={`${snap?.providers.filter((p) => p.total > 0).length ?? 0} providers reporting`} />
        <StatTile label="Covered" value={covered} color="var(--teal)" sub="current signal present" />
        <StatTile label="Gaps" value={gaps} color="var(--ember)" sub="missing or stale" />
        <StatTile label="Source" value={coverageSource} color="var(--ember)" sub="observability coverage" />
      </div>

      {snap !== null ? (
        <Panel className="mt" title="Signal sources" sub={`${providers.length} observability collectors feeding the graph`}>
          <div className="signal-source-grid">
            {providers.map((p) => (
              <div className="signal-source" key={p.provider}>
                <span className="cglyph" style={{ width: 28, height: 28 }}>{p.provider.slice(0, 1).toUpperCase()}</span>
                <span className="cell-stack" style={{ minWidth: 0 }}>
                  <span style={{ fontWeight: 600, fontSize: ".84rem" }}>{p.provider}</span>
                  <small className="mono">{p.total} correlations</small>
                </span>
                <span className={`status-pill ${p.source === "unavailable" ? "bad" : ""}`}>
                  <i />{p.source}
                </span>
              </div>
            ))}
          </div>
        </Panel>
      ) : null}

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2.2fr)", gap: "var(--gap)" }}>
        <Panel title="By provider" sub="covered / gaps">
          <table className="tbl">
            <thead><tr><th>Provider</th><th>Total</th><th>Covered</th><th>Gaps</th><th>State</th></tr></thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.provider}>
                  <td className="t-name">{p.provider}</td>
                  <td className="mono">{p.total}</td>
                  <td><Badge tone="teal">{p.covered}</Badge></td>
                  <td>{p.gaps > 0 ? <Badge tone="crit">{p.gaps}</Badge> : <span className="t-mut">0</span>}</td>
                  <td>{p.source === "unavailable" ? <Badge tone="crit" dot>{p.source}</Badge> : <span className="t-mut">{p.source}</span>}</td>
                </tr>
              ))}
              {snap !== null && snap.providers.every((p) => p.total === 0) ? (
                <tr><td colSpan={5} className="empty">{err ? `Failed to load: ${err}` : providerEmptyMessage}</td></tr>
              ) : null}
            </tbody>
          </table>
          {snap && snap.signals.length > 0 ? (
            <>
              <div className="section-label" style={{ marginTop: 14 }}>By signal</div>
              <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                {snap.signals.map((s) => <Badge key={s.signal} tone="neutral">{s.signal} · {s.count}</Badge>)}
              </div>
            </>
          ) : null}
        </Panel>

        <Panel className="flush" title="Coverage matrix" sub="Target × signal — covered · partial/stale · gap">
          {snap === null ? (
            <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading observability coverage...</p></div>
          ) : (
            <div className="table-scroll">
              <table className="tbl wide">
                <thead>
                  <tr>
                    <th>Target</th>
                    {matrix.signals.map((signal) => <th key={signal}>{signal}</th>)}
                  </tr>
                </thead>
                <tbody>
                  {matrix.rows.map((row) => (
                    <tr key={row.target}>
                      <td className="t-name">{row.target}</td>
                      {matrix.signals.map((signal) => {
                        const cell = row.signals.get(signal);
                        return (
                          <td key={signal}>
                            {cell ? <CoverageBadge row={cell} /> : <Badge tone="neutral">gap</Badge>}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                  {matrix.rows.length === 0 ? (
                    <tr>
                      <td colSpan={Math.max(1, matrix.signals.length + 1)} className="empty">
                        No coverage matrix rows from this source.
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </div>

      <div className="grid mt" style={{ gridTemplateColumns: "1fr", gap: "var(--gap)" }}>
        <Panel className="flush" title="Coverage correlations" sub={coverageSource}
          action={<div className="searchbox" style={{ minWidth: 200, height: 34 }}><input placeholder="Filter coverage…" value={q} onChange={(e) => setQ(e.target.value)} /></div>}>
          {snap === null ? (
            <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading observability coverage...</p></div>
          ) : (
            <table className="tbl">
              <thead><tr><th>Provider</th><th>Signal</th><th>Target</th><th>Object</th><th>Resource</th><th>Source</th><th>Freshness</th><th>Status</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className="t-name">{r.provider}</td>
                    <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{r.signal}</td>
                    <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.target || "—"}</td>
                    <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.object || "—"}</td>
                    <td className="t-mut" style={{ fontSize: ".76rem" }}>{r.resourceClass || "—"}</td>
                    <td className="t-mut" style={{ fontSize: ".76rem" }}>{r.sourceKind || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{r.freshness || "—"}</td>
                    <td>{r.covered ? <Badge tone="teal">{r.status}</Badge> : <Badge tone="crit">{r.status}</Badge>}</td>
                  </tr>
                ))}
                {rows.length === 0 ? <tr><td colSpan={8} className="empty">{err ? `Failed to load: ${err}` : "No coverage correlations from this source."}</td></tr> : null}
              </tbody>
            </table>
          )}
        </Panel>
      </div>
    </div>
  );
}

function CoverageBadge({ row }: { readonly row: CoverageRow }): React.JSX.Element {
  if (row.covered) return <Badge tone="teal">{row.status}</Badge>;
  if (row.status.toLowerCase() === "stale" || row.freshness.toLowerCase() === "stale") return <Badge tone="neutral">{row.status}</Badge>;
  return <Badge tone="crit">{row.status}</Badge>;
}

interface MatrixRow {
  readonly target: string;
  readonly signals: ReadonlyMap<string, CoverageRow>;
}

function buildCoverageMatrix(rows: readonly CoverageRow[]): { readonly signals: readonly string[]; readonly rows: readonly MatrixRow[] } {
  const signals = [...new Set(rows.map((row) => row.signal).filter(Boolean))].sort((a, b) => a.localeCompare(b));
  const byTarget = new Map<string, Map<string, CoverageRow>>();
  for (const row of rows) {
    const target = row.target || row.object || row.resourceClass || row.provider;
    const current = byTarget.get(target) ?? new Map<string, CoverageRow>();
    current.set(row.signal, row);
    byTarget.set(target, current);
  }
  return {
    signals,
    rows: [...byTarget.entries()]
      .map(([target, targetSignals]) => ({ target, signals: targetSignals }))
      .sort((a, b) => a.target.localeCompare(b.target))
  };
}
