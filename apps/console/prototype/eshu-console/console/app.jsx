/* Eshu Console — app shell, nav, routing, tweaks. */
const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "accent": "teal",
  "surface": "graphite",
  "density": "cozy",
  "heroMode": "graph",
  "graphStyle": "layered",
  "chartStyle": "bars"
}/*EDITMODE-END*/;

function canonicalRoute(route) {
  return window.ESHU_ROUTES ? window.ESHU_ROUTES.canonicalRoute(route) : route;
}

function routeHash(route, suffix) {
  return window.ESHU_ROUTES ? window.ESHU_ROUTES.hashFor(route, suffix) : "#" + route + (suffix || "");
}

function setRouteHash(route, suffix) {
  if (window.ESHU_ROUTES) window.ESHU_ROUTES.setHash(route, suffix);
  else location.hash = route + (suffix || "");
}

const NAV = [
  { group: "Overview", items: [
    { id: "dashboard", label: "Dashboard", icon: "dashboard" },
    { id: "explorer", label: "Graph Explorer", icon: "graph" }
  ] },
  { group: "Inventory", items: [
    { id: "repos", label: "Repositories", icon: "catalog", count: (m) => m.services.filter((s) => s.repo).length },
    { id: "catalog", label: "Catalog", icon: "box", count: (m) => m.services.length },
    { id: "findings", label: "Findings", icon: "findings", alert: true, count: (m) => m.findings.length + m.vulns.length },
    { id: "images", label: "Images", icon: "box", count: (m) => (m.imageInventory || []).length || m.services.filter((s) => s.image).length },
    { id: "iac", label: "IaC", icon: "layers", count: (m) => (m.iacParityRows || []).length || m.cloudResources.filter((r) => r.tf).length },
    { id: "vulnerabilities", label: "Vulnerabilities", icon: "vuln", alert: true, count: (m) => m.vulns.length }
  ] },
  { group: "Code", items: [
    { id: "deadcode", label: "Dead code", icon: "findings", count: (m) => m.deadCode.length },
    { id: "codegraph", label: "Code graph", icon: "branch" }
  ] },
  { group: "Cloud & Telemetry", items: [
    { id: "topology", label: "Topology", icon: "graph" },
    { id: "cloud", label: "Cloud", icon: "cloud", count: (m) => m.cloudResources.length },
    { id: "observability", label: "Observability", icon: "pulse" },
    { id: "sbom", label: "SBOM", icon: "shield", count: (m) => (m.sbomInventory && m.sbomInventory.buckets.length) || m.vulns.length },
    { id: "dependencies", label: "Dependencies", icon: "branch", count: (m) => (m.dependencyInventory || []).length }
  ] },
  { group: "System", items: [
    { id: "admin", label: "Operations", icon: "admin" }
  ] }
];

const TITLES = {
  dashboard: ["Dashboard", "Read-only code-to-cloud graph status & evidence"],
  explorer: ["Graph Explorer", "Drill into the live NornicDB relationship graph"],
  repos: ["Repositories", "Browse every indexed source repository"],
  reposource: ["Repository source", "Indexed repository tree and source viewer"],
  catalog: ["Catalog", "Services, repositories & workloads"],
  findings: ["Findings", "What needs human attention — one worklist"],
  images: ["Images", "Container image inventory and package risk"],
  iac: ["IaC", "Terraform and ArgoCD evidence"],
  deadcode: ["Dead code", "Unreferenced symbols — analyzer findings"],
  codegraph: ["Code graph", "Symbol & module relationships (CALLS / IMPORTS)"],
  vulnerabilities: ["Vulnerabilities", "CVE register — vulnerability intelligence"],
  topology: ["Topology", "Full code-to-cloud path for a service"],
  cloud: ["Cloud", "Multi-cloud resource inventory — code-to-cloud"],
  observability: ["Observability", "Signal coverage correlated per service"],
  sbom: ["SBOM", "Package evidence and advisory reachability"],
  dependencies: ["Dependencies", "Source, service and datastore dependency edges"],
  workspace: ["Workspace", "Entity dossier — story, evidence, deployment path"],
  admin: ["Operations", "Eshu runtime & NornicDB backend health"]
};

