/* Eshu Console — core pages: ServiceDrawer, Dashboard, Explorer. */
const { useState: useStateP, useMemo: useMemoP } = React;

/* shared: node inspector content */
function NodeInspector({ node, data, onOpenService }) {
  const D = data || ESHU;
  const ks = ESHU.kindStyle[node.kind] || {};
  const det = (D.nodeDetail || {})[node.id];
  const svc = node.kind === "service" ? (D.services || []).find((s) => s.id === node.id.replace("svc:", "")) : null;
  return (
    <div className="inspector">
      <div className="insp-head">
        <span className="cglyph" style={{ width: 30, height: 30, color: ks.color, borderColor: ks.color }}>{(ks.label || "?")[0]}</span>
        <div>
          <div className="insp-kind">{ks.label}</div>
          <div className="insp-title">{node.label}</div>
        </div>
      </div>
      {node.sub ? <div className="t-mut" style={{ fontSize: ".82rem", fontFamily: "var(--mono)" }}>{node.sub}</div> : null}
      <div className="row wrap" style={{ gap: 8 }}>
        <TruthChip level={det ? det.truth : svc ? svc.truth : "exact"} />
        <FreshDot state={det ? det.freshness : svc ? svc.freshness : "fresh"} />
      </div>
      {det ?
      <div>
          <div className="section-label">Typed evidence</div>
          <div className="insp-evi">{det.evidence.map((e, i) => <div className="insp-evi-row" key={i}>{e}</div>)}</div>
        </div> :

      <div className="insp-evi"><div className="insp-evi-row">{node.label} resolved from canonical graph</div></div>
      }
      {svc ? <button className="btn-ghost active" style={{ width: "100%", justifyContent: "center" }} onClick={() => onOpenService(svc.id)}>Open service spotlight →</button> : null}
    </div>);

}

/* build a blast-radius graph: transitive dependents that break if `s` fails */
function buildBlastGraph(D, s) {
  if (!s) return { nodes: [], edges: [] };
  const rev = {};
  D.services.forEach((svc) => (svc.deps || []).forEach((d) => {(rev[d] = rev[d] || []).push(svc.id);}));
  const dist = { [s.id]: 0 };
  const q = [s.id];
  const edges = [];
  const seen = new Set();
  while (q.length) {
    const cur = q.shift();
    if (dist[cur] >= 3) continue;
    (rev[cur] || []).forEach((dep) => {
      const ek = dep + ">" + cur;
      if (!seen.has(ek)) {edges.push({ s: dep, t: cur, verb: "DEPENDS_ON", layer: "runtime" });seen.add(ek);}
      if (dist[dep] === undefined) {dist[dep] = dist[cur] + 1;q.push(dep);}
    });
  }
  const nodes = Object.keys(dist).map((nid) => {
    const svc = D.servicesById ? D.servicesById[nid] : null;
    return {
      id: nid, kind: nid === s.id ? "service" : svc && svc.kind === "lib" ? "library" : "service",
      label: svc && svc.name || nid,
      sub: dist[nid] === 0 ? "impact origin" : "hop " + dist[nid] + (svc && svc.tier ? " · " + svc.tier : ""),
      hero: nid === s.id, truth: svc ? svc.truth : "exact"
    };
  });
  return { nodes, edges };
}

