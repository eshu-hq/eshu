import { describe, expect, it } from "vitest";

import type { ConsoleRoute } from "../src/e2e/routeAssertions";
import { selectLiveE2ERoutes } from "./liveE2ERouteSelection";

const routes = [
  { path: "/catalog", label: "Catalog", area: "service" },
  { path: "/repositories", label: "Repositories", area: "repositories" },
] satisfies readonly ConsoleRoute[];

describe("selectLiveE2ERoutes", () => {
  it("keeps the complete eligible route set when no filter is supplied", () => {
    expect(selectLiveE2ERoutes(routes, undefined)).toEqual(routes);
  });

  it("selects exact paths in requested order and preserves warm repeats", () => {
    expect(selectLiveE2ERoutes(routes, " /repositories, /catalog, /repositories ")).toEqual([
      routes[1],
      routes[0],
      routes[1],
    ]);
  });

  it("fails closed for empty or unknown path filters", () => {
    expect(() => selectLiveE2ERoutes(routes, " , ")).toThrow(/at least one exact path/);
    expect(() => selectLiveE2ERoutes(routes, "/missing")).toThrow(/unknown or ineligible/);
  });
});
