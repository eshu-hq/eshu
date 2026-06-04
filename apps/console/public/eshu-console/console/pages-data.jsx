/* Eshu Console — data pages: Explorer, Catalog, Findings, Vulnerabilities, Admin. */
const { useState: useStateD, useMemo: useMemoD } = React;

/* ================================================================ EXPLORER */
function Explorer({ onOpenService, graphStyle, setGraphStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [scope, setScope] = useStateD("focus");
  const estate = useMemoD(() => D.buildEstateGraph(), []);
  const baseGraph = scope === "estate" ? estate : D.graph;
  const [sel, setSel] = useStateD(D.graph.nodes.find((n) => n.hero));
  const [layers, setLayers] = useStateD(() => { const o = {}; Object.keys(D.layerColor).forEach((k) => o[k] = true); return o; });
  const layerKeys = Object.keys(D.layerColor);

  const filteredGraph = useMemoD(() => {
    let nodes = baseGraph.nodes;
    if (verifiedOnly) nodes = nodes.filter((n) => n.truth !== "inferred");
    const nodeIds = new Set(nodes.map((n) => n.id));
    const edges = baseGraph.edges.filter((e) => layers[e.layer] && nodeIds.has(e.s) && nodeIds.has(e.t));
    const keep = new Set();
    edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
    nodes.forEach((n) => { if (n.hero) keep.add(n.id); });
    return { nodes: nodes.filter((n) => keep.has(n.id) || baseGraph.edges.length === 0), edges };
  }, [baseGraph, layers, verifiedOnly]);

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div><h2>Graph Explorer</h2><p>Pan, zoom and drill into the live NornicDB graph. Switch between the api-node-boats focus view and the full indexed estate, toggle relationship layers, then select any node.</p></div>
        <div className="row" style={{ gap: 8 }}>
          <div className="seg"><button className={scope === "focus" ? "active" : ""} onClick={() => setScope("focus")}>Focus</button><button className={scope === "estate" ? "active" : ""} onClick={() => { setScope("estate"); setGraphStyle("radial"); }}>Full estate</button></div>
          <div className="seg"><button className={graphStyle === "layered" ? "active" : ""} onClick={() => setGraphStyle("layered")}>Layered</button><button className={graphStyle === "radial" ? "active" : ""} onClick={() => setGraphStyle("radial")}>Radial</button></div>
        </div>
      </div>

      <div className="explorer-filters">
        <span className="row" style={{ gap: 7, color: "var(--subtle)", fontSize: ".78rem", fontWeight: 700, textTransform: "uppercase", letterSpacing: ".08em", marginRight: 4 }}><Icon.filter size={15} />Layers</span>
        {layerKeys.map((k) => {
          const n = D.relationships.filter((r) => r.layer === k).reduce((a, r) => a + r.count, 0);
          return (
            <button key={k} className={cx("layer-toggle", layers[k] ? "on" : "off")} style={{ "--lc": D.layerColor[k] }} onClick={() => setLayers((l) => ({ ...l, [k]: !l[k] }))}>
              <i style={{ background: D.layerColor[k] }} /><span style={{ textTransform: "capitalize" }}>{k}</span><span className="lt-n">{fmt(n)}</span>
            </button>
          );
        })}
      </div>

      <div className="explorer-layout">
        <div className="gcanvas-shell">
          <GraphCanvas graph={filteredGraph} layout={graphStyle} height={640} onSelect={setSel} selectedId={sel && sel.id} />
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>{scope === "estate" ? D.services.length + " indexed services & libraries · real @dmm/* dependency edges" : "api-node-boats neighbourhood · curated evidence"}</div>
        </div>
        <Panel title="Inspector" glyph={<Icon.search />}>
          {sel ? <NodeInspector node={sel} onOpenService={onOpenService} /> : <p className="empty">Select a node.</p>}
          <div className="section-label" style={{ marginTop: 18 }}>Node kinds</div>
          <div className="grid g-2" style={{ gap: 7 }}>
            {Object.entries(D.kindStyle).map(([k, v]) => <span key={k} className="row" style={{ gap: 8, fontSize: ".76rem", color: "var(--muted)" }}><i style={{ width: 8, height: 8, borderRadius: 2, background: v.color, flex: "none" }} />{v.label}</span>)}
          </div>
        </Panel>
      </div>
    </div>
  );
}

