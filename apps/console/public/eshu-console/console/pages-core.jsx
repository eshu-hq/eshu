/* Eshu Console — core pages: ServiceDrawer, Dashboard, Explorer. */
const { useState: useStateP, useMemo: useMemoP } = React;

/* shared: node inspector content */
function NodeInspector({ node, onOpenService }) {
  const ks = ESHU.kindStyle[node.kind] || {};
  const det = ESHU.nodeDetail[node.id];
  const svc = node.kind === "service" ? ESHU.services.find((s) => s.id === node.id.replace("svc:", "")) : null;
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
        <TruthChip level={det ? det.truth : (svc ? svc.truth : "exact")} />
        <FreshDot state={det ? det.freshness : (svc ? svc.freshness : "fresh")} />
      </div>
      {det ? (
        <div>
          <div className="section-label">Typed evidence</div>
          <div className="insp-evi">{det.evidence.map((e, i) => <div className="insp-evi-row" key={i}>{e}</div>)}</div>
        </div>
      ) : (
        <div className="insp-evi"><div className="insp-evi-row">{node.label} resolved from canonical graph</div></div>
      )}
      {svc ? <button className="btn-ghost active" style={{ width: "100%", justifyContent: "center" }} onClick={() => onOpenService(svc.id)}>Open service spotlight →</button> : null}
    </div>
  );
}

