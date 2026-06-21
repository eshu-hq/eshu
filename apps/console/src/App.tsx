// App.tsx — redesigned dark console shell.
// Private mode renders only the Eshu API. Demo mode is an explicit prospect
// fixture source, not a failed-live fallback. main.tsx wraps this in
// <BrowserRouter>.
import { lazy, Suspense, useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type MouseEvent } from "react";
import { NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import {
  Bell,
  Boxes,
  Cloud,
  Code2,
  FileText,
  FolderGit2,
  GitBranch,
  Hexagon,
  History,
  Images,
  KeyRound,
  Layers,
  LayoutDashboard,
  ListChecks,
  Network,
  PackageSearch,
  Route as RouteIcon,
  Search,
  ServerCog,
  Activity,
  ShieldCheck,
  TriangleAlert,
  Workflow,
  Waves,
  Waypoints
} from "lucide-react";
import { EshuApiClient } from "./api/client";
import { createDemoApiClient, demoApiBaseUrl, demoDefaults, demoRepositories } from "./api/demoClient";
import { loadConsoleSnapshot } from "./api/eshuConsoleLive";
import { loadRepositories, type RepoListItem } from "./api/repoCatalog";
import { loadConsoleEnvironment, saveConsoleEnvironment } from "./config/environment";
import { demoModel } from "./console/demoModel";
import { emptyConsoleModel, modelFromSnapshot } from "./console/liveModel";
import type { ConsoleModel } from "./console/types";
import { fmt } from "./console/types";
import { DashboardPage } from "./pages/DashboardPage";
import { AskPage } from "./pages/AskPage";
import { CatalogPage } from "./pages/CatalogPage";
import { FindingsPage } from "./pages/FindingsPage";
import { OperationsPage } from "./pages/OperationsPage";
import { CollectorReadinessPage } from "./pages/CollectorReadinessPage";
import { IncidentContextPage } from "./pages/IncidentContextPage";
import { VulnerabilitiesPage } from "./pages/VulnerabilitiesPage";
import { VulnDetailPage } from "./pages/VulnDetailPage";
import { SbomPage } from "./pages/SbomPage";
import { ObservabilityPage } from "./pages/ObservabilityPage";
import { DependenciesPage } from "./pages/DependenciesPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { ServiceEvidenceGraphPage } from "./pages/ServiceEvidenceGraphPage";
import { ServiceReportPage } from "./pages/ServiceReportPage";
import { NodesPage } from "./pages/NodesPage";
import { IacPage } from "./pages/IacPage";
import { ReplatformingPage } from "./pages/ReplatformingPage";
import { RepositoriesPage } from "./pages/RepositoriesPage";
import { RepoSourcePage } from "./pages/RepoSourcePage";
import { ImagesPage } from "./pages/ImagesPage";
import { CapabilityMatrixPage } from "./pages/CapabilityMatrixPage";
import { SurfaceInventoryPage } from "./pages/SurfaceInventoryPage";
import { FreshnessCausalityPage } from "./pages/FreshnessCausalityPage";
import { CloudPage } from "./pages/CloudPage";
import { CloudDriftPage } from "./pages/CloudDriftPage";
import { SecretsIamPage } from "./pages/SecretsIamPage";
import { TopologyPage } from "./pages/TopologyPage";
import { DeadCodePage } from "./pages/DeadCodePage";
import { CodeGraphPage } from "./pages/CodeGraphPage";
import { ImpactPage } from "./pages/ImpactPage";
import { ExposurePathPage } from "./pages/ExposurePathPage";
import { CICDRunCorrelationsPage } from "./pages/CICDRunCorrelationsPage";
import { ChangedSincePage } from "./pages/ChangedSincePage";
import { ServiceDrawer } from "./components/ServiceDrawer";
import { ConnectionState, SourcePopover, type SourceState } from "./components/SourceControls";
import "./styles.css";
import "./appShell.css";

// WorkspacePage pulls the d3 force-simulation relationship/deployment views,
// the heaviest non-diagram dependency in the console. It is a deep-link leaf
// route (/workspace/:entityKind/:entityId), not part of the dashboard-first
// path, so it is code-split via React.lazy to keep d3 out of the main entry
// chunk. Suspense shows a lightweight loading state while the chunk downloads.
const WorkspacePage = lazy(() =>
  import("./pages/WorkspacePage").then((module) => ({ default: module.WorkspacePage }))
);

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
      { to: "/ask", label: "Ask Eshu", icon: Search },
      { to: "/impact", label: "Impact", icon: Network },
      { to: "/exposure", label: "Exposure Path", icon: RouteIcon },
      { to: "/changed-since", label: "Changed Since", icon: History },
      { to: "/explorer", label: "Graph Explorer", icon: GitBranch },
      { to: "/service-story", label: "Service Story", icon: Waypoints },
      { to: "/service-report", label: "Service Report", icon: FileText },
      { to: "/nodes", label: "Nodes", icon: Hexagon }
    ]
  },
  {
    label: "Inventory",
    items: [
      { to: "/repositories", label: "Repositories", icon: FolderGit2, count: (m) => nonZero(m.runtime.repositories) },
      { to: "/catalog", label: "Catalog", icon: Boxes, count: (m) => nonZero(m.services?.length ?? 0) },
      { to: "/findings", label: "Findings", icon: TriangleAlert, count: (m) => nonZero((m.findings?.length ?? 0) + (m.vulnerabilities?.length ?? 0)), alert: true },
      { to: "/images", label: "Images", icon: Images, count: (m) => nonZero(m.images?.length ?? 0) },
      { to: "/iac", label: "IaC", icon: Network, count: (m) => nonZero(m.iacResources?.length ?? 0) },
      { to: "/replatforming", label: "Replatforming", icon: Network },
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
      { to: "/secrets-iam", label: "Secrets/IAM", icon: KeyRound },
      { to: "/incidents", label: "Incidents", icon: TriangleAlert },
      { to: "/ci-cd/run-correlations", label: "CI/CD", icon: Workflow },
      { to: "/cloud-drift", label: "Cloud Drift", icon: TriangleAlert, alert: true },
      { to: "/observability", label: "Observability", icon: Waves },
      { to: "/sbom", label: "SBOM", icon: PackageSearch, count: (m) => nonZero(m.sbom?.total ?? 0) },
      { to: "/dependencies", label: "Dependencies", icon: Boxes, count: (m) => nonZero(m.dependencies?.length ?? 0) }
    ]
  },
  {
    label: "System",
    items: [
      { to: "/capabilities", label: "Capabilities", icon: ListChecks },
      { to: "/collector-readiness", label: "Collector Readiness", icon: ShieldCheck, count: (m) => nonZero(m.collectorReadiness?.length ?? 0) },
      { to: "/surface-inventory", label: "Surface Inventory", icon: Layers },
      { to: "/operations", label: "Operations", icon: ServerCog },
      { to: "/freshness-causality", label: "Freshness", icon: Activity }
    ]
  }
];