/* ================================================================= CATALOG */
function Catalog({ onOpenService, data }) {
  const D = data || ESHU;
  const [q, setQ] = useStateD("");
  const [tier, setTier] = useStateD("all");
  const rows = D.services.filter((s) => (tier === "all" || s.tier === tier) && (q === "" || (s.name + s.repo + s.owner).toLowerCase().includes(q.toLowerCase())));
  return (
    <div className="page">
      <div className="page-intro"><h2>Catalog</h2><p>Every indexed service, repository and workload with coverage, freshness and truth level. Select a row to open its spotlight.</p></div>
      <Panel className="flush" title={rows.length + " services"} sub="Sorted by deployment criticality"
        action={
          <div className="row" style={{ gap: 8 }}>
            <div className="searchbox" style={{ minWidth: 220, height: 34 }}><Icon.search size={15} /><input placeholder="Filter catalog…" value={q} onChange={(e) => setQ(e.target.value)} /></div>
            <div className="seg">{["all", "tier-1", "tier-2", "tier-3", "lib"].map((t) => <button key={t} className={tier === t ? "active" : ""} onClick={() => setTier(t)}>{t === "all" ? "All" : t === "lib" ? "Libs" : t}</button>)}</div>
          </div>
        }>
        <table className="tbl">
          <thead><tr><th>Service</th><th>Tier</th><th>System</th><th>Owner</th><th>Language</th><th>Runtime</th><th>Security</th><th>Coverage</th><th>Truth</th><th>Freshness</th></tr></thead>
          <tbody>
            {rows.map((s) => (
              <tr key={s.id} onClick={() => onOpenService(s.id)}>
                <td className="cell-stack"><span className="t-name">{s.name}</span><small>{s.repo}</small></td>
                <td><span className={"tag-tier tier-" + s.tier}>{s.tier === "lib" ? "library" : s.tier}</span></td>
                <td className="t-mut" style={{ fontSize: ".8rem" }}>{s.system}</td>
                <td className="t-mut">{s.owner}</td>
                <td><span className="row" style={{ gap: 7 }}><i style={{ width: 8, height: 8, borderRadius: 9, background: D.lang[s.lang].color }} />{D.lang[s.lang].label}</span></td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{s.kind === "lib" ? "—" : s.envs.length + " env"}</td>
                <td style={{ minWidth: 130 }}><SeverityBar counts={{ critical: s.crit, high: s.high, medium: s.med, low: s.low }} sev={D.sev} />
                  <div className="row" style={{ gap: 8, marginTop: 6 }}>{s.crit ? <span className="sev-tag" style={{ color: D.sev.critical, fontSize: ".68rem" }}><i style={{ background: D.sev.critical }} />{s.crit}</span> : null}{s.high ? <span className="sev-tag" style={{ color: D.sev.high, fontSize: ".68rem" }}><i style={{ background: D.sev.high }} />{s.high}</span> : null}</div>
                </td>
                <td className="mono" style={{ fontSize: ".82rem" }}>{Math.round(s.coverage * 100)}%</td>
                <td><TruthChip level={s.truth} /></td>
                <td><FreshDot state={s.freshness} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

/* ================================================================ FINDINGS */
function Findings({ onOpenService, verifiedOnly, data }) {
  const D = data || ESHU;
  const [type, setType] = useStateD("all");
  const [sev, setSev] = useStateD("all");
  const pool = verifiedOnly ? D.findings.filter((f) => f.truth !== "inferred") : D.findings;
  const hidden = D.findings.length - pool.length;
  const types = ["all"].concat(Array.from(new Set(pool.map((f) => f.type))));
  const rows = pool.filter((f) => (type === "all" || f.type === type) && (sev === "all" || f.severity === sev));
  const byType = {}; pool.forEach((f) => byType[f.type] = (byType[f.type] || 0) + 1);
  const sevCount = {}; pool.forEach((f) => sevCount[f.severity] = (sevCount[f.severity] || 0) + 1);

  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>Everything that needs human attention — drift, vulnerabilities, dead code, missing evidence, stale answers and incident correlations — each carrying its truth level and source.</p></div>
      <div className="grid g-4">
        <StatTile label="Open findings" value={pool.length} color="var(--ember)" sub={hidden ? hidden + " inferred hidden" : "across the fleet"} />
        <StatTile label="Critical" value={sevCount.critical || 0} color="var(--crit)" sub="immediate action" />
        <StatTile label="Drift detected" value={byType.Drift || 0} color="var(--violet)" sub="live vs declared" />
        <StatTile label="Stale answers" value={(byType["Stale answer"] || 0) + (byType["Missing evidence"] || 0)} color="var(--med)" sub="freshness / evidence gaps" />
      </div>
      <Panel className="flush mt" title="All findings" sub={hidden ? hidden + " inferred finding(s) hidden" : undefined}
        action={<div className="row" style={{ gap: 8 }}>
          <div className="seg">{["all", "critical", "high", "medium", "low"].map((s) => <button key={s} className={sev === s ? "active" : ""} onClick={() => setSev(s)}>{s === "all" ? "All" : s}</button>)}</div>
        </div>}>
        <div className="row wrap" style={{ gap: 7, padding: "14px var(--pad) 0" }}>
          {types.map((t) => <button key={t} className={cx("btn-ghost", type === t && "active")} onClick={() => setType(t)}>{t === "all" ? "All types" : t}{t !== "all" ? " · " + byType[t] : ""}</button>)}
        </div>
        <table className="tbl mt">
          <thead><tr><th>Severity</th><th>Finding</th><th>Type</th><th>Entity</th><th>Source</th><th>Truth</th><th>Age</th><th></th></tr></thead>
          <tbody>
            {rows.map((f) => (
              <tr key={f.id} onClick={() => { const svc = D.services.find((s) => s.id === f.entity); if (svc) onOpenService(svc.id); }}>
                <td><span className="sev-tag" style={{ color: D.sev[f.severity] }}><i style={{ background: D.sev[f.severity] }} />{f.severity}</span></td>
                <td className="cell-stack" style={{ maxWidth: 360 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span><small>{f.detail}</small></td>
                <td><Badge tone="neutral">{f.type}</Badge></td>
                <td className="t-name" style={{ fontSize: ".8rem" }}>{f.entity}</td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{f.source}</td>
                <td><TruthChip level={f.truth} /></td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.age}</td>
                <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

/* =========================================================== VULNERABILITIES */
function Vulnerabilities({ onOpenService, chartStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [sev, setSev] = useStateD("all");
  const pool = verifiedOnly ? D.vulns.filter((v) => v.prov !== "inferred") : D.vulns;
  const hidden = D.vulns.length - pool.length;
  const rows = pool.filter((v) => sev === "all" || v.severity === sev).slice().sort((a, b) => b.cvss - a.cvss);
  const sevCount = { critical: 0, high: 0, medium: 0, low: 0 };
  pool.forEach((v) => sevCount[v.severity]++);
  const kevCount = pool.filter((v) => v.kev).length;
  const fixable = pool.filter((v) => v.fixAvailable).length;
  const byEco = {}; pool.forEach((v) => byEco[v.ecosystem] = (byEco[v.ecosystem] || 0) + 1);
  const ecoColor = { go: "#14b8a6", npm: "#f0506e", pypi: "#a78bfa", maven: "#f472b6", deb: "#ff9d2e", apk: "#4f8cff" };

  return (
    <div className="page">
      <div className="page-intro"><h2>Vulnerabilities</h2><p>Vulnerability intelligence correlated to deployed images and reachable services — sourced from CISA KEV, FIRST EPSS, NVD, OSV and GHSA, and joined to the graph by <span className="mono">AFFECTED_BY</span> edges.</p></div>

      <div className="grid g-4">
        <StatTile label="Open CVEs" value={pool.length} color="var(--crit)" sub={sevCount.critical + " critical · " + sevCount.high + " high" + (hidden ? " · " + hidden + " inferred hidden" : "")} />
        <StatTile label="KEV-listed" value={kevCount} color="var(--crit)" trend={kevCount ? { dir: "down", text: "act now" } : undefined} sub="known exploited" />
        <StatTile label="Fix available" value={fixable + "/" + pool.length} color="var(--teal)" sub="patch path exists" />
        <StatTile label="New (14d)" value={D.metrics.newVulns.reduce((a, b) => a + b, 0)} spark={D.metrics.newVulns} color="var(--ember)" sub="intake from feeds" />
      </div>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.4fr) minmax(0,1fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="New vulnerabilities" sub="Daily intake over the last 14 days" glyph={<Icon.pulse />}>
          <AreaChart data={D.metrics.newVulns} color="var(--crit)" h={170} unit=" CVEs" />
        </Panel>
        <Panel title="By severity" glyph={<Icon.shield />}>
          <div style={{ display: "grid", placeItems: "center" }}>
            <Donut size={132} thickness={17} segments={["critical", "high", "medium", "low"].map((k) => ({ label: k, value: sevCount[k], color: D.sev[k] }))} center={{ value: pool.length, label: "CVEs" }} />
          </div>
          <div className="row wrap" style={{ gap: 12, justifyContent: "center", marginTop: 12 }}>{["critical", "high", "medium", "low"].map((k) => <span key={k} className="sev-tag" style={{ color: D.sev[k] }}><i style={{ background: D.sev[k] }} />{sevCount[k]}</span>)}</div>
        </Panel>
        <Panel title="By ecosystem" glyph={<Icon.box />}>
          <BarRows rows={Object.entries(byEco).map(([k, v]) => ({ label: k, value: v, color: ecoColor[k] || "var(--teal)" })).sort((a, b) => b.value - a.value)} />
        </Panel>
      </div>

      <Panel className="flush mt" title="CVE register" sub="Sorted by CVSS — joined to reachable services"
        action={<div className="seg">{["all", "critical", "high", "medium"].map((s) => <button key={s} className={sev === s ? "active" : ""} onClick={() => setSev(s)}>{s === "all" ? "All" : s}</button>)}</div>}>
        <table className="tbl">
          <thead><tr><th>CVE</th><th>Severity</th><th>CVSS</th><th>EPSS</th><th>Package</th><th>Affected services</th><th>Fix</th><th>Source</th></tr></thead>
          <tbody>
            {rows.map((v) => (
              <tr key={v.cve}>
                <td className="cell-stack"><span className="row" style={{ gap: 7 }}><span className="t-name" style={{ fontSize: ".8rem" }}>{v.cve}</span>{v.kev ? <span className="kev-flag">KEV</span> : null}</span><small style={{ maxWidth: 260 }}>{v.title}</small></td>
                <td><span className="sev-tag" style={{ color: D.sev[v.severity] }}><i style={{ background: D.sev[v.severity] }} />{v.severity}</span></td>
                <td><span className="mono" style={{ fontSize: ".82rem", color: D.sev[v.severity] }}>{v.cvss}</span></td>
                <td><span className="score-bar"><i style={{ width: (v.epss * 100) + "%", background: v.epss > 0.5 ? "var(--crit)" : "var(--med)" }} /></span> <span className="mono" style={{ fontSize: ".72rem", color: "var(--muted)" }}>{(v.epss * 100).toFixed(0)}%</span></td>
                <td className="cell-stack"><span className="t-mut mono" style={{ fontSize: ".78rem" }}>{v.pkg}</span><small>{v.version} · {v.ecosystem}</small></td>
                <td><div className="row wrap" style={{ gap: 5 }}>{v.services.slice(0, 3).map((s) => <button key={s} className="dep-chip" style={{ fontSize: ".7rem", padding: "3px 7px" }} onClick={() => onOpenService(s)}>{s}</button>)}{v.services.length > 3 ? <span className="t-mut" style={{ fontSize: ".72rem" }}>+{v.services.length - 3}</span> : null}</div></td>
                <td>{v.fixAvailable ? <Badge tone="teal">{v.fixed}</Badge> : <Badge tone="crit">none</Badge>}</td>
                <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{v.source}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

/* =================================================================== ADMIN */
function Admin({ source, data }) {
  const D = data || ESHU, m = D.metrics, r = D.runtime;
  const apps = D.argocdApps;
  const indexedCount = apps.filter((a) => a.indexed).length;
  const live = source && source.mode === "live" && source.status === "connected";
  return (
    <div className="page">
      <div className="page-intro"><h2>Operations</h2><p>Eshu runtime and NornicDB graph-backend health. Ingestion pipeline, reducer queues, graph writes and query performance. Data source: <strong style={{ color: live ? "var(--teal)" : "var(--bone)" }}>{source ? (source.mode === "demo" ? "demo (static extraction)" : live ? "live Eshu API" : "live (unreachable — demo fallback)") : "demo"}</strong>.</p></div>

      <div className="grid g-4">
        <StatTile label="Write throughput" value={fmt(m.writeTps.at(-1)) + "/s"} spark={m.writeTps} color="var(--teal)" trend={{ dir: "up", text: "steady" }} sub="graph mutations" />
        <StatTile label="Query p99" value={m.queryP99.at(-1) + "ms"} spark={m.queryP99} color="var(--ember)" trend={{ dir: "flat", text: "within SLO" }} sub="NornicDB read path" />
        <StatTile label="Cache hit" value={m.cacheHit.at(-1) + "%"} spark={m.cacheHit} color="var(--blue)" trend={{ dir: "up", text: "+0.4%" }} sub="adjacency cache" />
        <StatTile label="Dead letters" value={r.deadLetters} spark={m.deadLetters} color="var(--violet)" trend={{ dir: "down", text: "−2" }} sub="needs replay" />
      </div>

      <div className="grid g-2 mt">
        <Panel title="Reducer queue depth" sub="Outstanding work items awaiting reduction" glyph={<Icon.layers />}>
          <AreaChart data={m.queueDepth} color="var(--violet)" h={180} unit=" items" />
        </Panel>
        <Panel title="Graph growth" sub="Total nodes & relationships in NornicDB" glyph={<Icon.db />}>
          <MultiLine seriesList={[{ label: "edges", data: m.graphEdges, color: "var(--ember)" }, { label: "nodes", data: m.graphNodes, color: "var(--teal)" }]} h={180} unit="" />
          <div className="chart-legend"><span><i style={{ background: "var(--teal)" }} />{fmt(r.nodes)} nodes</span><span><i style={{ background: "var(--ember)" }} />{fmt(r.edges)} edges</span></div>
        </Panel>
      </div>

      <Panel className="flush mt" title="ArgoCD deployed workloads" sub={apps.length + " applications · " + indexedCount + " with source indexed in this workspace"} glyph={<Icon.layers />}
        action={<span className="t-mut mono" style={{ fontSize: ".74rem" }}>helm-charts/argocd</span>}>
        <div className="argocd-grid">
          {apps.map((a) => (
            <div className={cx("argocd-app", a.indexed && "indexed")} key={a.name} title={a.indexed ? "Source indexed" : "Deploy-only — source not in workspace"}>
              <span className="row" style={{ gap: 7, minWidth: 0 }}><i style={{ width: 7, height: 7, borderRadius: 9, background: a.indexed ? "var(--teal)" : "var(--subtle)", flex: "none" }} /><span className="argocd-name">{a.name}</span></span>
              {a.kind === "portal" ? <span className="argocd-tag">portal</span> : null}
            </div>
          ))}
        </div>
      </Panel>

      <Panel className="flush mt" title="Collectors" sub={D.collectors.length + " fact sources feeding the graph"} glyph={<Icon.cloud />}>
        <table className="tbl">
          <thead><tr><th>Collector</th><th>Instance</th><th>Status</th><th>Facts</th><th>Scopes</th><th>Latency</th><th>Cadence</th><th>Last run</th></tr></thead>
          <tbody>
            {D.collectors.map((c) => {
              const k = D.collectorKinds[c.kind];
              return (
                <tr key={c.instance}>
                  <td><span className="row" style={{ gap: 10 }}><CollectorGlyph kind={c.kind} /><span className="cell-stack"><span style={{ fontWeight: 600 }}>{k.label}</span><small>{c.note}</small></span></span></td>
                  <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{c.instance}</td>
                  <td><span className="status-pill" style={{ color: D.statusColor[c.status] }}><i style={{ background: D.statusColor[c.status] }} />{c.status}</span></td>
                  <td className="mono" style={{ fontSize: ".82rem" }}>{fmt(c.facts)}</td>
                  <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.scopes}</td>
                  <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.latencyMs ? c.latencyMs + "ms" : "—"}</td>
                  <td className="t-mut" style={{ fontSize: ".78rem" }}>{c.cadence}</td>
                  <td><FreshDot state={c.freshness} /><div className="t-mut mono" style={{ fontSize: ".72rem", marginTop: 2 }}>{c.lastRun}</div></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

Object.assign(window, { Explorer, Catalog, Findings, Vulnerabilities, Admin });
