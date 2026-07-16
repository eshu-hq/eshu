import { Bell, Search, ShieldCheck } from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { logout } from "./api/authSession";
import { EshuApiClient } from "./api/client";
import type { BrowserSessionResponse } from "./api/client";
import { createDemoApiClient, demoApiBaseUrl, demoRepositories } from "./api/demoClient";
import type { RepoListItem } from "./api/repoCatalog";
import { bootFromKey, bootFromSession } from "./appBoot";
import { AppRoutes } from "./appRoutes";
import { buildAllowedNavSet } from "./auth/capabilityAccess";
import { AppSidebar } from "./components/AppSidebar";
import { ServiceDrawer } from "./components/ServiceDrawer";
import { ConnectionState, SourcePopover, type SourceState } from "./components/SourceControls";
import { loadConsoleEnvironment, saveConsoleEnvironment } from "./config/environment";
import { demoModel } from "./console/demoModel";
import { emptyConsoleModel } from "./console/liveModel";
import type { ConsoleModel } from "./console/types";
import { NAV_ITEMS, type NavItem } from "./i18n/navigation";
import { ConsoleI18nProvider, FormattedMessage, useConsoleIntl } from "./i18n/provider";
import { shellMessageDescriptors } from "./i18n/shellMessages";
import { AuthGate } from "./pages/AuthGate";
import "./styles.css";
import "./appShell.css";

