/* Eshu Console — data pages: Explorer, Catalog, Findings, Vulnerabilities. */
const { useState: useStateD, useMemo: useMemoD } = React;
/* ================================================================ EXPLORER */
function Explorer({ onOpenService, onOpenNode, graphStyle, setGraphStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [scope, setScope] = useStateD("focus");
  const estate = useMemoD(() => D.buildEstateGraph(), []);
  const baseGraph = scope === "estate" ? estate : D.graph;
  const heroNode = baseGraph.nodes.find((n) => n.hero) || baseGraph.nodes[0];

  const [sel, setSel] = useStateD({ type: "node", node: heroNode });
  const [layers, setLayers] = useStateD(() => {const o = {};Object.keys(D.layerColor).forEach((k) => o[k] = true);return o;});
  const [focusRoot, setFocusRoot] = useStateD(null);
  const [visibleIds, setVisibleIds] = useStateD(() => new Set());
  const [crumbs, setCrumbs] = useStateD([]);
  const [isolatedVerb, setIsolatedVerb] = useStateD(null);
  const [pinnedEdge, setPinnedEdge] = useStateD(null);
  const [traceMode, setTraceMode] = useStateD(false);
  const [traceA, setTraceA] = useStateD(null);
  const [traceB, setTraceB] = useStateD(null);
  const layerKeys = Object.keys(D.layerColor);

  const baseAdj = useMemoD(() => {
    const m = {};
    baseGraph.edges.forEach((e) => { (m[e.s] = m[e.s] || new Set()).add(e.t); (m[e.t] = m[e.t] || new Set()).add(e.s); });
    return m;
  }, [baseGraph]);
  const neighborsOf = (id) => baseAdj[id] ? Array.from(baseAdj[id]) : [];

  // reset drill state whenever the scope (focus vs estate) changes
  React.useEffect(() => {
    setFocusRoot(null); setVisibleIds(new Set()); setCrumbs([]); setIsolatedVerb(null);
    setPinnedEdge(null); setTraceMode(false); setTraceA(null); setTraceB(null);
    const h = baseGraph.nodes.find((n) => n.hero) || baseGraph.nodes[0];
    setSel({ type: "node", node: h });
  }, [scope]);

  const selectNode = (n) => {
    if (!n) return;
    if (traceMode) {
      if (!traceA) setTraceA(n.id);
      else if (!traceB && n.id !== traceA) setTraceB(n.id);
      else { setTraceA(n.id); setTraceB(null); }
    }
    setSel({ type: "node", node: n });
  };
  const selectEdge = (e) => e && setSel({ type: "edge", edge: e });
  function pinEdge(e) { setPinnedEdge((p) => (p && edgeKey(p) === edgeKey(e)) ? null : e); }
  function startTrace() { setTraceMode((m) => { const nx = !m; if (!nx) { setTraceA(null); setTraceB(null); } return nx; }); }

  function focusHere(n) {
    const s = new Set([n.id]); neighborsOf(n.id).forEach((id) => s.add(id));
    setFocusRoot(n.id); setVisibleIds(s);
    setCrumbs((c) => [...c, { id: n.id, label: n.label, kind: n.kind }]);
    setSel({ type: "node", node: n });
  }
  function expandNode(n) {
    if (!focusRoot) { focusHere(n); return; }
    setVisibleIds((prev) => { const s = new Set(prev); s.add(n.id); neighborsOf(n.id).forEach((id) => s.add(id)); return s; });
  }
  function refocusIndex(i) {
    if (i === 0) { setFocusRoot(null); setVisibleIds(new Set()); setCrumbs([]); return; }
    const newCrumbs = crumbs.slice(0, i);
    const last = newCrumbs[newCrumbs.length - 1];
    const node = baseGraph.nodes.find((n) => n.id === last.id);
    const s = new Set([node.id]); neighborsOf(node.id).forEach((id) => s.add(id));
    setFocusRoot(node.id); setVisibleIds(s); setCrumbs(newCrumbs); setSel({ type: "node", node });
  }
  function isolate(edge) { setIsolatedVerb((v) => v === edge.verb ? null : edge.verb); }

  const scopedNodes = useMemoD(() => focusRoot ? baseGraph.nodes.filter((n) => visibleIds.has(n.id)) : baseGraph.nodes, [baseGraph, focusRoot, visibleIds]);

  const filteredGraph = useMemoD(() => {
    if (pinnedEdge) {
      const ids = new Set([pinnedEdge.s, pinnedEdge.t]);
      return { nodes: baseGraph.nodes.filter((n) => ids.has(n.id)), edges: [pinnedEdge] };
    }
    let nodes = scopedNodes.slice();
    if (verifiedOnly) nodes = nodes.filter((n) => n.truth !== "inferred");
    const nodeIds = new Set(nodes.map((n) => n.id));
    let edges = baseGraph.edges.filter((e) => layers[e.layer] && nodeIds.has(e.s) && nodeIds.has(e.t));
    if (isolatedVerb) edges = edges.filter((e) => e.verb === isolatedVerb);
    if (isolatedVerb) {
      const keep = new Set(); edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
      nodes = nodes.filter((n) => keep.has(n.id));
    } else if (!focusRoot) {
      const keep = new Set(); edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
      nodes.forEach((n) => { if (n.hero) keep.add(n.id); });
      nodes = nodes.filter((n) => keep.has(n.id) || baseGraph.edges.length === 0);
    }
    return { nodes, edges };
  }, [scopedNodes, layers, verifiedOnly, isolatedVerb, focusRoot, baseGraph, pinnedEdge]);

  const tracePathResult = useMemoD(() => (traceMode && traceA && traceB) ? tracePath(filteredGraph, traceA, traceB) : null, [traceMode, traceA, traceB, filteredGraph]);
  const traceLabel = (id) => { const n = filteredGraph.nodes.find((x) => x.id === id) || baseGraph.nodes.find((x) => x.id === id); return n ? n.label : id; };

  // which nodes still have hidden neighbours (drive the + expand badge)
  const expandedSet = useMemoD(() => {
    const s = new Set();
    filteredGraph.nodes.forEach((n) => {
      const all = neighborsOf(n.id).every((id) => focusRoot ? visibleIds.has(id) : true);
      if (all) s.add(n.id);
    });
    return s;
  }, [filteredGraph, visibleIds, focusRoot, baseAdj]);

  const trail = focusRoot ? [{ label: scope === "estate" ? "Full estate" : "Focus graph", kind: null }].concat(crumbs.map((c) => ({ label: c.label, kind: c.kind }))) : [];

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div><h2>Graph Explorer</h2><p>Pan, zoom and drill into the live NornicDB graph. Click any <strong>node</strong> for its evidence and connections, click any <strong>edge</strong> to read the typed relationship facts, then expand neighbours to walk the graph outward.</p></div>
        <div className="row" style={{ gap: 8 }}>
          <div className="seg"><button className={scope === "focus" ? "active" : ""} onClick={() => setScope("focus")}>Focus</button><button className={scope === "estate" ? "active" : ""} onClick={() => {setScope("estate");setGraphStyle("radial");}}>Full estate</button></div>
          <div className="seg"><button className={graphStyle === "layered" ? "active" : ""} onClick={() => setGraphStyle("layered")}>Layered</button><button className={graphStyle === "radial" ? "active" : ""} onClick={() => setGraphStyle("radial")}>Radial</button></div>
          <button className={cx("trace-toggle", traceMode && "on")} onClick={startTrace} title="Trace the shortest path between two nodes"><Icon.branch size={14} /> Trace path</button>
        </div>
      </div>

      <div className="explorer-filters">
        <span className="row" style={{ gap: 7, color: "var(--subtle)", fontSize: ".78rem", fontWeight: 700, textTransform: "uppercase", letterSpacing: ".08em", marginRight: 4 }}><Icon.filter size={15} />Layers</span>
        {layerKeys.map((k) => {
          const n = D.relationships.filter((r) => r.layer === k).reduce((a, r) => a + r.count, 0);
          return (
            <button key={k} className={cx("layer-toggle", layers[k] ? "on" : "off")} style={{ "--lc": D.layerColor[k] }} onClick={() => setLayers((l) => ({ ...l, [k]: !l[k] }))}>
              <i style={{ background: D.layerColor[k] }} /><span style={{ textTransform: "capitalize" }}>{k}</span><span className="lt-n">{fmt(n)}</span>
            </button>);

        })}
      </div>

      {isolatedVerb ? (
        <div className="isolate-bar"><Icon.branch size={15} /> Isolated to <span className="iso-verb">{isolatedVerb}</span> edges — showing only nodes joined by this relationship. <button onClick={() => setIsolatedVerb(null)}>Clear</button></div>
      ) : null}
      {pinnedEdge ? (
        <div className="isolate-bar pin-bar"><Icon.filter size={15} /> Pinned to <span className="iso-verb">{pinnedEdge.verb}</span> — <span className="mono">{traceLabel(pinnedEdge.s)}</span> → <span className="mono">{traceLabel(pinnedEdge.t)}</span>. Graph filtered to just this relationship. <button onClick={() => setPinnedEdge(null)}>Clear</button></div>
      ) : null}
      {traceMode ? (
        <div className="isolate-bar trace-bar">
          <Icon.branch size={15} />
          {!traceA ? <span>Trace mode · click the <strong>source</strong> node…</span>
            : !traceB ? <span>Source <span className="mono trace-end">{traceLabel(traceA)}</span> · now click the <strong>target</strong> node…</span>
            : tracePathResult ? <span>Path <span className="mono trace-end">{traceLabel(traceA)}</span> {tracePathResult.seq.slice(1).map((id, i) => <React.Fragment key={id}><span className="trace-arrow">→</span><button className="trace-hop" onClick={() => onOpenNode(filteredGraph.nodes.find((n) => n.id === id), filteredGraph)}>{traceLabel(id)}</button></React.Fragment>)} · <strong>{tracePathResult.hops} hops</strong></span>
            : <span>No path between <span className="mono trace-end">{traceLabel(traceA)}</span> and <span className="mono trace-end">{traceLabel(traceB)}</span> in this view — widen layers or scope.</span>}
          <button onClick={() => { setTraceA(null); setTraceB(null); }}>Reset</button>
          <button onClick={startTrace}>Exit</button>
        </div>
      ) : null}

      <div className="explorer-layout">
        <div className="gcanvas-shell">
          <GraphCanvas graph={filteredGraph} layout={graphStyle} height={640}
            onSelect={selectNode} onSelectEdge={selectEdge} onExpand={expandNode} onClear={() => {}}
            selectedId={sel && sel.type === "node" ? sel.node.id : null}
            selectedEdge={sel && sel.type === "edge" ? sel.edge : null}
            expandedIds={expandedSet} tracePath={tracePathResult} />
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>{focusRoot ? "Focused walk · double-click a node or press ＋ to expand its neighbours · " + filteredGraph.nodes.length + " nodes shown" : scope === "estate" ? D.services.length + " indexed services & libraries · real @acme/* dependency edges" : "svc-catalog neighbourhood · curated evidence · double-click a node to focus & walk outward"}</div>
        </div>
        <Panel title="Inspector" glyph={<Icon.search />}>
          <GraphInspector sel={sel} graph={filteredGraph} onOpenService={onOpenService} onOpenNode={onOpenNode}
            onSelectNode={selectNode} onSelectEdge={selectEdge} onExpand={expandNode} onFocus={focusHere}
            onIsolate={isolate} isolatedVerb={isolatedVerb} expandedIds={expandedSet}
            onPin={pinEdge} pinnedEdge={pinnedEdge}
            breadcrumb={trail} onCrumb={refocusIndex}
            emptyHint="Select any node or relationship edge." />
          <div className="section-label" style={{ marginTop: 18 }}>Node kinds</div>
          <div className="grid g-2" style={{ gap: 7 }}>
            {Object.entries(D.kindStyle).map(([k, v]) => <span key={k} className="row" style={{ gap: 8, fontSize: ".76rem", color: "var(--muted)" }}><i style={{ width: 8, height: 8, borderRadius: 2, background: v.color, flex: "none" }} />{v.label}</span>)}
          </div>
        </Panel>
      </div>
    </div>);

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
            {rows.map((s) =>
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
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

/* ================================================================ FINDINGS */
/* shared sub-nav so Findings (worklist) and the CVE register feel like one surface */
function FindingsTabs({ active }) {
  return (
    <div className="dep-toggle" style={{ marginBottom: 14 }}>
      <button className={active === "worklist" ? "active" : ""} onClick={() => { location.hash = "findings"; }}>Worklist</button>
      <button className={active === "cves" ? "active" : ""} onClick={() => { location.hash = "vulnerabilities"; }}>CVE register</button>
    </div>
  );
}
const SEVRANK = { critical: 4, high: 3, medium: 2, low: 1 };

function Findings({ onOpenService, onOpenVuln, verifiedOnly, data }) {
  const D = data || ESHU;
  const [type, setType] = useStateD("all");
  const [sev, setSev] = useStateD("all");
  const fpool = verifiedOnly ? D.findings.filter((f) => f.truth !== "inferred") : D.findings;
  const vpool = verifiedOnly ? D.vulns.filter((v) => v.prov !== "inferred") : D.vulns;
  // CVEs join the unified worklist as first-class rows; deep detail lives in the CVE register
  const vulnRows = vpool.map((v) => ({
    id: "cve-" + v.cve, type: "Vulnerability", severity: v.severity,
    title: v.cve + " · " + v.title,
    detail: v.pkg + "@" + v.version + " · " + v.ecosystem + (v.fixAvailable ? " · fix " + v.fixed : " · no fix"),
    entity: (v.services && v.services[0]) || "—", source: v.source,
    truth: v.prov === "inferred" ? "inferred" : "derived", age: v.firstSeen || "live",
    _cve: v.cve, _kev: v.kev, _cvss: v.cvss
  }));
  const pool = fpool.concat(vulnRows);
  const hidden = (D.findings.length - fpool.length) + (D.vulns.length - vpool.length);
  const types = ["all"].concat(Array.from(new Set(pool.map((f) => f.type))));
  const rows = pool.filter((f) => (type === "all" || f.type === type) && (sev === "all" || f.severity === sev)).sort((a, b) => (SEVRANK[b.severity] || 0) - (SEVRANK[a.severity] || 0));
  const byType = {};pool.forEach((f) => byType[f.type] = (byType[f.type] || 0) + 1);
  const sevCount = {};pool.forEach((f) => sevCount[f.severity] = (sevCount[f.severity] || 0) + 1);
  const kevCount = vpool.filter((v) => v.kev).length;

  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>One worklist for everything that needs attention — drift, version skew, legacy dependencies, missing evidence, incidents and <strong>vulnerabilities</strong> — each carrying its truth level and source. The <strong>CVE register</strong> tab keeps the deep security view with EPSS, KEV &amp; blast-radius.</p></div>
      <FindingsTabs active="worklist" />
      <div className="grid g-4">
        <StatTile label="Open items" value={pool.length} color="var(--ember)" sub={hidden ? hidden + " inferred hidden" : "across the fleet"} />
        <StatTile label="Critical" value={sevCount.critical || 0} color="var(--crit)" sub="immediate action" />
        <StatTile label="Vulnerabilities" value={byType.Vulnerability || 0} color="var(--crit)" sub={kevCount + " KEV-listed"} onClick={() => { location.hash = "vulnerabilities"; }} cta="CVE register" />
        <StatTile label="Drift / evidence" value={(byType.Drift || 0) + (byType["Missing evidence"] || 0) + (byType["Stale answer"] || 0)} color="var(--violet)" sub="freshness & drift gaps" />
      </div>
      <Panel className="flush mt" title="Unified worklist" sub={hidden ? hidden + " inferred item(s) hidden" : undefined}
      action={<div className="row" style={{ gap: 8 }}>
          <div className="seg">{["all", "critical", "high", "medium", "low"].map((s) => <button key={s} className={sev === s ? "active" : ""} onClick={() => setSev(s)}>{s === "all" ? "All" : s}</button>)}</div>
        </div>}>
        <div className="row wrap" style={{ gap: 7, padding: "14px var(--pad) 0" }}>
          {types.map((t) => <button key={t} className={cx("btn-ghost", type === t && "active")} onClick={() => setType(t)}>{t === "all" ? "All types" : t}{t !== "all" ? " · " + byType[t] : ""}</button>)}
        </div>
        <table className="tbl mt">
          <thead><tr><th>Severity</th><th>Finding</th><th>Type</th><th>Entity</th><th>Source</th><th>Truth</th><th>Age</th><th></th></tr></thead>
          <tbody>
            {rows.map((f) =>
            <tr key={f.id} onClick={() => { if (f._cve) { onOpenVuln && onOpenVuln(f._cve); return; } const svc = D.services.find((s) => s.id === f.entity); if (svc) onOpenService(svc.id); }} style={{ cursor: "pointer" }}>
                <td><span className="sev-tag" style={{ color: D.sev[f.severity] }}><i style={{ background: D.sev[f.severity] }} />{f.severity}</span></td>
                <td className="cell-stack" style={{ maxWidth: 360 }}><span className="row" style={{ gap: 7 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span>{f._kev ? <span className="kev-flag">KEV</span> : null}</span><small>{f.detail}</small></td>
                <td><span className="row" style={{ gap: 7 }}><Badge tone={f.type === "Vulnerability" ? "crit" : "neutral"}>{f.type}</Badge>{f._cvss != null ? <span className="mono" style={{ fontSize: ".74rem", color: D.sev[f.severity] }}>{f._cvss}</span> : null}</span></td>
                <td className="t-name" style={{ fontSize: ".8rem" }}>{f.entity}</td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{f.source}</td>
                <td><TruthChip level={f.truth} /></td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.age}</td>
                <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

/* =========================================================== VULNERABILITIES */
/* CVE -> affected nodes graph: the vulnerability at the centre. */
function buildVulnGraph(D, v) {
  const center = { id: "vuln:" + v.cve, kind: "vuln", label: v.cve, sub: (v.kev ? "KEV · " : "") + "CVSS " + v.cvss, hero: true, truth: "exact" };
  const pkg = { id: "pkg:" + v.pkg, kind: "library", label: v.pkg, sub: v.version + " · " + v.ecosystem, truth: "exact" };
  const nodes = [center, pkg];
  const edges = [{ s: "pkg:" + v.pkg, t: center.id, verb: "AFFECTED_BY", layer: "security" }];
  (v.services || []).forEach((sid) => {
    const svc = D.servicesById && D.servicesById[sid];
    nodes.push({ id: sid, kind: svc && svc.kind === "lib" ? "library" : "service", label: svc ? svc.name : sid, sub: svc ? (svc.tier || "") + (svc.system ? " · " + svc.system : "") : "", truth: svc ? svc.truth : "derived" });
    edges.push({ s: sid, t: "pkg:" + v.pkg, verb: "DEPENDS_ON", layer: "runtime" });
  });
  return { nodes, edges };
}

function VulnDetail({ v, D, onBack, onOpenService, onOpenNode }) {
  const graph = useMemoD(() => buildVulnGraph(D, v), [D, v]);
  const affected = (v.services || []).map((sid) => D.servicesById && D.servicesById[sid] || { id: sid, name: sid });
  return (
    <div className="page">
      <button className="repo-back" onClick={onBack}><Icon.arrow size={14} style={{ transform: "rotate(180deg)" }} /> All vulnerabilities</button>
      <div className="repo-detail-head">
        <div>
          <div className="row" style={{ gap: 10, flexWrap: "wrap" }}>
            <span className="repo-icon" style={{ background: "color-mix(in oklab, var(--crit) 16%, transparent)", color: "var(--crit)" }}><Icon.vuln size={18} /></span>
            <h2 style={{ fontFamily: "var(--mono)", fontSize: "1.3rem" }}>{v.cve}</h2>
            {v.kev ? <span className="kev-flag">KEV</span> : null}
            <span className="sev-tag" style={{ color: D.sev[v.severity] }}><i style={{ background: D.sev[v.severity] }} />{v.severity}</span>
          </div>
          <p className="t-mut" style={{ fontSize: ".9rem", margin: "10px 0 0", maxWidth: "78ch", lineHeight: 1.5 }}>{v.title}</p>
        </div>
        {v.fixAvailable ? <Badge tone="teal">fix: {v.fixed}</Badge> : <Badge tone="crit">no fix</Badge>}
      </div>

      <div className="repo-stats">
        <div><dt>CVSS</dt><dd style={{ color: D.sev[v.severity] }}>{v.cvss}</dd></div>
        <div><dt>EPSS</dt><dd>{Math.round((v.epss || 0) * 100)}%</dd></div>
        <div><dt>KEV</dt><dd style={{ color: v.kev ? "var(--crit)" : "var(--muted)" }}>{v.kev ? "listed" : "no"}</dd></div>
        <div><dt>Package</dt><dd style={{ fontSize: ".92rem" }}>{v.pkg}</dd></div>
        <div><dt>Affected version</dt><dd style={{ fontSize: ".92rem" }}>{v.version}</dd></div>
        <div><dt>Fixed in</dt><dd style={{ fontSize: ".92rem", color: v.fixAvailable ? "var(--teal)" : "var(--crit)" }}>{v.fixAvailable ? v.fixed : "none"}</dd></div>
        <div><dt>Ecosystem</dt><dd style={{ fontSize: ".92rem" }}>{v.ecosystem}</dd></div>
        <div><dt>Source</dt><dd style={{ fontSize: ".92rem" }}>{v.source}</dd></div>
      </div>

      <Panel title="Blast radius" sub={"AFFECTED_BY graph — " + v.cve + " at the centre, reachable services around it · click any node to drill"} glyph={<Icon.graph />}>
        <GraphCanvas graph={graph} layout="radial" height={420} onSelect={(n) => { if (onOpenNode) onOpenNode(n, graph); else if (D.servicesById && D.servicesById[n.id]) onOpenService(n.id); }} selectedId={"vuln:" + v.cve} />
      </Panel>

      <Panel className="flush mt" title={"Affected services (" + affected.length + ")"} sub="Reachable via a vulnerable dependency — click to open">
        <table className="tbl">
          <thead><tr><th>Service</th><th>Tier</th><th>System</th><th>Truth</th></tr></thead>
          <tbody>
            {affected.map((s) =>
            <tr key={s.id} onClick={() => onOpenService(s.id)} style={{ cursor: "pointer" }}>
                <td className="t-name">{s.name}</td>
                <td>{s.tier ? <span className={"tag-tier tier-" + s.tier}>{s.tier === "lib" ? "library" : s.tier}</span> : "—"}</td>
                <td className="t-mut" style={{ fontSize: ".8rem" }}>{s.system || "—"}</td>
                <td>{s.truth ? <TruthChip level={s.truth} /> : "—"}</td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

function Vulnerabilities({ onOpenService, onOpenNode, chartStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [sev, setSev] = useStateD("all");
  const hashCve = (((location.hash || "").split("?")[1] || "").match(/cve=([^&]+)/) || [])[1];
  const [openVuln, setOpenVuln] = useStateD(hashCve ? decodeURIComponent(hashCve) : null);
  const pool = verifiedOnly ? D.vulns.filter((v) => v.prov !== "inferred") : D.vulns;
  const hidden = D.vulns.length - pool.length;
  const rows = pool.filter((v) => sev === "all" || v.severity === sev).slice().sort((a, b) => b.cvss - a.cvss);
  const sevCount = { critical: 0, high: 0, medium: 0, low: 0 };
  pool.forEach((v) => sevCount[v.severity]++);
  const kevCount = pool.filter((v) => v.kev).length;
  const fixable = pool.filter((v) => v.fixAvailable).length;
  const byEco = {};pool.forEach((v) => byEco[v.ecosystem] = (byEco[v.ecosystem] || 0) + 1);
  const ecoColor = { go: "#14b8a6", npm: "#f0506e", pypi: "#a78bfa", maven: "#f472b6", deb: "#ff9d2e", apk: "#4f8cff" };
  const activeVuln = openVuln ? D.vulns.find((v) => v.cve === openVuln) : null;
  if (activeVuln) return <VulnDetail v={activeVuln} D={D} onBack={() => { setOpenVuln(null); if ((location.hash || "").indexOf("?") >= 0) location.hash = "vulnerabilities"; }} onOpenService={onOpenService} onOpenNode={onOpenNode} />;

  return (
    <div className="page">
      <div className="page-intro"><h2>Vulnerabilities</h2><p>CVE register — vulnerability intelligence correlated to deployed images and reachable services, sourced from CISA KEV, FIRST EPSS, NVD, OSV and GHSA, and joined to the graph by <span className="mono">AFFECTED_BY</span> edges. This is the deep security view of the Findings worklist.</p></div>
      <FindingsTabs active="cves" />

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
            {rows.map((v) =>
            <tr key={v.cve} onClick={() => setOpenVuln(v.cve)} style={{ cursor: "pointer" }}>
                <td className="cell-stack"><span className="row" style={{ gap: 7 }}><span className="t-name vuln-link" style={{ fontSize: ".8rem" }}>{v.cve}</span>{v.kev ? <span className="kev-flag">KEV</span> : null}</span><small style={{ maxWidth: 260 }}>{v.title}</small></td>
                <td><span className="sev-tag" style={{ color: D.sev[v.severity] }}><i style={{ background: D.sev[v.severity] }} />{v.severity}</span></td>
                <td><span className="mono" style={{ fontSize: ".82rem", color: D.sev[v.severity] }}>{v.cvss}</span></td>
                <td><span className="score-bar"><i style={{ width: v.epss * 100 + "%", background: v.epss > 0.5 ? "var(--crit)" : "var(--med)" }} /></span> <span className="mono" style={{ fontSize: ".72rem", color: "var(--muted)" }}>{(v.epss * 100).toFixed(0)}%</span></td>
                <td className="cell-stack"><span className="t-mut mono" style={{ fontSize: ".78rem" }}>{v.pkg}</span><small>{v.version} · {v.ecosystem}</small></td>
                <td><div className="row wrap" style={{ gap: 5 }}>{v.services.slice(0, 3).map((s) => <button key={s} className="dep-chip" style={{ fontSize: ".7rem", padding: "3px 7px" }} onClick={(e) => {e.stopPropagation();onOpenService(s);}}>{s}</button>)}{v.services.length > 3 ? <span className="t-mut" style={{ fontSize: ".72rem" }}>+{v.services.length - 3}</span> : null}</div></td>
                <td>{v.fixAvailable ? <Badge tone="teal">{v.fixed}</Badge> : <Badge tone="crit">none</Badge>}</td>
                <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{v.source}</td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

Object.assign(window, { Explorer, Catalog, Findings, Vulnerabilities });
