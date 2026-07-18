// appRoutes.tsx — extracted route table for App.tsx.
// Keeps App.tsx under 500 lines by housing the full <Routes> declaration here.
import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router-dom";

import type { BrowserSessionAuth, EshuApiClient } from "./api/client";
import { demoDefaults } from "./api/demoClient";
import type { RepoListItem } from "./api/repoCatalog";
import { APP_ROUTE_PATHS } from "./appRoutePaths";
import { AdminRouteGuard } from "./auth/AdminRouteGuard";
import type { SourceState } from "./components/SourceControls";
import type { ConsoleModel } from "./console/types";
import { AdminPage } from "./pages/AdminPage";
import { AskPage } from "./pages/AskPage";
import { CapabilityMatrixPage } from "./pages/CapabilityMatrixPage";
import { CatalogPage } from "./pages/CatalogPage";
import { ChangedSincePage } from "./pages/ChangedSincePage";
import { CICDRunCorrelationsPage } from "./pages/CICDRunCorrelationsPage";
import { CloudDriftPage } from "./pages/CloudDriftPage";
import { CloudPage } from "./pages/CloudPage";
import { CollectorReadinessPage } from "./pages/CollectorReadinessPage";
import { DashboardPage } from "./pages/DashboardPage";
import { DeadCodePage } from "./pages/DeadCodePage";
import { DependenciesPage } from "./pages/DependenciesPage";
import { ExplorerPage } from "./pages/ExplorerPage";
import { ExposurePathPage } from "./pages/ExposurePathPage";
import { FindingsPage } from "./pages/FindingsPage";
import { FreshnessCausalityPage } from "./pages/FreshnessCausalityPage";
import { IacPage } from "./pages/IacPage";
import { ImagesPage } from "./pages/ImagesPage";
import { ImpactPage } from "./pages/ImpactPage";
import { IncidentContextPage } from "./pages/IncidentContextPage";
import { NodesPage } from "./pages/NodesPage";
import { ObservabilityPage } from "./pages/ObservabilityPage";
import { OperationsPage } from "./pages/OperationsPage";
import { ProfilePage } from "./pages/ProfilePage";
import { RelationshipsPage } from "./pages/RelationshipsPage";
import { ReplatformingPage } from "./pages/ReplatformingPage";
import { RepositoriesPage } from "./pages/RepositoriesPage";
import { RepoSourcePage } from "./pages/RepoSourcePage";
import { SbomPage } from "./pages/SbomPage";
import { SecretsIamPage } from "./pages/SecretsIamPage";
import { ServiceEvidenceGraphPage } from "./pages/ServiceEvidenceGraphPage";
import { ServiceReportPage } from "./pages/ServiceReportPage";
import { StatusPage } from "./pages/StatusPage";
import { SurfaceInventoryPage } from "./pages/SurfaceInventoryPage";
import { TopologyPage } from "./pages/TopologyPage";
import { VulnDetailPage } from "./pages/VulnDetailPage";
import type { RepositoryCatalogState } from "./repositoryCatalogLifecycle";

// WorkspacePage is code-split via React.lazy (issue #3331), so this route has an
// extra dynamic-import hop before its content renders. See App.tsx for details.
const WorkspacePage = lazy(() =>
  import("./pages/WorkspacePage").then((module) => ({ default: module.WorkspacePage })),
);

// SemanticSearchPage is code-split via React.lazy (issue #4024) so its
// search surface stays out of the eagerly loaded main bundle.
const SemanticSearchPage = lazy(() =>
  import("./pages/SemanticSearchPage").then((module) => ({
    default: module.SemanticSearchPage,
  })),
);

// GuidedQuestionsPage is code-split via React.lazy (issue #4746) so its
// query-playbooks live surface stays out of the eagerly loaded main bundle.
const GuidedQuestionsPage = lazy(() =>
  import("./pages/GuidedQuestionsPage").then((module) => ({
    default: module.GuidedQuestionsPage,
  })),
);

