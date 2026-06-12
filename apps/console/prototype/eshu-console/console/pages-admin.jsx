/* Eshu Console — operations/admin prototype page. */
function Admin({ source, data, onOpenCollector, onOpenNode }) {
  const D = data || ESHU,m = D.metrics,r = D.runtime;
  const apps = D.argocdApps;
  const indexedCount = apps.filter((a) => a.indexed).length;
  const live = source && source.mode === "live" && source.status === "connected";
  return (
    <div className="page">
      <div className="page-intro"><h2>Operations</h2><p>Eshu runtime and NornicDB graph-backend health. Ingestion pipeline, reducer queues, graph writes and query performance. Data source: <strong style={{ color: live ? "var(--teal)" : "var(--bone)" }}>{source ? source.mode === "demo" ? "demo (static extraction)" : live ? "live Eshu API" : "live (unreachable — demo fallback)" : "demo"}</strong>.</p></div>

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
          {apps.map((a) =>
          <div className={cx("argocd-app", a.indexed && "indexed")} key={a.name} title={a.indexed ? "Source indexed" : "Deploy-only — source not in workspace"}>
              <span className="row" style={{ gap: 7, minWidth: 0 }}><i style={{ width: 7, height: 7, borderRadius: 9, background: a.indexed ? "var(--teal)" : "var(--subtle)", flex: "none" }} /><span className="argocd-name">{a.name}</span></span>
              {a.kind === "portal" ? <span className="argocd-tag">portal</span> : null}
            </div>
          )}
        </div>
      </Panel>

      <Panel className="flush mt" title="Collectors" sub={D.collectors.length + " fact sources feeding the graph · click any collector to see what it produces"} glyph={<Icon.cloud />}>
        <div className="domain-strip">
          {Object.keys(COLLECTOR_DOMAIN).map((dom) => {
            const list = D.collectors.filter((c) => COLLECTOR_DOMAIN[dom].includes(c.kind));
            if (!list.length) return null;
            const cc = list.reduce((a, c) => { a[c.status] = (a[c.status] || 0) + 1; return a; }, {});
            return (
              <div className="domain-card" key={dom}>
                <div className="domain-card-top"><span className="domain-name">{dom}</span><span className="domain-count">{list.length}</span></div>
                <div className="domain-dots">
                  {cc.healthy ? <span style={{ color: "var(--teal)" }}><i style={{ background: "var(--teal)" }} />{cc.healthy}</span> : null}
                  {cc.degraded ? <span style={{ color: "var(--med)" }}><i style={{ background: "var(--med)" }} />{cc.degraded}</span> : null}
                  {cc.stale ? <span style={{ color: "var(--crit)" }}><i style={{ background: "var(--crit)" }} />{cc.stale}</span> : null}
                </div>
              </div>
            );
          })}
        </div>
        <table className="tbl collectors-tbl">
          <thead><tr><th>Collector</th><th>Instance</th><th>Status</th><th>Facts</th><th>Scopes</th><th>Latency</th><th>Cadence</th><th>Last run</th><th></th></tr></thead>
          <tbody>
            {Object.keys(COLLECTOR_DOMAIN).map((dom) => {
              const list = D.collectors.filter((c) => COLLECTOR_DOMAIN[dom].includes(c.kind));
              if (!list.length) return null;
              return (
                <React.Fragment key={dom}>
                  <tr className="group-row"><td colSpan={9}><span className="group-label">{dom}</span><span className="group-meta">{list.length} {list.length === 1 ? "collector" : "collectors"} · {fmt(list.reduce((a, c) => a + c.facts, 0))} facts</span></td></tr>
                  {list.map((c) => {
                    const k = D.collectorKinds[c.kind];
                    return (
                      <tr key={c.instance} className="collector-row" onClick={() => onOpenCollector && onOpenCollector(c)} style={{ cursor: "pointer" }}>
                        <td><span className="row" style={{ gap: 10 }}><CollectorGlyph kind={c.kind} /><span className="cell-stack"><span style={{ fontWeight: 600 }}>{k.label}</span><small>{c.note}</small></span></span></td>
                        <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{c.instance}</td>
                        <td><span className="status-pill" style={{ color: D.statusColor[c.status] }}><i style={{ background: D.statusColor[c.status] }} />{c.status}</span></td>
                        <td className="mono" style={{ fontSize: ".82rem" }}>{fmt(c.facts)}</td>
                        <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.scopes}</td>
                        <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.latencyMs ? c.latencyMs + "ms" : "—"}</td>
                        <td className="t-mut" style={{ fontSize: ".78rem" }}>{c.cadence}</td>
                        <td><FreshDot state={c.freshness} /><div className="t-mut mono" style={{ fontSize: ".72rem", marginTop: 2 }}>{c.lastRun}</div></td>
                        <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
                      </tr>);
                  })}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </Panel>
    </div>);

}

Object.assign(window, { Admin });
