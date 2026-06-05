// pages/FindingsPage.tsx
import type { ConsoleModel } from "../console/types";
import { Panel, StatTile, TruthChip, Badge } from "../components/atoms";

export function FindingsPage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const byType = new Map<string, number>();
  model.findings.forEach((f) => byType.set(f.type, (byType.get(f.type) ?? 0) + 1));
  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>What needs human attention — each finding carries its truth level and source.</p></div>
      <div className="grid g-4">
        <StatTile label="Open findings" value={model.findings.length} color="var(--ember)" sub={model.source === "live" ? "live from the graph" : "demo"} />
        <StatTile label="Dead code" value={byType.get("Dead code") ?? 0} color="var(--violet)" sub="graph-backed candidates" />
        <StatTile label="Vulnerability" value={byType.get("Vulnerability") ?? 0} color="var(--crit)" sub="reachable advisories" />
        <StatTile label="Types" value={byType.size} color="var(--blue)" sub="distinct categories" />
      </div>
      <Panel className="flush mt" title="All findings">
        <table className="tbl">
          <thead><tr><th>Finding</th><th>Type</th><th>Entity</th><th>Truth</th></tr></thead>
          <tbody>
            {model.findings.map((f) => (
              <tr key={f.id}>
                <td className="cell-stack" style={{ maxWidth: 460 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span><small>{f.detail}</small></td>
                <td><Badge tone="neutral">{f.type}</Badge></td>
                <td className="t-name" style={{ fontSize: ".8rem" }}>{f.entity}</td>
                <td><TruthChip level={f.truth === "fallback" ? "inferred" : f.truth === "derived" ? "derived" : "exact"} /></td>
              </tr>
            ))}
            {model.findings.length === 0 ? <tr><td colSpan={4} className="empty">No findings from this source.</td></tr> : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}
