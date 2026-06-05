// pages/OperationsPage.tsx
import type { ConsoleModel } from "../console/types";
import { fmt } from "../console/types";
import { Panel, StatTile, FreshDot, CollectorGlyph } from "../components/atoms";
import { AreaChart, BarRows } from "../components/charts";

export function OperationsPage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const r = model.runtime;
  const langRows = model.languages.slice().sort((a, b) => b.count - a.count)
    .map((l) => ({ label: l.language, value: l.count }));
  return (
    <div className="page">
      <div className="page-intro"><h2>Operations</h2><p>Eshu runtime &amp; NornicDB backend health. Source: <strong style={{ color: model.source === "live" ? "var(--teal)" : "var(--bone)" }}>{model.source === "live" ? "live API" : "demo"}</strong>.</p></div>
      <div className="grid g-4">
        <StatTile label="Index status" value={r.indexStatus} color="var(--teal)" sub={`profile ${r.profile}`} />
        <StatTile label="Queue outstanding" value={r.queueOutstanding} spark={model.series.queueDepth.length ? model.series.queueDepth : undefined} color="var(--violet)" sub={`${r.inFlight} in-flight`} />
        <StatTile label="Dead letters" value={r.deadLetters} color="var(--crit)" sub="needs replay" />
        <StatTile label="Succeeded" value={fmt(r.succeeded)} color="var(--blue)" sub="work items (run)" />
      </div>
      <div className="grid g-2 mt">
        <Panel title="Reducer queue depth" sub="Outstanding work items">{model.series.queueDepth.length ? <AreaChart data={model.series.queueDepth} color="var(--violet)" h={180} unit=" items" /> : <p className="empty" style={{ padding: "32px 12px" }}>Current depth above. Trend history requires the metrics time-series API (#1434).</p>}</Panel>
        <Panel title="Repositories by language" sub="GET /api/v0/repositories/by-language">{langRows.length ? <BarRows rows={langRows} /> : <p className="empty">No language inventory from this source.</p>}</Panel>
      </div>
      <Panel className="flush mt" title="Collectors / ingesters" sub={`${model.ingesters.length} fact sources`}>
        <table className="tbl">
          <thead><tr><th>Collector</th><th>Instance</th><th>State</th><th>Facts</th><th>Freshness</th></tr></thead>
          <tbody>
            {model.ingesters.map((c) => (
              <tr key={c.id}>
                <td><span className="row" style={{ gap: 10 }}><CollectorGlyph kind={c.kind} /><span style={{ fontWeight: 600 }}>{c.kind}</span></span></td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{c.id}</td>
                <td><span className="status-pill" style={{ color: c.state === "healthy" ? "var(--teal)" : c.state === "degraded" ? "var(--med)" : "var(--crit)" }}><i style={{ background: "currentColor" }} />{c.state}</span></td>
                <td className="mono" style={{ fontSize: ".82rem" }}>{fmt(c.facts)}</td>
                <td><FreshDot state={c.freshness === "building" ? "lagging" : c.freshness === "stale" || c.freshness === "unavailable" ? "stale" : "fresh"} /></td>
              </tr>
            ))}
            {model.ingesters.length === 0 ? <tr><td colSpan={5} className="empty">No ingester status from this source.</td></tr> : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}