/* direct callers/importers: who depends on `s` (1 hop) */
function buildCallersGraph(D, s) {
  if (!s) return { nodes: [], edges: [] };
  const callers = D.services.filter((svc) => (svc.deps || []).includes(s.id));
  const isLib = s.kind === "lib";
  const nodes = [{ id: s.id, kind: isLib ? "library" : "service", label: s.name, sub: isLib ? "imported library" : "this service", hero: true, truth: s.truth }];
  const edges = [];
  callers.forEach((c) => {
    nodes.push({ id: c.id, kind: c.kind === "lib" ? "library" : "service", label: c.name, sub: (c.tier || "") + (c.system ? " · " + c.system : ""), truth: c.truth });
    edges.push({ s: c.id, t: s.id, verb: isLib ? "IMPORTS" : "DEPENDS_ON", layer: isLib ? "code" : "runtime" });
  });
  return { nodes, edges };
}
function ServiceDrawer({ id, onClose, onOpenService, onOpenVuln, onOpenNode, data, source }) {
  const D = data || ESHU;
  const liveMode = source && source.mode === "live" && source.status === "connected";
  const [blastOpen, setBlastOpen] = useStateP(false);
  const [callersOpen, setCallersOpen] = useStateP(false);
  const [netOpen, setNetOpen] = useStateP(false);
  const [findingsOpen, setFindingsOpen] = useStateP(true);
  const [expandedCve, setExpandedCve] = useStateP(null);
  const bodyRef = React.useRef(null);
  const callersRef = React.useRef(null);
  function openCallers() {
    setCallersOpen(true);
    setTimeout(() => { if (bodyRef.current && callersRef.current) bodyRef.current.scrollTo({ top: Math.max(0, callersRef.current.offsetTop - 70), behavior: "smooth" }); }, 70);
  }
  const s = D.services.find((x) => x.id === id);
  const blastGraph = useMemoP(() => buildBlastGraph(D, s), [D, s]);
  const callersGraph = useMemoP(() => buildCallersGraph(D, s), [D, s]);
  const netGraph = useMemoP(() => buildServiceNetwork(D, s), [D, s]);
  if (!s) return null;
  const langInfo = ESHU.lang[s.lang];
  const isLib = s.kind === "lib";
  const stages = isLib ?
  [
  { title: "Source", items: [{ label: s.repo.split("/").pop(), sub: s.host + " · " + s.repo, verb: "REPO", color: "#f3ebdd", evidence: ["INDEXED_FROM " + s.host + ":" + s.repo, "default branch: main", "language: " + langInfo.label] }] },
  { title: "Publish", items: [{ label: "@acme/" + s.name, sub: "npm · v" + s.version, verb: "PUBLISHES", color: "#2dd4bf", evidence: ["PUBLISHES @acme/" + s.name + "@" + s.version, "registry: npm (private)", "provenance: build attestation"] }] },
  { title: "Consumers", items: [{ label: s.callers + " importers", sub: "across the estate", verb: "IMPORTS", color: "#c4b59a", action: openCallers }] }] :

  [
  { title: "Source", items: [{ label: s.repo.split("/").pop(), sub: s.host + " · " + s.repo, verb: "REPO", color: "#f3ebdd", evidence: ["INDEXED_FROM " + s.host + ":" + s.repo, "default branch: main", "owner: " + s.owner] }] },
  { title: "Build", items: [{ label: s.image ? s.image.split(":").pop() : s.version, sub: "ECR image", verb: "DEPLOYS_FROM", color: "#22d3ee", evidence: ["DEPLOYS_FROM " + (s.image || s.name + ":" + s.version), "registry: ECR", "base: node:20-alpine"] }] },
  { title: "Workload", items: [{ label: "Deployment", sub: "ns: api-node" + (s.port ? " :" + s.port : ""), verb: "RUNS_AS", color: "#4f8cff", evidence: ["RUNS_AS Deployment/" + s.name, "namespace: api-node", s.port ? "containerPort: " + s.port : "ClusterIP service"] }] },
  { title: "Runtime", items: s.envs.length ? s.envs.map((e) => ({ label: e, sub: "EKS · us-east-1", verb: "RUNS_IN", color: "#9ca3af", drillTo: null, evidence: ["RUNS_IN " + e, "cluster: eks-" + e, "region: us-east-1", "managed by ArgoCD"] })) : [{ label: "—", sub: "no runtime indexed", verb: "", color: "#9ca3af" }] }];

  const relVulns = D.vulns.filter((v) => v.services.includes(s.id));
  // unify findings the drawer can actually enumerate: CVE records + entity findings.
  const svcFindings = relVulns.map((v) => ({ kind: "cve", severity: v.severity, vuln: v, key: v.cve }))
  .concat((D.findings || []).filter((f) => f.entity === s.id).map((f) => ({ kind: "finding", severity: f.severity, finding: f, key: f.id })));
  const sevCounts = svcFindings.reduce((a, f) => {if (a[f.severity] != null) a[f.severity]++;return a;}, { critical: 0, high: 0, medium: 0, low: 0 });
  const findingsTotal = svcFindings.length;
  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label={s.name + " spotlight"}>
        <div className="drawer-head">
          <div className="row" style={{ gap: 12 }}>
            <span className="cglyph" style={{ width: 34, height: 34, color: langInfo.color, borderColor: langInfo.color, fontSize: ".6rem" }}>{langInfo.label.slice(0, 2)}</span>
            <div>
              <div className="row" style={{ gap: 8 }}>
                <strong style={{ fontFamily: "var(--mono)", fontSize: "1.02rem" }}>{s.name}</strong>
                <span className={"tag-tier tier-" + s.tier}>{s.tier}</span>
              </div>
              <div className="t-mut" style={{ fontSize: ".76rem" }}>{s.repo} · {s.owner}</div>            </div>
          </div>
          <button className="drawer-close" onClick={onClose} aria-label="Close"><Icon.close size={16} /></button>
        </div>
        <div className="drawer-body" ref={bodyRef}>
          <div className="row wrap" style={{ gap: 10 }}>
            <TruthChip level={s.truth} /><FreshDot state={s.freshness} />
            <Badge tone="neutral" dot color={langInfo.color}>{langInfo.label}</Badge>
            <Badge tone="violet">{s.system}</Badge>
            <Badge tone="neutral">{Math.round(s.coverage * 100)}% coverage</Badge>
          </div>
          <p style={{ color: "var(--muted)", lineHeight: 1.6, margin: 0 }}>{s.story}</p>

          <div className="meta-dl" style={{ gridTemplateColumns: "repeat(4,1fr)" }}>
            <button type="button" className={"meta-cell meta-clickable" + (callersOpen ? " is-open" : "")} onClick={() => setCallersOpen((o) => !o)} title={"Expand " + (isLib ? "importers" : "callers")}>
              <dt>{isLib ? "Importers" : "Callers"} <span className="meta-expand">{callersOpen ? "▾" : "▸"}</span></dt><dd>{s.callers}</dd>
            </button>
            <div><dt>Calls out</dt><dd>{s.calls}</dd></div>
            <button type="button" className={"meta-cell meta-clickable" + (blastOpen ? " is-open" : "")} onClick={() => setBlastOpen((o) => !o)} title="Expand impact graph">
              <dt>Blast radius <span className="meta-expand">{blastOpen ? "▾" : "▸"}</span></dt><dd>{s.blastRadius}</dd>
            </button>
            <div><dt>{isLib ? "Version" : "Environments"}</dt><dd>{isLib ? s.version : s.envs.length}</dd></div>
          </div>

          {callersOpen ?
          <div className="blast-expand" ref={callersRef}>
            <div className="section-label" style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}><span style={{ whiteSpace: "nowrap" }}>{isLib ? "Importers" : "Callers"}</span> <span className="t-mut" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>{isLib ? "repos that import" : "services that depend directly on"} {s.name} — click a node to open it</span></div>
            {callersGraph.nodes.length > 1 ?
            <GraphCanvas graph={callersGraph} data={D} layout="radial" height={300} onSelect={(n) => {if (D.servicesById && D.servicesById[n.id]) onOpenService(n.id);}} selectedId={s.id} /> :
            <p className="empty">No direct {isLib ? "importers" : "callers"} indexed for {s.name}.</p>}
          </div> :
          null}

          {blastOpen ?
          <div className="blast-expand">
            <div className="section-label" style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}><span style={{ whiteSpace: "nowrap" }}>Impact graph</span> <span className="t-mut" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>transitive dependents that break if {s.name} fails — click a node to open it</span></div>
            {blastGraph.nodes.length > 1 ?
            <GraphCanvas graph={blastGraph} data={D} layout="radial" height={300} onSelect={(n) => {if (D.servicesById && D.servicesById[n.id]) onOpenService(n.id);}} selectedId={s.id} /> :
            <p className="empty">No downstream dependents indexed — {s.name} is a leaf in the current graph.</p>}
          </div> :
          null}

          {!isLib ? (
          <div>
            <div className="row" style={{ justifyContent: "space-between", marginBottom: 8 }}>
              <div className="section-label" style={{ margin: 0 }}>Cloud network</div>
              <button type="button" className="findings-toggle" onClick={() => setNetOpen((o) => !o)} title="Show network topology">{Math.max(0, netGraph.nodes.length - 1)} resources <span>{netOpen ? "▾" : "▸"}</span></button>
            </div>
            {netOpen ? (
              <div className="node-hood">
                <GraphCanvas graph={netGraph} data={D} layout="radial" height={300} onSelect={(n) => onOpenNode && onOpenNode(n, netGraph)} selectedId={"svc:" + s.id} />
              </div>
            ) : <p className="t-mut" style={{ fontSize: ".82rem", margin: 0, lineHeight: 1.5 }}>VPC, security group, IRSA role &amp; datastores for {s.name} — every node declared by Terraform and clickable. Expand to see the topology.</p>}
          </div>
          ) : null}

          <div>
            <div className="section-label">Deployment path</div>
            <LaneFlow stages={stages} onDrill={onOpenService} />
          </div>

          <div>
            <div className="section-label">{isLib ? "Imports" : "Runtime dependencies"}</div>
            <div className="row wrap" style={{ gap: 8 }}>
              {s.deps.length ? s.deps.map((d) => <button className="dep-chip" key={d} onClick={() => onOpenService(d)}><i style={{ width: 6, height: 6, borderRadius: 9, background: "var(--teal)" }} />{d}</button>) : <span className="empty" style={{ padding: 0 }}>No internal dependencies indexed.</span>}
            </div>
            {s.stores && s.stores.length ?
            <div className="row wrap" style={{ gap: 8, marginTop: 10 }}>
                {s.stores.map((d) => <span className="dep-chip" key={d} style={{ borderColor: "color-mix(in oklab, #f59e0b 40%, var(--line))" }}><i style={{ width: 6, height: 6, borderRadius: 2, background: "#f59e0b" }} />{d}</span>)}
              </div> :
            null}
          </div>

          <div>
            <div className="row" style={{ justifyContent: "space-between", marginBottom: 8 }}>
              <div className="section-label" style={{ margin: 0 }}>Security posture</div>
              <button type="button" className="findings-toggle" data-comment-anchor="security-findings-count" onClick={() => setFindingsOpen((o) => !o)} title="Show findings">{findingsTotal} {findingsTotal === 1 ? "finding" : "findings"} <span>{findingsOpen ? "▾" : "▸"}</span></button>
            </div>
            {findingsTotal ?
            <>
            <SeverityBar counts={sevCounts} sev={ESHU.sev} />
            <div className="row wrap" style={{ gap: 14, marginTop: 10 }}>
              {["critical", "high", "medium", "low"].map((k) =>
              <span key={k} className="sev-tag" style={{ color: ESHU.sev[k], opacity: sevCounts[k] ? 1 : 0.4 }}><i style={{ background: ESHU.sev[k] }} />{sevCounts[k]} {k}</span>
              )}
            </div>
            </> :
            <p className="empty" style={{ padding: "6px 0 0", textAlign: "left" }}>No findings indexed for {s.name} — source, build &amp; deploy evidence is clean.</p>}
            {findingsOpen && findingsTotal ?
            <div className="insp-evi mt vuln-list">
                {svcFindings.map((item) => {
                if (item.kind === "finding") {
                  const f = item.finding;
                  const open = expandedCve === f.id;
                  return (
                    <div className={"vuln-item" + (open ? " is-open" : "")} key={f.id}>
                      <button type="button" className="vuln-row" onClick={() => setExpandedCve(open ? null : f.id)}>
                        <span className="vuln-caret">{open ? "▾" : "▸"}</span>
                        <span className="vuln-cve" style={{ color: ESHU.sev[f.severity] }}>{f.type}</span>
                        <span className="t-mut vuln-pkg">{f.title}</span>
                        <span className="vuln-cvss" style={{ color: ESHU.sev[f.severity] }}>{f.severity}</span>
                      </button>
                      {open ?
                      <div className="vuln-detail">
                          <p className="vuln-title">{f.detail}</p>
                          <div className="vuln-sub">source {f.source} · <TruthChip level={f.truth} /> · {f.age} old</div>
                        </div> : null}
                    </div>);
                }
                const v = item.vuln;
                const open = expandedCve === v.cve;
                return (
                  <div className={"vuln-item" + (open ? " is-open" : "")} key={v.cve}>
                      <button type="button" className="vuln-row" onClick={() => setExpandedCve(open ? null : v.cve)}>
                        <span className="vuln-caret">{open ? "▾" : "▸"}</span>
                        <span className="vuln-cve">{v.cve}</span>
                        <span className="t-mut vuln-pkg">{v.pkg}</span>
                        {v.kev ? <span className="kev-flag">KEV</span> : null}
                        <span className="vuln-cvss" style={{ color: ESHU.sev[v.severity] }}>{v.cvss}</span>
                      </button>
                      {open ?
                    <div className="vuln-detail">
                          <p className="vuln-title">{v.title}</p>
                          <div className="vuln-meta">
                            <div><dt>Severity</dt><dd style={{ color: ESHU.sev[v.severity] }}>{v.severity}</dd></div>
                            <div><dt>CVSS</dt><dd>{v.cvss}</dd></div>
                            <div><dt>EPSS</dt><dd>{Math.round((v.epss || 0) * 100)}%</dd></div>
                            <div><dt>Fix</dt><dd style={{ color: v.fixAvailable ? "var(--teal)" : "var(--crit)" }}>{v.fixAvailable ? v.fixed : "none"}</dd></div>
                          </div>
                          <div className="vuln-sub"><span className="mono">{v.pkg}@{v.version}</span> · {v.ecosystem} · source {v.source}</div>
                          {v.services && v.services.length ?
                      <div className="vuln-affected">
                              <span className="t-mut" style={{ fontSize: ".7rem" }}>Affected services</span>
                              <div className="row wrap" style={{ gap: 6, marginTop: 5 }}>{v.services.map((sid) => <button key={sid} className="dep-chip" onClick={() => onOpenService(sid)}>{D.servicesById && D.servicesById[sid] && D.servicesById[sid].name || sid}</button>)}</div>
                            </div> :
                      null}
                          {onOpenVuln ? <button type="button" className="vuln-fulllink" onClick={() => onOpenVuln(v.cve)}>View full vulnerability →</button> : null}
                        </div> :
                    null}
                    </div>);

              })}
              </div> :

            null}
          </div>
          <p className="t-mut" style={{ fontSize: ".72rem", borderTop: "1px solid var(--line)", paddingTop: 14, margin: 0, lineHeight: 1.5 }}>
            <span className="mono" style={{ color: "var(--subtle)" }}>provenance</span> · {liveMode ? "Live service spotlight: source, build, deploy, vulnerability, incident and runtime freshness facts come from the connected Eshu API where each route has live coverage." : "Demo service spotlight: source, build and deploy facts read from the bundled workspace; vulnerability, incident and runtime-freshness signals are representative of what Eshu's collectors would attach live."}
          </p>
        </div>
      </aside>
    </>);

}

