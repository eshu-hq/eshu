/* Live Graph Explorer parity for the standalone prototype.
   Demo mode keeps the richer legacy prototype explorer. Live mode uses the same
   bounded query contracts as apps/console/src/pages/ExplorerPage.tsx. */
(function () {
  const LegacyExplorer = window.Explorer;
  const { useEffect: useEffectXP, useMemo: useMemoXP, useRef: useRefXP, useState: useStateXP } = React;
  const LAYERS = ["code", "deploy", "infra", "runtime", "security", "ops"];
  const VERB_LAYER = {
    CALLS: "code", IMPORTS: "code", INHERITS: "code", OVERRIDES: "code", REFERENCES: "code",
    DEPLOYS_FROM: "deploy", BUILDS: "deploy", DISCOVERS_CONFIG_IN: "deploy",
    DECLARED_BY: "infra", STORES_IN: "infra", ASSUMES_ROLE: "infra",
    RUNS_IN: "runtime", RUNS_AS: "runtime", DEPENDS_ON: "runtime", EXPOSES: "runtime",
    AFFECTED_BY: "security", OBSERVED_INCIDENT: "ops", TRACKED_BY: "ops"
  };

  function layerFor(verb) {
    return VERB_LAYER[String(verb || "").toUpperCase()] || "runtime";
  }

  function kindFor(type) {
    const t = String(type || "").toLowerCase();
    if (t.indexOf("service") >= 0) return "service";
    if (t.indexOf("workload") >= 0 || t.indexOf("deployment") >= 0) return "workload";
    if (t.indexOf("repo") >= 0) return "repo";
    if (t.indexOf("module") >= 0 || t.indexOf("package") >= 0 || t.indexOf("library") >= 0) return "library";
    if (t.indexOf("function") >= 0 || t.indexOf("class") >= 0 || t.indexOf("symbol") >= 0) return "client";
    if (t.indexOf("resource") >= 0 || t.indexOf("aws") >= 0 || t.indexOf("cloud") >= 0) return "aws";
    return "service";
  }

  function recommendedMode(kind) {
    const k = String(kind || "").toLowerCase();
    if (!k) return "direct";
    if (["function", "file", "class", "method", "symbol", "interface", "field", "variable"].some((v) => k.indexOf(v) >= 0)) {
      return "direct";
    }
    if (["service", "workload", "deployment", "repo", "resource", "aws", "infra", "cloud", "module", "package", "library", "endpoint", "queue", "bucket", "database", "table"].some((v) => k.indexOf(v) >= 0)) {
      return "neighborhood";
    }
    return "direct";
  }

  function node(id, label, kind, sub, col, hero, truth) {
    return { id, label: label || id, kind: kind || "service", sub, col, hero: Boolean(hero), truth: truth || "exact" };
  }

  function centerOnly(id, label, type) {
    return { nodes: [node(id || label, label || id, kindFor(type), type, 1, true, "exact")], edges: [] };
  }

  async function resolveHandle(client, query) {
    try {
      const env = await client.post("/api/v0/entities/resolve", { name: query, limit: 1 });
      const data = env.data || {};
      const rows = Array.isArray(data.entities) ? data.entities : (Array.isArray(data.matches) ? data.matches : []);
      const top = rows[0] || {};
      const labels = Array.isArray(top.labels) ? top.labels : [];
      const kind = top.type || labels[0] || "";
      return {
        id: top.id || top.entity_id || "",
        name: top.name || query,
        kind,
        repoId: repositoryIDForResolved(top.id || top.entity_id || "", top.repo_id || "", kind),
        repoName: top.repo_name || "",
        mode: recommendedMode(kind)
      };
    } catch (_) {
      return { id: "", name: query, kind: "", repoId: "", repoName: "", mode: "direct" };
    }
  }

  function repositoryIDForResolved(id, repoId, kind) {
    const resolvedRepoId = String(repoId || "").trim();
    if (resolvedRepoId) return resolvedRepoId;
    const resolvedId = String(id || "").trim();
    if (resolvedId && String(kind || "").toLowerCase().indexOf("repo") >= 0) return resolvedId;
    return "";
  }

  function codeRelationshipsToGraph(data, fallback) {
    const centerId = data.entity_id || fallback.id || fallback.name;
    const centerType = (Array.isArray(data.labels) && data.labels[0]) || fallback.kind;
    const nodes = new Map();
    const edges = [];
    nodes.set(centerId, node(centerId, data.name || fallback.name, kindFor(centerType), centerType, 1, true, "exact"));
    (data.incoming || []).forEach((edge) => {
      const id = edge.source_id || edge.source_name;
      if (!id) return;
      const verb = String(edge.type || "RELATED").toUpperCase();
      if (id !== centerId && !nodes.has(id)) nodes.set(id, node(id, edge.source_name || id, "client", "", 0, false, "exact"));
      edges.push({ s: id, t: centerId, verb, layer: layerFor(verb) });
    });
    (data.outgoing || []).forEach((edge) => {
      const id = edge.target_id || edge.target_name;
      if (!id) return;
      const verb = String(edge.type || "RELATED").toUpperCase();
      if (id !== centerId && !nodes.has(id)) nodes.set(id, node(id, edge.target_name || id, "client", "", 2, false, "exact"));
      edges.push({ s: centerId, t: id, verb, layer: layerFor(verb) });
    });
    return { nodes: Array.from(nodes.values()), edges };
  }

  async function loadDirectGraph(client, resolved) {
    if (!resolved.id) return { graph: centerOnly(resolved.name, resolved.name, resolved.kind), resolved };
    try {
      const env = await client.post("/api/v0/code/relationships", { entity_id: resolved.id, depth: 1 });
      return { graph: codeRelationshipsToGraph(env.data || {}, resolved), resolved };
    } catch (e) {
      if (String((e && e.message) || e).indexOf("HTTP 404") >= 0) {
        return { graph: centerOnly(resolved.id, resolved.name, resolved.kind), resolved };
      }
      throw e;
    }
  }

  function repoFromArtifact(artifact, side) {
    const id = String(side === "source" ? artifact.source_repo_id || "" : artifact.target_repo_id || "").trim();
    const name = String(side === "source" ? artifact.source_repo_name || "" : artifact.target_repo_name || "").trim();
    if (!id && !name) return null;
    return { id: id || "repository:" + name, name: name || id };
  }

  function uniqueRepos(repos) {
    const seen = new Set();
    return repos.filter((repo) => {
      if (!repo || seen.has(repo.id)) return false;
      seen.add(repo.id);
      return true;
    });
  }

  function deploymentStoryToGraph(data, fallbackName) {
    const serviceName = String(data.name || fallbackName || "").trim();
    const sourceName = String(data.repo_name || serviceName || "").trim();
    const artifacts = (((data.deployment_evidence || {}).artifacts) || [])
      .filter((artifact) => String(artifact.relationship_type || "").toUpperCase() === "DEPLOYS_FROM");
    const sourceRepo = artifacts.map((artifact) => repoFromArtifact(artifact, "target"))
      .find((repo) => repo && repo.name === sourceName) || { id: "repository:" + sourceName, name: sourceName };
    const serviceId = "workload:" + serviceName;
    const nodes = new Map();
    const edges = [];
    const edgeKeys = new Set();
    const chartIds = new Set();

    addStoryNode(nodes, node(serviceId, serviceName, "workload", "Workload", 3, true, "derived"));
    addStoryNode(nodes, node(sourceRepo.id, sourceRepo.name, "repo", "Source repository", 2, false, "derived"));

    const charts = uniqueRepos(artifacts
      .filter(isHelmChartArtifact)
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter((repo) => repo && repo.id !== sourceRepo.id));
    charts.forEach((repo) => {
      chartIds.add(repo.id);
      addStoryNode(nodes, node(repo.id, repo.name, "repo", "Helm chart", 1, false, "derived"));
      addStoryEdge(edges, edgeKeys, repo.id, sourceRepo.id, "DEPLOYS_FROM");
    });

    const controllers = uniqueRepos(artifacts
      .filter(isDeploymentControllerArtifact)
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter((repo) => repo && repo.id !== sourceRepo.id && !chartIds.has(repo.id)));
    controllers.forEach((repo) => {
      addStoryNode(nodes, node(repo.id, repo.name, "repo", "Deployment controller", 0, false, "derived"));
      if (!charts.length) addStoryEdge(edges, edgeKeys, repo.id, sourceRepo.id, "DEPLOYS_FROM");
      charts.forEach((chart) => addStoryEdge(edges, edgeKeys, repo.id, chart.id, "DEPLOYS_FROM"));
    });

    if (artifacts.length) addStoryEdge(edges, edgeKeys, sourceRepo.id, serviceId, "DEPLOYS_FROM");
    return { nodes: Array.from(nodes.values()), edges };
  }

  function addStoryNode(nodes, value) {
    if (!nodes.has(value.id)) nodes.set(value.id, value);
  }

  function addStoryEdge(edges, seen, s, t, verb) {
    const key = s + "\u0000" + t + "\u0000" + verb;
    if (seen.has(key)) return;
    seen.add(key);
    edges.push({ s, t, verb, layer: layerFor(verb) });
  }

  function isHelmChartArtifact(artifact) {
    const family = String(artifact.artifact_family || "").toLowerCase();
    const path = String(artifact.path || "").toLowerCase();
    const sourceRepo = String(artifact.source_repo_name || "").toLowerCase();
    return family === "helm" && (path.endsWith("/chart.yaml") || sourceRepo.indexOf("helm") >= 0 || sourceRepo.indexOf("chart") >= 0);
  }

  function isDeploymentControllerArtifact(artifact) {
    const family = String(artifact.artifact_family || "").toLowerCase();
    return family === "argocd" || family === "kustomize";
  }

  function entityMapToGraph(data, fallbackName) {
    const candidate = (((data.resolution || {}).candidates) || [])[0] || {};
    const labels = Array.isArray(candidate.labels) ? candidate.labels : [];
    const centerId = candidate.id || data.from || fallbackName;
    const nodes = new Map();
    const edges = [];
    nodes.set(centerId, node(centerId, candidate.name || fallbackName, kindFor(labels[0]), labels[0], 1, true, "exact"));
    ((((data.evidence || {}).relationships) || [])).forEach((rel) => {
      const id = String(rel.entity_id || rel.entity_name || "").trim();
      const label = String(rel.entity_name || rel.entity_id || "").trim();
      if (!id || id === centerId) return;
      const relLabels = Array.isArray(rel.entity_labels) ? rel.entity_labels : [];
      const verb = String(rel.relationship_type || (Array.isArray(rel.relationship_types) && rel.relationship_types[0]) || "RELATED").toUpperCase();
      const incoming = String(rel.direction || "outgoing").toLowerCase() === "incoming";
      if (!nodes.has(id)) nodes.set(id, node(id, label || id, kindFor(relLabels[0]), relLabels[0], incoming ? 0 : 2, false, "exact"));
      edges.push(incoming ? { s: id, t: centerId, verb, layer: layerFor(verb) } : { s: centerId, t: id, verb, layer: layerFor(verb) });
    });
    return { nodes: Array.from(nodes.values()), edges };
  }

  async function loadNeighborhoodGraph(client, resolved) {
    try {
      const env = await client.get("/api/v0/services/" + encodeURIComponent(resolved.name) + "/context");
      const story = deploymentStoryToGraph(env.data || {}, resolved.name);
      if (story.edges.length) return { graph: story, resolved };
    } catch (e) {
      if (!shouldFallbackFromContext(e)) throw e;
    }
    const repoStory = await loadRepositoryDeploymentStoryGraph(client, resolved);
    if (repoStory) return { graph: repoStory, resolved };
    const env = await client.post("/api/v0/impact/entity-map", { from: resolved.name, depth: 2 });
    return { graph: entityMapToGraph(env.data || {}, resolved.name), resolved };
  }

  async function loadRepositoryDeploymentStoryGraph(client, resolved) {
    const repoId = String(resolved.repoId || "").trim();
    if (!repoId) return null;
    try {
      const env = await client.get("/api/v0/repositories/" + encodeURIComponent(repoId) + "/context");
      const data = env.data || {};
      const repository = data.repository || {};
      const story = deploymentStoryToGraph({
        name: resolved.name,
        repo_name: repository.name || resolved.repoName || resolved.name,
        deployment_evidence: data.deployment_evidence
      }, resolved.name);
      return story.edges.length ? story : null;
    } catch (e) {
      if (shouldFallbackFromContext(e)) return null;
      throw e;
    }
  }

  function shouldFallbackFromContext(error) {
    const msg = String((error && error.message) || error || "");
    return msg.indexOf("HTTP 404") >= 0 ||
      msg.indexOf("not_found") >= 0 ||
      msg.indexOf("service_not_found") >= 0 ||
      msg.indexOf("unknown_service") >= 0;
  }

  function hashQuery() {
    const parts = String(location.hash || "").split("?");
    if (parts.length < 2) return "";
    return new URLSearchParams(parts.slice(1).join("?")).get("q") || "";
  }

  function centerId(graph) {
    const center = (graph.nodes || []).find((n) => n.hero);
    return center && center.id;
  }

  function modeForNode(value) {
    return value.kind === "client" || value.kind === "library" ? "direct" : "neighborhood";
  }

  function LiveExplorer({ data, client, onOpenService }) {
    const D = data || ESHU;
    const [layout, setLayout] = useStateXP("layered");
    const [mode, setMode] = useStateXP("direct");
    const [query, setQuery] = useStateXP(hashQuery() || "");
    const [graph, setGraph] = useStateXP({ nodes: [], edges: [] });
    const [selected, setSelected] = useStateXP(null);
    const [busy, setBusy] = useStateXP(false);
    const [error, setError] = useStateXP("");
    const [hint, setHint] = useStateXP("Search a service, repository, symbol, or resource to load live graph relationships.");
    const [layers, setLayers] = useStateXP(() => {
      const out = {};
      LAYERS.forEach((layer) => { out[layer] = true; });
      return out;
    });
    const modePinned = useRefXP(false);

    const filtered = useMemoXP(() => {
      const edges = graph.edges.filter((edge) => layers[edge.layer] !== false);
      const keep = new Set();
      edges.forEach((edge) => { keep.add(edge.s); keep.add(edge.t); });
      graph.nodes.forEach((value) => { if (value.hero || graph.edges.length === 0) keep.add(value.id); });
      return { nodes: graph.nodes.filter((value) => keep.has(value.id)), edges };
    }, [graph, layers]);
    const labels = useMemoXP(() => new Map(graph.nodes.map((value) => [value.id, value.label])), [graph.nodes]);

    async function expand(name, forcedMode) {
      if (!name || !client) return;
      setBusy(true); setError(""); setHint("");
      try {
        const resolved = await resolveHandle(client, name);
        const nextMode = forcedMode || (modePinned.current ? mode : resolved.mode);
        const result = nextMode === "neighborhood"
          ? await loadNeighborhoodGraph(client, resolved)
          : await loadDirectGraph(client, resolved);
        if (nextMode !== mode) setMode(nextMode);
        setGraph(result.graph);
        setSelected(result.graph.nodes.find((value) => value.hero) || result.graph.nodes[0] || null);
        setQuery(result.resolved.name);
        if (nextMode === "direct" && result.graph.edges.length === 0) {
          setHint("No direct code relationships for this entity. Try Neighborhood for service, repo, and cloud context.");
        }
      } catch (e) {
        setError((e && e.message) || "failed");
      } finally {
        setBusy(false);
      }
    }

    async function centerOn(value) {
      if (!value || value.id === centerId(graph)) return;
      const nextMode = modeForNode(value);
      modePinned.current = true;
      setMode(nextMode);
      setQuery(value.label);
      await expand(value.label, nextMode);
    }

    useEffectXP(() => {
      const seed = hashQuery();
      if (!seed) return;
      setQuery(seed);
      void expand(seed);
      // The prototype route seed should run once per live client.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [client]);

    return (
      <div className="page" style={{ maxWidth: "none" }}>
        <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
          <div>
            <h2>Graph Explorer</h2>
            <p>Live graph drilldown. Direct uses POST /api/v0/code/relationships; Neighborhood uses service context first, then POST /api/v0/impact/entity-map.</p>
          </div>
          <div className="seg">
            <button className={layout === "layered" ? "active" : ""} onClick={() => setLayout("layered")}>Layered</button>
            <button className={layout === "radial" ? "active" : ""} onClick={() => setLayout("radial")}>Radial</button>
          </div>
        </div>

        <div className="explorer-filters" style={{ gap: 8 }}>
          <div className="searchbox" style={{ minWidth: 320, height: 38, margin: 0 }}>
            <Icon.search size={16} />
            <input placeholder="api-node-platform, helm-charts, searchByPortalId..." value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void expand(query); }} />
          </div>
          <button className="btn-ghost active" disabled={busy || !query} onClick={() => void expand(query)}>{busy ? "Loading..." : "Load"}</button>
          <div className="seg">
            <button className={mode === "direct" ? "active" : ""} onClick={() => { modePinned.current = true; setMode("direct"); if (query) void expand(query, "direct"); }}>Direct</button>
            <button className={mode === "neighborhood" ? "active" : ""} onClick={() => { modePinned.current = true; setMode("neighborhood"); if (query) void expand(query, "neighborhood"); }}>Neighborhood</button>
          </div>
          {error ? <span className="src-err" style={{ marginTop: 0 }}>! {error}</span> : null}
          {!error && hint ? <span className="t-mut" style={{ marginTop: 0, fontSize: ".78rem" }}>{hint}</span> : null}
        </div>

        <div className="explorer-filters">
          {LAYERS.map((layer) => {
            const count = graph.edges.filter((edge) => edge.layer === layer).length;
            return (
              <button key={layer} className={cx("layer-toggle", layers[layer] ? "on" : "off")} style={{ "--lc": D.layerColor[layer] }} onClick={() => setLayers((current) => Object.assign({}, current, { [layer]: !current[layer] }))}>
                <i style={{ background: D.layerColor[layer] }} /><span style={{ textTransform: "capitalize" }}>{layer}</span><span className="lt-n">{count}</span>
              </button>
            );
          })}
        </div>

        <div className="explorer-layout">
          <div className="gcanvas-shell">
            <GraphCanvas graph={filtered} layout={layout} height={640} onSelect={setSelected} selectedId={selected && selected.id} />
          </div>
          <Panel title="Inspector">
            {selected ? (
              <div className="inspector">
                <div className="insp-head">
                  <span className="cglyph" style={{ width: 30, height: 30, color: (D.kindStyle[selected.kind] || {}).color || "#9aa4af", borderColor: (D.kindStyle[selected.kind] || {}).color || "#9aa4af" }}>{selected.kind.slice(0, 1).toUpperCase()}</span>
                  <div><div className="insp-kind">{selected.kind}</div><div className="insp-title">{selected.label}</div></div>
                </div>
                {selected.sub ? <div className="t-mut mono" style={{ fontSize: ".82rem" }}>{selected.sub}</div> : null}
                {selected.truth ? <TruthChip level={selected.truth} /> : null}
                <button className="btn-ghost" disabled={busy || selected.id === centerId(graph)} style={{ width: "100%", justifyContent: "center" }} onClick={() => void centerOn(selected)}>
                  {selected.id === centerId(graph) ? "Current center" : busy ? "Loading..." : "Center graph here"}
                </button>
                {(selected.kind === "service" || selected.kind === "workload") && onOpenService ? (
                  <button className="btn-ghost" style={{ width: "100%", justifyContent: "center" }} onClick={() => onOpenService(selected.label)}>Open service context</button>
                ) : null}
                <div className="section-label">Edges</div>
                <div className="insp-evi">
                  {graph.edges.filter((edge) => edge.s === selected.id || edge.t === selected.id).map((edge, index) => {
                    const endpoint = edge.s === selected.id ? edge.t : edge.s;
                    const label = labels.get(endpoint) || endpoint;
                    return <div className="insp-evi-row" key={index} title={label === endpoint ? undefined : endpoint}>{edge.verb} {edge.s === selected.id ? "->" : "<-"} {label}</div>;
                  })}
                  {!graph.edges.filter((edge) => edge.s === selected.id || edge.t === selected.id).length ? <p className="empty">No relationships returned for this node.</p> : null}
                </div>
              </div>
            ) : (
              <p className="empty">Search for an entity to load live relationships.</p>
            )}
          </Panel>
        </div>
      </div>
    );
  }

  window.Explorer = function Explorer(props) {
    if (!props.client && LegacyExplorer) return <LegacyExplorer {...props} />;
    return <LiveExplorer {...props} />;
  };
})();
