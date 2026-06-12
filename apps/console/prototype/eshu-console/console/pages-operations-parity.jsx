/* Live-parity override for the prototype Operations page.
   Loaded after pages-data.jsx so connected-live mode mirrors the live TSX
   console's supported metric contracts instead of showing demo-only telemetry. */
const DemoAdminPage = window.Admin;

function opsNumber(value, fallback) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function opsSeries(metrics, key) {
  const values = metrics && Array.isArray(metrics[key]) ? metrics[key] : [];
  return values.filter((value) => typeof value === "number" && Number.isFinite(value));
}

function opsLast(values) {
  return values.length ? values[values.length - 1] : null;
}

function opsLatencySummary(metrics) {
  const p50 = opsLast(opsSeries(metrics, "queryP50"));
  const p95 = opsLast(opsSeries(metrics, "queryP95"));
  const p99 = opsLast(opsSeries(metrics, "queryP99"));
  if (p50 === null && p95 === null && p99 === null) return "GET /api/v0/metrics/timeseries";
  return "p50 " + (p50 === null ? "-" : p50) + "ms | p95 " + (p95 === null ? "-" : p95) + "ms | p99 " + (p99 === null ? "-" : p99) + "ms";
}

function opsGraphRows(metrics) {
  const nodes = opsLast(opsSeries(metrics, "graphNodes"));
  const edges = opsLast(opsSeries(metrics, "graphEdges"));
  const rows = [];
  if (nodes !== null) rows.push({ label: "nodes", value: nodes, color: "var(--teal)" });
  if (edges !== null) rows.push({ label: "edges", value: edges, color: "var(--ember)" });
  return rows;
}

function opsCollectorStateColor(state) {
  if (state === "healthy" || state === "active") return "var(--teal)";
  if (state === "deactivated" || state === "stale") return "var(--crit)";
  return "var(--med)";
}

function opsCollectorRows(data) {
  return Array.isArray(data && data.collectors) ? data.collectors : [];
}

function opsLiveOperations({ data, source }) {
  const D = data || ESHU;
  const metrics = D.metrics || {};
  const runtime = D.runtime || {};
  const queueDepth = opsSeries(metrics, "queueDepth");
  const deadLetters = opsSeries(metrics, "deadLetters");
  const queryP99 = opsSeries(metrics, "queryP99");
  const graphNodes = opsSeries(metrics, "graphNodes");
  const graphEdges = opsSeries(metrics, "graphEdges");
  const graphTrend = graphEdges.length ? graphEdges : graphNodes;
  const graphRows = opsGraphRows(metrics);
  const collectors = opsCollectorRows(D);
  const indexStatus = source && source.live && source.live.indexStatus && source.live.indexStatus.status
    ? source.live.indexStatus.status
    : runtime.indexStatus || "live";

  return (
    <div className="page">
      <div className="page-intro"><h2>Operations</h2><p>Eshu runtime and NornicDB backend health from the live API. Connected live Operations renders explicit contract-pending states instead of demo-only telemetry.</p></div>

      <div className="grid g-4">
        <StatTile label="Index status" value={indexStatus} color="var(--teal)" sub={"profile " + (runtime.profile || "live")} />
        <StatTile label="Queue outstanding" value={opsNumber(runtime.queueOutstanding, opsLast(queueDepth) || 0)} spark={queueDepth.length ? queueDepth : undefined} color="var(--violet)" sub={opsNumber(runtime.inFlight, 0) + " in-flight"} />
        <StatTile label="Dead letters" value={opsNumber(runtime.deadLetters, opsLast(deadLetters) || 0)} spark={deadLetters.length ? deadLetters : undefined} color="var(--crit)" sub="needs replay" />
        <StatTile label="Succeeded" value={fmt(opsNumber(runtime.succeeded, 0))} color="var(--blue)" sub="work items (run)" />
      </div>

      <div className="grid g-2 mt">
        <Panel title="Reducer queue depth" sub="queueDepth from GET /api/v0/metrics/timeseries" glyph={<Icon.layers />}>
          {queueDepth.length ? <AreaChart data={queueDepth} color="var(--violet)" h={180} unit=" items" /> : <p className="empty" style={{ padding: "32px 12px" }}>Current queue counters are available above. Trend history appears when the metrics source has recent queue_depth samples.</p>}
        </Panel>
        <Panel title="Query latency" sub={opsLatencySummary(metrics)} glyph={<Icon.pulse />}>
          {queryP99.length ? <AreaChart data={queryP99} color="var(--crit)" h={180} unit="ms" /> : <p className="empty" style={{ padding: "32px 12px" }}>Query latency history appears when the metrics source has recent query_p99 samples.</p>}
        </Panel>
      </div>

      <Panel className="mt" title="Graph growth" sub="graphNodes and graphEdges from GET /api/v0/metrics/timeseries" glyph={<Icon.db />}>
        {graphTrend.length ? (
          <div className="grid g-2" style={{ alignItems: "center" }}>
            <AreaChart data={graphTrend} color={graphEdges.length ? "var(--ember)" : "var(--teal)"} h={180} />
            <BarRows rows={graphRows} />
          </div>
        ) : <p className="empty" style={{ padding: "32px 12px" }}>Graph growth history appears when the metrics source has recent graph_nodes or graph_edges samples.</p>}
      </Panel>

      <Panel className="mt" title="Metric contract pending" sub="Tracked in issue #2216" glyph={<Icon.shield />}>
        <p className="empty" style={{ padding: "12px 0", textAlign: "left" }}>write-throughput, cache-hit, and vulnerability-feed intake decorations do not have named live metric series yet. Connected-live mode keeps those demo-only sparklines out of Operations until issue #2216 defines source-backed contracts.</p>
      </Panel>

      <Panel className="flush mt" title="Collectors / ingesters" sub={collectors.length + " fact sources"} glyph={<Icon.cloud />}>
        <table className="tbl">
          <thead><tr><th>Collector</th><th>Instance</th><th>State</th><th>Facts</th><th>Freshness</th></tr></thead>
          <tbody>
            {collectors.map((collector) => {
              const state = collector.status || collector.state || "unknown";
              return (
                <tr key={collector.id || collector.instance || collector.kind}>
                  <td><span className="row" style={{ gap: 10 }}><Icon.layers /><span style={{ fontWeight: 600 }}>{collector.kind || "collector"}</span></span></td>
                  <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{collector.instance || collector.id || "-"}</td>
                  <td><span className="status-pill" style={{ color: opsCollectorStateColor(state) }}><i style={{ background: "currentColor" }} />{state}</span></td>
                  <td className="mono" style={{ fontSize: ".82rem" }}>{collector.facts === null || typeof collector.facts === "undefined" ? "-" : fmt(collector.facts)}</td>
                  <td><FreshDot state={collector.freshness === "stale" || collector.freshness === "unavailable" ? "stale" : "fresh"} /></td>
                </tr>
              );
            })}
            {collectors.length === 0 ? <tr><td colSpan={5} className="empty">No ingester status from this source.</td></tr> : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

function AdminParityPage(props) {
  const source = props && props.source;
  const connectedLive = source && source.mode === "live" && source.status === "connected";
  if (!connectedLive && DemoAdminPage) return <DemoAdminPage {...props} />;
  return opsLiveOperations(props || {});
}

window.Admin = AdminParityPage;
