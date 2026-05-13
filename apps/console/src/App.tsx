import { useEffect, useState } from "react";
import { NavLink, Route, Routes } from "react-router-dom";
import { EshuApiClient } from "./api/client";
import { StatusStrip } from "./components/StatusStrip";
import type { RuntimeStatusSummary } from "./components/StatusStrip";
import { loadConsoleEnvironment } from "./config/environment";
import { CatalogPage } from "./pages/CatalogPage";
import { DashboardPage } from "./pages/DashboardPage";
import { FindingsPage } from "./pages/FindingsPage";
import { HomePage } from "./pages/HomePage";
import { WorkspacePage } from "./pages/WorkspacePage";
import "./styles.css";

const unavailableRuntime: RuntimeStatusSummary = {
  freshnessState: "unavailable",
  health: "unavailable",
  profile: "local_authoritative"
} as const;

export function App(): React.JSX.Element {
  const environment = loadConsoleEnvironment();
  const [runtime, setRuntime] = useState<RuntimeStatusSummary>(unavailableRuntime);

  useEffect(() => {
    if (environment.mode === "demo") {
      setRuntime({
        freshnessState: "fresh",
        health: "demo",
        profile: "local_full_stack"
      });
      return;
    }

    const client = new EshuApiClient({ baseUrl: environment.apiBaseUrl });
    void client
      .getJson<{ readonly status?: string }>("/api/v0/index-status")
      .then((status) => {
        setRuntime({
          freshnessState: status.status === "healthy" ? "fresh" : "building",
          health: status.status === "healthy" ? "ready" : "degraded",
          profile: "local_authoritative"
        });
      })
      .catch(() => {
        setRuntime(unavailableRuntime);
      });
  }, [environment.apiBaseUrl, environment.mode]);

  return (
    <div className="console-shell">
      <header className="console-header">
        <a className="console-brand" href="/">
          Eshu Console
        </a>
        <nav className="console-nav" aria-label="Console">
          <NavLink to="/">Story</NavLink>
          <NavLink to="/dashboard">Dashboard</NavLink>
          <NavLink to="/catalog">Catalog</NavLink>
          <NavLink to="/findings">Findings</NavLink>
        </nav>
      </header>
      <StatusStrip environment={environment} runtime={runtime} />
      <main>
        <Routes>
          <Route element={<HomePage />} path="/" />
          <Route element={<DashboardPage />} path="/dashboard" />
          <Route element={<CatalogPage />} path="/catalog" />
          <Route element={<FindingsPage />} path="/findings" />
          <Route element={<WorkspacePage />} path="/workspace/:entityKind/:entityId" />
        </Routes>
      </main>
    </div>
  );
}
