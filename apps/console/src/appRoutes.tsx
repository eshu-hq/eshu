// appRoutes.tsx — extracted route table for App.tsx.
// Keeps App.tsx under 500 lines by housing the full <Routes> declaration here.
import { lazy, Suspense } from "react";
import { Route, Routes } from "react-router-dom";

import type { EshuApiClient } from "./api/client";
import { demoDefaults } from "./api/demoClient";
import type { RepoListItem } from "./api/repoCatalog";
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
import { CodeGraphPage } from "./pages/CodeGraphPage";
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
import { VulnerabilitiesPage } from "./pages/VulnerabilitiesPage";

// WorkspacePage is code-split via React.lazy (issue #3331), so this route has an
// extra dynamic-import hop before its content renders. See App.tsx for details.
const WorkspacePage = lazy(() =>
  import("./pages/WorkspacePage").then((module) => ({ default: module.WorkspacePage })),
);

export interface AppRoutesProps {
  readonly model: ConsoleModel;
  readonly client: EshuApiClient | undefined;
  readonly source: SourceState;
  readonly repositories: readonly RepoListItem[];
  readonly onOpenService: (name: string) => void;
}

// AppRoutes renders the full route table for the connected console shell.
// Only rendered when source.status === "connected".
export function AppRoutes({
  model,
  client,
  source,
  repositories,
  onOpenService,
}: AppRoutesProps): React.JSX.Element {
  const sourceLabel = source.mode === "demo" ? "demo fixtures" : "live";
  const cloudDriftDemoDefaults = source.mode === "demo" ? demoDefaults.cloudDrift : undefined;

  return (
    <Routes>
      <Route
        path="/"
        element={
          <DashboardPage
            model={model}
            client={client}
            onOpenService={onOpenService}
            repositories={repositories}
          />
        }
      />
      <Route path="/status" element={<StatusPage client={client} />} />
      <Route
        path="/dashboard"
        element={
          <DashboardPage
            model={model}
            client={client}
            onOpenService={onOpenService}
            repositories={repositories}
          />
        }
      />
      <Route path="/ask" element={<AskPage source={source} />} />
      <Route path="/impact" element={<ImpactPage model={model} client={client} />} />
      <Route path="/exposure" element={<ExposurePathPage client={client} />} />
      <Route path="/changed-since" element={<ChangedSincePage client={client} model={model} />} />
      <Route
        path="/explorer"
        element={<ExplorerPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route path="/relationships" element={<RelationshipsPage model={model} client={client} />} />
      <Route
        path="/service-story"
        element={
          <ServiceEvidenceGraphPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path="/service-story/:serviceName"
        element={
          <ServiceEvidenceGraphPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path="/service-report"
        element={<ServiceReportPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route
        path="/service-report/:serviceName"
        element={<ServiceReportPage model={model} client={client} onOpenService={onOpenService} />}
      />
      <Route path="/nodes" element={<NodesPage client={client} sourceLabel={sourceLabel} />} />
      <Route path="/code-graph" element={<CodeGraphPage model={model} client={client} />} />
      <Route path="/repositories" element={<RepositoriesPage client={client} model={model} />} />
      <Route path="/repositories/:id/source" element={<RepoSourcePage client={client} />} />
      <Route path="/cloud" element={<CloudPage client={client} sourceLabel={sourceLabel} />} />
      <Route
        path="/ci-cd/run-correlations"
        element={<CICDRunCorrelationsPage client={client} model={model} />}
      />
      <Route
        path="/cloud-drift"
        element={<CloudDriftPage client={client} demoDefaults={cloudDriftDemoDefaults} />}
      />
      <Route path="/secrets-iam" element={<SecretsIamPage model={model} client={client} />} />
      <Route
        path="/topology"
        element={<TopologyPage client={client} model={model} onOpenService={onOpenService} />}
      />
      <Route
        path="/incidents"
        element={
          <IncidentContextPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path="/incidents/:incidentId/context"
        element={
          <IncidentContextPage model={model} client={client} onOpenService={onOpenService} />
        }
      />
      <Route
        path="/catalog"
        element={<CatalogPage model={model} onOpenService={onOpenService} />}
      />
      <Route path="/images" element={<ImagesPage client={client} sourceLabel={sourceLabel} />} />
      <Route
        path="/capabilities"
        element={<CapabilityMatrixPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path="/surface-inventory"
        element={<SurfaceInventoryPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route
        path="/iac"
        element={<IacPage model={model} client={client} sourceLabel={sourceLabel} />}
      />
      <Route path="/replatforming" element={<ReplatformingPage model={model} client={client} />} />
      <Route path="/findings" element={<FindingsPage model={model} />} />
      <Route path="/dead-code" element={<DeadCodePage client={client} model={model} />} />
      <Route
        path="/vulnerabilities"
        element={<VulnerabilitiesPage model={model} client={client} />}
      />
      <Route
        path="/vulnerabilities/:id"
        element={<VulnDetailPage model={model} client={client} />}
      />
      <Route path="/sbom" element={<SbomPage client={client} sourceLabel={sourceLabel} />} />
      <Route
        path="/dependencies"
        element={<DependenciesPage client={client} sourceLabel={sourceLabel} />}
      />
      <Route path="/observability" element={<ObservabilityPage client={client} />} />
      <Route
        path="/collector-readiness"
        element={
          <CollectorReadinessPage
            rows={model.collectorReadiness}
            provenance={model.provenance.collectorReadiness ?? "empty"}
          />
        }
      />
      <Route path="/operations" element={<OperationsPage model={model} />} />
      <Route path="/freshness-causality" element={<FreshnessCausalityPage client={client} />} />
      <Route path="/profile" element={<ProfilePage client={client} />} />
      <Route path="/admin" element={<AdminPage client={client} baseUrl={source.base} />} />
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
  );
}