const CodeGraphPage = lazy(() =>
  import("./pages/CodeGraphPage").then((module) => ({ default: module.CodeGraphPage })),
);

// VulnerabilitiesPage owns the advisory catalog, its bounded filter client,
// and the reachable-vulnerability view. Keep that route-specific surface out
// of the eager console shell while preserving an honest loading state.
const VulnerabilitiesPage = lazy(() =>
  import("./pages/VulnerabilitiesPage").then((module) => ({
    default: module.VulnerabilitiesPage,
  })),
);

export interface AppRoutesProps {
  readonly model: ConsoleModel;
  readonly client: EshuApiClient | undefined;
  readonly source: SourceState;
  readonly repositories: readonly RepoListItem[];
  readonly repositoryCatalog?: RepositoryCatalogState;
  readonly onOpenService: (name: string) => void;
  // auth gates /admin (issue #4969): the route guard and AdminPage's
  // per-panel gating both derive from this session auth. Undefined in tests
  // that don't exercise gating — fails open, matching capabilityAccess.ts.
  readonly auth?: BrowserSessionAuth | null;
}

// AppRoutes renders the full route table for the connected console shell.
// Only rendered when source.status === "connected".
export function AppRoutes({
  model,
  client,
  source,
  repositories,
  repositoryCatalog,
  auth,
  onOpenService,
}: AppRoutesProps): React.JSX.Element {
  const sourceLabel = source.mode === "demo" ? "demo fixtures" : "live";
  const cloudDriftDemoDefaults = source.mode === "demo" ? demoDefaults.cloudDrift : undefined;

  return (
    <Routes>
      <Route
        path={APP_ROUTE_PATHS.root}
        element={
          <DashboardPage
            model={model}
            client={client}
            onOpenService={onOpenService}
            repositories={repositories}
            repositoryCatalog={repositoryCatalog}
          />
        }
      />
      <Route path={APP_ROUTE_PATHS.status} element={<StatusPage client={client} />} />
      <Route
        path={APP_ROUTE_PATHS.dashboard}
        element={
          <DashboardPage
            model={model}
            client={client}
            onOpenService={onOpenService}
            repositories={repositories}
            repositoryCatalog={repositoryCatalog}
          />
        }
      />
      <Route path={APP_ROUTE_PATHS.ask} element={<AskPage source={source} />} />
      <Route
        path={APP_ROUTE_PATHS.semanticSearch}
        element={
          <Suspense
            fallback={
              <section className="page-shell">
                <h1>Loading semantic search</h1>
                <p>Loading live data.</p>
              </section>
            }
          >
            <SemanticSearchPage client={client} repositoryCatalog={repositoryCatalog} />
          </Suspense>
        }
      />
      <Route
        path={APP_ROUTE_PATHS.guidedQuestions}
        element={
          <Suspense
            fallback={
              <section className="page-shell">
                <h1>Loading guided questions</h1>
                <p>Loading live data.</p>
              </section>
            }
          >
            <GuidedQuestionsPage client={client} source={source} />
          </Suspense>
        }
      />
      <Route path={APP_ROUTE_PATHS.impact} element={<ImpactPage model={model} client={client} />} />
      <Route
        path={APP_ROUTE_PATHS.exposure}
        element={
          <ExposurePathPage
            catalogTruncated={model.serviceCatalogSummary?.truncated}
            client={client}
            services={model.services}
          />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.changedSince}
        element={<ChangedSincePage client={client} repositories={repositories} />}
      />
      <Route
        path={APP_ROUTE_PATHS.explorer}
        element={<ExplorerPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route
        path={APP_ROUTE_PATHS.relationships}
        element={<RelationshipsPage model={model} client={client} />}
      />
      <Route
        path={APP_ROUTE_PATHS.serviceStory}
        element={
          <ServiceEvidenceGraphPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.serviceStoryDetail}
        element={
          <ServiceEvidenceGraphPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.serviceReport}
        element={<ServiceReportPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route
        path={APP_ROUTE_PATHS.serviceReportDetail}
        element={<ServiceReportPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route
        path={APP_ROUTE_PATHS.nodes}
        element={<NodesPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.codeGraph}
        element={
          <Suspense
            fallback={
              <section className="page-shell">
                <h1>Loading Code Graph</h1>
                <p>Loading repository-scoped graph controls.</p>
              </section>
            }
          >
            <CodeGraphPage
              model={model}
              client={client}
              repositories={repositories}
              repositoryCatalog={repositoryCatalog}
            />
          </Suspense>
        }
      />
      <Route
        path={APP_ROUTE_PATHS.repositories}
        element={
          <RepositoriesPage client={client} model={model} repositoryCatalog={repositoryCatalog} />
        }
      />
      <Route path={APP_ROUTE_PATHS.repositorySource} element={<RepoSourcePage client={client} />} />
      <Route
        path={APP_ROUTE_PATHS.cloud}
        element={<CloudPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.ciCdRunCorrelations}
        element={<CICDRunCorrelationsPage client={client} model={model} />}
      />
      <Route
        path={APP_ROUTE_PATHS.cloudDrift}
        element={<CloudDriftPage client={client} demoDefaults={cloudDriftDemoDefaults} />}
      />
      <Route
        path={APP_ROUTE_PATHS.secretsIam}
        element={<SecretsIamPage model={model} client={client} />}
      />
      <Route
        path={APP_ROUTE_PATHS.topology}
        element={<TopologyPage client={client} model={model} onOpenService={onOpenService} />}
      />
      <Route
        path={APP_ROUTE_PATHS.incidents}
        element={
          <IncidentContextPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.incidentContext}
        element={
          <IncidentContextPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.catalog}
        element={<CatalogPage model={model} onOpenService={onOpenService} />}
      />
      <Route
        path={APP_ROUTE_PATHS.images}
        element={<ImagesPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.capabilities}
        element={<CapabilityMatrixPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.surfaceInventory}
        element={<SurfaceInventoryPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.iac}
        element={<IacPage model={model} client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.replatforming}
        element={<ReplatformingPage model={model} client={client} />}
      />
      <Route path={APP_ROUTE_PATHS.findings} element={<FindingsPage model={model} />} />
      <Route
        path={APP_ROUTE_PATHS.deadCode}
        element={
          <DeadCodePage client={client} model={model} repositoryCatalog={repositoryCatalog} />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.vulnerabilities}
        element={
          <Suspense
            fallback={
              <section className="page narrow" aria-live="polite">
                <h1>Loading vulnerabilities</h1>
                <p>Loading vulnerability intelligence.</p>
              </section>
            }
          >
            <VulnerabilitiesPage model={model} client={client} />
          </Suspense>
        }
      />
      <Route
        path={APP_ROUTE_PATHS.vulnerabilityDetail}
        element={<VulnDetailPage model={model} client={client} />}
      />
      <Route
        path={APP_ROUTE_PATHS.sbom}
        element={<SbomPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path={APP_ROUTE_PATHS.dependencies}
        element={<DependenciesPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route path={APP_ROUTE_PATHS.observability} element={<ObservabilityPage client={client} />} />
      <Route
        path={APP_ROUTE_PATHS.collectorReadiness}
        element={
          <CollectorReadinessPage
            rows={model.collectorReadiness}
            provenance={model.provenance.collectorReadiness ?? "empty"}
          />
        }
      />
      <Route
        path={APP_ROUTE_PATHS.operations}
        element={<OperationsPage model={model} client={client} />}
      />
      <Route
        path={APP_ROUTE_PATHS.freshnessCausality}
        element={<FreshnessCausalityPage client={client} />}
      />
      <Route path={APP_ROUTE_PATHS.profile} element={<ProfilePage client={client} />} />
      <Route
        path={APP_ROUTE_PATHS.admin}
        element={
          <AdminRouteGuard auth={auth}>
            <AdminPage client={client} baseUrl={source.base} auth={auth} />
          </AdminRouteGuard>
        }
      />
      <Route
        path={APP_ROUTE_PATHS.workspace}
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
  );
}
