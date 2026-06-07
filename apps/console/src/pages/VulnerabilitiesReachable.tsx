// pages/VulnerabilitiesReachable.tsx
// Advisories correlated to reachable services via the impact findings surface
// (GET /api/v0/supply-chain/impact/findings). This is admitted impact truth, as
// opposed to the broader known-intelligence catalog.
import { Link } from "react-router-dom";
import type { ConsoleModel, Severity } from "../console/types";
import { SEVERITY_COLOR } from "../console/types";
import { Panel, StatTile, Badge } from "../components/atoms";
import { Donut } from "../components/charts";

export function ReachableAdvisories({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const rows = model.vulnerabilities.slice().sort((a, b) => b.cvss - a.cvss);
  const sevCount: Record<Severity, number> = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  rows.forEach((v) => { const k = v.severity as Severity; if (k in sevCount) sevCount[k] += 1; });
  const kev = rows.filter((v) => v.kev).length;
  const fixable = rows.filter((v) => v.fixedVersion).length;
  const unavailable = model.provenance.vulnerabilities === "unavailable";
  return (
    <div>
      <p className="t-mut" style={{ fontSize: ".82rem", margin: "0 0 var(--gap)" }}>
        Reachable advisories — <span className="mono">GET /api/v0/supply-chain/impact/findings</span>.
      </p>
      <div className="grid g-4">
        <StatTile label="Open advisories" value={rows.length} color="var(--crit)" sub={`${sevCount.critical} critical · ${sevCount.high} high`} />
        <StatTile label="KEV-listed" value={kev} color="var(--crit)" sub="known exploited" />
        <StatTile label="Fix available" value={`${fixable}/${rows.length || 0}`} color="var(--teal)" sub="patch path exists" />
        <StatTile label="Source" value={model.source === "live" ? "live" : "demo"} color="var(--ember)" sub="impact findings" />
      </div>
      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}>
        <Panel title="By severity">
          <div style={{ display: "grid", placeItems: "center" }}>
            <Donut size={138} thickness={17} center={{ value: rows.length, label: "advisories" }}
              segments={(["critical", "high", "medium", "low"] as const).map((k) => ({ label: k, value: sevCount[k], color: SEVERITY_COLOR[k] }))} />
          </div>
        </Panel>
        <Panel className="flush" title="Advisory register" sub="Sorted by CVSS">
          <table className="tbl">
            <thead><tr><th>ID</th><th>Severity</th><th>CVSS</th><th>Package</th><th>Services</th><th>Fix</th></tr></thead>
            <tbody>
              {rows.map((v) => (
                <tr key={v.id}>
                  <td className="row" style={{ gap: 7 }}><Link to={`/vulnerabilities/${encodeURIComponent(v.id)}`} className="t-name link-btn" style={{ fontSize: ".8rem" }}>{v.id}</Link>{v.kev ? <span className="kev-flag">KEV</span> : null}</td>
                  <td><span className="sev-tag" style={{ color: SEVERITY_COLOR[(v.severity as Severity) in SEVERITY_COLOR ? (v.severity as Severity) : "medium"] }}><i style={{ background: "currentColor" }} />{v.severity}</span></td>
                  <td className="mono" style={{ fontSize: ".82rem" }}>{v.cvss || "—"}</td>
                  <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{v.package}</td>
                  <td className="t-mut" style={{ fontSize: ".76rem" }}>{v.services.slice(0, 2).join(", ")}{v.services.length > 2 ? ` +${v.services.length - 2}` : ""}</td>
                  <td>{v.fixedVersion ? <Badge tone="teal">{v.fixedVersion}</Badge> : <Badge tone="crit">none</Badge>}</td>
                </tr>
              ))}
              {rows.length === 0 ? (
                <tr><td colSpan={6} className="empty">
                  {unavailable
                    ? "Impact findings are unavailable — the supply-chain impact read model did not respond."
                    : "No advisories from this source — requires the vulnerability-intelligence collector."}
                </td></tr>
              ) : null}
            </tbody>
          </table>
        </Panel>
      </div>
    </div>
  );
}
