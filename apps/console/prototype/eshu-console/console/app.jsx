/* Eshu Console — app shell, nav, routing, tweaks. */
const { useState: useStateA, useEffect: useEffectA } = React;

const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "accent": "teal",
  "surface": "graphite",
  "density": "cozy",
  "heroMode": "graph",
  "graphStyle": "layered",
  "chartStyle": "bars"
}/*EDITMODE-END*/;

const NAV = [
  { group: "Overview", items: [
    { id: "dashboard", label: "Dashboard", icon: "dashboard" },
    { id: "explorer", label: "Graph Explorer", icon: "graph" }
  ] },
  { group: "Inventory", items: [
    { id: "repos", label: "Repositories", icon: "catalog", count: () => ESHU.services.filter((s) => s.repo).length },
    { id: "catalog", label: "Catalog", icon: "box", count: () => ESHU.services.length },
    { id: "findings", label: "Findings", icon: "findings", alert: true, count: () => ESHU.findings.length + ESHU.vulns.length },
    { id: "images", label: "Images", icon: "box", count: () => ESHU.services.filter((s) => s.image).length },
    { id: "iac", label: "IaC", icon: "layers", count: () => ESHU.cloudResources.filter((r) => r.tf).length },
    { id: "vulnerabilities", label: "Vulnerabilities", icon: "vuln", alert: true, count: () => ESHU.vulns.length }
  ] },
  { group: "Code", items: [
    { id: "deadcode", label: "Dead code", icon: "findings", count: () => ESHU.deadCode.length },
    { id: "codegraph", label: "Code graph", icon: "branch" }
  ] },
  { group: "Cloud & Telemetry", items: [
    { id: "topology", label: "Topology", icon: "graph" },
    { id: "cloud", label: "Cloud", icon: "cloud", count: () => ESHU.cloudResources.length },
    { id: "observability", label: "Observability", icon: "pulse" },
    { id: "sbom", label: "SBOM", icon: "shield", count: () => ESHU.vulns.length },
    { id: "dependencies", label: "Dependencies", icon: "branch" }
  ] },
  { group: "System", items: [
    { id: "admin", label: "Operations", icon: "admin" }
  ] }
];

const TITLES = {
  dashboard: ["Dashboard", "Read-only code-to-cloud graph status & evidence"],
  explorer: ["Graph Explorer", "Drill into the live NornicDB relationship graph"],
  repos: ["Repositories", "Browse every indexed source repository"],
  catalog: ["Catalog", "Services, repositories & workloads"],
  findings: ["Findings", "What needs human attention — one worklist"],
  images: ["Images", "Container image inventory and package risk"],
  iac: ["IaC", "Terraform and ArgoCD evidence"],
  deadcode: ["Dead code", "Unreferenced symbols — analyzer findings"],
  codegraph: ["Code graph", "Symbol & module relationships (CALLS / IMPORTS)"],
  vulnerabilities: ["Findings", "CVE register — vulnerability intelligence"],
  topology: ["Topology", "Full code-to-cloud path for a service"],
  cloud: ["Cloud", "Multi-cloud resource inventory — code-to-cloud"],
  observability: ["Observability", "Signal coverage correlated per service"],
  sbom: ["SBOM", "Package evidence and advisory reachability"],
  dependencies: ["Dependencies", "Source, service and datastore dependency edges"],
  admin: ["Operations", "Eshu runtime & NornicDB backend health"]
};