function liveArray(live, key) {
  return Array.isArray(live && live[key]) ? live[key] : [];
}

function lastMetric(metrics, key) {
  const values = metrics && Array.isArray(metrics[key]) ? metrics[key] : [];
  return values.length ? values[values.length - 1] : 0;
}

function liveMetrics(metrics) {
  const source = metrics || {};
  return {
    ingestRate: Array.isArray(source.ingestRate) ? source.ingestRate : [],
    queueDepth: Array.isArray(source.queueDepth) ? source.queueDepth : [],
    deadLetters: Array.isArray(source.deadLetters) ? source.deadLetters : [],
    graphNodes: Array.isArray(source.graphNodes) ? source.graphNodes : [],
    graphEdges: Array.isArray(source.graphEdges) ? source.graphEdges : [],
    writeTps: [],
    queryP50: Array.isArray(source.queryP50) ? source.queryP50 : [],
    queryP95: Array.isArray(source.queryP95) ? source.queryP95 : [],
    queryP99: Array.isArray(source.queryP99) ? source.queryP99 : [],
    cacheHit: [],
    newVulns: []
  };
}

function liveRuntime(live, metrics) {
  const source = (live && live.runtime) || {};
  const services = liveArray(live, "services");
  const cloudResources = liveArray(live, "cloudResources");
  return Object.assign({}, source, {
    nodes: lastMetric(metrics, "graphNodes"),
    edges: lastMetric(metrics, "graphEdges"),
    repos: source.repos || source.repositories || 0,
    workloads: source.workloads || 0,
    services: services.length,
    cloudResources: cloudResources.length,
    queueOutstanding: source.queueOutstanding || 0,
    inFlight: source.inFlight || 0,
    deadLetters: source.deadLetters || 0,
    succeeded: source.succeeded || 0,
    indexStatus: source.indexStatus || "unavailable",
    backendVersion: source.backendVersion || "live"
  });
}

function liveConsoleData(base, live) {
  const metrics = liveMetrics(live && live.metrics);
  return Object.assign({}, base, live || {}, {
    org: "live",
    services: liveArray(live, "services"),
    vulns: liveArray(live, "vulns"),
    findings: liveArray(live, "findings"),
    relationships: liveArray(live, "relationships"),
    collectors: liveArray(live, "collectors"),
    cloudAccounts: liveArray(live, "cloudAccounts"),
    cloudResources: liveArray(live, "cloudResources"),
    imageInventory: liveArray(live, "imageInventory"),
    iacParityRows: liveArray(live, "iacParityRows"),
    dependencyInventory: liveArray(live, "dependencyInventory"),
    advisoryCatalog: liveArray(live, "advisoryCatalog"),
    deadCode: liveArray(live, "deadCode"),
    argocdApps: liveArray(live, "argocdApps"),
    langInventory: liveArray(live, "langInventory"),
    graph: (live && live.graph) || { nodes: [], edges: [] },
    nodeDetail: (live && live.nodeDetail) || {},
    codeImports: (live && live.codeImports) || {},
    obsCoverage: (live && live.obsCoverage) || {},
    obsCoverageSnapshot: (live && live.obsCoverageSnapshot) || { rows: [], providers: [], signals: [], source: "empty" },
    sbomSummary: (live && live.sbomSummary) || { total: 0, byStatus: {}, byArtifactKind: {}, truth: null },
    sbomInventory: (live && live.sbomInventory) || { groupBy: "subject_digest", buckets: [], truncated: false },
    metrics,
    runtime: liveRuntime(live, metrics)
  });
}

