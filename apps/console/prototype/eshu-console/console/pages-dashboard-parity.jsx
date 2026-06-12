/* Eshu Console - prototype dashboard live parity overlay.
   Demo mode delegates to the original dashboard. Live mode seeds the atlas from
   the same bounded entity-map contract used by the production console. */
(function () {
  const DemoDashboard = window.Dashboard;
  const { useEffect: useEffectD, useState: useStateD } = React;
  const MEANINGFUL_DASHBOARD_EDGES = 2;
  const MAX_DASHBOARD_SEEDS = 8;

  function dashData(response) {
    if (response && response.error) throw new Error(response.error.message || response.error.code || "api error");
    return response && Object.prototype.hasOwnProperty.call(response, "data") ? response.data : response;
  }

  function serviceSeeds(D) {
    return (D.services || []).filter((service) => String(service.name || "").trim()).slice(0, MAX_DASHBOARD_SEEDS);
  }

  function kindFor(labels) {
    const text = Array.isArray(labels) ? String(labels[0] || "").toLowerCase() : String(labels || "").toLowerCase();
    if (text.indexOf("repo") >= 0) return "repo";
    if (text.indexOf("workload") >= 0 || text.indexOf("deployment") >= 0) return "workload";
    if (text.indexOf("resource") >= 0 || text.indexOf("cloud") >= 0) return "aws";
    return "service";
  }

  function layerFor(verb) {
    const v = String(verb || "").toUpperCase();
    if (v.indexOf("DEPLOY") >= 0 || v.indexOf("BUILD") >= 0) return "deploy";
    if (v.indexOf("CONFIG") >= 0 || v.indexOf("STORE") >= 0 || v.indexOf("ROLE") >= 0) return "infra";
    if (v.indexOf("CALL") >= 0 || v.indexOf("IMPORT") >= 0) return "code";
    return "runtime";
  }

  function entityMapToGraph(data, seedName) {
    const candidate = (((data.resolution || {}).candidates) || [])[0] || {};
    const labels = Array.isArray(candidate.labels) ? candidate.labels : [];
    const centerId = String(candidate.id || data.from || seedName);
    const centerName = String(candidate.name || data.from || seedName);
    const nodes = new Map([[centerId, { id: centerId, label: centerName, kind: kindFor(labels), sub: labels[0] || "Live entity", col: 1, hero: true, truth: "exact" }]]);
    const edges = [];
    ((((data.evidence || {}).relationships) || [])).forEach((rel) => {
      const id = String(rel.entity_id || rel.entity_name || "").trim();
      if (!id || id === centerId) return;
      const label = String(rel.entity_name || rel.entity_id || "").trim();
      const relLabels = Array.isArray(rel.entity_labels) ? rel.entity_labels : [];
      const verb = String(rel.relationship_type || (Array.isArray(rel.relationship_types) && rel.relationship_types[0]) || "RELATED").toUpperCase();
      const incoming = String(rel.direction || "outgoing").toLowerCase() === "incoming";
      if (!nodes.has(id)) nodes.set(id, { id, label: label || id, kind: kindFor(relLabels), sub: relLabels[0] || "", col: incoming ? 0 : 2, truth: "exact" });
      edges.push(incoming ? { s: id, t: centerId, verb, layer: layerFor(verb), evidence: entityMapEdgeEvidence(rel, verb, incoming) } : { s: centerId, t: id, verb, layer: layerFor(verb), evidence: entityMapEdgeEvidence(rel, verb, incoming) });
    });
    return { nodes: Array.from(nodes.values()), edges };
  }

  async function loadSeedGraph(client, seeds) {
    let best = null;
    for (const seed of seeds) {
      const env = await client.post("/api/v0/impact/entity-map", { from: seed.name, depth: 2 });
      const graph = entityMapToGraph(dashData(env) || {}, seed.name);
      const next = { seed, graph };
      if (graph.edges.length >= MEANINGFUL_DASHBOARD_EDGES) return next;
      if (!best || graph.edges.length > best.graph.edges.length) best = next;
    }
    return best;
  }

  function relationshipRows(graph) {
    const counts = {};
    graph.edges.forEach((edge) => {
      const key = edge.verb + "\u0000" + edge.layer;
      counts[key] = counts[key] || { verb: edge.verb, layer: edge.layer, count: 0, detail: "Live entity-map relationships" };
      counts[key].count += 1;
    });
    return Object.keys(counts).map((key) => counts[key]).sort((a, b) => b.count - a.count);
  }

  function Dashboard(props) {
    const D = props.data || ESHU;
    const client = props.client;
    const seeds = serviceSeeds(D);
    const [state, setState] = useStateD({ status: client ? "loading" : "demo", graph: null, error: "" });
    const [selected, setSelected] = useStateD(null);

    useEffectD(() => {
      let cancelled = false;
      if (!client) { setState({ status: "demo", graph: null, error: "" }); return () => { cancelled = true; }; }
      if (!seeds.length) { setState({ status: "empty", graph: null, error: "" }); return () => { cancelled = true; }; }
      setState({ status: "loading", graph: null, error: "" });
      loadSeedGraph(client, seeds)
        .then((result) => { if (!cancelled) setState({ status: result ? "live" : "empty", graph: result && result.graph, error: "" }); })
        .catch((error) => { if (!cancelled) setState({ status: "unavailable", graph: null, error: (error && error.message) || "unavailable" }); });
      return () => { cancelled = true; };
    }, [client, seeds.map((seed) => seed.name).join("\u0000")]);

    if (!client && (!props.source || props.source.mode !== "live")) return <DemoDashboard {...props} />;
    if (!client) {
      const status = props.source && props.source.status === "unavailable" ? "unavailable" : "connecting";
      return <div className="page"><div className="page-intro"><h2>Dashboard</h2><p>No live relationship atlas: Eshu API {status}.</p></div></div>;
    }
    const runtime = D.runtime || {};
    if (state.status === "loading" || state.status === "demo") {
      return <div className="page"><div className="page-intro"><h2>Dashboard</h2><p>Loading live relationship atlas from <span className="mono">POST /api/v0/impact/entity-map</span>...</p></div></div>;
    }
    if (state.status === "empty" || state.status === "unavailable") {
      const message = state.status === "unavailable" ? state.error : "No live relationship atlas seeds returned from the catalog.";
      return <div className="page"><div className="page-intro"><h2>Dashboard</h2><p>No live relationship atlas: {message}</p></div></div>;
    }
    const rawGraph = state.graph || { nodes: seeds[0] ? [{ id: seeds[0].id || seeds[0].name, label: seeds[0].name, kind: "service", sub: "loading live relationships", hero: true }] : [], edges: [] };
    const graph = { nodes: Array.isArray(rawGraph.nodes) ? rawGraph.nodes : [], edges: Array.isArray(rawGraph.edges) ? rawGraph.edges : [] };
    const relRows = relationshipRows(graph).map((row) => ({ label: row.verb, value: row.count, color: ESHU.layerColor[row.layer] || "var(--teal)", detail: row.detail }));
    const sel = selected && graph.nodes.find((node) => node.id === selected.id) ? selected : (graph.nodes.find((node) => node.hero) || graph.nodes[0] || null);
    const nodeLabels = new Map(graph.nodes.map((node) => [node.id, node.label]));
    return (
      <div className="page">
        <div className="grid g-4">
          <StatTile label="Graph nodes" value={fmt(runtime.nodes || 0)} color="var(--teal)" sub={runtime.backendVersion ? "NornicDB - " + runtime.backendVersion : "live graph metric"} />
          <StatTile label="Relationships" value={fmt(runtime.edges || graph.edges.length)} color="var(--ember)" sub={relRows.length ? relRows.length + " typed verbs observed" : "entity-map atlas"} />
          <StatTile label="Indexed repos" value={runtime.repos || 0} color="var(--blue)" sub={(runtime.services || (D.services || []).length) + " services - " + (runtime.workloads || 0) + " workloads"} />
          <StatTile label="Queue outstanding" value={runtime.queueOutstanding || 0} color="var(--violet)" sub={(runtime.inFlight || 0) + " in-flight - " + (runtime.deadLetters || 0) + " dead-letter"} />
        </div>
        <Panel className="mt" title="Code-to-cloud relationship atlas" sub="Live entity-map neighbourhood - click any node to inspect it" glyph={<Icon.graph />}
          action={sel && props.onOpenService ? <button className="btn-ghost" onClick={() => props.onOpenService(sel.label)}>Open spotlight -&gt;</button> : null}>
          <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) 300px", gap: "var(--gap)", alignItems: "start" }}>
            <GraphCanvas graph={graph} data={D} layout={props.graphStyle || "layered"} height={500} onSelect={setSelected} selectedId={sel && sel.id} />
            <div className="panel" style={{ background: "var(--bg-field)", boxShadow: "none" }}>
              <div className="panel-body">
                {sel ? (
                  <div className="inspector">
                    <div className="insp-kind">{sel.kind}</div>
                    <div className="insp-title">{sel.label}</div>
                    {sel.sub ? <div className="t-mut mono" style={{ fontSize: ".82rem" }}>{sel.sub}</div> : null}
                    {state.status === "loading" ? <p className="empty">Loading live relationships...</p> : null}
                    <div className="insp-evi">
                      {graph.edges.filter((edge) => edge.s === sel.id || edge.t === sel.id).map((edge, index) => {
                        const endpointID = edge.s === sel.id ? edge.t : edge.s;
                        const endpointLabel = nodeLabels.get(endpointID) || endpointID;
                        return (
                          <div className="insp-evi-row" key={index} title={endpointLabel === endpointID ? undefined : endpointID}>
                            {edge.verb} {edge.s === sel.id ? "->" : "<-"} {endpointLabel}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ) : <p className="empty">No live relationship atlas nodes returned yet.</p>}
              </div>
            </div>
          </div>
        </Panel>
        <Panel className="mt" title="Relationship coverage" sub="Most-observed live entity-map verbs" glyph={<Icon.branch />}>
          {relRows.length ? <BarRows rows={relRows} /> : <p className="empty">No live entity-map relationships returned yet.</p>}
        </Panel>
      </div>
    );
  }

  window.Dashboard = Dashboard;
})();