function App() {
  const [t, setTweak] = useTweaks(TWEAK_DEFAULTS);
  const [route, setRoute] = useStateA(() => (location.hash || "#dashboard").slice(1).split("?")[0] || "dashboard");
  const [drawer, setDrawer] = useStateA(null);
  const [graphStyle, setGraphStyle] = useStateA(t.graphStyle);
  const [verifiedOnly, setVerifiedOnly] = useStateA(false);
  const [srcOpen, setSrcOpen] = useStateA(false);
  const STORE = "eshu.console.environment";
  const [source, setSource] = useStateA(() => {
    try { const e = JSON.parse(localStorage.getItem(STORE) || "{}"); return { mode: "demo", base: e.apiBaseUrl || "/eshu-api/", key: e.apiKey || "", status: "idle", msg: "", live: null }; }
    catch (_) { return { mode: "demo", base: "/eshu-api/", key: "", status: "idle", msg: "", live: null }; }
  });

  async function connectLive(base, key) {
    setSource((s) => ({ ...s, mode: "live", base, key, status: "connecting", msg: "", live: null }));
    try {
      const client = new ESHU.EshuApiClient({ baseUrl: base, apiKey: key });
      await client.get("/api/v0/index-status"); // health probe
      const live = await ESHU.loadLive(client);
      try { localStorage.setItem(STORE, JSON.stringify({ apiBaseUrl: base, apiKey: key, mode: "private", recentApiBaseUrls: [base] })); } catch (_) {}
      setSource({ mode: "live", base, key, status: "connected", msg: "", live });
      setSrcOpen(false);
    } catch (e) {
      setSource({ mode: "live", base, key, status: "unavailable", msg: (e && e.message) || "unreachable", live: null });
    }
  }
  function useDemo() { setSource((s) => ({ ...s, mode: "demo", status: "idle", msg: "", live: null })); setSrcOpen(false); }

  const data = (source.mode === "live" && source.live) ? Object.assign({}, ESHU, source.live) : ESHU;
  const liveSections = source.live ? Object.keys(source.live.prov || {}).filter((k) => source.live.prov[k] === "live") : [];

  useEffectA(() => {
    const onHash = () => setRoute(((location.hash || "#dashboard").slice(1).split("?")[0]) || "dashboard");
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

  function go(id) { location.hash = id; setRoute(id); }
  const openService = (id) => setDrawer({ type: "service", id });
  const openNode = (node, graph) => {
    const sid = resolveServiceId(node, data);
    if (sid) setDrawer({ type: "service", id: sid });
    else setDrawer({ type: "node", node, graph: graph || data.graph });
  };
  const openCollector = (collector) => setDrawer({ type: "collector", collector });
  function openVuln(cve) { setDrawer(null); location.hash = "vulnerabilities?cve=" + encodeURIComponent(cve); setRoute("vulnerabilities"); }
  const goAndClose = (route) => { setDrawer(null); go(route); };

  const [title, sub] = TITLES[route] || TITLES.dashboard;

  return (
    <div className="shell">
      <nav className="sidebar">
        <a className="brand" href="#dashboard" onClick={() => go("dashboard")}>
          <img className="brand-mark" src="assets/eshu-icon.svg" alt="" />
          <span><span className="brand-name">e<b>shu</b></span><span className="brand-sub">Context Graph</span></span>
        </a>
        {NAV.map((grp) => (
          <div key={grp.group}>
            <div className="nav-group-label">{grp.group}</div>
            {grp.items.map((it) => {
              const I = Icon[it.icon];
              const c = it.count ? it.count() : null;
              return (
                <a key={it.id} className={cx("nav-item", route === it.id && "active")} href={"#" + it.id} onClick={() => go(it.id)}>
                  <I /> {it.label}
                  {c != null && c > 0 ? <span className={cx("nav-count", it.alert && "alert")}>{c}</span> : null}
                </a>
              );
            })}
          </div>
        ))}
        <div className="sidebar-foot">
          <div className="backend-card">
            <div className="bc-top"><i />NornicDB · healthy</div>
            <div className="bc-meta"><span>{ESHU.runtime.backendVersion}</span><span>{fmt(ESHU.runtime.nodes)} nodes</span></div>
          </div>
          <div className="row" style={{ justifyContent: "space-between", padding: "0 4px" }}>
            <span className="t-mut" style={{ fontSize: ".7rem" }}>Profile</span>
            <span className="mono" style={{ fontSize: ".7rem", color: "var(--muted)" }}>{ESHU.runtime.profile}</span>
          </div>
        </div>
      </nav>

      <div className="main">
        <header className="topbar">
          <div className="topbar-title"><h1>{title}</h1><span>{sub}</span></div>
          <div className="searchbox">
            <Icon.search size={16} />
            <input placeholder="Search repos, services, CVEs, evidence…" />
            <kbd>⌘K</kbd>
          </div>
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
          <div className="prov-banner"><Icon.db size={14} /> Live Eshu API · <span className="mono">{source.base}</span> — {liveSections.length ? liveSections.join(", ") + " from the graph" : "connected"}; sections without a live endpoint show demo facts.</div>
        ) : null}
        {source.mode === "live" && source.status === "unavailable" ? (
          <div className="prov-banner warn"><Icon.bolt size={14} /> Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? " · " + source.msg : ""}. Showing demo facts. Serve this page behind the /eshu-api/ proxy (browser can't reach localhost cross-origin).</div>
        ) : null}

        {route === "dashboard" ? <Dashboard data={data} onOpenService={openService} onOpenNode={openNode} heroMode={t.heroMode} graphStyle={graphStyle} chartStyle={t.chartStyle} /> : null}
        {route === "explorer" ? <Explorer data={data} onOpenService={openService} onOpenNode={openNode} graphStyle={graphStyle} setGraphStyle={(v) => { setGraphStyle(v); setTweak("graphStyle", v); }} verifiedOnly={verifiedOnly} /> : null}
        {route === "repos" ? <Repos data={data} onOpenService={openService} onOpenNode={openNode} /> : null}
        {route === "catalog" ? <Catalog data={data} onOpenService={openService} /> : null}
        {route === "findings" ? <Findings data={data} onOpenService={openService} onOpenVuln={openVuln} verifiedOnly={verifiedOnly} /> : null}
        {route === "images" ? <Images data={data} onOpenService={openService} /> : null}
        {route === "iac" ? <IaC data={data} onOpenService={openService} /> : null}
        {route === "deadcode" ? <DeadCode data={data} onOpenService={openService} /> : null}
        {route === "codegraph" ? <CodeGraph data={data} onOpenService={openService} /> : null}
        {route === "vulnerabilities" ? <Vulnerabilities data={data} onOpenService={openService} onOpenNode={openNode} chartStyle={t.chartStyle} verifiedOnly={verifiedOnly} /> : null}
        {route === "topology" ? <Topology data={data} onOpenNode={openNode} /> : null}
        {route === "cloud" ? <Cloud data={data} onOpenService={openService} onOpenNode={openNode} /> : null}
        {route === "observability" ? <Observability data={data} onOpenService={openService} onOpenNode={openNode} onOpenCollector={openCollector} /> : null}
        {route === "sbom" ? <SBOM data={data} onOpenService={openService} /> : null}
        {route === "dependencies" ? <Dependencies data={data} onOpenService={openService} /> : null}
        {route === "admin" ? <Admin data={data} source={source} onOpenCollector={openCollector} onOpenNode={openNode} /> : null}
      </div>

      {drawer && drawer.type === "service" ? <ServiceDrawer id={drawer.id} data={data} onClose={() => setDrawer(null)} onOpenService={openService} onOpenVuln={openVuln} onOpenNode={openNode} /> : null}
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
