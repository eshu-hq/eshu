// pages/FindingsPage.tsx
import { Link } from "react-router-dom";
import { uiTruth, type ConsoleModel, type FindingRow, type VulnRow } from "../console/types";
import { Panel, StatTile, TruthChip, Badge } from "../components/atoms";

export function FindingsPage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const rows = worklistRows(model);
  const byType = new Map<string, number>();
  rows.forEach((row) => byType.set(row.type, (byType.get(row.type) ?? 0) + 1));
  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>Unified worklist for dead code and reachable vulnerabilities. Each row links to the live drilldown surface that owns the evidence.</p></div>
      <div className="grid g-4">
        <StatTile label="Open findings" value={rows.length} color="var(--ember)" sub={model.source === "live" ? "live from the graph" : "demo"} />
        <StatTile label="Dead code" value={byType.get("Dead code") ?? 0} color="var(--violet)" sub="graph-backed candidates" />
        <StatTile label="Vulnerability" value={byType.get("Vulnerability") ?? 0} color="var(--crit)" sub="reachable advisories" />
        <StatTile label="Types" value={byType.size} color="var(--blue)" sub="distinct categories" />
      </div>
      <Panel className="flush mt" title="Unified worklist">
        <table className="tbl">
          <thead><tr><th>Finding</th><th>Type</th><th>Entity</th><th>Truth</th><th>Actions</th></tr></thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id}>
                <td className="cell-stack" style={{ maxWidth: 460 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{row.title}</span><small>{row.detail}</small></td>
                <td><Badge tone={row.type === "Vulnerability" ? "crit" : "neutral"}>{row.type}</Badge></td>
                <td className="t-name" style={{ fontSize: ".8rem" }}>{row.entity}</td>
                <td><TruthChip level={row.truth} /></td>
                <td>
                  <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                    {row.actions.map((action) => <Link key={action.label} className="btn-ghost" to={action.to}>{action.label}</Link>)}
                  </div>
                </td>
              </tr>
            ))}
            {rows.length === 0 ? <tr><td colSpan={5} className="empty">No findings from this source.</td></tr> : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

interface WorklistAction {
  readonly label: string;
  readonly to: string;
}

interface WorklistRow {
  readonly id: string;
  readonly type: string;
  readonly entity: string;
  readonly title: string;
  readonly detail: string;
  readonly truth: "exact" | "derived" | "inferred";
  readonly actions: readonly WorklistAction[];
}

function worklistRows(model: ConsoleModel): readonly WorklistRow[] {
  return [
    ...model.findings.map(findingRow),
    ...model.vulnerabilities.map(vulnerabilityRow)
  ];
}

function findingRow(finding: FindingRow): WorklistRow {
  const actions: WorklistAction[] = [];
  if (finding.type === "Dead code") {
    actions.push({ label: "Open graph", to: `/code-graph?candidate=${encodeURIComponent(finding.id)}` });
    if (finding.filePath) actions.push({ label: "Filter dead code", to: `/dead-code?q=${encodeURIComponent(finding.filePath)}` });
  }
  if (finding.entity) actions.push({ label: "Explore entity", to: `/explorer?q=${encodeURIComponent(finding.entity)}` });
  return {
    actions,
    detail: finding.detail,
    entity: finding.entity,
    id: finding.id,
    title: finding.title,
    truth: uiTruth(finding.truth),
    type: finding.type
  };
}

function vulnerabilityRow(vulnerability: VulnRow): WorklistRow {
  const primaryEntity = vulnerability.services[0] ?? "service not resolved";
  const detail = `${vulnerability.package} · CVSS ${vulnerability.cvss}${vulnerability.fixedVersion ? ` · fix ${vulnerability.fixedVersion}` : ""}`;
  const actions: WorklistAction[] = [
    { label: "Open CVE", to: `/vulnerabilities/${encodeURIComponent(vulnerability.id)}` }
  ];
  if (primaryEntity !== "service not resolved") {
    actions.push({ label: "Explore entity", to: `/explorer?q=${encodeURIComponent(primaryEntity)}` });
  }
  return {
    actions,
    detail,
    entity: primaryEntity,
    id: `vulnerability:${vulnerability.id}`,
    title: `${vulnerability.id} · ${vulnerability.package}`,
    truth: "derived",
    type: "Vulnerability"
  };
}