function App() {
  const [t, setTweak] = useTweaks(TWEAK_DEFAULTS);
  const [route, setRoute] = useStateA(() => canonicalRoute((location.hash || "#dashboard").slice(1).split("?")[0] || "dashboard"));
  const [drawer, setDrawer] = useStateA(null);
  const [graphStyle, setGraphStyle] = useStateA(t.graphStyle);
  const [verifiedOnly, setVerifiedOnly] = useStateA(false);
  const [srcOpen, setSrcOpen] = useStateA(false);
  const [liveClient, setLiveClient] = useStateA(null);
  const [searchQuery, setSearchQuery] = useStateA("");
  const searchInputRef = useRefA(null);
  const STORE = "eshu.console.environment";
  const [source, setSource] = useStateA(() => {
    try { const e = JSON.parse(localStorage.getItem(STORE) || "{}"); return { mode: "demo", base: e.apiBaseUrl || "/eshu-api/", key: "", status: "idle", msg: "", live: null }; }
    catch (_) { return { mode: "demo", base: "/eshu-api/", key: "", status: "idle", msg: "", live: null }; }
  });

  async function connectLive(base, key) {
    setLiveClient(null);
    setSource((s) => ({ ...s, mode: "live", base, key, status: "connecting", msg: "", live: null }));
    try {
      const client = new ESHU.EshuApiClient({ baseUrl: base, apiKey: key });
      await client.get("/api/v0/index-status"); // health probe
      const live = await ESHU.loadLive(client);
      try { localStorage.setItem(STORE, JSON.stringify({ apiBaseUrl: base, mode: "private", recentApiBaseUrls: [base] })); } catch (_) {}
      setLiveClient(client);
      setSource({ mode: "live", base, key, status: "connected", msg: "", live });
      setSrcOpen(false);
    } catch (e) {
      setLiveClient(null);
      setSource({ mode: "live", base, key, status: "unavailable", msg: (e && e.message) || "unreachable", live: null });
    }
  }
  function useDemo() { setLiveClient(null); setSource((s) => ({ ...s, mode: "demo", status: "idle", msg: "", live: null })); setSrcOpen(false); }

  const data = (source.mode === "live" && source.live) ? liveConsoleData(ESHU, source.live) : ESHU;
  if (source.mode === "live" && Array.isArray(data.services)) {
    data.servicesById = {};
    data.services.forEach((service) => { data.servicesById[service.id] = service; });
  }
  const liveSections = source.live ? Object.keys(source.live.prov || {}).filter((k) => source.live.prov[k] === "live") : [];

  useEffectA(() => {
    const onHash = () => setRoute(canonicalRoute(((location.hash || "#dashboard").slice(1).split("?")[0]) || "dashboard"));
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  useEffectA(() => { setGraphStyle(t.graphStyle); }, [t.graphStyle]);
  useEffectA(() => {
    const r = document.documentElement;
    r.setAttribute("data-accent", t.accent);
    r.setAttribute("data-surface", t.surface);
    r.setAttribute("data-density", t.density);
  }, [t.accent, t.surface, t.density]);
  useEffectA(() => {
    function onKey(e) { if (e.key === "Escape") { setDrawer(null); setSrcOpen(false); } }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffectA(() => { document.documentElement.setAttribute("data-verified", verifiedOnly ? "on" : "off"); }, [verifiedOnly]);

  function go(id) { setRouteHash(id); setRoute(canonicalRoute(id)); }
  const openService = (id) => setDrawer({ type: "service", id });
  const openNode = (node, graph) => {
    const sid = resolveServiceId(node, data);
    if (sid) setDrawer({ type: "service", id: sid });
    else setDrawer({ type: "node", node, graph: graph || data.graph });
  };
  const openCollector = (collector) => setDrawer({ type: "collector", collector });
  function openVuln(cve) { setDrawer(null); setRouteHash("vulnerabilities", "?cve=" + encodeURIComponent(cve)); setRoute("vulnerabilities"); }
  const goAndClose = (route) => { setDrawer(null); go(route); };

  function runSearch(rawQuery) {
    const query = rawQuery.trim();
    if (!query) return;
    const needle = query.toLowerCase();
    const repositoryId = prototypeRepositorySearchTarget(data, needle);
    if (repositoryId) {
      setDrawer(null);
      setRouteHash("reposource", "/" + encodeURIComponent(repositoryId) + "/source");
      setRoute("reposource");
      return;
    }
    const service = (data.services || []).find((s) => [s.name, s.id, s.repo].filter(Boolean).some((value) => String(value).toLowerCase().includes(needle)));
    if (service) {
      openService(service.id || service.name);
      return;
    }
    const vulnerabilityId = prototypeVulnerabilitySearchTarget(data, needle);
    if (vulnerabilityId) {
      openVuln(vulnerabilityId);
      return;
    }
    setDrawer(null);
    setRouteHash("explorer", "?q=" + encodeURIComponent(query));
    setRoute("explorer");
  }

  function submitSearch(event) {
    event.preventDefault();
    runSearch(searchQuery);
  }

  function submitSearchKey(event) {
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    event.preventDefault();
    runSearch(event.currentTarget.value);
  }

  function submitSearchButton(event) {
    event.preventDefault();
    runSearch((searchInputRef.current && searchInputRef.current.value) || searchQuery);
  }

  const [title, sub] = TITLES[route] || TITLES.dashboard;
  const runtime = data.runtime || {};
  const backendState = source.mode === "live" ? (runtime.indexStatus || source.status || "live") : "healthy";
  const backendVersion = runtime.backendVersion || "live";
  const runtimeProfile = runtime.profile || source.mode;

  return (
    <div className="shell">
      <nav className="sidebar">
        <a className="brand" href={routeHash("dashboard")} onClick={() => go("dashboard")}>
          <img className="brand-mark" src="assets/eshu-icon.svg" alt="" />
          <span><span className="brand-name">e<b>shu</b></span><span className="brand-sub">Context Graph</span></span>
        </a>
        {NAV.map((grp) => (
          <div key={grp.group}>
            <div className="nav-group-label">{grp.group}</div>
            {grp.items.map((it) => {
              const I = Icon[it.icon];
              const c = it.count ? it.count(data) : null;
              return (
                <a key={it.id} className={cx("nav-item", route === it.id && "active")} href={routeHash(it.id)} onClick={() => go(it.id)}>
                  <I /> {it.label}
                  {c != null && c > 0 ? <span className={cx("nav-count", it.alert && "alert")}>{c}</span> : null}
                </a>
              );
            })}
          </div>
        ))}
        <div className="sidebar-foot">
          <div className="backend-card">
            <div className="bc-top"><i />NornicDB · {backendState}</div>
            <div className="bc-meta"><span>{backendVersion}</span><span>{fmt(runtime.nodes || 0)} nodes</span></div>
          </div>
          <div className="row" style={{ justifyContent: "space-between", padding: "0 4px" }}>
            <span className="t-mut" style={{ fontSize: ".7rem" }}>Profile</span>
            <span className="mono" style={{ fontSize: ".7rem", color: "var(--muted)" }}>{runtimeProfile}</span>
          </div>
        </div>
      </nav>

      <div className="main">
        <header className="topbar">
          <div className="topbar-title"><h1>{title}</h1><span>{sub}</span></div>
          <form className="searchbox" onSubmit={submitSearch}>
            <button className="search-submit" type="submit" aria-label="Search" onClick={submitSearchButton}>
              <Icon.search size={16} />
            </button>
            <input ref={searchInputRef} placeholder="Search repos, services, CVEs, evidence…" value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)} onKeyDown={submitSearchKey} />
            <kbd>⌘K</kbd>
          </form>
          <button className={cx("topbar-btn verify-btn", verifiedOnly && "on")} title="Show verified evidence only (hide inferred / representative facts)" onClick={() => setVerifiedOnly((v) => !v)}>
            <Icon.shield />
          </button>
          <div className="source-wrap">
            <button className={cx("source-pill", "src-" + source.status)} onClick={() => setSrcOpen((o) => !o)}>
              <i />{source.mode === "demo" ? "Demo data" : source.status === "connected" ? "Live" : source.status === "connecting" ? "Connecting…" : "Live (offline)"}
            </button>
            {srcOpen ? <SourcePopover source={source} onDemo={useDemo} onConnect={connectLive} onClose={() => setSrcOpen(false)} /> : null}
          </div>
          <button className="topbar-btn" title="Alerts"><Icon.bell /></button>
          <div className="topbar-profile">ES</div>
        </header>
        {verifiedOnly ? (
          <div className="prov-banner"><Icon.shield size={14} /> Verified evidence only — hiding facts with <span className="truth-chip" style={{ "--tc": "var(--ember)" }}><i />inferred</span> truth (representative runtime / scan data Eshu would collect live). Findings & vulnerabilities are filtered.</div>
        ) : null}
        {source.mode === "live" && source.status === "connected" ? (
          <div className="prov-banner"><Icon.db size={14} /> Live Eshu API · <span className="mono">{source.base}</span> — {liveSections.length ? liveSections.join(", ") + " from live graph/API loaders" : "connected"}; unsupported sections show explicit empty/unavailable states.</div>
        ) : null}
        {source.mode === "live" && source.status === "unavailable" ? (
          <div className="prov-banner warn"><Icon.bolt size={14} /> Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? " · " + source.msg : ""}. Showing demo facts. Serve this page behind the /eshu-api/ proxy (browser can't reach localhost cross-origin).</div>
        ) : null}

        {route === "dashboard" ? <Dashboard data={data} client={liveClient} source={source} onOpenService={openService} onOpenNode={openNode} heroMode={t.heroMode} graphStyle={graphStyle} chartStyle={t.chartStyle} /> : null}
        {route === "explorer" ? <Explorer data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} graphStyle={graphStyle} setGraphStyle={(v) => { setGraphStyle(v); setTweak("graphStyle", v); }} verifiedOnly={verifiedOnly} /> : null}
        {route === "repos" ? <Repos data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} /> : null}
        {route === "reposource" ? <RepoSource data={data} client={liveClient} /> : null}
        {route === "catalog" ? <Catalog data={data} client={liveClient} onOpenService={openService} /> : null}
        {route === "findings" ? <Findings data={data} client={liveClient} onOpenService={openService} onOpenVuln={openVuln} verifiedOnly={verifiedOnly} /> : null}
        {route === "images" ? <Images data={data} onOpenService={openService} /> : null}
        {route === "iac" ? <IaC data={data} onOpenService={openService} /> : null}
        {route === "deadcode" ? <DeadCode data={data} onOpenService={openService} /> : null}
        {route === "codegraph" ? <CodeGraph data={data} client={liveClient} onOpenService={openService} /> : null}
        {route === "vulnerabilities" ? <Vulnerabilities data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} chartStyle={t.chartStyle} verifiedOnly={verifiedOnly} /> : null}
        {route === "topology" ? <Topology data={data} client={liveClient} onOpenNode={openNode} onOpenService={openService} /> : null}
        {route === "cloud" ? <Cloud data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} /> : null}
        {route === "observability" ? <Observability data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} onOpenCollector={openCollector} /> : null}
        {route === "sbom" ? <SBOM data={data} onOpenService={openService} /> : null}
        {route === "dependencies" ? <Dependencies data={data} onOpenService={openService} /> : null}
        {route === "workspace" ? <Workspace data={data} client={liveClient} onOpenService={openService} onOpenNode={openNode} /> : null}
        {route === "admin" ? <Admin data={data} source={source} onOpenCollector={openCollector} onOpenNode={openNode} /> : null}
      </div>

      {drawer && drawer.type === "service" ? <ServiceDrawer id={drawer.id} data={data} source={source} onClose={() => setDrawer(null)} onOpenService={openService} onOpenVuln={openVuln} onOpenNode={openNode} /> : null}
      {drawer && drawer.type === "node" ? <NodeDrawer node={drawer.node} graph={drawer.graph} data={data} onClose={() => setDrawer(null)} onOpenNode={openNode} onOpenService={openService} onOpenVuln={openVuln} onGo={goAndClose} /> : null}
      {drawer && drawer.type === "collector" ? <CollectorDrawer collector={drawer.collector} data={data} onClose={() => setDrawer(null)} onGo={goAndClose} onOpenNode={openNode} /> : null}

      <TweaksPanel title="Tweaks">
        <TweakSection label="Theme" />
        <TweakColor label="Accent" value={t.accent === "teal" ? "#14b8a6" : t.accent === "ember" ? "#ff8a00" : t.accent === "violet" ? "#8b5cf6" : "#4f8cff"}
          options={["#14b8a6", "#ff8a00", "#8b5cf6", "#4f8cff"]}
          onChange={(v) => setTweak("accent", { "#14b8a6": "teal", "#ff8a00": "ember", "#8b5cf6": "violet", "#4f8cff": "blue" }[v])} />
        <TweakRadio label="Surface" value={t.surface} options={["graphite", "ink", "slate"]} onChange={(v) => setTweak("surface", v)} />
        <TweakRadio label="Density" value={t.density} options={["compact", "cozy", "comfortable"]} onChange={(v) => setTweak("density", v)} />
        <TweakSection label="Dashboard hero" />
        <TweakRadio label="Hero" value={t.heroMode} options={["graph", "health", "spotlight"]} onChange={(v) => setTweak("heroMode", v)} />
        <TweakSection label="Data viz" />
        <TweakRadio label="Graph layout" value={t.graphStyle} options={["layered", "radial"]} onChange={(v) => setTweak("graphStyle", v)} />
        <TweakRadio label="Coverage chart" value={t.chartStyle} options={["bars", "donut"]} onChange={(v) => setTweak("chartStyle", v)} />
      </TweaksPanel>
    </div>
  );
}

