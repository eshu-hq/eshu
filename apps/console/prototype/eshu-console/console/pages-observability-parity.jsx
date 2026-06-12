/* Eshu Console - prototype observability live parity overlay.
   Demo mode delegates to the legacy page. Live mode renders only provider-owned
   coverage correlations from the same endpoint family as the production console. */
(function () {
  const LegacyObservability = window.Observability;
  const { useEffect: useEffectOP, useMemo: useMemoOP, useState: useStateOP } = React;
  const PROVIDERS = ["grafana", "prometheus", "loki", "tempo"];
  const SIGNAL_LABEL = {
    alerts: "Alerts",
    dashboards: "Dashboards",
    logs: "Logs",
    metrics: "Metrics",
    synthetics: "Synthetics",
    traces: "Traces"
  };
  const SIGNALS = ["metrics", "logs", "traces", "dashboards", "alerts", "synthetics"];
  const PROVIDER_QUERY_MARKERS = [
    "provider=grafana",
    "provider=prometheus",
    "provider=loki",
    "provider=tempo"
  ];

  function envData(response) {
    return response && Object.prototype.hasOwnProperty.call(response, "data") ? response.data : response;
  }

  function text(value) {
    return value == null ? "" : String(value);
  }

  function signalFor(row) {
    const signal = text(row.coverage_signal);
    const map = {
      alarm: "alerts",
      dashboard: "dashboards",
      log_signal: "logs",
      rule: "alerts",
      scrape_target: "metrics",
      trace_signal: "traces"
    };
    return map[signal] || signal || "unknown";
  }

  function stateFor(row) {
    const status = text(row.coverage_status).toLowerCase();
    if (status === "covered") return "covered";
    if (status === "gap") return "gap";
    const outcome = text(row.outcome).toLowerCase();
    if (outcome === "exact" || outcome === "derived") return "covered";
    if (outcome === "stale") return "partial";
    return status || "gap";
  }

  function mapRow(row, provider) {
    const status = text(row.coverage_status) || stateFor(row);
    return {
      id: text(row.correlation_id) || [provider, signalFor(row), text(row.observability_object_ref), text(row.target_service_ref)].join(":"),
      provider: text(row.provider) || provider,
      signal: signalFor(row),
      object: text(row.observability_object_ref) || text(row.observability_resource_uid),
      target: text(row.target_service_ref) || text(row.target_uid),
      resourceClass: text(row.resource_class),
      sourceKind: text(row.source_kind),
      freshness: text(row.freshness_state),
      status,
      state: stateFor(row),
      covered: status.toLowerCase() === "covered" || stateFor(row) === "covered",
      reason: text(row.reason)
    };
  }

  async function loadProvider(client, provider) {
    const rows = [];
    let after = "";
    for (let page = 0; page < 10; page += 1) {
      const cursor = after ? "&after_correlation_id=" + encodeURIComponent(after) : "";
      const env = await client.get("/api/v0/observability/coverage/correlations?provider=" + provider + "&limit=200" + cursor);
      const data = envData(env) || {};
      const recs = Array.isArray(data.correlations) ? data.correlations : (Array.isArray(data.results) ? data.results : []);
      recs.forEach((row) => rows.push(mapRow(row, provider)));
      const next = data.next_cursor || {};
      after = text(next.after_correlation_id);
      if (!data.truncated || !after) break;
    }
    return { provider, source: rows.length ? "live" : "empty", rows, error: "" };
  }

  async function loadCoverage(client) {
    const results = [];
    for (const provider of PROVIDERS) {
      try {
        results.push(await loadProvider(client, provider));
      } catch (error) {
        results.push({ provider, source: "unavailable", rows: [], error: (error && error.message) || "failed" });
      }
    }
    const seen = {};
    const rows = [];
    results.forEach((result) => {
      result.rows.forEach((row) => {
        if (seen[row.id]) return;
        seen[row.id] = true;
        rows.push(row);
      });
    });
    const providers = results.map((result) => ({
      provider: result.provider,
      total: result.rows.length,
      covered: result.rows.filter((row) => row.covered).length,
      gaps: result.rows.filter((row) => !row.covered).length,
      source: result.source,
      error: result.error
    }));
    return {
      rows,
      providers,
      source: rows.length ? "live" : providers.some((provider) => provider.source === "unavailable") ? "unavailable" : "empty"
    };
  }

  function snapshotFromData(D) {
    const snap = D && D.obsCoverageSnapshot;
    if (!snap) return null;
    return {
      rows: Array.isArray(snap.rows) ? snap.rows : [],
      providers: Array.isArray(snap.providers) ? snap.providers : [],
      source: snap.source || "empty"
    };
  }

  function serviceName(D, id) {
    const services = (D && D.services) || [];
    const match = services.find((service) => service.id === id || service.name === id);
    return match ? match.name : id;
  }

  function serviceRows(D, rows) {
    const byTarget = {};
    rows.forEach((row) => {
      const target = row.target || "unresolved";
      byTarget[target] = byTarget[target] || { id: target, name: serviceName(D, target), signals: {}, total: 0, covered: 0, gaps: 0 };
      const current = byTarget[target].signals[row.signal];
      if (!current || (current.state !== "covered" && row.covered)) {
        byTarget[target].signals[row.signal] = row;
      }
    });
    Object.keys(byTarget).forEach((target) => {
      const item = byTarget[target];
      SIGNALS.forEach((signal) => {
        const row = item.signals[signal];
        if (!row) return;
        item.total += 1;
        if (row.covered) item.covered += 1;
        else item.gaps += 1;
      });
    });
    return Object.keys(byTarget).map((key) => byTarget[key]).sort((a, b) => b.gaps - a.gaps || a.name.localeCompare(b.name));
  }

  function providerTone(provider) {
    if (provider.source === "unavailable") return "crit";
    if (provider.source === "empty") return "neutral";
    return provider.gaps ? "neutral" : "teal";
  }

  function stateGlyph(row) {
    if (!row) return { glyph: "-", color: "var(--subtle)", label: "not reported" };
    if (row.covered) return { glyph: "ok", color: "var(--teal)", label: row.status || "covered" };
    if (row.state === "partial") return { glyph: "partial", color: "var(--med)", label: row.status || "partial" };
    return { glyph: "gap", color: "var(--crit)", label: row.status || "gap" };
  }

  function ProviderCards({ providers }) {
    return (
      <div className="signal-source-grid">
        {providers.map((provider) => (
          <div className="signal-source" key={provider.provider}>
            <span className="cglyph" style={{ width: 28, height: 28 }}>{provider.provider.slice(0, 1).toUpperCase()}</span>
            <span className="cell-stack" style={{ minWidth: 0 }}>
              <span style={{ fontWeight: 600, fontSize: ".84rem" }}>{provider.provider}</span>
              <small className="mono">{provider.covered}/{provider.total} covered - {provider.gaps} gaps</small>
            </span>
            <Badge tone={providerTone(provider)}>{provider.source}</Badge>
          </div>
        ))}
      </div>
    );
  }

  function Observability(props) {
    const D = props.data || ESHU;
    const client = props.client;
    const initial = snapshotFromData(D);
    const [state, setState] = useStateOP({ status: client ? "loading" : "demo", snapshot: initial, error: "" });

    useEffectOP(() => {
      let cancelled = false;
      if (!client) {
        setState({ status: "demo", snapshot: snapshotFromData(D), error: "" });
        return () => { cancelled = true; };
      }
      setState({ status: initial ? "live" : "loading", snapshot: initial, error: "" });
      loadCoverage(client)
        .then((snapshot) => {
          if (!cancelled) setState({ status: snapshot.source, snapshot, error: "" });
        })
        .catch((error) => {
          if (!cancelled) setState({ status: "unavailable", snapshot: initial, error: (error && error.message) || "unavailable" });
        });
      return () => { cancelled = true; };
    }, [client, initial && initial.source, initial && initial.rows.length]);

    const snapshot = state.snapshot || { rows: [], providers: [], source: state.status };
    const rows = Array.isArray(snapshot.rows) ? snapshot.rows : [];
    const providers = Array.isArray(snapshot.providers) ? snapshot.providers : [];
    const services = useMemoOP(() => serviceRows(D, rows), [D, rows]);
    const unavailable = state.status === "unavailable" || snapshot.source === "unavailable";
    const empty = state.status !== "loading" && !rows.length && !unavailable;
    const covered = rows.filter((row) => row.covered).length;
    const gaps = rows.length - covered;

    if (!client) return <LegacyObservability {...props} />;

    return (
      <div className="page" style={{ maxWidth: "none" }}>
        <div className="page-intro">
          <h2>Observability</h2>
          <p>Live provider coverage from <span className="mono">GET /api/v0/observability/coverage/correlations</span>. Provider anchors: {PROVIDER_QUERY_MARKERS.join(", ")}.</p>
        </div>

        <div className="grid g-4">
          <StatTile label="Coverage rows" value={fmt(rows.length)} color="var(--teal)" sub="provider correlations" />
          <StatTile label="Covered" value={fmt(covered)} color="var(--teal)" sub="coverage_status=covered" />
          <StatTile label="Gaps" value={fmt(gaps)} color={gaps ? "var(--crit)" : "var(--teal)"} sub="provider-owned gaps" />
          <StatTile label="Providers" value={providers.filter((p) => p.source === "live").length + "/" + PROVIDERS.length} color={unavailable ? "var(--crit)" : "var(--blue)"} sub={snapshot.source || state.status} />
        </div>

        <Panel className="mt" title="Provider coverage" sub="grafana, prometheus, loki, and tempo provider queries" glyph={<Icon.pulse />}>
          {providers.length ? <ProviderCards providers={providers} /> : <p className="empty">Loading live observability provider coverage...</p>}
          {unavailable ? <p className="empty">Live observability coverage unavailable{state.error ? ": " + state.error : ""}.</p> : null}
          {empty ? <p className="empty">No live observability coverage correlations returned by the provider endpoints.</p> : null}
        </Panel>

        <Panel className="flush mt" title="Coverage matrix" sub="Per service by signal from reducer-owned coverage correlations" glyph={<Icon.spark />}>
          <div className="cov-scroll">
            <table className="tbl cov-matrix">
              <thead>
                <tr>
                  <th>Service</th>
                  {SIGNALS.map((signal) => <th key={signal} className="cov-col"><span>{SIGNAL_LABEL[signal]}</span></th>)}
                  <th>Reported</th>
                </tr>
              </thead>
              <tbody>
                {services.map((service) => (
                  <tr key={service.id} className="cov-row">
                    <td className="cell-stack" onClick={() => props.onOpenService && props.onOpenService(service.id)} style={{ cursor: "pointer" }}>
                      <span className="t-name" style={{ fontSize: ".82rem" }}>{service.name}</span>
                      <small className="mono">{service.id}</small>
                    </td>
                    {SIGNALS.map((signal) => {
                      const state = stateGlyph(service.signals[signal]);
                      return (
                        <td key={signal} className="cov-cell" title={SIGNAL_LABEL[signal] + ": " + state.label}>
                          <span className="cov-mark mono" style={{ color: state.color }}>{state.glyph}</span>
                        </td>
                      );
                    })}
                    <td className="mono" style={{ fontSize: ".78rem", color: service.gaps ? "var(--crit)" : "var(--teal)" }}>{service.covered}/{service.total}</td>
                  </tr>
                ))}
                {services.length === 0 ? <tr><td colSpan={8}><p className="empty">{unavailable ? "Live observability coverage unavailable." : "No live observability coverage correlations returned."}</p></td></tr> : null}
              </tbody>
            </table>
          </div>
        </Panel>

        <Panel className="flush mt" title="Coverage correlations" sub={rows.length + " provider rows"} glyph={<Icon.db />}>
          <table className="tbl">
            <thead><tr><th>Provider</th><th>Signal</th><th>Object</th><th>Target</th><th>Freshness</th><th>Status</th></tr></thead>
            <tbody>
              {rows.slice(0, 120).map((row) => (
                <tr key={row.id}>
                  <td className="t-name">{row.provider}</td>
                  <td className="mono t-mut" style={{ fontSize: ".78rem" }}>{row.signal}</td>
                  <td className="t-mut" style={{ fontSize: ".78rem" }}>{row.object || "-"}</td>
                  <td className="t-mut" style={{ fontSize: ".78rem" }}>{row.target || "-"}</td>
                  <td className="mono t-mut" style={{ fontSize: ".74rem" }}>{row.freshness || "-"}</td>
                  <td>{row.covered ? <Badge tone="teal">{row.status}</Badge> : <Badge tone="crit">{row.status}</Badge>}</td>
                </tr>
              ))}
              {rows.length === 0 ? <tr><td colSpan={6}><p className="empty">No live observability coverage correlations returned.</p></td></tr> : null}
            </tbody>
          </table>
        </Panel>
      </div>
    );
  }

  window.Observability = Observability;
})();