/* ============================================================ SERVICE DRAWER */
function ServiceDrawer({ id, onClose, onOpenService, data }) {
  const D = data || ESHU;
  const s = D.services.find((x) => x.id === id);
  if (!s) return null;
  const langInfo = ESHU.lang[s.lang];
  const sevCounts = { critical: s.crit, high: s.high, medium: s.med, low: s.low };
  const isLib = s.kind === "lib";
  const stages = isLib
    ? [
      { title: "Source", items: [{ label: s.repo.split("/").pop(), sub: s.host + " · " + s.repo, verb: "REPO", color: "#f3ebdd" }] },
      { title: "Publish", items: [{ label: "@dmm/" + s.name, sub: "npm · v" + s.version, verb: "PUBLISHES", color: "#2dd4bf" }] },
      { title: "Consumers", items: [{ label: s.callers + " importers", sub: "across the estate", verb: "IMPORTS", color: "#c4b59a" }] }
    ]
    : [
      { title: "Source", items: [{ label: s.repo.split("/").pop(), sub: s.host + " · " + s.repo, verb: "REPO", color: "#f3ebdd" }] },
      { title: "Build", items: [{ label: s.image ? s.image.split(":").pop() : s.version, sub: "ECR image", verb: "DEPLOYS_FROM", color: "#22d3ee" }] },
      { title: "Workload", items: [{ label: "Deployment", sub: "ns: api-node" + (s.port ? " :" + s.port : ""), verb: "RUNS_AS", color: "#4f8cff" }] },
      { title: "Runtime", items: s.envs.length ? s.envs.map((e) => ({ label: e, sub: "EKS · us-east-1", verb: "RUNS_IN", color: "#9ca3af" })) : [{ label: "—", sub: "no runtime indexed", verb: "", color: "#9ca3af" }] }
    ];
  const relVulns = D.vulns.filter((v) => v.services.includes(s.id));
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
        <div className="drawer-body">
          <div className="row wrap" style={{ gap: 10 }}>
            <TruthChip level={s.truth} /><FreshDot state={s.freshness} />
            <Badge tone="neutral" dot color={langInfo.color}>{langInfo.label}</Badge>
            <Badge tone="violet">{s.system}</Badge>
            <Badge tone="neutral">{Math.round(s.coverage * 100)}% coverage</Badge>
          </div>
          <p style={{ color: "var(--muted)", lineHeight: 1.6, margin: 0 }}>{s.story}</p>

          <div className="meta-dl" style={{ gridTemplateColumns: "repeat(4,1fr)" }}>
            <div><dt>{isLib ? "Importers" : "Callers"}</dt><dd>{s.callers}</dd></div>
            <div><dt>Calls out</dt><dd>{s.calls}</dd></div>
            <div><dt>Blast radius</dt><dd>{s.blastRadius}</dd></div>
            <div><dt>{isLib ? "Version" : "Environments"}</dt><dd>{isLib ? s.version : s.envs.length}</dd></div>
          </div>

          <div>
            <div className="section-label">Deployment path</div>
            <LaneFlow stages={stages} />
          </div>

          <div>
            <div className="section-label">{isLib ? "Imports" : "Runtime dependencies"}</div>
            <div className="row wrap" style={{ gap: 8 }}>
              {s.deps.length ? s.deps.map((d) => <button className="dep-chip" key={d} onClick={() => onOpenService(d)}><i style={{ width: 6, height: 6, borderRadius: 9, background: "var(--teal)" }} />{d}</button>) : <span className="empty" style={{ padding: 0 }}>No internal dependencies indexed.</span>}
            </div>
            {s.stores && s.stores.length ? (
              <div className="row wrap" style={{ gap: 8, marginTop: 10 }}>
                {s.stores.map((d) => <span className="dep-chip" key={d} style={{ borderColor: "color-mix(in oklab, #f59e0b 40%, var(--line))" }}><i style={{ width: 6, height: 6, borderRadius: 2, background: "#f59e0b" }} />{d}</span>)}
              </div>
            ) : null}
          </div>

          <div>
            <div className="row" style={{ justifyContent: "space-between", marginBottom: 8 }}>
              <div className="section-label" style={{ margin: 0 }}>Security posture</div>
              <span className="t-mut" style={{ fontSize: ".74rem", fontFamily: "var(--mono)" }}>{s.crit + s.high + s.med + s.low} findings</span>
            </div>
            <SeverityBar counts={sevCounts} sev={ESHU.sev} />
            <div className="row wrap" style={{ gap: 14, marginTop: 10 }}>
              {["critical", "high", "medium", "low"].map((k) => (
                <span key={k} className="sev-tag" style={{ color: ESHU.sev[k] }}><i style={{ background: ESHU.sev[k] }} />{sevCounts[k]} {k}</span>
              ))}
            </div>
            {relVulns.length ? (
              <div className="insp-evi mt">
                {relVulns.slice(0, 4).map((v) => (
                  <div className="insp-evi-row" key={v.cve} style={{ justifyContent: "space-between" }}>
                    <span>{v.cve} · {v.pkg}</span>
                    <span style={{ color: ESHU.sev[v.severity], marginLeft: "auto" }}>{v.kev ? "KEV " : ""}{v.cvss}</span>
                  </div>
                ))}
              </div>
            ) : null}
          </div>
          <p className="t-mut" style={{ fontSize: ".72rem", borderTop: "1px solid var(--line)", paddingTop: 14, margin: 0, lineHeight: 1.5 }}>
            <span className="mono" style={{ color: "var(--subtle)" }}>provenance</span> · source, build & deploy facts read directly from <span className="mono">{s.repo}</span>. Vulnerability, incident & runtime-freshness signals are representative of what Eshu's collectors would attach live.
          </p>
        </div>
      </aside>
    </>
  );
}

