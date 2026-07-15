// Declarative catalog for the private/live console gate. Keep browser execution
// and evaluation in their own modules so this inventory remains easy to audit
// against App.tsx and the sidebar navigation.

import { APP_ROUTE_PATHS } from "../appRoutePaths";
import { primaryConsoleRoutes } from "./consoleRouteCatalogPrimaryRoutes";
import { secondaryConsoleRoutes } from "./consoleRouteCatalogSecondaryRoutes";
import type { ConsoleRoute, NetworkAllowRule } from "./consoleRouteCatalogTypes";

export type {
  ConsoleAuthMode,
  ConsoleRoute,
  NetworkAllowRule,
  RouteArea,
  RouteWorkflowSpec,
  WorkflowEmptyState,
  WorkflowField,
  WorkflowFollowLink,
  WorkflowResponseExpectation,
  WorkflowTab,
} from "./consoleRouteCatalogTypes";

// Every entry is a real route enumerated from apps/console/src/App.tsx.
// Parameterized routes are exercised through their base listing route.
const routeCatalog: readonly ConsoleRoute[] = [...primaryConsoleRoutes, ...secondaryConsoleRoutes];

const parameterizedCoverage: Readonly<Record<string, readonly string[]>> = {
  [APP_ROUTE_PATHS.repositories]: [
    APP_ROUTE_PATHS.repositories,
    APP_ROUTE_PATHS.repositorySource,
    APP_ROUTE_PATHS.workspace,
  ],
  [APP_ROUTE_PATHS.incidents]: [APP_ROUTE_PATHS.incidents, APP_ROUTE_PATHS.incidentContext],
  [APP_ROUTE_PATHS.vulnerabilities]: [
    APP_ROUTE_PATHS.vulnerabilities,
    APP_ROUTE_PATHS.vulnerabilityDetail,
  ],
};

export const consoleRoutes: readonly ConsoleRoute[] = routeCatalog.map((route) => ({
  ...route,
  productionPaths: route.productionPaths ?? parameterizedCoverage[route.path] ?? [route.path],
}));

// Only the exact browser-session fallback handshake is justified. The route
// evaluator matches pathname, method, and status exactly.
export const defaultNetworkAllowList: readonly NetworkAllowRule[] = [
  {
    method: "GET",
    pathname: "/eshu-api/api/v0/auth/browser-session",
    status: 401,
    reason: "no browser-session cookie; configured shared-key fallback follows",
  },
  {
    method: "POST",
    pathname: "/eshu-api/api/v0/auth/browser-session",
    status: 400,
    reason: "all-scope shared key has no tenant context; bearer fallback follows",
  },
];
