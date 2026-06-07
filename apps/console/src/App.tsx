// App.tsx — redesigned dark console shell.
// Live data only: the console renders the Eshu API exclusively. There is no demo
// or sample data path. Before a connection exists (or after one fails) the shell
// shows an explicit loading / needs-connection / error state instead of any
// fabricated numbers. main.tsx already wraps this in <BrowserRouter>.
import { useEffect, useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { EshuApiClient } from "./api/client";
import { loadConsoleSnapshot } from "./api/eshuConsoleLive";
import { loadConsoleEnvironment, saveConsoleEnvironment } from "./config/environment";
import { emptyConsoleModel, modelFromSnapshot } from "./console/liveModel";
import type { ConsoleModel } from "./console/types";
import { fmt } from "./console/types";
import { DashboardPage } from "./pages/DashboardPage";
import { CatalogPage } from "./pages/CatalogPage";
import { FindingsPage } from "./pages/FindingsPage";
import { OperationsPage } from "./pages/OperationsPage";
import { VulnerabilitiesPage } from "./pages/VulnerabilitiesPage";
import { VulnDetailPage } from "./pages/VulnDetailPage";
import { ObservabilityPage } from "./pages/ObservabilityPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { RepositoriesPage } from "./pages/RepositoriesPage";
import { RepoSourcePage } from "./pages/RepoSourcePage";
import { ServiceDrawer } from "./components/ServiceDrawer";
import "./styles.css";

const NAV: readonly { to: string; label: string }[] = [
  { to: "/dashboard", label: "Dashboard" },
  { to: "/explorer", label: "Graph Explorer" },
  { to: "/repositories", label: "Repositories" },
  { to: "/catalog", label: "Catalog" },
  { to: "/findings", label: "Findings" },
  { to: "/vulnerabilities", label: "Vulnerabilities" },
  { to: "/observability", label: "Observability" },
  { to: "/operations", label: "Operations" }
];

// Connection lifecycle. The console requires a live API; "needs-connection" is the
// initial state when no saved environment exists, and "error" is a failed connect.
type ConnStatus = "needs-connection" | "connecting" | "connected" | "error";

type SourceState = { base: string; key: string; status: ConnStatus; msg: string };

export function App(): React.JSX.Element {
  const env = loadConsoleEnvironment();
  const hasSavedEnv = env.mode === "private" && (env.apiBaseUrl || "").length > 0;
  const [model, setModel] = useState<ConsoleModel>(emptyConsoleModel());
  const [source, setSource] = useState<SourceState>({
    base: env.apiBaseUrl || "/eshu-api/",
    key: env.apiKey || "",
    status: hasSavedEnv ? "connecting" : "needs-connection",
    msg: ""
  });
  const [open, setOpen] = useState(false);
  const [client, setClient] = useState<EshuApiClient | undefined>();
  const [drawer, setDrawer] = useState<string | null>(null);

  async function connect(base: string, key: string): Promise<void> {
    setSource((s) => ({ ...s, base, key, status: "connecting", msg: "" }));
    try {
      const next = new EshuApiClient({ baseUrl: base, apiKey: key });
      const snap = await loadConsoleSnapshot(next);
      saveConsoleEnvironment({ mode: "private", apiBaseUrl: base, apiKey: key, recentApiBaseUrls: [base] });
      setClient(next);
      setModel(modelFromSnapshot(snap));
      setSource({ base, key, status: "connected", msg: "" });
      setOpen(false);
    } catch (e) {
      // No demo fallback: keep an explicit empty/unavailable model so panels show
      // "—" / "API not available" rather than invented data.
      setClient(undefined);
      setModel(emptyConsoleModel("unavailable"));
      setSource({ base, key, status: "error", msg: e instanceof Error ? e.message : "unreachable" });
    }
  }
  const openService = (name: string): void => setDrawer(name);

  // Boot straight into live data when a saved environment exists. With no saved
  // environment, stay in "needs-connection" until the operator connects.
  useEffect(() => {
    if (hasSavedEnv) void connect(env.apiBaseUrl, env.apiKey || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent): void { if (e.key === "Escape") { setOpen(false); setDrawer(null); } }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pill =
    source.status === "connected" ? "Live"
      : source.status === "connecting" ? "Connecting…"
        : source.status === "error" ? "Live (offline)"
          : "Not connected";

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
            <div className="bc-meta"><span>{source.status === "connected" ? "live" : pill.toLowerCase()}</span><span>{source.status === "connected" ? `${fmt(model.runtime.repositories)} repos` : "—"}</span></div>
          </div>
        </div>
      </nav>
      <div className="main">
        <header className="topbar">
          <div className="topbar-title"><h1>Eshu Console</h1><span>Read-only code-to-cloud graph</span></div>
          <div className="source-wrap" style={{ marginLeft: "auto" }}>
            <button className={`source-pill src-${source.status}`} onClick={() => setOpen((o) => !o)}>
              <i />{pill}
            </button>
            {open ? <SourcePopover source={source} onConnect={connect} onClose={() => setOpen(false)} /> : null}
          </div>
        </header>
        {source.status === "error" ? (
          <div className="prov-banner warn">Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? ` · ${source.msg}` : ""}. <button className="link-btn" onClick={() => setOpen(true)}>Edit data source</button></div>
        ) : null}
        {source.status === "connected" ? (
          <Routes>
            <Route path="/" element={<DashboardPage model={model} client={client} onOpenService={openService} />} />
            <Route path="/dashboard" element={<DashboardPage model={model} client={client} onOpenService={openService} />} />
            <Route path="/explorer" element={<ExplorerPage model={model} client={client} onOpenService={openService} />} />
            <Route path="/repositories" element={<RepositoriesPage client={client} />} />
            <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
            <Route path="/catalog" element={<CatalogPage model={model} onOpenService={openService} />} />
            <Route path="/findings" element={<FindingsPage model={model} />} />
            <Route path="/vulnerabilities" element={<VulnerabilitiesPage model={model} />} />
            <Route path="/vulnerabilities/:id" element={<VulnDetailPage model={model} client={client} />} />
            <Route path="/observability" element={<ObservabilityPage client={client} />} />
            <Route path="/operations" element={<OperationsPage model={model} />} />
          </Routes>
        ) : (
          <ConnectionState status={source.status} onConnect={() => setOpen(true)} />
        )}
      </div>
      {drawer && client ? <ServiceDrawer name={drawer} model={model} client={client} onClose={() => setDrawer(null)} /> : null}
    </div>
  );
}

// ConnectionState renders the non-connected lifecycle: a loading spinner while
// connecting, or a prompt to connect when there is no live API yet. No data is
// shown here — the console never fabricates numbers without a live connection.
function ConnectionState({ status, onConnect }: {
  readonly status: ConnStatus;
  readonly onConnect: () => void;
}): React.JSX.Element {
  if (status === "connecting") {
    return (
      <div className="conn-state" role="status" aria-live="polite">
        <div className="conn-spinner" aria-hidden />
        <p>Connecting to the Eshu API…</p>
      </div>
    );
  }
  const title = status === "error" ? "No live data" : "Connect to a live Eshu API";
  const detail = status === "error"
    ? "The last connection attempt failed. Check the base URL and API key, then reconnect."
    : "The console renders live API data only. Enter your Eshu API base URL (and key, if required) to begin.";
  return (
    <div className="conn-state">
      <h2>{title}</h2>
      <p>{detail}</p>
      <button className="btn-ghost active" onClick={onConnect}>Open data source</button>
    </div>
  );
}

function SourcePopover({ source, onConnect, onClose }: {
  readonly source: SourceState;
  readonly onConnect: (base: string, key: string) => void;
  readonly onClose: () => void;
}): React.JSX.Element {
  const [base, setBase] = useState(source.base || "/eshu-api/");
  const [key, setKey] = useState(source.key || "");
  return (
    <>
      <div className="popover-scrim" onClick={onClose} />
      <div className="popover" role="dialog" aria-label="Data source">
        <div className="popover-head"><strong>Data source</strong><span className="t-mut" style={{ fontSize: ".72rem" }}>read-only · live</span></div>
        <div className="source-opt col active">
          <div><strong>Live Eshu API</strong><span>application/eshu.envelope+json</span></div>
          <div className="row" style={{ gap: 6, marginTop: 8 }}>
            <input className="popover-input mono" value={base} onChange={(e) => setBase(e.target.value)} placeholder="/eshu-api/" />
            <button className="btn-ghost active" onClick={() => onConnect(base, key)}>Connect</button>
          </div>
          <input className="popover-input mono" type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="API key (Bearer)" style={{ width: "100%", marginTop: 6 }} autoComplete="off" />
          {source.status === "error" ? <span className="src-err">⚠ {source.msg || "unreachable"}</span> : null}
          {source.status === "connected" ? <span className="src-ok">✓ connected</span> : null}
          {source.status === "connecting" ? <span className="t-mut" style={{ fontSize: ".72rem" }}>connecting…</span> : null}
        </div>
        <p className="t-mut" style={{ fontSize: ".7rem", margin: "4px 2px 0", lineHeight: 1.5 }}>The console dev server proxies <span className="mono">/eshu-api/</span> → <span className="mono">127.0.0.1:8080</span>. Key is kept in memory for this session only.</p>
      </div>
    </>
  );
}