export const App = (): React.JSX.Element => (
  <ConsoleI18nProvider>
    <AppShell />
  </ConsoleI18nProvider>
);
function AppShell(): React.JSX.Element {
  const intl = useConsoleIntl();
  const location = useLocation();
  const navigate = useNavigate();
  const env = loadConsoleEnvironment();
  const hasDemoEnv = env.mode === "demo";
  const hasSavedEnv = env.mode === "private" && (env.apiBaseUrl || "").length > 0;
  const [model, setModel] = useState<ConsoleModel>(() =>
    hasDemoEnv ? demoModel : emptyConsoleModel(),
  );
  const [source, setSource] = useState<SourceState>({
    base: hasDemoEnv ? demoApiBaseUrl : env.apiBaseUrl || "/eshu-api/",
    key: env.apiKey || "",
    mode: hasDemoEnv ? "demo" : "private",
    status: hasDemoEnv ? "connected" : hasSavedEnv ? "connecting" : "needs-connection",
    msg: "",
  });
  const [open, setOpen] = useState(false);
  const [client, setClient] = useState<EshuApiClient | undefined>(() =>
    hasDemoEnv ? createDemoApiClient() : undefined,
  );
  const [repositories, setRepositories] = useState<readonly RepoListItem[]>(() =>
    hasDemoEnv ? demoRepositories : [],
  );
  const [session, setSession] = useState<BrowserSessionResponse | null>(null);
  const [drawer, setDrawer] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [verifiedOnly, setVerifiedOnly] = useState(false);
  const showLogin = !hasDemoEnv && source.status === "needs-connection";
  const visibleModel = verifiedOnly ? verifiedConsoleModel(model) : model;
  const allowedNav = buildAllowedNavSet(session?.auth);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const bootedRef = useRef(false);
  const unreachableMessage = intl.formatMessage(shellMessageDescriptors.unreachable);
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
      const result = await bootFromKey(base, key);
      if (result === null) {
        setClient(undefined);
        setRepositories([]);
        setSession(null);
        setModel(emptyConsoleModel("unavailable"));
        setSource({ base, key: "", mode: "private", status: "needs-connection", msg: "" });
        setOpen(false);
        return;
      }
      setClient(result.client);
      setModel(result.model);
      setRepositories(result.repositories);
      setSession(result.session);
      setSource({
        base,
        key: result.session === null ? key : "",
        mode: "private",
        status: "connected",
        msg: "",
      });
      setOpen(false);
    } catch (e) {
      setClient(undefined);
      setRepositories([]);
      setSession(null);
      setModel(emptyConsoleModel("unavailable"));
      setSource({
        base,
        key,
        mode: "private",
        status: "error",
        msg: e instanceof Error ? e.message : unreachableMessage,
      });
    }
  }

  function handleLoginSuccess(resp: BrowserSessionResponse): void {
    setSession(resp);
    const base = source.base;
    setSource((s) => ({ ...s, status: "connecting", msg: "" }));
    setModel(emptyConsoleModel("loading"));
    bootFromSession(base)
      .then((result) => {
        if (result !== null) {
          setClient(result.client);
          setModel(result.model);
          setRepositories(result.repositories);
          setSession(result.session);
          setSource({ base, key: "", mode: "private", status: "connected", msg: "" });
        } else {
          setModel(emptyConsoleModel("unavailable"));
          setSource((s) => ({
            ...s,
            status: "error",
            msg: intl.formatMessage(shellMessageDescriptors.sessionDataUnavailable),
          }));
        }
      })
      .catch((e: unknown) => {
        setModel(emptyConsoleModel("unavailable"));
        setSource((s) => ({
          ...s,
          status: "error",
          msg: e instanceof Error ? e.message : unreachableMessage,
        }));
      });
  }
  function handleLogout(): void {
    if (client === undefined) return;
    logout(client)
      .then(() => {
        setSession(null);
        setClient(undefined);
        setModel(emptyConsoleModel());
        setRepositories([]);
        setSource((s) => ({ ...s, status: "needs-connection", msg: "" }));
      })
      .catch(() => {
        setSource((s) => ({
          ...s,
          msg: intl.formatMessage(shellMessageDescriptors.logoutFailed),
        }));
      });
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
      [row.name, row.id, row.repo].some((value) => value.toLowerCase().includes(needle)),
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

  useEffect(() => {
    if (hasSavedEnv && !bootedRef.current) {
      bootedRef.current = true;
      const base = env.apiBaseUrl;
      setSource((s) => ({ ...s, status: "connecting", msg: "" }));
      bootFromSession(base)
        .then((result) => {
          if (result !== null) {
            setClient(result.client);
            setModel(result.model);
            setRepositories(result.repositories);
            setSession(result.session);
            setSource({ base, key: "", mode: "private", status: "connected", msg: "" });
          } else if (env.apiKey.trim().length > 0) {
            // A build-time local shared key cannot always mint a tenant-bound
            // browser session. Reuse the existing bootFromKey bearer fallback
            // when no session cookie exists, including after a full reload.
            void connect(base, env.apiKey);
          } else {
            setSource((s) => ({ ...s, status: "needs-connection", msg: "" }));
          }
        })
        .catch(() => {
          void connect(base, env.apiKey || "");
        });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    function onKey(e: globalThis.KeyboardEvent): void {
      if (e.key === "Escape") {
        setOpen(false);
        setDrawer(null);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const pill =
    source.status === "connected"
      ? source.mode === "demo"
        ? intl.formatMessage(shellMessageDescriptors.sourceDemoFixtures)
        : intl.formatMessage(shellMessageDescriptors.sourceLive)
      : source.status === "connecting"
        ? intl.formatMessage(shellMessageDescriptors.sourceConnecting)
        : source.status === "error"
          ? intl.formatMessage(shellMessageDescriptors.sourceLiveOffline)
          : intl.formatMessage(shellMessageDescriptors.sourceNotConnected);
  const activeItem = activeNavItem(location.pathname);
  const pageTitle =
    location.pathname === "/" || activeItem === undefined
      ? intl.formatMessage(shellMessageDescriptors.title)
      : intl.formatMessage({ id: activeItem.messageId });
  const backendMode =
    source.status === "connected"
      ? source.mode === "demo"
        ? intl.formatMessage(shellMessageDescriptors.sourceDemoShort)
        : intl.formatMessage(shellMessageDescriptors.sourceLiveShort)
      : pill.toLocaleLowerCase();

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
      <AppSidebar
        allowedNav={allowedNav}
        visibleModel={visibleModel}
        model={model}
        source={source}
        backendMode={backendMode}
      />
      <main className="main">
        <header className="topbar">
          <div className="topbar-title">
            <h1>{pageTitle}</h1>
            <span>
              <FormattedMessage {...shellMessageDescriptors.subtitle} />
            </span>
          </div>
          <form className="searchbox" onSubmit={submitSearch}>
            <button
              className="search-submit"
              type="submit"
              aria-label={intl.formatMessage(shellMessageDescriptors.searchButton)}
              onClick={submitSearchButton}
            >
              <Search aria-hidden />
            </button>
            <input
              ref={searchInputRef}
              aria-label={intl.formatMessage(shellMessageDescriptors.searchInput)}
              placeholder={intl.formatMessage(shellMessageDescriptors.searchPlaceholder)}
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
              onKeyDown={submitSearchKey}
            />
            <kbd>⌘K</kbd>
          </form>
          {/* aria-label="Show verified evidence only" stays aligned with the prototype shell. */}
          <button
            aria-label={intl.formatMessage(shellMessageDescriptors.verifiedOnlyToggle)}
            aria-pressed={verifiedOnly}
            className={`topbar-btn verify-btn${verifiedOnly ? " on" : ""}`}
            title={intl.formatMessage(shellMessageDescriptors.verifiedOnlyToggle)}
            type="button"
            onClick={() => setVerifiedOnly((value) => !value)}
          >
            <ShieldCheck aria-hidden />
          </button>
          <span
            className="topbar-signal"
            title={intl.formatMessage(shellMessageDescriptors.noNotifications)}
          >
            <Bell aria-hidden />
          </span>
          {session !== null ? (
            <button
              className="topbar-btn"
              type="button"
              title={intl.formatMessage(shellMessageDescriptors.signOut)}
              aria-label={intl.formatMessage(shellMessageDescriptors.signOut)}
              onClick={handleLogout}
            >
              <FormattedMessage {...shellMessageDescriptors.signOut} />
            </button>
          ) : null}
          <div className="source-wrap">
            <button
              className={`source-pill src-${source.status}`}
              onClick={() => setOpen((o) => !o)}
            >
              <i />
              {pill}
            </button>
            {open ? (
              <SourcePopover
                source={source}
                onConnect={connect}
                onDemo={activateDemo}
                onClose={() => setOpen(false)}
              />
            ) : null}
          </div>
        </header>
        {source.status === "connected" && source.mode === "demo" ? (
          <div className="prov-banner">
            <strong>
              <FormattedMessage {...shellMessageDescriptors.demoBannerTitle} />
            </strong>
            <span>
              <FormattedMessage {...shellMessageDescriptors.demoBannerBody} />
            </span>
          </div>
        ) : null}
        {verifiedOnly ? (
          <div className="prov-banner">
            <ShieldCheck aria-hidden size={14} />
            <FormattedMessage {...shellMessageDescriptors.verifiedBanner} />
          </div>
        ) : null}
        {source.status === "error" ? (
          <div className="prov-banner warn">
            <FormattedMessage
              {...shellMessageDescriptors.apiUnavailable}
              values={{
                base: <span className="mono">{source.base}</span>,
                detail: source.msg ? ` · ${source.msg}` : "",
              }}
            />{" "}
            <button className="link-btn" onClick={() => setOpen(true)}>
              <FormattedMessage {...shellMessageDescriptors.editDataSource} />
            </button>
          </div>
        ) : null}
        {showLogin ? (
          <AuthGate
            client={new EshuApiClient({ baseUrl: source.base })}
            onSuccess={handleLoginSuccess}
            baseUrl={source.base}
          />
        ) : source.status === "connected" ? (
          <div className="page-shell">
            <AppRoutes
              model={visibleModel}
              client={client}
              source={source}
              repositories={repositories}
              onOpenService={openService}
              auth={session?.auth}
            />
          </div>
        ) : (
          <ConnectionState status={source.status} onConnect={() => setOpen(true)} />
        )}
      </main>
      {drawer && client ? (
        <ServiceDrawer
          name={drawer}
          model={visibleModel}
          client={client}
          onClose={() => setDrawer(null)}
        />
      ) : null}
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
      edges: model.graph.edges.filter((edge) => nodeIds.has(edge.s) && nodeIds.has(edge.t)),
    },
  };
}

function vulnerabilitySearchTarget(model: ConsoleModel, needle: string): string | null {
  const exactVulnerability = model.vulnerabilities.find((row) => row.id.toLowerCase() === needle);
  if (exactVulnerability) return exactVulnerability.id;
  const exactAdvisory = model.advisories.find((row) =>
    [row.id, row.cveId, row.ghsaId].some((value) => value.toLowerCase() === needle),
  );
  if (exactAdvisory) return exactAdvisory.cveId || exactAdvisory.ghsaId || exactAdvisory.id;
  return null;
}

function repositorySearchTarget(
  repositories: readonly RepoListItem[],
  needle: string,
): string | null {
  const exactRepository = repositories.find((row) =>
    [row.id, row.name, row.repoSlug].some((value) => value.toLowerCase() === needle),
  );
  return exactRepository?.id ?? null;
}
