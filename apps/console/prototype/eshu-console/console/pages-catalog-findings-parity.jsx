/* Live-parity override for Catalog and Findings.
   Demo mode delegates to pages-data.jsx; connected-live mode names the API
   contracts and renders honest empty states. */
const DemoCatalogPage = window.Catalog;
const DemoFindingsPage = window.Findings;
const SEVERITY_RANK_CF = { critical: 4, high: 3, medium: 2, low: 1 };

function catalogRows(data) {
  return Array.isArray(data && data.services) ? data.services : [];
}

function findingRows(data, verifiedOnly) {
  const findings = Array.isArray(data && data.findings) ? data.findings : [];
  const vulns = Array.isArray(data && data.vulns) ? data.vulns : [];
  const filteredFindings = verifiedOnly ? findings.filter((row) => row.truth !== "inferred") : findings;
  const filteredVulns = verifiedOnly ? vulns.filter((row) => row.prov !== "inferred") : vulns;
  return filteredFindings.map((row) => ({
    id: row.id,
    title: row.title || row.id,
    detail: row.detail || row.source || "",
    entity: row.entity || "-",
    severity: row.severity || "medium",
    source: row.source || "POST /api/v0/code/dead-code",
    truth: row.truth || "derived",
    type: row.type || "Finding"
  })).concat(filteredVulns.map((row) => ({
    id: "vulnerability:" + row.cve,
    title: (row.cve || row.id) + " - " + (row.pkg || row.package || "package"),
    detail: (row.title || row.version || "") + (row.fixAvailable ? " - fix " + row.fixed : ""),
    entity: (row.services && row.services[0]) || "service not resolved",
    severity: row.severity || "medium",
    source: row.source || "GET /api/v0/supply-chain/impact/findings",
    truth: row.prov === "inferred" ? "inferred" : "derived",
    type: "Vulnerability",
    cve: row.cve || row.id
  })));
}

function Catalog(props) {
  if (!props.client && DemoCatalogPage) return <DemoCatalogPage {...props} />;
  const D = props.data || ESHU;
  const rows = catalogRows(D);
  return (
    <div className="page">
      <div className="page-intro"><h2>Catalog</h2><p>Every indexed service, repository and workload from <span className="mono">GET /api/v0/catalog?limit=2000</span>.</p></div>
      <Panel className="flush" title={rows.length + " entries"} sub="live catalog rows">
        <table className="tbl">
          <thead><tr><th>Name</th><th>Kind</th><th>Repository</th><th>Environments</th><th>Truth</th><th>Freshness</th></tr></thead>
          <tbody>{rows.map((service) => (
            <tr key={service.id} onClick={() => props.onOpenService && props.onOpenService(service.id || service.name)} style={{ cursor: "pointer" }}>
              <td className="t-name">{service.name || service.id}</td>
              <td className="t-mut">{service.kind || "workload"}</td>
              <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{service.repo || "-"}</td>
              <td className="t-mut">{service.envs && service.envs.length ? service.envs.join(", ") : "-"}</td>
              <td><TruthChip level={service.truth || "derived"} /></td>
              <td><FreshDot state={service.freshness || "fresh"} /></td>
            </tr>
          ))}{rows.length === 0 ? <tr><td colSpan={6} className="empty">No catalog entries from this source.</td></tr> : null}</tbody>
        </table>
      </Panel>
    </div>
  );
}

function Findings(props) {
  if (!props.client && DemoFindingsPage) return <DemoFindingsPage {...props} />;
  const D = props.data || ESHU;
  const rows = findingRows(D, props.verifiedOnly).sort((a, b) => (SEVERITY_RANK_CF[b.severity] || 0) - (SEVERITY_RANK_CF[a.severity] || 0));
  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>Unified live worklist from <span className="mono">POST /api/v0/code/dead-code</span> and <span className="mono">GET /api/v0/supply-chain/impact/findings</span>.</p></div>
      <FindingsTabs active="worklist" />
      <div className="grid g-4">
        <StatTile label="Open findings" value={rows.length} color="var(--ember)" sub="live worklist rows" />
        <StatTile label="Dead code" value={rows.filter((row) => row.type !== "Vulnerability").length} color="var(--violet)" sub="analyzer candidates" />
        <StatTile label="Vulnerabilities" value={rows.filter((row) => row.type === "Vulnerability").length} color="var(--crit)" sub="reachable advisories" />
        <StatTile label="Types" value={new Set(rows.map((row) => row.type)).size} color="var(--blue)" sub="distinct categories" />
      </div>
      <Panel className="flush mt" title="Unified worklist">
        <table className="tbl">
          <thead><tr><th>Severity</th><th>Finding</th><th>Type</th><th>Entity</th><th>Source</th><th>Truth</th><th></th></tr></thead>
          <tbody>{rows.map((row) => (
            <tr key={row.id} onClick={() => { if (row.cve && props.onOpenVuln) props.onOpenVuln(row.cve); else if (props.onOpenService) props.onOpenService(row.entity); }} style={{ cursor: "pointer" }}>
              <td><span className="sev-tag" style={{ color: (D.sev && D.sev[row.severity]) || "var(--med)" }}><i style={{ background: (D.sev && D.sev[row.severity]) || "var(--med)" }} />{row.severity}</span></td>
              <td className="cell-stack" style={{ maxWidth: 420 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{row.title}</span><small>{row.detail}</small></td>
              <td><Badge tone={row.type === "Vulnerability" ? "crit" : "neutral"}>{row.type}</Badge></td>
              <td className="t-name" style={{ fontSize: ".8rem" }}>{row.entity}</td>
              <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{row.source}</td>
              <td><TruthChip level={row.truth} /></td>
              <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
            </tr>
          ))}{rows.length === 0 ? <tr><td colSpan={7} className="empty">No findings from this source.</td></tr> : null}</tbody>
        </table>
      </Panel>
    </div>
  );
}

window.Catalog = Catalog;
window.Findings = Findings;
