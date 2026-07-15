import { APP_ROUTE_PATHS } from "../appRoutePaths";
import { type ConsoleRoute, liveState } from "./consoleRouteCatalogTypes";
import {
  bootstrapOwnership,
  boundedGetResponse,
  getResponse,
  postResponse,
  routeOwnership,
} from "./consoleRouteResponseOwnership";

const dashboardAtlasOwnership = {
  bootstrap: [getResponse("/api/v0/catalog"), getResponse("/api/v0/repositories")],
  retainedDataBootstrap: [
    postResponse("/api/v0/entities/resolve"),
    postResponse("/api/v0/impact/entity-map"),
  ],
} as const;

export const primaryConsoleRoutes: readonly ConsoleRoute[] = [
  {
    path: "/",
    label: "Dashboard",
    area: "dashboard",
    workflow: liveState(
      "dashboard-root-live-atlas",
      [".dashboard-atlas-panel .gcanvas-svg"],
      [],
      [
        {
          selector: ".dashboard-atlas-panel .gcanvas .empty",
          exactText: "No graph entities are available from the live model yet.",
        },
      ],
      dashboardAtlasOwnership,
    ),
  },
  {
    path: "/dashboard",
    label: "Dashboard (alias)",
    area: "dashboard",
    workflow: liveState(
      "dashboard-alias-live-atlas",
      [".dashboard-atlas-panel .gcanvas-svg"],
      [],
      [
        {
          selector: ".dashboard-atlas-panel .gcanvas .empty",
          exactText: "No graph entities are available from the live model yet.",
        },
      ],
      dashboardAtlasOwnership,
    ),
  },
  {
    path: "/status",
    label: "Status",
    area: "operations",
    workflow: {
      id: "status-live-overview",
      kind: "state",
      anySelectors: [".status-hero"],
      requiredResponses: [getResponse("/api/v0/status/collector-readiness")],
      forbiddenText: "Status is unavailable from this source.",
    },
  },
  {
    path: "/repositories",
    label: "Repositories",
    area: "repositories",
    workflow: {
      id: "repositories-source-workspace-retained-routes",
      kind: "repositoryDetails",
      sourceLinkSelector: 'a[href^="/repositories/"][href$="/source"]',
      sourceOutcomeSelector: ".repo-source-page .tbl tbody tr",
      workspaceOutcomeSelector:
        '.workspace-page [aria-label="Truth and freshness"], .workspace-page [aria-label="Service Atlas"]',
    },
  },
  {
    path: "/catalog",
    label: "Service Catalog",
    area: "service",
    workflow: liveState(
      "service-catalog-live-table",
      [".tbl tbody .t-name"],
      [],
      [
        {
          selector: ".tbl tbody td.empty",
          exactText: "No catalog entries from this source.",
        },
      ],
      bootstrapOwnership(getResponse("/api/v0/catalog")),
    ),
  },
  {
    path: "/service-story",
    label: "Service Story",
    area: "service",
    productionPaths: [APP_ROUTE_PATHS.serviceStory, APP_ROUTE_PATHS.serviceStoryDetail],
    workflow: {
      id: "service-story-parameterized-live-result",
      kind: "state",
      anySelectors: [".seg-result"],
      requiredResponses: [
        boundedGetResponse("/api/v0/services/", "/story"),
        postResponse("/api/v0/visualizations/derive"),
      ],
      expectedPathPrefix: "/service-story/",
      forbiddenSelectors: ['[role="alert"]'],
    },
  },
  {
    path: "/service-report",
    label: "Service Report",
    area: "service",
    productionPaths: [APP_ROUTE_PATHS.serviceReport, APP_ROUTE_PATHS.serviceReportDetail],
    workflow: {
      id: "service-report-parameterized-live-result",
      kind: "state",
      anySelectors: [".srp-result"],
      requiredResponses: [boundedGetResponse("/api/v0/investigations/services/", "")],
      expectedPathPrefix: "/service-report/",
      forbiddenSelectors: ['[role="alert"]'],
    },
  },
  {
    path: "/explorer",
    label: "Explorer",
    area: "graph",
    workflow: {
      id: "explorer-retained-entity-graph",
      kind: "submit",
      fields: [
        {
          requestKey: "name",
          selector: 'input[placeholder="Entity / symbol / service name…"]',
          valueEnv: "ESHU_E2E_SERVICE_NAME",
        },
      ],
      role: "button",
      name: "Load",
      expectedRequestPath: "/api/v0/entities/resolve",
      expectedRequestMethod: "POST",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".explorer-layout .gcanvas-svg",
      forbiddenSelectors: [".src-err", '[role="alert"]'],
    },
  },
  {
    path: "/code-graph",
    label: "Code Graph",
    area: "graph",
    workflow: {
      id: "code-graph-live-canvas",
      kind: "state",
      anySelectors: [".gcanvas-svg"],
      requiredResponses: [
        { path: "/api/v0/code/dead-code", method: "POST", acceptedStatuses: [200] },
        {
          path: "/api/v0/code/relationships/story",
          method: "POST",
          acceptedStatuses: [200],
        },
        {
          path: "/api/v0/code/relationships",
          method: "POST",
          acceptedStatuses: [200],
        },
        {
          path: "/api/v0/code/imports/investigate",
          method: "POST",
          acceptedStatuses: [200],
        },
      ],
      forbiddenSelectors: [".src-err"],
      forbiddenTexts: [
        "Failed to load live dead-code candidates:",
        "Relationships unavailable:",
        "Import cycle analysis unavailable:",
      ],
    },
  },
  {
    path: "/topology",
    label: "Topology",
    area: "graph",
    workflow: liveState(
      "topology-live-stage",
      [".topology-stage .topology-canvas"],
      ["No services are available from this source."],
      [
        {
          selector: ".topology-page > .empty",
          exactText: "No services are available from this source.",
        },
      ],
      {
        bootstrap: [getResponse("/api/v0/catalog")],
        retainedDataRoute: [
          boundedGetResponse("/api/v0/services/", "/story"),
          boundedGetResponse("/api/v0/services/", "/context"),
        ],
      },
    ),
  },
  {
    path: "/cloud",
    label: "Cloud",
    area: "cloud",
    workflow: {
      id: "cloud-resource-inventory",
      kind: "state",
      anySelectors: [".gcanvas-svg", ".page .empty"],
      requiredResponses: [getResponse("/api/v0/cloud/resources")],
      forbiddenText: "Failed to load:",
    },
  },
  {
    path: "/cloud-drift",
    label: "Cloud Drift",
    area: "cloud",
    workflow: {
      id: "cloud-drift-live-surfaces",
      kind: "submit",
      fields: [
        {
          requestKey: "scope_id",
          selector: 'input[aria-label="Scope ID filter"]',
          valueEnv: "ESHU_E2E_AWS_SCOPE_ID",
        },
      ],
      role: "button",
      name: "Load drift findings",
      scopeSelector: ".evidence-toolbar",
      expectedRequestPath: "/api/v0/cloud/runtime-drift/findings",
      expectedRequestMethod: "POST",
      acceptedResponseStatuses: [200],
      additionalExpectedRequests: [
        {
          path: "/api/v0/aws/runtime-drift/findings",
          method: "POST",
          acceptedStatuses: [200],
        },
        {
          path: "/api/v0/iac/unmanaged-resources",
          method: "POST",
          acceptedStatuses: [200],
        },
        {
          path: "/api/v0/iac/terraform-import-plan/candidates",
          method: "POST",
          acceptedStatuses: [200],
        },
      ],
      outcomeSelector: ".evidence-workbench > .panel:first-child tbody tr",
      additionalOutcomeSelectors: [
        ".evidence-workbench > .panel:nth-child(2) tbody tr",
        ".evidence-workbench > .panel:nth-child(3) tbody tr",
        '.cloud-drift-summary[data-import-plan-status="loaded"]',
      ],
      forbiddenSelectors: [".src-err"],
    },
  },
  {
    path: "/iac",
    label: "IaC",
    area: "cloud",
    workflow: liveState(
      "iac-live-inventory",
      ['[aria-label="IaC evidence workbench"] tbody .cell-stack'],
      ["IaC inventory is not available from this API", "Failed to load IaC resources:"],
      [
        {
          selector: '[aria-label="IaC evidence workbench"] tbody td.empty',
          exactText: "No Terraform/IaC resources have been indexed yet.",
        },
      ],
      routeOwnership(getResponse("/api/v0/iac/resources")),
    ),
  },
  {
    path: "/images",
    label: "Images",
    area: "cloud",
    workflow: liveState(
      "images-live-inventory",
      [".table-scroll .tbl.wide tbody .t-name"],
      ["Image inventory unavailable from this source."],
      [
        {
          selector: ".table-scroll .tbl.wide tbody td.empty",
          exactText: "No container images from this source.",
        },
      ],
      routeOwnership(getResponse("/api/v0/images")),
    ),
  },
  {
    path: "/observability",
    label: "Observability",
    area: "observability",
    workflow: liveState(
      "observability-live-coverage",
      [".signal-source-grid .signal-source", ".table-scroll .tbl.wide tbody .t-name"],
      ["Failed to load:", "No coverage correlations from this source."],
      [],
      routeOwnership(
        getResponse("/api/v0/observability/coverage/correlations", { provider: "grafana" }),
        getResponse("/api/v0/observability/coverage/correlations", { provider: "prometheus" }),
        getResponse("/api/v0/observability/coverage/correlations", { provider: "loki" }),
        getResponse("/api/v0/observability/coverage/correlations", { provider: "tempo" }),
      ),
    ),
  },
  {
    path: "/incidents",
    label: "Incidents",
    area: "observability",
    workflow: {
      id: "incident-context-retained-route",
      kind: "submit",
      fields: [
        {
          selector: 'input[aria-label="Incident id"]',
          valueEnv: "ESHU_E2E_INCIDENT_ID",
        },
      ],
      role: "button",
      name: "Review incident",
      scopeSelector: ".incident-query",
      expectedRequestPath: "/api/v0/incidents/${ESHU_E2E_INCIDENT_ID}/context",
      expectedRequestMethod: "GET",
      acceptedResponseStatuses: [200],
      expectedPagePath: "/incidents/${ESHU_E2E_INCIDENT_ID}/context",
      outcomeSelector: ".incident-summary",
      forbiddenSelectors: [".incident-context-page .src-err"],
      forbiddenText: "No incident context loaded.",
    },
  },
  {
    path: "/freshness-causality",
    label: "Freshness",
    area: "observability",
    workflow: liveState(
      "freshness-live-causality",
      [".table-scroll .tbl.wide tbody tr"],
      ["Freshness causality unavailable from this source."],
      [
        {
          selector: ".table-scroll .tbl.wide tbody td.empty",
          exactText: "No freshness causes are currently observed in the runtime.",
        },
      ],
      routeOwnership(getResponse("/api/v0/status/freshness-causality")),
    ),
  },
];