const NAV_ITEMS = NAV_GROUPS.flatMap((group) => group.items);

export function App(): React.JSX.Element {
  const location = useLocation();
  const navigate = useNavigate();
  const env = loadConsoleEnvironment();
  const hasDemoEnv = env.mode === "demo";
  const hasSavedEnv = env.mode === "private" && (env.apiBaseUrl || "").length > 0;
  const [model, setModel] = useState<ConsoleModel>(() => hasDemoEnv ? demoModel : emptyConsoleModel());
  const [source, setSource] = useState<SourceState>({
    base: hasDemoEnv ? demoApiBaseUrl : env.apiBaseUrl || "/eshu-api/",
    key: env.apiKey || "",
    mode: hasDemoEnv ? "demo" : "private",
    status: hasDemoEnv ? "connected" : hasSavedEnv ? "connecting" : "needs-connection",
    msg: ""
  });
  const [open, setOpen] = useState(false);
  const [client, setClient] = useState<EshuApiClient | undefined>(() => hasDemoEnv ? createDemoApiClient() : undefined);
  const [repositories, setRepositories] = useState<readonly RepoListItem[]>(() => hasDemoEnv ? demoRepositories : []);
  const [drawer, setDrawer] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [verifiedOnly, setVerifiedOnly] = useState(false);
  const visibleModel = verifiedOnly ? verifiedConsoleModel(model) : model;
  const searchInputRef = useRef<HTMLInputElement>(null);
  // Boot guard: React StrictMode runs effects twice in development, which would
  // otherwise fire two concurrent boot connects whose in-flight fetches abort
  // each other (issue #1727: ERR_ABORTED -> Catalog blank). The ref dedupes the
  // boot connect to a single run; user-initiated reconnects from the source
  // popover stay unguarded.
  const bootedRef = useRef(false);

  function activateDemo(): void {
    saveConsoleEnvironment({ mode: "demo", apiBaseUrl: "", apiKey: "", recentApiBaseUrls: [] });
    setClient(createDemoApiClient());
    setModel(demoModel);
    setRepositories(demoRepositories);
    setSource({ base: demoApiBaseUrl, key: "", mode: "demo", status: "connected", msg: "" });
    setOpen(false);
  }

  async function connect(base: string, key: string): Promise<void> {
    setSource((s) => ({ ...s, base, key, mode: "private", status: "connecting", msg: "" }));
    setModel(emptyConsoleModel("loading"));
    try {
      const next = new EshuApiClient({ baseUrl: base, apiKey: key });
      const [snap, repoRows] = await Promise.all([
        loadConsoleSnapshot(next),
        loadRepositories(next).catch((): readonly RepoListItem[] => [])
      ]);
      saveConsoleEnvironment({ mode: "private", apiBaseUrl: base, apiKey: key, recentApiBaseUrls: [base] });
      setClient(next);
      setModel(modelFromSnapshot(snap));
      setRepositories(repoRows);
      setSource({ base, key, mode: "private", status: "connected", msg: "" });
      setOpen(false);
    } catch (e) {
      // No demo fallback: keep an explicit empty/unavailable model so panels show
      // "—" / "API not available" rather than invented data.
      setClient(undefined);
      setRepositories([]);
      setModel(emptyConsoleModel("unavailable"));
      setSource({ base, key, mode: "private", status: "error", msg: e instanceof Error ? e.message : "unreachable" });
    }
  }
  const openService = (name: string): void => setDrawer(name);

  function runSearch(rawQuery: string): void {
    const query = rawQuery.trim();
    if (query.length === 0) return;
    const needle = query.toLowerCase();
    const repositoryId = repositorySearchTarget(repositories, needle);
    if (repositoryId) {
      navigate(`/repositories/${encodeURIComponent(repositoryId)}/source`);
      return;
    }
    const service = visibleModel.services.find((row) =>
      [row.name, row.id, row.repo].some((value) => value.toLowerCase().includes(needle))
    );
    if (service) {
      openService(service.name);
      return;
    }
    const vulnerabilityId = vulnerabilitySearchTarget(visibleModel, needle);
    if (vulnerabilityId) {
      navigate(`/vulnerabilities/${encodeURIComponent(vulnerabilityId)}`);
      return;
    }
    navigate(`/explorer?q=${encodeURIComponent(query)}`);
  }

  // Boot straight into private data when a saved private environment exists.
  // With no saved environment, stay in "needs-connection" until the operator
  // connects. The bootedRef guard makes this StrictMode-safe: the discarded
  // first dev run does not launch a second boot connect that would abort the
  // surviving run's in-flight fetches and blank out sections like Catalog
  // (issue #1727).
  useEffect(() => {
    if (hasSavedEnv && !bootedRef.current) {
      bootedRef.current = true;
      void connect(env.apiBaseUrl, env.apiKey || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    function onKey(e: globalThis.KeyboardEvent): void { if (e.key === "Escape") { setOpen(false); setDrawer(null); } }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pill =
    source.status === "connected" ? source.mode === "demo" ? "Demo fixtures" : "Live"
      : source.status === "connecting" ? "Connecting…"
        : source.status === "error" ? "Live (offline)"
          : "Not connected";
  const activeItem = activeNavItem(location.pathname);
  const pageTitle = location.pathname === "/" ? "Eshu Console" : activeItem?.label ?? "Eshu Console";

  function submitSearch(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    runSearch(searchQuery);
  }

  function submitSearchKey(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
    event.preventDefault();
    runSearch(event.currentTarget.value);
  }

  function submitSearchButton(event: MouseEvent<HTMLButtonElement>): void {
    event.preventDefault();
    runSearch(searchInputRef.current?.value ?? searchQuery);
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
            <div className="bc-meta"><span>{source.status === "connected" ? source.mode === "demo" ? "demo" : "live" : pill.toLowerCase()}</span><span>{source.status === "connected" ? `${fmt(model.runtime.repositories)} repos` : "—"}</span></div>
          </div>
        </div>
      </nav>
      <div className="main">
        <header className="topbar">
          <div className="topbar-title"><h1>{pageTitle}</h1><span>Read-only code-to-cloud graph status & evidence</span></div>
          <form className="searchbox" onSubmit={submitSearch}>
            <button className="search-submit" type="submit" aria-label="Search" onClick={submitSearchButton}>
              <Search aria-hidden />
            </button>
            <input
              ref={searchInputRef}
              aria-label="Search Eshu"
              placeholder="Search repos, services, CVEs, evidence…"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              onKeyDown={submitSearchKey}
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
            {open ? <SourcePopover source={source} onConnect={connect} onDemo={activateDemo} onClose={() => setOpen(false)} /> : null}
          </div>
        </header>
        {source.status === "connected" && source.mode === "demo" ? (
          <div className="prov-banner"><strong>Prospect demo</strong><span>Demo fixtures only; no real workspace or customer data is being queried.</span></div>
        ) : null}
        {verifiedOnly ? (
          <div className="prov-banner"><ShieldCheck aria-hidden size={14} /> Verified evidence only — hiding inferred findings and graph nodes.</div>
        ) : null}
        {source.status === "error" ? (
          <div className="prov-banner warn">Eshu API unavailable at <span className="mono">{source.base}</span>{source.msg ? ` · ${source.msg}` : ""}. <button className="link-btn" onClick={() => setOpen(true)}>Edit data source</button></div>
        ) : null}
        {source.status === "connected" ? (
          <Routes>
            <Route path="/" element={<DashboardPage model={visibleModel} client={client} onOpenService={openService} repositories={repositories} />} />
            <Route path="/dashboard" element={<DashboardPage model={visibleModel} client={client} onOpenService={openService} repositories={repositories} />} />
            <Route path="/ask" element={<AskPage source={source} />} />
            <Route path="/impact" element={<ImpactPage model={visibleModel} client={client} />} />
            <Route path="/exposure" element={<ExposurePathPage client={client} />} />
            <Route path="/changed-since" element={<ChangedSincePage client={client} model={visibleModel} />} />
            <Route path="/explorer" element={<ExplorerPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/service-story" element={<ServiceEvidenceGraphPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/service-story/:serviceName" element={<ServiceEvidenceGraphPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/service-report" element={<ServiceReportPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/service-report/:serviceName" element={<ServiceReportPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/nodes" element={<NodesPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/code-graph" element={<CodeGraphPage model={visibleModel} client={client} />} />
            <Route path="/repositories" element={<RepositoriesPage client={client} model={visibleModel} />} />
            <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
            <Route path="/cloud" element={<CloudPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/ci-cd/run-correlations" element={<CICDRunCorrelationsPage client={client} model={visibleModel} />} />
            <Route path="/cloud-drift" element={<CloudDriftPage client={client} demoDefaults={source.mode === "demo" ? demoDefaults.cloudDrift : undefined} />} />
            <Route path="/secrets-iam" element={<SecretsIamPage model={visibleModel} client={client} />} />
            <Route path="/topology" element={<TopologyPage client={client} model={visibleModel} onOpenService={openService} />} />
            <Route path="/incidents" element={<IncidentContextPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/incidents/:incidentId/context" element={<IncidentContextPage model={visibleModel} client={client} onOpenService={openService} />} />
            <Route path="/catalog" element={<CatalogPage model={visibleModel} onOpenService={openService} />} />
            <Route path="/images" element={<ImagesPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/capabilities" element={<CapabilityMatrixPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/surface-inventory" element={<SurfaceInventoryPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/iac" element={<IacPage model={visibleModel} client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/replatforming" element={<ReplatformingPage model={visibleModel} client={client} />} />
            <Route path="/findings" element={<FindingsPage model={visibleModel} />} />
            <Route path="/dead-code" element={<DeadCodePage client={client} model={visibleModel} />} />
            <Route path="/vulnerabilities" element={<VulnerabilitiesPage model={visibleModel} client={client} />} />
            <Route path="/vulnerabilities/:id" element={<VulnDetailPage model={visibleModel} client={client} />} />
            <Route path="/sbom" element={<SbomPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/dependencies" element={<DependenciesPage client={client} sourceLabel={source.mode === "demo" ? "demo fixtures" : "live"} />} />
            <Route path="/observability" element={<ObservabilityPage client={client} />} />
            <Route path="/collector-readiness" element={<CollectorReadinessPage rows={visibleModel.collectorReadiness} provenance={visibleModel.provenance.collectorReadiness ?? "empty"} />} />
            <Route path="/operations" element={<OperationsPage model={visibleModel} />} />
            <Route path="/freshness-causality" element={<FreshnessCausalityPage client={client} />} />
            <Route
              path="/workspace/:entityKind/:entityId"
              element={
                <Suspense
                  fallback={
                    <section className="page-shell">
                      <h1>Loading workspace</h1>
                      <p>Loading live data.</p>
                    </section>
                  }
                >
                  <WorkspacePage />
                </Suspense>
              }
            />
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

function vulnerabilitySearchTarget(model: ConsoleModel, needle: string): string | null {
  const exactVulnerability = model.vulnerabilities.find((row) => row.id.toLowerCase() === needle);
  if (exactVulnerability) return exactVulnerability.id;
  const exactAdvisory = model.advisories.find((row) =>
    [row.id, row.cveId, row.ghsaId].some((value) => value.toLowerCase() === needle)
  );
  if (exactAdvisory) return exactAdvisory.cveId || exactAdvisory.ghsaId || exactAdvisory.id;
  return null;
}

function repositorySearchTarget(repositories: readonly RepoListItem[], needle: string): string | null {
  const exactRepository = repositories.find((row) =>
    [row.id, row.name, row.repoSlug].some((value) => value.toLowerCase() === needle)
  );
  return exactRepository?.id ?? null;
}

function nonZero(value: number): number | null {
  return value > 0 ? value : null;
}
