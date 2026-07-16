import type { ConsoleAuthMode, ConsoleRoute } from "../src/e2e/consoleRouteCatalog.ts";

export interface AuthExcludedRoute {
  readonly path: string;
  readonly requiredAuthMode: ConsoleAuthMode;
}

export interface AuthRouteCoverage {
  readonly authMode: ConsoleAuthMode;
  readonly catalogRouteCount: number;
  readonly eligibleRoutes: readonly ConsoleRoute[];
  readonly excludedByAuth: readonly AuthExcludedRoute[];
}

// createAuthRouteCoverage keeps exclusions out of the pass/fail denominator.
// The separate auth E2E proves excluded browser-session surfaces.
export function createAuthRouteCoverage(
  routes: readonly ConsoleRoute[],
  authMode: ConsoleAuthMode,
): AuthRouteCoverage {
  const eligibleRoutes: ConsoleRoute[] = [];
  const excludedByAuth: AuthExcludedRoute[] = [];
  for (const route of routes) {
    const requiredAuthMode = route.authMode ?? "bearer";
    if (authMode === "browser_session" || requiredAuthMode === authMode) eligibleRoutes.push(route);
    else excludedByAuth.push({ path: route.path, requiredAuthMode });
  }
  return { authMode, catalogRouteCount: routes.length, eligibleRoutes, excludedByAuth };
}

export function formatAuthRouteCoverage(coverage: AuthRouteCoverage): string {
  const header =
    `console-live-e2e: ${coverage.authMode} eligibility ` +
    `${coverage.eligibleRoutes.length}/${coverage.catalogRouteCount}; ` +
    `${coverage.excludedByAuth.length} browser-session route(s) excluded from this verdict\n`;
  return (
    header +
    coverage.excludedByAuth
      .map(
        (route) =>
          `  EXCLUDED ${route.path} (requires ${route.requiredAuthMode}; ` +
          "proved by console:e2e:auth)\n",
      )
      .join("")
  );
}

export function authCoverageReport(coverage: AuthRouteCoverage): object {
  return {
    authMode: coverage.authMode,
    catalogRouteCount: coverage.catalogRouteCount,
    eligibleRouteCount: coverage.eligibleRoutes.length,
    excludedByAuth: coverage.excludedByAuth,
  };
}
