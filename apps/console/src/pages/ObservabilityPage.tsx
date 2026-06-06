// pages/ObservabilityPage.tsx
// Observability coverage browser for the four observability collectors
// (grafana, prometheus/mimir, loki, tempo). Loads reducer-owned coverage
// correlations live from GET /api/v0/observability/coverage/correlations (one
// anchored request per provider) and renders covered-vs-gap rollups plus a
// coverage table. No fabricated rows — honest empty/error states.
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadObservabilityCoverage } from "../api/eshuObservability";
import type { ObservabilitySnapshot } from "../api/eshuObservability";
import { Panel, StatTile, Badge } from "../components/atoms";

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
    q === "" || `${r.provider} ${r.signal} ${r.object} ${r.resourceClass}`.toLowerCase().includes(q.toLowerCase())
  );
  const total = snap?.rows.length ?? 0;
  const covered = (snap?.rows ?? []).filter((r) => r.covered).length;
  const gaps = total - covered;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Observability</h2>
        <p>Coverage correlations for grafana, prometheus/mimir, loki, and tempo — <span className="mono">GET /api/v0/observability/coverage/correlations</span>.</p>
      </div>

      <div className="grid g-4">
        <StatTile label="Coverage correlations" value={total} color="var(--blue)" sub={`${snap?.providers.filter((p) => p.total > 0).length ?? 0} providers reporting`} />
        <StatTile label="Covered" value={covered} color="var(--teal)" sub="current signal present" />
        <StatTile label="Gaps" value={gaps} color="var(--ember)" sub="missing or stale" />
        <StatTile label="Source" value={snap === null ? "…" : snap.source} color="var(--ember)" sub="observability coverage" />
      </div>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2.2fr)", gap: "var(--gap)" }}>
        <Panel title="By provider" sub="covered / gaps">
          <table className="tbl">
            <thead><tr><th>Provider</th><th>Total</th><th>Covered</th><th>Gaps</th></tr></thead>
            <tbody>
              {(snap?.providers ?? []).map((p) => (
                <tr key={p.provider}>
                  <td className="t-name">{p.provider}</td>
                  <td className="mono">{p.total}</td>
                  <td><Badge tone="teal">{p.covered}</Badge></td>
                  <td>{p.gaps > 0 ? <Badge tone="crit">{p.gaps}</Badge> : <span className="t-mut">0</span>}</td>
                </tr>
              ))}
              {(snap?.providers ?? []).every((p) => p.total === 0) ? (
                <tr><td colSpan={4} className="empty">{err ? `Failed to load: ${err}` : "No observability coverage from this source — requires the grafana/loki/tempo/mimir collectors."}</td></tr>
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

        <Panel className="flush" title="Coverage correlations" sub={snap === null ? "loading…" : "live"}
          action={<div className="searchbox" style={{ minWidth: 200, height: 34 }}><input placeholder="Filter coverage…" value={q} onChange={(e) => setQ(e.target.value)} /></div>}>
          {snap === null ? (
            <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading observability coverage…</p></div>
          ) : (
            <table className="tbl">
              <thead><tr><th>Provider</th><th>Signal</th><th>Object</th><th>Resource</th><th>Source</th><th>Freshness</th><th>Status</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className="t-name">{r.provider}</td>
                    <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{r.signal}</td>
                    <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.object || "—"}</td>
                    <td className="t-mut" style={{ fontSize: ".76rem" }}>{r.resourceClass || "—"}</td>
                    <td className="t-mut" style={{ fontSize: ".76rem" }}>{r.sourceKind || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{r.freshness || "—"}</td>
                    <td>{r.covered ? <Badge tone="teal">{r.status}</Badge> : <Badge tone="crit">{r.status}</Badge>}</td>
                  </tr>
                ))}
                {rows.length === 0 ? <tr><td colSpan={7} className="empty">{err ? `Failed to load: ${err}` : "No coverage correlations from this source."}</td></tr> : null}
              </tbody>
            </table>
          )}
        </Panel>
      </div>
    </div>
  );
}