/* ================================================================ DASHBOARD */
function Dashboard({ onOpenService, onOpenNode, heroMode, graphStyle, chartStyle, data }) {
  const D = data || ESHU;
  const hero = D.graph.nodes.find((n) => n.hero);
  const [sel, setSel] = useStateP(hero ? { type: "node", node: hero } : null);
  const selectNode = (n) => n && setSel({ type: "node", node: n });
  const selectEdge = (e) => e && setSel({ type: "edge", edge: e });
  const totalFindings = D.findings.length;
  const critFindings = D.findings.filter((f) => f.severity === "critical");
  const collectorCounts = D.collectors.reduce((a, c) => {a[c.status] = (a[c.status] || 0) + 1;return a;}, {});

  const stat = [
  { label: "Graph nodes", value: fmt(D.runtime.nodes), spark: D.metrics.graphNodes, color: "var(--teal)", trend: { dir: "up", text: "+2.1%" }, sub: "NornicDB · " + D.runtime.backendVersion },
  { label: "Relationships", value: fmt(D.runtime.edges), spark: D.metrics.graphEdges, color: "var(--ember)", trend: { dir: "up", text: "+3.4%" }, sub: D.relationships.length + " typed verbs observed" },
  { label: "Indexed repos", value: D.runtime.repos, spark: D.metrics.ingestRate, color: "var(--blue)", trend: { dir: "flat", text: "0" }, sub: D.runtime.services + " services · " + D.runtime.workloads + " workloads", onClick: () => { window.ESHU_ROUTES.setHash("repos"); }, cta: "Browse" },
  { label: "Queue outstanding", value: D.runtime.queueOutstanding, spark: D.metrics.queueDepth, color: "var(--violet)", trend: { dir: "down", text: "−18%" }, sub: D.runtime.inFlight + " in-flight · " + D.runtime.deadLetters + " dead-letter" }];


  const relRows = D.relationships.slice().sort((a, b) => b.count - a.count).slice(0, 8).map((r) => ({ label: r.verb, value: r.count, color: D.layerColor[r.layer], detail: r.detail }));
  const vulnByService = D.services.map((s) => ({ label: s.name, value: s.crit * 4 + s.high * 2 + s.med, color: s.crit ? "var(--crit)" : s.high ? "var(--high)" : "var(--med)" })).sort((a, b) => b.value - a.value).slice(0, 6);
  const sevTotals = D.services.reduce((a, s) => {a.critical += s.crit;a.high += s.high;a.medium += s.med;a.low += s.low;return a;}, { critical: 0, high: 0, medium: 0, low: 0 });

  return (
    <div className="page">
      <div className="grid g-4">
        {stat.map((s) => <StatTile key={s.label} {...s} />)}
      </div>

      {/* HERO */}
      {heroMode === "health" ?
      <div className="grid g-2 mt" style={{ gridTemplateColumns: "minmax(0,0.9fr) minmax(0,1.1fr)" }}>
          <RunHealthPanel />
          <Panel title="NornicDB query latency" sub="p50 / p95 / p99 over the last 24h" glyph={<Icon.db />}>
            <MultiLine seriesList={[
          { label: "p50", data: D.metrics.queryP50, color: "var(--teal)" },
          { label: "p95", data: D.metrics.queryP95, color: "var(--ember)" },
          { label: "p99", data: D.metrics.queryP99, color: "var(--crit)" }]
          } h={184} unit="ms" />
            <div className="chart-legend">
              <span><i style={{ background: "var(--teal)" }} />p50 {D.metrics.queryP50.at(-1)}ms</span>
              <span><i style={{ background: "var(--ember)" }} />p95 {D.metrics.queryP95.at(-1)}ms</span>
              <span><i style={{ background: "var(--crit)" }} />p99 {D.metrics.queryP99.at(-1)}ms</span>
            </div>
          </Panel>
        </div> :
      heroMode === "spotlight" ?
      <div className="mt"><FeaturedService onOpenService={onOpenService} /></div> :

      <Panel className="mt" title="Code-to-cloud relationship atlas" sub="svc-catalog neighbourhood — click any node or relationship edge to read its evidence" glyph={<Icon.graph />}
      action={<button className="btn-ghost" onClick={() => onOpenService("svc-catalog")}>Open spotlight →</button>}>
          <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) 300px", gap: "var(--gap)", alignItems: "start" }}>
            <GraphCanvas graph={D.graph} data={D} layout={graphStyle} height={500}
              onSelect={selectNode} onSelectEdge={selectEdge} onClear={() => setSel(null)}
              selectedId={sel && sel.type === "node" ? sel.node.id : null}
              selectedEdge={sel && sel.type === "edge" ? sel.edge : null} />
            <div className="panel" style={{ background: "var(--bg-field)", boxShadow: "none" }}>
              <div className="panel-body"><GraphInspector data={D} sel={sel} graph={D.graph} onOpenService={onOpenService} onOpenNode={onOpenNode} onSelectNode={selectNode} onSelectEdge={selectEdge} emptyHint="Select any node or relationship edge to read its evidence." /></div>
            </div>
          </div>
        </Panel>
      }

      {/* throughput + collectors */}
      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.5fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="Ingestion throughput" sub="Facts committed per minute across all collectors" glyph={<Icon.pulse />}
        action={<div className="seg"><button className="active">24h</button><button>7d</button><button>30d</button></div>}>
          <AreaChart data={ESHU.metrics.ingestRate} color="var(--teal)" h={190} unit=" f/m" labels={ESHU.metrics.ingestRate.map((_, i) => `t-${ESHU.metrics.ingestRate.length - 1 - i}`)} />
        </Panel>
        <Panel title="Collector health" sub={D.collectors.length + " collectors feeding the graph"} glyph={<Icon.layers />}
        action={<a className="btn-ghost" href={window.ESHU_ROUTES.hashFor("admin")}>All</a>}>
          <div className="health-row">
            <Donut size={120} thickness={15} segments={[
            { label: "healthy", value: collectorCounts.healthy || 0, color: "var(--teal)" },
            { label: "degraded", value: collectorCounts.degraded || 0, color: "var(--med)" },
            { label: "stale", value: collectorCounts.stale || 0, color: "var(--crit)" }]
            } center={{ value: D.collectors.length, label: "collectors" }} />
            <div className="kv-list">
              <div className="kv"><span><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 9, background: "var(--teal)", marginRight: 7 }} />Healthy</span><strong>{collectorCounts.healthy || 0}</strong></div>
              <div className="kv"><span><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 9, background: "var(--med)", marginRight: 7 }} />Degraded</span><strong>{collectorCounts.degraded || 0}</strong></div>
              <div className="kv"><span><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 9, background: "var(--crit)", marginRight: 7 }} />Stale</span><strong>{collectorCounts.stale || 0}</strong></div>
              <div className="kv" style={{ borderTop: "1px solid var(--line)", paddingTop: 9 }}><span>Facts / 24h</span><strong style={{ color: "var(--teal-bright)" }}>+1.42M</strong></div>
            </div>
          </div>
        </Panel>
      </div>

      {/* relationships + security */}
      <div className="grid g-3 mt">
        <Panel title="Relationship coverage" sub="Most-observed typed verbs" glyph={<Icon.branch />} className="span-2">
          {chartStyle === "donut" ?
          <div className="health-row">
              <Donut size={130} thickness={16} segments={D.relationships.slice(0, 6).map((r) => ({ label: r.verb, value: r.count, color: D.layerColor[r.layer] }))} center={{ value: String(D.relationships.length), label: "verbs" }} />
              <div className="kv-list">{relRows.slice(0, 6).map((r) => <div className="kv" key={r.label}><span className="mono" style={{ fontSize: ".78rem" }}><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: r.color, marginRight: 7 }} />{r.label}</span><strong>{fmt(r.value)}</strong></div>)}</div>
            </div> :
          <BarRows rows={relRows} />}
        </Panel>
        <Panel title="Security posture" sub={sevTotals.critical + " critical across the fleet"} glyph={<Icon.shield />}
        action={<a className="btn-ghost" href="#vulnerabilities">Triage →</a>}>
          <div style={{ display: "grid", placeItems: "center", marginBottom: 14 }}>
            <Donut size={138} thickness={17} segments={[
            { label: "critical", value: sevTotals.critical, color: ESHU.sev.critical },
            { label: "high", value: sevTotals.high, color: ESHU.sev.high },
            { label: "medium", value: sevTotals.medium, color: ESHU.sev.medium },
            { label: "low", value: sevTotals.low, color: ESHU.sev.low }]
            } center={{ value: sevTotals.critical + sevTotals.high, label: "crit + high" }} />
          </div>
          <div className="row wrap" style={{ gap: 12, justifyContent: "center" }}>
            {["critical", "high", "medium", "low"].map((k) => <span key={k} className="sev-tag" style={{ color: ESHU.sev[k] }}><i style={{ background: ESHU.sev[k] }} />{sevTotals[k]}</span>)}
          </div>
        </Panel>
      </div>

      {/* critical findings */}
      <Panel className="mt flush" title="Needs attention" sub="Highest-severity findings with evidence" glyph={<Icon.findings />}
      action={<a className="btn-ghost" href="#findings">All findings ({totalFindings}) →</a>}>
        <table className="tbl">
          <thead><tr><th>Severity</th><th>Finding</th><th>Entity</th><th>Source</th><th>Truth</th><th>Age</th></tr></thead>
          <tbody>
            {D.findings.filter((f) => f.severity === "critical" || f.severity === "high").map((f) =>
            <tr key={f.id} onClick={() => {const svc = D.services.find((s) => s.id === f.entity);if (svc) onOpenService(svc.id);}}>
                <td><span className="sev-tag" style={{ color: ESHU.sev[f.severity] }}><i style={{ background: ESHU.sev[f.severity] }} />{f.severity}</span></td>
                <td className="cell-stack"><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span><small>{f.type}</small></td>
                <td className="t-name">{f.entity}</td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.source}</td>
                <td><TruthChip level={f.truth} /></td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.age}</td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

