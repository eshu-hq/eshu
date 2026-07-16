import type { ConsoleRoute } from "../src/e2e/routeAssertions";

export function selectLiveE2ERoutes(
  eligibleRoutes: readonly ConsoleRoute[],
  value: string | undefined,
): readonly ConsoleRoute[] {
  if (value === undefined) return eligibleRoutes;
  const requestedPaths = value
    .split(",")
    .map((path) => path.trim())
    .filter((path) => path !== "");
  if (requestedPaths.length === 0) {
    throw new Error("ESHU_E2E_ROUTE_PATHS must contain at least one exact path");
  }
  const routesByPath = new Map(eligibleRoutes.map((route) => [route.path, route]));
  return requestedPaths.map((path) => {
    const route = routesByPath.get(path);
    if (route === undefined) {
      throw new Error(`ESHU_E2E_ROUTE_PATHS contains unknown or ineligible path: ${path}`);
    }
    return route;
  });
}