function prototypeVulnerabilitySearchTarget(D, needle) {
  const reachable = (D.vulns || []).find((v) => String(v.cve || "").toLowerCase() === needle);
  if (reachable) return reachable.cve;
  const advisory = (D.advisoryCatalog || []).find((row) =>
    [row.id, row.cve, row.ghsa].filter(Boolean).some((value) => String(value).toLowerCase() === needle)
  );
  if (!advisory) return null;
  return advisory.cve || advisory.ghsa || advisory.id;
}

function prototypeRepositorySearchTarget(D, needle) {
  const repository = (D.services || []).find((service) => {
    const isRepositoryOnly = service.system === "Repository" || service.kind === "repo" || String(service.id || "").startsWith("repository:");
    if (!isRepositoryOnly) return false;
    return [service.id, service.name, service.repo].filter(Boolean).some((value) => String(value).toLowerCase() === needle);
  });
  return (repository && (repository.id || repository.name)) || null;
}

function SourcePopover({ source, onDemo, onConnect, onClose }) {
  const [base, setBase] = useStateA(source.base || "/eshu-api/");
  const [key, setKey] = useStateA(source.key || "");
  return (
    <>
      <div className="popover-scrim" onClick={onClose} />
      <div className="popover" role="dialog" aria-label="Data source">
        <div className="popover-head"><strong>Data source</strong><span className="t-mut" style={{ fontSize: ".72rem" }}>read-only</span></div>
        <button className={cx("source-opt", source.mode === "demo" && "active")} onClick={onDemo}>
          <div><strong>Demo data</strong><span>Bundled sample workspace — swap fixtures freely</span></div>
          {source.mode === "demo" ? <Icon.shield size={15} /> : null}
        </button>
        <div className={cx("source-opt", "col", source.mode === "live" && "active")}>
          <div><strong>Live Eshu API</strong><span>application/eshu.envelope+json · read-only</span></div>
          <div className="row" style={{ gap: 6, marginTop: 8 }}>
            <input className="popover-input mono" value={base} onChange={(e) => setBase(e.target.value)} placeholder="/eshu-api/ or https://eshu.internal" />
            <button className="btn-ghost active" onClick={() => onConnect(base, key)}>Connect</button>
          </div>
          <input className="popover-input mono" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="Authorization: Bearer … (API key)" style={{ width: "100%", marginTop: 6 }} autoComplete="off" />
          {source.mode === "live" && source.status === "unavailable" ? <span className="src-err">⚠ {source.msg || "unreachable"} — showing demo</span> : null}
          {source.mode === "live" && source.status === "connected" ? <span className="src-ok">✓ connected</span> : null}
        </div>
        <p className="t-mut" style={{ fontSize: ".7rem", margin: "4px 2px 0", lineHeight: 1.5 }}>Local Compose proxies <span className="mono">/eshu-api/</span> → <span className="mono">127.0.0.1:8080</span>. Truth & freshness from the envelope are preserved, never flattened.</p>
      </div>
    </>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
