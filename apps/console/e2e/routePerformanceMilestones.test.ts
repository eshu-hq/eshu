import { describe, expect, it } from "vitest";

import { consoleRoutes, type ConsoleRoute } from "../src/e2e/routeAssertions";
import { firstUsefulSelectorForRoute } from "./routePerformanceMilestones";

describe("firstUsefulSelectorForRoute", () => {
  it("maps state, action, and repository workflows to route-owned content", () => {
    const state = {
      path: "/catalog",
      label: "Catalog",
      area: "service",
      workflow: {
        id: "catalog",
        kind: "state",
        anySelectors: [".catalog-row"],
        emptyStates: [{ selector: ".catalog-empty", exactText: "Empty" }],
      },
    } satisfies ConsoleRoute;
    const submit = {
      path: "/search",
      label: "Search",
      area: "ask",
      workflow: {
        id: "search",
        kind: "submit",
        fields: [{ selector: "input", value: "query" }],
        role: "button",
        name: "Search",
        expectedRequestPath: "/api/v0/search",
        expectedRequestMethod: "POST",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".results",
      },
    } satisfies ConsoleRoute;
    const repositories = {
      path: "/repositories",
      label: "Repositories",
      area: "repositories",
      workflow: {
        id: "repositories",
        kind: "repositoryDetails",
        firstUsefulSelector: '[role="group"][aria-label="Repository view"]',
        sourceLinkSelector: 'a[href$="/source"]',
        sourceOutcomeSelector: ".source",
        workspaceOutcomeSelector: ".workspace",
      },
    } satisfies ConsoleRoute;
    const click = {
      path: "/admin",
      label: "Admin",
      area: "system",
      workflow: {
        id: "admin",
        kind: "click",
        firstUsefulSelector: "#identity-access-tab-sign-in-policy",
        role: "tab",
        name: "Sign-in policy",
        outcomeSelector: "#identity-access-panel-sign-in-policy",
        loadedStateSelector: "#policy-require-sso",
        expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
      },
    } satisfies ConsoleRoute;

    expect(firstUsefulSelectorForRoute(state)).toBe(".catalog-row, .catalog-empty");
    expect(firstUsefulSelectorForRoute(submit)).toBe("input");
    expect(firstUsefulSelectorForRoute(repositories)).toBe(
      '[role="group"][aria-label="Repository view"]',
    );
    expect(firstUsefulSelectorForRoute(click)).toBe("#identity-access-tab-sign-in-policy");
  });

  it("does not wait for a post-interaction selector before the workflow runs", () => {
    const admin = consoleRoutes.find((route) => route.path === "/admin");
    const repositories = consoleRoutes.find((route) => route.path === "/repositories");

    expect(firstUsefulSelectorForRoute(admin!)).toBe("#identity-access-tab-sign-in-policy");
    expect(firstUsefulSelectorForRoute(admin!)).not.toBe("#policy-require-sso");
    expect(firstUsefulSelectorForRoute(repositories!)).toBe(
      '[role="group"][aria-label="Repository view"]',
    );
    expect(firstUsefulSelectorForRoute(repositories!)).not.toContain('/source"]');
  });

  it("defines route-owned first-useful content for every live route", () => {
    expect(consoleRoutes.filter((route) => firstUsefulSelectorForRoute(route) === null)).toEqual(
      [],
    );
  });
});
