// App.tsx — redesigned dark console shell.
// Replaces the existing apps/console/src/App.tsx. main.tsx already wraps this in
// <BrowserRouter>. Demo by default; switch to Live to hydrate from the API.
import { useEffect, useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { EshuApiClient } from "./api/client";
import { loadConsoleSnapshot } from "./api/eshuConsoleLive";
import { loadConsoleEnvironment, saveConsoleEnvironment } from "./config/environment";
import { demoModel, modelFromSnapshot } from "./console/demoModel";
import type { ConsoleModel } from "./console/types";
import { fmt } from "./console/types";
import { DashboardPage } from "./pages/DashboardPage";
import { CatalogPage } from "./pages/CatalogPage";
import { FindingsPage } from "./pages/FindingsPage";
import { OperationsPage } from "./pages/OperationsPage";
import { VulnerabilitiesPage } from "./pages/VulnerabilitiesPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { ServiceDrawer } from "./components/ServiceDrawer";
import "./styles.css";

const NAV: readonly { to: string; label: string }[] = [
  { to: "/dashboard", label: "Dashboard" },
  { to: "/explorer", label: "Graph Explorer" },
  { to: "/catalog", label: "Catalog" },
  { to: "/findings", label: "Findings" },
  { to: "/vulnerabilities", label: "Vulnerabilities" },
  { to: "/operations", label: "Operations" }
];

type SourceState = {
  mode: "demo" | "live"; base: string; key: string;
  status: "idle" | "connecting" | "connected" | "unavailable"; msg: string;
};

export function App(): React.JSX.Element {
  const env = loadConsoleEnvironment();
  const [model, setModel] = useState<ConsoleModel>(demoModel);
  const [source, setSource] = useState<SourceState>({ mode: "demo", base: env.apiBaseUrl || "/eshu-api/", key: env.apiKey || "", status: "idle", msg: "" });
  const [open, setOpen] = useState(false);
  const [client, setClient] = useState<EshuApiClient | undefined>();
  const [drawer, setDrawer] = useState<string | null>(null);

  async function connect(base: string, key: string): Promise<void> {
    setSource((s) => ({ ...s, mode: "live", base, key, status: "connecting", msg: "" }));
    try {
      const next = new EshuApiClient({ baseUrl: base, apiKey: key });
      const snap = await loadConsoleSnapshot(next);
      saveConsoleEnvironment({ mode: "private", apiBaseUrl: base, apiKey: key, recentApiBaseUrls: [base] });
      setClient(next);
      setModel(modelFromSnapshot(snap));
      setSource({ mode: "live", base, key, status: "connected", msg: "" });
      setOpen(false);
    } catch (e) {
      setModel(demoModel);
      setSource({ mode: "live", base, key, status: "unavailable", msg: e instanceof Error ? e.message : "unreachable" });
    }
  }
  function useDemo(): void { setClient(undefined); setModel(demoModel); setSource((s) => ({ ...s, mode: "demo", status: "idle", msg: "" })); setOpen(false); }
  const openService = (name: string): void => setDrawer(name);

  // Boot straight into live data when a saved environment exists, so the console
  // uses the API by default. Falls back to demo only if the API is unreachable.
  useEffect(() => {
    if (env.mode === "private" && (env.apiBaseUrl || "").length > 0) {
      void connect(env.apiBaseUrl, env.apiKey || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent): void { if (e.key === "Escape") { setOpen(false); setDrawer(null); } }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="shell">
      <nav className="sidebar">
        <a className="brand" href="/"><span><span className="brand-name">e<b>shu</b></span><span className="brand-sub">Context Graph</span></span></a>
        <div className="nav-group-label">Console</div>
        {NAV.map((n) => (
          <NavLink key={n.to} to={n.to} className={({ isActive }) => `nav-item${isActive ? " active" : ""}`}>{n.label}</NavLink>
        ))}
        <div className="sidebar-foot">
          <div className="backend-card">
            <div className="bc-top"><i />{model.runtime.indexStatus}</div>
            <div className="bc-meta"><span>{model.source}</span><span>{fmt(model.runtime.repositories)} repos</span></div>
          </div>
        </div>
      </nav>
      <div className="main">
        <header className="topbar">
          <div className="topbar-title"><h1>Eshu Console</h1><span>Read-only code-to-cloud graph</span></div>
          <div className="source-wrap" style={{ marginLeft: "auto" }}>
            <button className={`source-pill src-${source.status}`} onClick={() => setOpen((o) => !o)}>
              <i />{source.mode === "demo" ? "Demo data" : source.status === "connected" ? "Live" : source.status === "connecting" ? "Connecting…" : "Live (offline)"}
            </button>
            {open ? <SourcePopover source={source} onDemo={useDemo} onConnect={connect} onClose={() => setOpen(false)} /> : null}
          </div>
        </header>
        {source.mode === "live" && source.status === "unavailable" ? (
          <div className="prov-banner warn">Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? ` · ${source.msg}` : ""}. Showing demo facts.</div>
        ) : null}
        <Routes>
          <Route path="/" element={<DashboardPage model={model} onOpenService={openService} />} />
          <Route path="/dashboard" element={<DashboardPage model={model} onOpenService={openService} />} />
          <Route path="/explorer" element={<ExplorerPage model={model} client={client} onOpenService={openService} />} />
          <Route path="/catalog" element={<CatalogPage model={model} onOpenService={openService} />} />
          <Route path="/findings" element={<FindingsPage model={model} />} />
          <Route path="/vulnerabilities" element={<VulnerabilitiesPage model={model} />} />
          <Route path="/operations" element={<OperationsPage model={model} />} />
        </Routes>
      </div>
      {drawer ? <ServiceDrawer name={drawer} model={model} client={client} onClose={() => setDrawer(null)} /> : null}
    </div>
  );
}

function SourcePopover({ source, onDemo, onConnect, onClose }: {
  readonly source: SourceState;
  readonly onDemo: () => void;
  readonly onConnect: (base: string, key: string) => void;
  readonly onClose: () => void;
}): React.JSX.Element {
  const [base, setBase] = useState(source.base || "/eshu-api/");
  const [key, setKey] = useState(source.key || "");
  return (
    <>
      <div className="popover-scrim" onClick={onClose} />
      <div className="popover" role="dialog" aria-label="Data source">
        <div className="popover-head"><strong>Data source</strong><span className="t-mut" style={{ fontSize: ".72rem" }}>read-only</span></div>
        <button className={`source-opt${source.mode === "demo" ? " active" : ""}`} onClick={onDemo}>
          <div><strong>Demo data</strong><span>Bundled sample workspace</span></div>
        </button>
        <div className={`source-opt col${source.mode === "live" ? " active" : ""}`}>
          <div><strong>Live Eshu API</strong><span>application/eshu.envelope+json</span></div>
          <div className="row" style={{ gap: 6, marginTop: 8 }}>
            <input className="popover-input mono" value={base} onChange={(e) => setBase(e.target.value)} placeholder="/eshu-api/" />
            <button className="btn-ghost active" onClick={() => onConnect(base, key)}>Connect</button>
          </div>
          <input className="popover-input mono" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="API key (Bearer)" style={{ width: "100%", marginTop: 6 }} autoComplete="off" />
          {source.mode === "live" && source.status === "unavailable" ? <span className="src-err">⚠ {source.msg || "unreachable"} — showing demo</span> : null}
          {source.mode === "live" && source.status === "connected" ? <span className="src-ok">✓ connected</span> : null}
        </div>
        <p className="t-mut" style={{ fontSize: ".7rem", margin: "4px 2px 0", lineHeight: 1.5 }}>The console dev server proxies <span className="mono">/eshu-api/</span> → <span className="mono">127.0.0.1:8080</span>. Key is stored only in the browser.</p>
      </div>
    </>
  );
}