/* ================================================================ DASHBOARD */
function Dashboard({ onOpenService, heroMode, graphStyle, chartStyle, data }) {
  const D = data || ESHU;
  const [sel, setSel] = useStateP(D.graph.nodes.find((n) => n.hero));
  const totalFindings = D.findings.length;
  const critFindings = D.findings.filter((f) => f.severity === "critical");
  const collectorCounts = D.collectors.reduce((a, c) => { a[c.status] = (a[c.status] || 0) + 1; return a; }, {});

  function handleSelect(n) {
    setSel(n);
    if (n.kind === "service") { /* keep inline; user opens via button */ }
  }

  const stat = [
    { label: "Graph nodes", value: fmt(D.runtime.nodes), spark: D.metrics.graphNodes, color: "var(--teal)", trend: { dir: "up", text: "+2.1%" }, sub: "NornicDB · " + D.runtime.backendVersion },
    { label: "Relationships", value: fmt(D.runtime.edges), spark: D.metrics.graphEdges, color: "var(--ember)", trend: { dir: "up", text: "+3.4%" }, sub: "12 typed verbs observed" },
    { label: "Indexed repos", value: D.runtime.repos, spark: D.metrics.ingestRate, color: "var(--blue)", trend: { dir: "flat", text: "0" }, sub: D.runtime.services + " services · " + D.runtime.workloads + " workloads" },
    { label: "Queue outstanding", value: D.runtime.queueOutstanding, spark: D.metrics.queueDepth, color: "var(--violet)", trend: { dir: "down", text: "−18%" }, sub: D.runtime.inFlight + " in-flight · " + D.runtime.deadLetters + " dead-letter" }
  ];

  const relRows = D.relationships.slice().sort((a, b) => b.count - a.count).slice(0, 8).map((r) => ({ label: r.verb, value: r.count, color: D.layerColor[r.layer], detail: r.detail }));
  const vulnByService = D.services.map((s) => ({ label: s.name, value: s.crit * 4 + s.high * 2 + s.med, color: s.crit ? "var(--crit)" : s.high ? "var(--high)" : "var(--med)" })).sort((a, b) => b.value - a.value).slice(0, 6);
  const sevTotals = D.services.reduce((a, s) => { a.critical += s.crit; a.high += s.high; a.medium += s.med; a.low += s.low; return a; }, { critical: 0, high: 0, medium: 0, low: 0 });

  return (
    <div className="page">
      <div className="grid g-4">
        {stat.map((s) => <StatTile key={s.label} {...s} />)}
      </div>

      {/* HERO */}
      {heroMode === "health" ? (
        <div className="grid g-2 mt" style={{ gridTemplateColumns: "minmax(0,0.9fr) minmax(0,1.1fr)" }}>
          <RunHealthPanel />
          <Panel title="NornicDB query latency" sub="p50 / p95 / p99 over the last 24h" glyph={<Icon.db />}>
            <MultiLine seriesList={[
              { label: "p50", data: D.metrics.queryP50, color: "var(--teal)" },
              { label: "p95", data: D.metrics.queryP95, color: "var(--ember)" },
              { label: "p99", data: D.metrics.queryP99, color: "var(--crit)" }
            ]} h={184} unit="ms" />
            <div className="chart-legend">
              <span><i style={{ background: "var(--teal)" }} />p50 {D.metrics.queryP50.at(-1)}ms</span>
              <span><i style={{ background: "var(--ember)" }} />p95 {D.metrics.queryP95.at(-1)}ms</span>
              <span><i style={{ background: "var(--crit)" }} />p99 {D.metrics.queryP99.at(-1)}ms</span>
            </div>
          </Panel>
        </div>
      ) : heroMode === "spotlight" ? (
        <div className="mt"><FeaturedService onOpenService={onOpenService} /></div>
      ) : (
        <Panel className="mt" title="Code-to-cloud relationship atlas" sub="api-node-boats neighbourhood — repo, image, client, runtime, infra, security & ops evidence" glyph={<Icon.graph />}
          action={<button className="btn-ghost" onClick={() => onOpenService("api-node-boats")}>Open spotlight →</button>}>
          <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) 300px", gap: "var(--gap)", alignItems: "start" }}>
            <GraphCanvas graph={D.graph} layout={graphStyle} height={500} onSelect={handleSelect} selectedId={sel && sel.id} />
            <div className="panel" style={{ background: "var(--bg-field)", boxShadow: "none" }}>
              <div className="panel-body">{sel ? <NodeInspector node={sel} onOpenService={onOpenService} /> : <p className="empty">Select a node to inspect its evidence.</p>}</div>
            </div>
          </div>
        </Panel>
      )}

      {/* throughput + collectors */}
      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.5fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="Ingestion throughput" sub="Facts committed per minute across all collectors" glyph={<Icon.pulse />}
          action={<div className="seg"><button className="active">24h</button><button>7d</button><button>30d</button></div>}>
          <AreaChart data={ESHU.metrics.ingestRate} color="var(--teal)" h={190} unit=" f/m" labels={ESHU.metrics.ingestRate.map((_, i) => `t-${ESHU.metrics.ingestRate.length - 1 - i}`)} />
        </Panel>
        <Panel title="Collector health" sub={D.collectors.length + " collectors feeding the graph"} glyph={<Icon.layers />}
          action={<a className="btn-ghost" href="#admin">All</a>}>
          <div className="health-row">
            <Donut size={120} thickness={15} segments={[
              { label: "healthy", value: collectorCounts.healthy || 0, color: "var(--teal)" },
              { label: "degraded", value: collectorCounts.degraded || 0, color: "var(--med)" },
              { label: "stale", value: collectorCounts.stale || 0, color: "var(--crit)" }
            ]} center={{ value: D.collectors.length, label: "collectors" }} />
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
          {chartStyle === "donut" ? (
            <div className="health-row">
              <Donut size={130} thickness={16} segments={D.relationships.slice(0, 6).map((r) => ({ label: r.verb, value: r.count, color: D.layerColor[r.layer] }))} center={{ value: "12", label: "verbs" }} />
              <div className="kv-list">{relRows.slice(0, 6).map((r) => <div className="kv" key={r.label}><span className="mono" style={{ fontSize: ".78rem" }}><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: r.color, marginRight: 7 }} />{r.label}</span><strong>{fmt(r.value)}</strong></div>)}</div>
            </div>
          ) : <BarRows rows={relRows} />}
        </Panel>
        <Panel title="Security posture" sub={sevTotals.critical + " critical across the fleet"} glyph={<Icon.shield />}
          action={<a className="btn-ghost" href="#vulnerabilities">Triage →</a>}>
          <div style={{ display: "grid", placeItems: "center", marginBottom: 14 }}>
            <Donut size={138} thickness={17} segments={[
              { label: "critical", value: sevTotals.critical, color: ESHU.sev.critical },
              { label: "high", value: sevTotals.high, color: ESHU.sev.high },
              { label: "medium", value: sevTotals.medium, color: ESHU.sev.medium },
              { label: "low", value: sevTotals.low, color: ESHU.sev.low }
            ]} center={{ value: sevTotals.critical + sevTotals.high, label: "crit + high" }} />
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
            {D.findings.filter((f) => f.severity === "critical" || f.severity === "high").map((f) => (
              <tr key={f.id} onClick={() => { const svc = D.services.find((s) => s.id === f.entity); if (svc) onOpenService(svc.id); }}>
                <td><span className="sev-tag" style={{ color: ESHU.sev[f.severity] }}><i style={{ background: ESHU.sev[f.severity] }} />{f.severity}</span></td>
                <td className="cell-stack"><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span><small>{f.type}</small></td>
                <td className="t-name">{f.entity}</td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.source}</td>
                <td><TruthChip level={f.truth} /></td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.age}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
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
    ["Succeeded (24h)", fmt(r.succeeded), null]
  ];
  return (
    <Panel title="Run readiness" sub={"Profile: " + r.profile} glyph={<Icon.bolt />}>
      <div className="meta-dl">
        {items.map(([k, v, c]) => <div key={k}><dt>{k}</dt><dd style={c ? { color: c } : null}>{v}</dd></div>)}
      </div>
    </Panel>
  );
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
    </Panel>
  );
}

Object.assign(window, { ServiceDrawer, Dashboard, NodeInspector });
