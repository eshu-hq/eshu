import {
  buildNetworkReportObservation,
  buildRouteReportObservation,
  type RouteSignals,
} from "./routeAssertions";

function healthySignals(overrides: Partial<RouteSignals> = {}): RouteSignals {
  return {
    connected: true,
    consoleErrors: [],
    demoBannerPresent: false,
    durationMs: 1234,
    mainContentChars: 140,
    network: [],
    route: { path: "/dashboard", label: "Dashboard", area: "dashboard" },
    sourceMode: "live",
    workflow: null,
    ...overrides,
  };
}

describe("buildRouteReportObservation", () => {
  it("preserves request status and visible data-shape proof without query strings or bodies", () => {
    const observation = buildRouteReportObservation(
      healthySignals({
        route: {
          path: "/semantic-search",
          label: "Semantic Search",
          area: "ask",
          workflow: {
            id: "semantic-search",
            kind: "state",
            anySelectors: [".sem-result-announce"],
          },
        },
        network: [
          {
            url: "https://host/eshu-api/api/v0/search/semantic?q=private-token",
            method: "POST",
            status: 200,
            failureText: null,
          },
        ],
        workflow: {
          id: "semantic-search",
          passed: true,
          detail: "rendered a visible semantic result",
          dataShapes: [{ selector: ".sem-result-announce", visibleCount: 1 }],
          requests: [{ method: "POST", pathname: "/eshu-api/api/v0/search/semantic", status: 200 }],
        },
      }),
    );

    expect(observation.network).toEqual([
      {
        failureText: null,
        method: "POST",
        pathname: "/eshu-api/api/v0/search/semantic",
        status: 200,
      },
    ]);
    expect(observation.workflow?.dataShapes).toEqual([
      { selector: ".sem-result-announce", visibleCount: 1 },
    ]);
    expect(JSON.stringify(observation)).not.toContain("private-token");
  });

  it("bounds retained request observations while marking truncation", () => {
    const network = Array.from({ length: 205 }, (_, index) => ({
      failureText: null,
      method: "GET",
      status: 200,
      url: `https://host/eshu-api/api/v0/entities/${index}`,
    }));

    const observation = buildRouteReportObservation(healthySignals({ network }));

    expect(observation.network).toHaveLength(200);
    expect(observation.networkTruncated).toBe(true);
  });

  it("redacts URL query values from retained transport failure text", () => {
    const observation = buildRouteReportObservation(
      healthySignals({
        network: [
          {
            failureText: "failed at https://host/eshu-api/api/v0/entities?credential=private-token",
            method: "GET",
            status: 0,
            url: "https://host/eshu-api/api/v0/entities",
          },
        ],
      }),
    );

    expect(JSON.stringify(observation)).not.toContain("private-token");
  });

  it("redacts dynamic resource identifiers from retained request paths", () => {
    const observation = buildRouteReportObservation(
      healthySignals({
        network: [
          {
            failureText: null,
            method: "GET",
            status: 200,
            url: "https://host/eshu-api/api/v0/repositories/repository%3Aprivate-id/story",
          },
          {
            failureText: null,
            method: "GET",
            status: 200,
            url: "https://host/eshu-api/api/v0/services/private-service/context",
          },
        ],
        workflow: {
          id: "private-resource-follow-up",
          passed: true,
          detail: "proved the retained route",
          requests: [
            {
              method: "GET",
              pathname: "/eshu-api/api/v0/repositories/repository%3Aprivate-id/story",
              status: 200,
            },
          ],
        },
      }),
    );

    expect(observation.network.map((request) => request.pathname)).toEqual([
      "/eshu-api/api/v0/repositories/:repository/story",
      "/eshu-api/api/v0/services/:service/context",
    ]);
    expect(observation.workflow?.requests?.[0]?.pathname).toBe(
      "/eshu-api/api/v0/repositories/:repository/story",
    );
    expect(JSON.stringify(observation)).not.toMatch(/private-id|private-service/);
  });

  it("redacts every dynamic production path without over-redacting static routes", () => {
    const report = buildNetworkReportObservation([
      {
        failureText: null,
        method: "GET",
        status: 200,
        url: "https://private-host/eshu-api/api/v0/investigations/services/team%2FPayments-42",
      },
      {
        failureText: null,
        method: "DELETE",
        status: 204,
        url: "https://private-host/eshu-api/api/v0/auth/local/invitations/123456/revoke",
      },
      {
        failureText: null,
        method: "DELETE",
        status: 204,
        url: "https://private-host/eshu-api/api/v0/auth/admin/idp-group-mappings/Map%2FRef-9",
      },
      {
        failureText: null,
        method: "DELETE",
        status: 204,
        url: "https://private-host/eshu-api/api/v0/auth/local/api-tokens/account-9988/revoke",
      },
      {
        failureText: null,
        method: "GET",
        status: 200,
        url: "https://private-host/eshu-api/api/v0/services/catalog/contextual",
      },
    ]);

    expect(report.network.map((request) => request.pathname)).toEqual([
      "/eshu-api/api/v0/investigations/services/:service",
      "/eshu-api/api/v0/auth/local/invitations/:invitation/revoke",
      "/eshu-api/api/v0/auth/admin/idp-group-mappings/:mapping",
      "/eshu-api/api/v0/auth/local/api-tokens/:token/revoke",
      "/eshu-api/api/v0/services/catalog/contextual",
    ]);
    expect(JSON.stringify(report)).not.toMatch(
      /team%2FPayments|123456|Map%2FRef|account-9988|private-host/i,
    );
  });
});

describe("buildNetworkReportObservation", () => {
  it("retains bounded bootstrap transport proof without query values", () => {
    const observations = Array.from({ length: 205 }, (_, index) => ({
      failureText:
        index === 0
          ? "failed at https://host/eshu-api/api/v0/status?credential=private-token"
          : null,
      method: "GET",
      status: index === 0 ? 0 : 200,
      url: `https://host/eshu-api/api/v0/entities/${index}?credential=private-token`,
    }));

    const report = buildNetworkReportObservation(observations);

    expect(report.network).toHaveLength(200);
    expect(report.networkTruncated).toBe(true);
    expect(report.network[0].pathname).toBe("/eshu-api/api/v0/entities/0");
    expect(JSON.stringify(report)).not.toContain("private-token");
  });

  it("redacts Vite filesystem module paths from bootstrap proof", () => {
    const report = buildNetworkReportObservation([
      {
        failureText: null,
        method: "GET",
        status: 200,
        url: "http://localhost/@fs/Users/operator/private-worktree/node_modules/vite/env.mjs",
      },
    ]);

    expect(report.network[0].pathname).toBe("/@fs/:local-module");
    expect(JSON.stringify(report)).not.toContain("operator/private-worktree");
  });
});
