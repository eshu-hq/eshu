// App.tsx — redesigned dark console shell.
// Live data only: the console renders the Eshu API exclusively. There is no demo
// or sample data path. Before a connection exists (or after one fails) the shell
// shows an explicit loading / needs-connection / error state instead of any
// fabricated numbers. main.tsx already wraps this in <BrowserRouter>.
import { useEffect, useRef, useState, type FormEvent } from "react";
import { NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import {
  Bell,
  Boxes,
  Cloud,
  Code2,
  FolderGit2,
  GitBranch,
  Images,
  LayoutDashboard,
  Network,
  PackageSearch,
  Search,
  ServerCog,
  ShieldCheck,
  TriangleAlert,
  Waves
} from "lucide-react";
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
import { SbomPage } from "./pages/SbomPage";
import { ObservabilityPage } from "./pages/ObservabilityPage";
import { DependenciesPage } from "./pages/DependenciesPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { IacPage } from "./pages/IacPage";
import { RepositoriesPage } from "./pages/RepositoriesPage";
import { RepoSourcePage } from "./pages/RepoSourcePage";
import { ImagesPage } from "./pages/ImagesPage";
import { CloudPage } from "./pages/CloudPage";
import { TopologyPage } from "./pages/TopologyPage";
import { DeadCodePage } from "./pages/DeadCodePage";
import { CodeGraphPage } from "./pages/CodeGraphPage";
import { WorkspacePage } from "./pages/WorkspacePage";
import { ServiceDrawer } from "./components/ServiceDrawer";
import "./styles.css";
import "./appShell.css";

type NavItem = {
  readonly to: string;
  readonly label: string;
  readonly icon: LucideIcon;
  readonly count?: (model: ConsoleModel) => number | string | null;
  readonly alert?: boolean;
};

const NAV_GROUPS: readonly { readonly label: string; readonly items: readonly NavItem[] }[] = [
  {
    label: "Overview",
    items: [
      { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
      { to: "/explorer", label: "Graph Explorer", icon: GitBranch }
    ]
  },
  {
    label: "Inventory",
    items: [
      { to: "/repositories", label: "Repositories", icon: FolderGit2, count: (m) => nonZero(m.runtime.repositories) },
      { to: "/catalog", label: "Catalog", icon: Boxes, count: (m) => nonZero(m.services?.length ?? 0) },
      { to: "/findings", label: "Findings", icon: TriangleAlert, count: (m) => nonZero(m.findings?.length ?? 0), alert: true },
      { to: "/images", label: "Images", icon: Images, count: (m) => nonZero(m.images?.length ?? 0) },
      { to: "/iac", label: "IaC", icon: Network, count: (m) => nonZero(m.iacResources?.length ?? 0) },
      { to: "/vulnerabilities", label: "Vulnerabilities", icon: ShieldCheck, count: (m) => nonZero(m.vulnerabilities?.length ?? 0), alert: true }
    ]
  },
  {
    label: "Code",
    items: [
      { to: "/dead-code", label: "Dead code", icon: TriangleAlert, count: (m) => nonZero(m.findings.filter((finding) => finding.type === "Dead code").length) },
      { to: "/code-graph", label: "Code graph", icon: Code2 }
    ]
  },
  {
    label: "Cloud & Telemetry",
    items: [
      { to: "/topology", label: "Topology", icon: GitBranch },
      { to: "/cloud", label: "Cloud", icon: Cloud },
      { to: "/observability", label: "Observability", icon: Waves },
      { to: "/sbom", label: "SBOM", icon: PackageSearch, count: (m) => nonZero(m.sbom?.total ?? 0) },
      { to: "/dependencies", label: "Dependencies", icon: Boxes, count: (m) => nonZero(m.dependencies?.length ?? 0) }
    ]
  },
  {
    label: "System",
    items: [
      { to: "/operations", label: "Operations", icon: ServerCog }
    ]
  }
];

const NAV_ITEMS = NAV_GROUPS.flatMap((group) => group.items);

// Connection lifecycle. The console requires a live API; "needs-connection" is the
// initial state when no saved environment exists, and "error" is a failed connect.
type ConnStatus = "needs-connection" | "connecting" | "connected" | "error";

type SourceState = { base: string; key: string; status: ConnStatus; msg: string };

export function App(): React.JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
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
  const [searchQuery, setSearchQuery] = useState("");
  const [verifiedOnly, setVerifiedOnly] = useState(false);
  const visibleModel = verifiedOnly ? verifiedConsoleModel(model) : model;
  // Boot guard: React StrictMode runs effects twice in development, which would
  // otherwise fire two concurrent boot connects whose in-flight fetches abort
  // each other (issue #1727: ERR_ABORTED -> Catalog blank). The ref dedupes the
  // boot connect to a single run; user-initiated reconnects from the source
  // popover stay unguarded.
  const bootedRef = useRef(false);

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
  // environment, stay in "needs-connection" until the operator connects. The
  // bootedRef guard makes this StrictMode-safe: the discarded first dev run does
  // not launch a second boot connect that would abort the surviving run's
  // in-flight fetches and blank out sections like Catalog (issue #1727).
  useEffect(() => {
    if (hasSavedEnv && !bootedRef.current) {
      bootedRef.current = true;
      void connect(env.apiBaseUrl, env.apiKey || "");
    }
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
  const activeItem = activeNavItem(location.pathname);
  const pageTitle = location.pathname === "/" ? "Eshu Console" : activeItem?.label ?? "Eshu Console";

  function submitSearch(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const query = searchQuery.trim();
    if (query.length === 0) return;
    const needle = query.toLowerCase();
    const service = visibleModel.services.find((row) =>
      [row.name, row.id, row.repo].some((value) => value.toLowerCase().includes(needle))
    );
    if (service) {
      openService(service.name);
      return;
    }
    navigate(`/explorer?q=${encodeURIComponent(query)}`);
  }

  return (
    <div className="shell">
      <nav className="sidebar">
        <a className="brand" href="/">
          <span className="brand-mark brand-glyph" aria-hidden><i /><i /><i /></span>
          <span><span className="brand-name">e<b>shu</b></span><span className="brand-sub">Context Graph</span></span>
        </a>
        {NAV_GROUPS.map((group) => (
          <div className="nav-section" key={group.label}>
            <div className="nav-group-label">{group.label}</div>
            {group.items.map((n) => {
              const Icon = n.icon;
              const count = n.count?.(visibleModel) ?? null;
              return (
                <NavLink key={n.to} to={n.to} aria-label={n.label} className={({ isActive }) => `nav-item${isActive ? " active" : ""}`}>
                  <Icon aria-hidden />
                  <span className="nav-label">{n.label}</span>
                  {count !== null ? <span aria-hidden className={`nav-count${n.alert ? " alert" : ""}`}>{count}</span> : null}
                </NavLink>
              );
            })}
          </div>
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
          <div className="topbar-title"><h1>{pageTitle}</h1><span>Read-only code-to-cloud graph status & evidence</span></div>
          <form className="searchbox" onSubmit={submitSearch}>
            <Search aria-hidden />
            <input
              aria-label="Search Eshu"
              placeholder="Search repos, services, CVEs, evidence…"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
            />
            <kbd>⌘K</kbd>
          </form>
          <button
            aria-label="Show verified evidence only"
            aria-pressed={verifiedOnly}
            className={`topbar-btn verify-btn${verifiedOnly ? " on" : ""}`}
            title="Show verified evidence only"
            type="button"
            onClick={() => setVerifiedOnly((value) => !value)}
          >
            <ShieldCheck aria-hidden />
          </button>
          <span className="topbar-signal" title="No local notifications"><Bell aria-hidden /></span>
          <div className="source-wrap">
            <button className={`source-pill src-${source.status}`} onClick={() => setOpen((o) => !o)}>
              <i />{pill}
            </button>
            {open ? <SourcePopover source={source} onConnect={connect} onClose={() => setOpen(false)} /> : null}
          </div>
        </header>
        {verifiedOnly ? (
          <div className="prov-banner"><ShieldCheck aria-hidden size={14} /> Verified evidence only — hiding inferred findings and graph nodes.</div>
        ) : null}
        {source.status === "error" ? (
          <div className="prov-banner warn">Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? ` · ${source.msg}` : ""}. <button className="link-btn" onClick={() => setOpen(true)}>Edit data source</button></div>
        ) : null}
        {source.status === "connected" ? (
          <Routes>
            <Route path="/" element={<DashboardPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/dashboard" element={<DashboardPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/explorer" element={<ExplorerPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/code-graph" element={<CodeGraphPage model={visibleModel} client={client} />} />
            <Route path="/repositories" element={<RepositoriesPage client={client} model={visibleModel} />} />
            <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
            <Route path="/cloud" element={<CloudPage client={client} />} />
            <Route path="/topology" element={<TopologyPage client={client} model={visibleModel} onOpenService={openService} />} />
            <Route path="/catalog" element={<CatalogPage model={visibleModel} onOpenService={openService} />} />
            <Route path="/images" element={<ImagesPage client={client} />} />
            <Route path="/iac" element={<IacPage model={visibleModel} client={client} />} />
            <Route path="/findings" element={<FindingsPage model={visibleModel} />} />
            <Route path="/dead-code" element={<DeadCodePage client={client} model={visibleModel} />} />
            <Route path="/vulnerabilities" element={<VulnerabilitiesPage model={visibleModel} client={client} />} />
            <Route path="/vulnerabilities/:id" element={<VulnDetailPage model={visibleModel} client={client} />} />
            <Route path="/sbom" element={<SbomPage client={client} />} />
            <Route path="/dependencies" element={<DependenciesPage client={client} />} />
            <Route path="/observability" element={<ObservabilityPage client={client} />} />
            <Route path="/operations" element={<OperationsPage model={visibleModel} />} />
            <Route path="/workspace/:entityKind/:entityId" element={<WorkspacePage />} />
          </Routes>
        ) : (
          <ConnectionState status={source.status} onConnect={() => setOpen(true)} />
        )}
      </div>
      {drawer && client ? <ServiceDrawer name={drawer} model={visibleModel} client={client} onClose={() => setDrawer(null)} /> : null}
    </div>
  );
}

function activeNavItem(pathname: string): NavItem | undefined {
  return NAV_ITEMS.find((item) => pathname === item.to || pathname.startsWith(`${item.to}/`));
}

function verifiedConsoleModel(model: ConsoleModel): ConsoleModel {
  const nodes = model.graph.nodes.filter((node) => node.truth !== "inferred");
  const nodeIds = new Set(nodes.map((node) => node.id));
  return {
    ...model,
    services: model.services.filter((service) => service.truth !== "fallback"),
    findings: model.findings.filter((finding) => finding.truth !== "fallback"),
    graph: {
      nodes,
      edges: model.graph.edges.filter((edge) => nodeIds.has(edge.s) && nodeIds.has(edge.t))
    }
  };
}

function nonZero(value: number): number | null {
  return value > 0 ? value : null;
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