function RunHealthPanel() {
  const r = ESHU.runtime;
  const items = [
  ["Index status", r.indexStatus, "var(--teal)"],
  ["Graph backend", r.backend + " " + r.backendVersion, null],
  ["Uptime", r.uptime, null],
  ["Cloud resources", fmt(r.cloudResources), null],
  ["Queue outstanding", r.queueOutstanding, null],
  ["In flight", r.inFlight, null],
  ["Dead letters", r.deadLetters, r.deadLetters > 10 ? "var(--crit)" : "var(--teal)"],
  ["Succeeded (24h)", fmt(r.succeeded), null]];

  return (
    <Panel title="Run readiness" sub={"Profile: " + r.profile} glyph={<Icon.bolt />}>
      <div className="meta-dl">
        {items.map(([k, v, c]) => <div key={k}><dt>{k}</dt><dd style={c ? { color: c } : null}>{v}</dd></div>)}
      </div>
    </Panel>);

}

function FeaturedService({ onOpenService }) {
  const s = ESHU.services[0];
  return (
    <Panel title="Service spotlight" sub="Featured tier-1 service" glyph={<Icon.box />}
    action={<button className="btn-ghost active" onClick={() => onOpenService(s.id)}>Full spotlight →</button>}>
      <div className="spotlight-hero">
        <div>
          <div className="row" style={{ gap: 10 }}>
            <strong style={{ fontSize: "1.3rem", fontFamily: "var(--mono)" }}>{s.name}</strong>
            <span className={"tag-tier tier-" + s.tier}>{s.tier}</span>
            <TruthChip level={s.truth} /><FreshDot state={s.freshness} />
          </div>
          <p className="sh-story">{s.story}</p>
          <div className="row wrap mt" style={{ gap: 8 }}>{s.deps.map((d) => <button className="dep-chip" key={d} onClick={() => onOpenService(d)}>{d}</button>)}</div>
        </div>
        <div className="meta-dl" style={{ gridTemplateColumns: "1fr 1fr" }}>
          <div><dt>Callers</dt><dd>{s.callers}</dd></div>
          <div><dt>Blast radius</dt><dd>{s.blastRadius}</dd></div>
          <div><dt>Critical</dt><dd style={{ color: "var(--crit)" }}>{s.crit}</dd></div>
          <div><dt>Coverage</dt><dd>{Math.round(s.coverage * 100)}%</dd></div>
        </div>
      </div>
    </Panel>);

}

Object.assign(window, { ServiceDrawer, Dashboard, NodeInspector });
